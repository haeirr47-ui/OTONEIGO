// Compact presentation reuses OTONEI's cover, heart, transport icons and theme. MIT, voici5986.
import { useEffect, useRef, useState, type CSSProperties } from "react";
import { Events } from "@wailsio/runtime";
import {
  FaHeart,
  FaRegHeart,
  FaPlay,
  FaPause,
  FaTimes,
  FaExternalLinkAlt,
  FaThumbtack,
  FaLock,
  FaVolumeUp,
} from "react-icons/fa";
import { MdSkipPrevious, MdSkipNext } from "react-icons/md";
import type { DesktopBridge, DesktopCommandName, DesktopSnapshot, DesktopView } from "./contracts";
import { acceptDesktopSnapshot, fromDesktopSnapshot, normalizeDesktopSettings } from "./desktopWire";
import { fromResolvedCover } from "./wire";
import { time } from "./format";

function DesktopCover({ bridge, coverRef }: { bridge: DesktopBridge; coverRef: string }) {
  const [source, setSource] = useState("/default_cover.svg");
  const attempt = useRef(0),
    generation = useRef(0);
  const recover = async () => {
    const current = generation.current;
    if (!coverRef || !bridge.ResolveCover || attempt.current >= 2) {
      setSource("/default_cover.svg");
      return;
    }
    const count = ++attempt.current;
    try {
      const next = fromResolvedCover(await bridge.ResolveCover(coverRef));
      if (current === generation.current) {
        // A deleted cache file can be recreated at the same content hash: remount the image once.
        setSource(next);
        setImageKey(`${current}:${count}`);
      }
    } catch {
      if (current === generation.current) setSource("/default_cover.svg");
    }
  };
  const [imageKey, setImageKey] = useState("");
  useEffect(() => {
    generation.current++;
    attempt.current = 0;
    setSource("/default_cover.svg");
    void recover();
    return () => {
      generation.current++;
    };
  }, [coverRef, bridge]);
  return (
    <img
      key={imageKey}
      className="desktop-cover"
      src={source}
      alt="专辑封面"
      onError={() => {
        if (source !== "/default_cover.svg") void recover();
      }}
    />
  );
}
export function DesktopViewContent({
  kind,
  snapshot: s,
  bridge,
  onError,
}: {
  kind: DesktopView;
  snapshot: DesktopSnapshot | null;
  bridge: DesktopBridge;
  onError: (message: string) => void;
}) {
  const options = normalizeDesktopSettings(s?.options);
  const available = !!s?.ready && !s.closing;
  const command = (name: DesktopCommandName, value = 0) => {
    if (!s || !available || !bridge.DispatchDesktopCommand) return;
    void bridge
      .DispatchDesktopCommand({ epoch: s.epoch, command: name, songId: s.songId, value })
      .catch(() => onError("操作暂未完成，请重试。"));
  };
  const native = (operation: (() => Promise<void>) | undefined) => {
    if (operation) void operation().catch(() => onError("窗口操作暂未完成，请重试。"));
  };
  const topmost = kind === "mini" ? options.miniAlwaysOnTop : options.lyricsAlwaysOnTop;
  const controls = (
    <div className="desktop-window-actions">
      <button
        type="button"
        aria-label="返回主窗口"
        title="返回主窗口"
        onClick={() => native(bridge.ShowMainWindow?.bind(bridge))}
      >
        <FaExternalLinkAlt />
      </button>
      <button
        type="button"
        aria-label="窗口置顶"
        title="窗口置顶"
        aria-pressed={topmost}
        disabled={!available}
        onClick={() => command(kind === "mini" ? "toggle-mini-topmost" : "toggle-lyrics-topmost")}
      >
        <FaThumbtack />
      </button>
      {kind === "lyrics" && (
        <button
          type="button"
          aria-label="锁定桌面歌词"
          title="锁定后可在主窗口设置或托盘解锁"
          disabled={!available}
          aria-pressed={options.lyricsLocked}
          onClick={() => command("toggle-lyrics-lock")}
        >
          <FaLock />
        </button>
      )}
      <button
        type="button"
        aria-label={kind === "mini" ? "隐藏迷你播放器" : "隐藏桌面歌词"}
        title="隐藏窗口"
        onClick={() => native(bridge.HideDesktopView ? () => bridge.HideDesktopView!(kind) : undefined)}
      >
        <FaTimes />
      </button>
    </div>
  );
  const status = s?.closing
    ? "播放器正在退出…"
    : !s || !s.ready
      ? "正在连接播放器…"
      : s.error || (s.playback === "loading" ? "正在缓冲…" : "");
  if (kind === "lyrics")
    return (
      <main
        className={`desktop-lyrics ${options.lyricsLocked ? "is-locked" : ""}`}
        style={
          { "--lyrics-size": `${options.lyricsFontSize}px`, "--lyrics-opacity": options.lyricsOpacity } as CSSProperties
        }
        aria-label="桌面歌词"
      >
        {!options.lyricsLocked && <header className="desktop-drag-region">{controls}</header>}
        <div className="desktop-lyric-lines" aria-live="off">
          <p
            className="desktop-current-lyric"
            style={{ "--lyric-progress": `${(s?.lyricProgress ?? 0) * 100}%` } as CSSProperties}
          >
            {s?.currentLyric || s?.title || "让音乐陪伴此刻"}
          </p>
          <p className="desktop-secondary-lyric">{s?.translation || s?.nextLyric || s?.artist || ""}</p>
        </div>
        <span className="desktop-status" role="status">
          {status}
        </span>
      </main>
    );
  return (
    <main className="desktop-mini" aria-label="迷你播放器">
      <header className="desktop-drag-region">
        <span className="desktop-brand">OTONEIGO</span>
        {controls}
      </header>
      <div className="desktop-mini-body">
        <DesktopCover bridge={bridge} coverRef={s?.coverRef ?? ""} />
        <div className="desktop-mini-info">
          <strong title={s?.title}>{s?.title || "还没有选择歌曲"}</strong>
          <span title={s?.artist}>{s?.artist || "从主窗口开始播放"}</span>
          <div className="desktop-transport">
            <button
              type="button"
              aria-label="上一首"
              disabled={!available || !s?.canPrevious}
              onClick={() => command("previous")}
            >
              <MdSkipPrevious />
            </button>
            <button
              type="button"
              className="desktop-play"
              aria-label={s?.playback === "paused" ? "播放" : "暂停"}
              disabled={!available || !s?.songId}
              onClick={() => command(s?.playback === "paused" ? "play" : "pause")}
            >
              {s?.playback === "paused" ? <FaPlay /> : <FaPause />}
            </button>
            <button
              type="button"
              aria-label="下一首"
              disabled={!available || !s?.canNext}
              onClick={() => command("next")}
            >
              <MdSkipNext />
            </button>
            <button
              type="button"
              className={`favorite ${s?.favorite ? "liked" : ""}`}
              aria-label={s?.favorite ? "取消收藏" : "收藏"}
              aria-pressed={s?.favorite ?? false}
              disabled={!available || !s?.songId}
              onClick={() => command("toggle-favorite")}
            >
              {s?.favorite ? <FaHeart /> : <FaRegHeart />}
            </button>
          </div>
        </div>
      </div>
      <div className="desktop-mini-sliders">
        <span>{time(s?.positionMs ?? 0)}</span>
        <input
          aria-label="播放进度"
          type="range"
          min="0"
          max={Math.max(1, s?.durationMs ?? 0)}
          step="1000"
          value={Math.min(s?.positionMs ?? 0, s?.durationMs || 1)}
          disabled={!available || !s?.songId || !s.durationMs}
          onChange={(e) => command("seek", Number(e.target.value))}
        />
        <span>{time(s?.durationMs ?? 0)}</span>
        <FaVolumeUp aria-hidden="true" />
        <input
          aria-label="音量"
          type="range"
          min="0"
          max="1"
          step="0.01"
          value={s?.volume ?? 0.8}
          disabled={!available}
          onChange={(e) => command("volume", Number(e.target.value))}
        />
      </div>
      <span className="desktop-status" role="status">
        {status}
      </span>
    </main>
  );
}
export function DesktopAuxView({ kind, bridge }: { kind: DesktopView; bridge: DesktopBridge }) {
  const [snapshot, setSnapshot] = useState<DesktopSnapshot | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let disposed = false;
    const accept = (data: unknown) => {
      if (disposed) return;
      try {
        const next = fromDesktopSnapshot(data);
        setSnapshot((previous) => (acceptDesktopSnapshot(previous, next) ? next : previous));
        setError("");
      } catch {
        setError("播放器状态暂不可用。");
      }
    };
    const stop = Events.On("otoneigo:desktop-snapshot", (event) => accept(event.data));
    Events.Emit("otoneigo:desktop-ready", kind);
    void bridge
      .GetDesktopSnapshot?.()
      .then(accept)
      .catch(() => {
        if (!disposed) setError("正在等待主窗口连接…");
      });
    return () => {
      disposed = true;
      stop();
    };
  }, [bridge, kind]);
  return (
    <>
      <DesktopViewContent kind={kind} snapshot={snapshot} bridge={bridge} onError={setError} />
      {error && (
        <p className="desktop-view-error" role="status">
          {error}
        </p>
      )}
    </>
  );
}

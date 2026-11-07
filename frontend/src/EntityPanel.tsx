import { useCallback, useEffect, useRef, useState } from "react";
import type { DesktopBridge, PublicPlaylistPage, TrackSnapshot } from "./contracts";
import type { EntityViewRequest } from "./entityView";
import { usePlayerSelector, usePlayerStore } from "./PlayerContext";
import { PlayerCover, TrackCard } from "./OtoneiPresentation";

interface PanelView {
  name: string;
  subtitle: string;
  description: string;
  coverRef: string;
  tracks: TrackSnapshot[];
  total: number;
  partial: boolean;
  hasMore: boolean;
  nextOffset: number;
  offset: number;
}

function fromPlaylist(page: PublicPlaylistPage): PanelView {
  return {
    name: page.name,
    subtitle: "",
    description: page.description,
    coverRef: page.coverRef,
    tracks: page.tracks,
    total: page.total,
    partial: page.partial,
    hasMore: page.hasMore,
    nextOffset: page.nextOffset,
    offset: page.offset,
  };
}

export function EntityPanel({
  bridge,
  request,
  onBack,
}: {
  bridge: DesktopBridge;
  request: EntityViewRequest;
  onBack: () => void;
}) {
  const store = usePlayerStore();
  const ready = usePlayerSelector((state) => state.hydration === "ready" && !!state.settings);
  const [view, setView] = useState<PanelView | null>(null);
  const [offset, setOffset] = useState(0);
  const [retry, setRetry] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);

  // biome-ignore lint/correctness/useExhaustiveDependencies: retry restarts a failed request; the loaded view stays cached until paging or navigation.
  useEffect(() => {
    const controller = new AbortController();
    const gen = ++generation.current;
    if (store.isClosing()) return;
    const load = async (signal: AbortSignal): Promise<PanelView> => {
      if (request.kind === "playlist") {
        if (!bridge.GetPublicPlaylist) throw new Error("UNSUPPORTED");
        return fromPlaylist(await bridge.GetPublicPlaylist(request.id, offset, signal));
      }
      if (!bridge.GetEntityDetail) throw new Error("UNSUPPORTED");
      const detail = await bridge.GetEntityDetail(request.kind, request.id, signal);
      return {
        name: detail.name,
        subtitle: detail.subtitle ?? "",
        description: detail.description ?? "",
        coverRef: detail.coverRef ?? "",
        tracks: detail.tracks,
        total: detail.total,
        partial: detail.partial,
        hasMore: false,
        nextOffset: 0,
        offset: 0,
      };
    };
    setLoading(true);
    setError("");
    void load(controller.signal)
      .then((next) => {
        if (gen === generation.current) setView(next);
      })
      .catch((failure: unknown) => {
        if (gen !== generation.current) return;
        const code = failure instanceof Error ? failure.message : "";
        setError(
          /LOGIN_REQUIRED|UNAUTHORIZED/i.test(code)
            ? "此内容暂需网易云登录，当前客户端不提供账号登录。"
            : /RATE_LIMITED|429/i.test(code)
              ? "请求较频繁，请稍后重试。"
              : "内容暂时无法加载，请检查网络后重试。",
        );
      })
      .finally(() => {
        if (gen === generation.current) setLoading(false);
      });
    return () => {
      controller.abort();
      generation.current++;
    };
  }, [bridge, store, request.kind, request.id, offset, retry]);

  const add = useCallback(
    (track: TrackSnapshot) => {
      if (!store.isClosing()) store.add(track);
    },
    [store],
  );
  const play = useCallback(
    (track: TrackSnapshot) => {
      const id = store.add(track);
      if (id) void store.select(id);
    },
    [store],
  );
  function changePage(value: number) {
    generation.current++;
    setOffset(value);
    setView(null);
  }
  const current = view && (request.kind !== "playlist" || view.offset === offset) ? view : null;
  const blocked = !ready || store.isClosing() || loading || !current?.tracks.length;
  const coverTrack: TrackSnapshot = {
    neteaseSongId: "",
    name: current?.name || request.name,
    artists: [],
    durationMs: 0,
    coverRef: current?.coverRef || "",
  };
  return (
    <section aria-label="详情" className="discovery-panel entity-panel">
      <button type="button" onClick={onBack}>
        返回
      </button>
      <div className="entity-detail-header">
        <PlayerCover track={coverTrack} large />
        <div>
          <h1>{current?.name || request.name}</h1>
          {current?.subtitle && <p className="muted">{current.subtitle}</p>}
          {current && (
            <p className="muted">
              本页 {current.tracks.length} 首 · 共 {current.total} 首可浏览
            </p>
          )}
        </div>
      </div>
      {current?.description && <p className="entity-detail-description muted">{current.description}</p>}
      {loading && <p role="status">正在加载内容…</p>}
      {error && (
        <div className="alert-light">
          <p role="alert">{error}</p>
          <button type="button" onClick={() => setRetry((value) => value + 1)}>
            重试
          </button>
        </div>
      )}
      {current?.partial && <p className="alert-light">仅显示部分歌曲资料；部分曲目暂不可用或超过本次浏览范围。</p>}
      {current && (
        <>
          <div className="playlist-toolbar">
            <button
              type="button"
              disabled={blocked}
              onClick={() => {
                if (!store.isClosing()) store.addAll(current.tracks, true);
              }}
            >
              替换队列并播放本页
            </button>
            <button
              type="button"
              disabled={blocked}
              onClick={() => {
                if (!store.isClosing()) store.playNext(current.tracks);
              }}
            >
              下一首播放本页
            </button>
            <button
              type="button"
              disabled={blocked}
              onClick={() => {
                if (!store.isClosing()) store.addAll(current.tracks);
              }}
            >
              加入本页
            </button>
          </div>
          <div className="music-list">
            {current.tracks.map((track) => (
              <TrackCard key={track.neteaseSongId} track={track} onPlay={play} onAdd={add} disabled={!ready} />
            ))}
          </div>
          {!current.tracks.length && <p className="alert-light">本页暂无可显示的歌曲资料。</p>}
          {request.kind === "playlist" && (
            <nav className="pagination" aria-label="详情翻页">
              <button
                type="button"
                disabled={loading || offset <= 0}
                onClick={() => changePage(Math.max(0, offset - 30))}
              >
                上一页
              </button>
              <span>第 {Math.floor(offset / 30) + 1} 页</span>
              <button
                type="button"
                disabled={loading || !current.hasMore}
                onClick={() => changePage(current.nextOffset)}
              >
                下一页
              </button>
            </nav>
          )}
        </>
      )}
    </section>
  );
}

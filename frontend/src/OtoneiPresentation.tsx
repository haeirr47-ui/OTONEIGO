import { useTrackActions } from "./TrackActions";
import { alignLyrics } from "./lyrics";
export { parseLyrics, alignLyrics } from "./lyrics";
import { validRemoteCover } from "./contracts";
import { AddToPlaylistButton } from "./PlaylistPanels";
// Adapted from OTONEI/src/components/{DesktopNavbar,MobileBottomNav,Header,SearchResultItem,
// DesktopPlayerView,DesktopPlayerControl,DesktopExpandedView,DesktopAlbumCover,PlayerSubComponents}.
// MIT License, copyright (c) 2026 voici5986. Desktop state/bridge/audio remain OTONEIGO-owned.
import {
  memo,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import {
  FaSearch,
  FaHeart,
  FaHistory,
  FaCog,
  FaPlay,
  FaPause,
  FaTimes,
  FaPlus,
  FaList,
  FaVolumeUp,
  FaKeyboard,
} from "react-icons/fa";
import { MdSkipPrevious, MdSkipNext, MdRepeat, MdRepeatOne, MdShuffle } from "react-icons/md";
import type { TrackSnapshot } from "./contracts";
import { shallowEqual, usePlayerSelector, usePlayerStore } from "./PlayerContext";
import { FavoriteButton } from "./LibraryPanels";
import { actualStreamQuality, time } from "./format";
import { containOverlayFocus } from "./overlayFocus";
import { handlePlayerShortcut, playerShortcuts, shortcutContext, shortcutUnavailable } from "./playerShortcuts";
export type View = "search" | "rankings" | "favorites" | "history" | "settings";
const items = [
  { id: "search", title: "发现音乐", icon: FaSearch },
  { id: "rankings", title: "排行榜", icon: FaList },
  { id: "favorites", title: "本地收藏", icon: FaHeart },
  { id: "history", title: "播放历史", icon: FaHistory },
  { id: "settings", title: "设置", icon: FaCog },
] as const;
export function OtoneiNavigation({ view, onNavigate }: { view: View; onNavigate: (view: View) => void }) {
  return (
    <>
      <div className="navigation-sidebar">
        <nav className="navbar navbar-expand-lg" aria-label="主导航">
          <div className="container-fluid">
            <div className="navbar-brand d-flex align-items-center">
              <img src="/logo.svg" alt="" width="32" height="32" />
              <span className="brand-text">OTONEIGO</span>
            </div>
            <div className="navbar-collapse">
              <div className="navbar-nav">
                {items.map(({ id, title, icon: Icon }) => (
                  <button
                    type="button"
                    key={id}
                    onClick={() => onNavigate(id)}
                    className={`nav-link nav-item ${view === id ? "active" : ""}`}
                    aria-current={view === id ? "page" : undefined}
                  >
                    <span className="nav-icon">
                      <Icon />
                    </span>
                    {title}
                  </button>
                ))}
              </div>
            </div>
            <div className="sidebar-caption">网易云音乐 · 本地音乐库</div>
          </div>
        </nav>
      </div>
      <nav className="mobile-tab-bar" aria-label="主导航">
        {items.map(({ id, title, icon: Icon }) => (
          <button
            type="button"
            key={id}
            className={`mobile-tab-item ${view === id ? "active" : ""}`}
            onClick={() => onNavigate(id)}
            aria-label={title}
            aria-current={view === id ? "page" : undefined}
          >
            <Icon aria-hidden="true" />
            <span className="mobile-tab-label">{title}</span>
          </button>
        ))}
      </nav>
    </>
  );
}
export function OtoneiHeader({
  query,
  onQuery,
  onSubmit,
  searching,
}: {
  query: string;
  onQuery: (query: string) => void;
  onSubmit: (event: FormEvent) => void;
  searching: boolean;
}) {
  return (
    <header className="app-header-desktop">
      <div className="header-search-container">
        <form onSubmit={onSubmit}>
          <div className="header-search-field-wrapper">
            <span className="header-search-icon" aria-hidden="true">
              <FaSearch />
            </span>
            <input
              type="search"
              aria-label="搜索歌曲或歌手"
              placeholder="搜索歌曲或歌手"
              className="header-search-input"
              value={query}
              onChange={(e) => onQuery(e.target.value)}
              autoComplete="off"
            />
            {query && (
              <button type="button" className="ui-icon-button" aria-label="清除搜索" onClick={() => onQuery("")}>
                <FaTimes />
              </button>
            )}
            <button type="submit" className="search-submit" disabled={!query.trim()}>
              搜索
            </button>
          </div>
        </form>
      </div>
      <span className="header-scope">{searching ? "正在搜索…" : "网易云音乐"}</span>
    </header>
  );
}
export const TrackCard = memo(function TrackCard({
  track,
  onPlay,
  onAdd,
  disabled,
  date,
}: {
  track: TrackSnapshot;
  onPlay: (track: TrackSnapshot) => void;
  onAdd: (track: TrackSnapshot) => void;
  disabled: boolean;
  date?: string;
}) {
  const active = usePlayerSelector((state) => state.currentSongId) === track.neteaseSongId;
  const actions = useTrackActions({ track, onPlay, onAdd, disabled });
  return (
    // biome-ignore lint/a11y/useSemanticElements: this card groups one track's title and action buttons and needs an accessible name; fieldset is a form grouping element and would misdescribe it, while section/article would add landmarks per row.
    <div
      className={`music-card ${active ? "is-active" : ""}`}
      role="group"
      aria-label={track.name}
      onContextMenu={actions.onContextMenu}
      onKeyDown={actions.onKeyDown}
    >
      <div className="music-card-row">
        <button
          type="button"
          className="music-card-info"
          onClick={() => onPlay(track)}
          disabled={disabled}
          aria-label={`播放 ${track.name} - ${track.artists.map((a) => a.name).join(" / ")}`}
        >
          <h6 title={track.name}>{track.name}</h6>
          <small>
            {track.artists.map((a) => a.name).join(" / ")} {track.album?.name && `· ${track.album.name}`}
          </small>
          {date && <span className="badge-time">{date}</span>}
        </button>
        <span className="duration">{time(track.durationMs)}</span>
        <div className="music-card-actions">
          <FavoriteButton track={track} />
          <AddToPlaylistButton track={track} />
          {actions.button}
          <button
            type="button"
            className="ui-icon-button"
            disabled={disabled}
            aria-label={`加入队列：${track.name}`}
            onClick={() => onAdd(track)}
          >
            <FaPlus />
          </button>
        </div>
      </div>
      {actions.overlay}
    </div>
  );
});
function Cover({ track, onClick, large = false }: { track?: TrackSnapshot; onClick?: () => void; large?: boolean }) {
  const store = usePlayerStore();
  const coverRevision = usePlayerSelector((state) => state.coverRevision);
  const [source, setSource] = useState("/default_cover.svg");
  const [imageAttempt, setImageAttempt] = useState(0);
  const coverAttempt = useRef({ generation: 0, retried: false });
  const imageRef = useRef<HTMLImageElement>(null);
  // biome-ignore lint/correctness/useExhaustiveDependencies: coverRevision is a cache-clear trigger; the body re-resolves through the store instead of reading it.
  useEffect(() => {
    let active = true;
    const attempt = { generation: coverAttempt.current.generation + 1, retried: false };
    coverAttempt.current = attempt;
    const ref = track?.coverRef;
    setSource("/default_cover.svg");
    if (!ref) return;
    const load = () => {
      void store.resolveCover(ref).then((value) => {
        if (active) setSource(value);
      });
    };
    const image = imageRef.current;
    if (large || !image || typeof IntersectionObserver === "undefined") load();
    else {
      const observer = new IntersectionObserver(
        (entries) => {
          if (entries.some((entry) => entry.isIntersecting)) {
            observer.disconnect();
            load();
          }
        },
        { rootMargin: "150px" },
      );
      observer.observe(image);
      return () => {
        active = false;
        coverAttempt.current.generation++;
        observer.disconnect();
      };
    }
    return () => {
      active = false;
      coverAttempt.current.generation++;
    };
  }, [track?.coverRef, coverRevision, store, large]);
  const image = (
    <img
      key={imageAttempt}
      ref={imageRef}
      src={source}
      loading={large ? "eager" : "lazy"}
      alt=""
      className="album-cover-image"
      referrerPolicy="no-referrer"
      onError={(e) => {
        const failed = e.currentTarget.getAttribute("src") || "";
        if (failed === "/default_cover.svg") return;
        const original = track?.coverRef;
        const attempt = coverAttempt.current;
        const gen = attempt.generation;
        if (original && /^\/artwork\/[a-f0-9]{64}$/.test(failed) && !attempt.retried) {
          attempt.retried = true;
          void store.recoverCover(original, failed).then((value) => {
            if (coverAttempt.current.generation !== gen) return;
            setSource(value);
            setImageAttempt((value) => value + 1);
          });
        } else
          setSource(original && validRemoteCover(original) && failed !== original ? original : "/default_cover.svg");
      }}
    />
  );
  return onClick ? (
    <button type="button" className="player-thumbnail album-cover-wrapper" onClick={onClick} aria-label="展开歌词">
      {image}
    </button>
  ) : (
    <div className={`album-cover-wrapper ${large ? "album-cover-large" : ""}`}>{image}</div>
  );
}
export function OtoneiPlayer({ current: providedCurrent }: { current?: TrackSnapshot } = {}) {
  const store = usePlayerStore();
  const state = usePlayerSelector(
    (state) => ({
      entries: state.queue.entries,
      currentEntryId: state.queue.currentEntryId,
      playMode: state.queue.playMode,
      current: state.queue.entries.find((entry) => entry.queueEntryId === state.queue.currentEntryId)?.track,
      hydration: state.hydration,
      settings: state.settings,
      stream: state.stream,
      playback: state.playback,
      libraryReady: state.libraryReady,
      qualityPolicy: state.qualityPolicy,
    }),
    shallowEqual,
  );
  const current = providedCurrent ?? state.current;
  const [expanded, setExpanded] = useState(false);
  const [queueOpen, setQueueOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const q = state;
  const ready = state.hydration === "ready";
  const expandedRef = useRef<HTMLElement>(null);
  const queueRef = useRef<HTMLElement>(null);
  const helpRef = useRef<HTMLElement>(null);
  const footerRef = useRef<HTMLElement>(null);
  const expandedOpener = useRef<HTMLElement | null>(null);
  const queueOpener = useRef<HTMLElement | null>(null);
  const helpOpener = useRef<HTMLElement | null>(null);
  const previousOverlay = useRef<"queue" | "lyrics" | "help" | null>(null);
  const openLyrics = useCallback(() => {
    if (store.isClosing()) return;
    if (!expanded) expandedOpener.current = document.activeElement as HTMLElement;
    setExpanded(true);
  }, [expanded, store]);
  const openQueue = useCallback(() => {
    if (store.isClosing()) return;
    queueOpener.current = document.activeElement as HTMLElement;
    setQueueOpen(true);
  }, [store]);
  const openHelp = useCallback(() => {
    if (store.isClosing() || document.querySelector('[role="dialog"], dialog[open]')) return;
    helpOpener.current = document.activeElement as HTMLElement;
    setHelpOpen(true);
  }, [store]);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) =>
      handlePlayerShortcut(event, store, { openQueue, openLyrics, openHelp }, document);
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [store, openQueue, openLyrics, openHelp]);
  useLayoutEffect(() => {
    const top = helpOpen ? "help" : queueOpen ? "queue" : expanded ? "lyrics" : null;
    const previous = previousOverlay.current;
    previousOverlay.current = top;
    const restore =
      previous === "help" ? helpOpener.current : previous === "queue" ? queueOpener.current : expandedOpener.current;
    const footer = footerRef.current;
    const dialog = helpOpen ? helpRef.current : queueOpen ? queueRef.current : expandedRef.current;
    if (!top || !dialog || !footer) {
      if (previous && restore?.isConnected) restore.focus();
      return;
    }
    const roots = helpOpen || queueOpen ? [dialog] : [dialog, footer];
    const parent = footer.parentElement;
    if (!parent) return;
    const initial =
      previous === "queue" && !queueOpen && restore?.isConnected
        ? restore
        : dialog.querySelector<HTMLElement>("button");
    return containOverlayFocus(roots, parent, initial || dialog, () => {
      if (helpOpen) setHelpOpen(false);
      else if (queueOpen) setQueueOpen(false);
      else setExpanded(false);
    });
  }, [expanded, queueOpen, helpOpen]);
  useLayoutEffect(
    () => () => {
      if (!previousOverlay.current) return;
      const opener =
        previousOverlay.current === "help" ? helpOpener.current : expandedOpener.current || queueOpener.current;
      if (opener?.isConnected) opener.focus();
    },
    [],
  );
  const iconButton = (label: string, icon: ReactNode, fn: () => void, disabled = false) => (
    <button
      type="button"
      className="control-icon-btn ui-icon-button"
      aria-label={label}
      title={label}
      onClick={fn}
      disabled={disabled}
    >
      {icon}
    </button>
  );
  const blocked = !ready || !current || !state.settings;
  const modeLabel = q.playMode === "shuffle" ? "随机播放" : q.playMode === "repeat-one" ? "单曲循环" : "列表循环";
  const helpContext = shortcutContext(store.getSnapshot(), store.isClosing());
  return (
    <>
      {helpOpen && (
        <>
          <button
            type="button"
            className="queue-backdrop"
            tabIndex={-1}
            data-overlay-backdrop=""
            aria-label="关闭快捷键帮助"
            onClick={() => setHelpOpen(false)}
          />
          <section
            ref={helpRef}
            className="shortcut-help"
            role="dialog"
            aria-modal="true"
            aria-labelledby="shortcut-help-title"
          >
            <div className="section-heading">
              <h2 id="shortcut-help-title">快捷键</h2>
              <button
                type="button"
                className="ui-icon-button"
                aria-label="关闭快捷键"
                onClick={() => setHelpOpen(false)}
              >
                <FaTimes />
              </button>
            </div>
            <p className="muted">仅在主窗口聚焦时生效；输入文字、操作控件或打开弹层时暂停。关闭帮助后即可使用。</p>
            <dl className="shortcut-help-list">
              {playerShortcuts.map((shortcut) => {
                const reason = shortcutUnavailable(shortcut, helpContext);
                return (
                  <div key={shortcut.id} className={reason ? "shortcut-unavailable" : undefined}>
                    <dt>
                      <kbd>{shortcut.keys}</kbd>
                    </dt>
                    <dd>
                      {shortcut.label}
                      {reason && <small>{reason}</small>}
                    </dd>
                  </div>
                );
              })}
            </dl>
            <p className="muted">Esc 关闭弹层 · Tab 切换弹层内的控件</p>
          </section>
        </>
      )}
      {expanded && (
        <section
          ref={expandedRef}
          className="player-expanded-view desktop-expanded-view"
          role="dialog"
          aria-label="歌词视图"
        >
          <button
            type="button"
            onClick={() => setExpanded(false)}
            className="close-lyrics-btn ui-icon-button"
            aria-label="收起歌词"
          >
            <FaTimes />
          </button>
          <div className="expanded-main-wrapper">
            <div className="album-info-section">
              <div className="album-cover-container">
                <Cover track={current} large />
              </div>
              <div className="track-details">
                <h2 className="track-title">{current?.name || "尚未选择歌曲"}</h2>
                <p className="track-artist-album">{current?.artists.map((a) => a.name).join(" / ")}</p>
              </div>
            </div>
            <PlayerLyrics />
          </div>
        </section>
      )}
      {queueOpen && (
        <>
          <button
            type="button"
            className="queue-backdrop"
            tabIndex={-1}
            data-overlay-backdrop=""
            aria-label="关闭播放队列"
            onClick={() => setQueueOpen(false)}
          />
          <section ref={queueRef} className="queue-drawer" role="dialog" aria-modal="true" aria-label="播放队列">
            <div className="section-heading">
              <h2>
                播放队列 <small>{q.entries.length}</small>
              </h2>
              <button
                type="button"
                className="ui-icon-button"
                aria-label="关闭队列"
                onClick={() => setQueueOpen(false)}
              >
                <FaTimes />
              </button>
            </div>
            {!ready && <p>正在读取队列…</p>}
            {ready && !q.entries.length && <p className="alert-light">从搜索结果加入歌曲</p>}
            <ol>
              {q.entries.map((e) => (
                <QueueTrackRow
                  key={e.queueEntryId}
                  entry={e}
                  selected={e.queueEntryId === q.currentEntryId}
                  disabled={!ready || !state.settings}
                />
              ))}
            </ol>
          </section>
        </>
      )}
      {/* biome-ignore lint/a11y/useSemanticElements: 播放器是页面底部的 footer landmark；role="region" 为 aria-label 提供可访问名称，改成 section 会丢失 footer 语义并破坏验收脚本的 footer 选择器。 */}
      <footer
        ref={footerRef}
        role="region"
        className={`audio-player ${expanded ? "expanded" : "collapsed"}`}
        aria-label="播放器"
      >
        <div className="player-inner">
          <PlayerProgress current={current} ready={ready} />
          <div className="player-content">
            <div className="player-info-controls">
              <div className="player-left-section">
                <Cover track={current} onClick={openLyrics} />
                <div className="track-info-container">
                  <h6 className="track-name">{current?.name || "尚未选择歌曲"}</h6>
                  <small className="track-artist">
                    {current?.artists.map((a) => a.name).join(" / ") || "搜索并播放一首音乐"}
                  </small>
                  <small className="stream-source">
                    {state.stream
                      ? `${state.stream.cacheHit ? "本地缓存 · " : ""}${state.stream.provider === "gd-netease" ? "GD 网易云备用音源" : "网易云直连"} · ${actualStreamQuality(state.stream)}${state.stream.actualFormat ? ` · ${state.stream.actualFormat}` : ""}${state.stream.status === "trial" ? " · 试听片段" : state.stream.status === "unknown" ? " · 完整性未确认" : ""}`
                      : ""}
                  </small>
                </div>
              </div>
              <div className="player-center-section">
                <div className="d-flex align-items-center justify-content-center">
                  {iconButton(
                    `播放模式：${modeLabel}`,
                    q.playMode === "shuffle" ? (
                      <MdShuffle size={22} />
                    ) : q.playMode === "repeat-one" ? (
                      <MdRepeatOne size={22} />
                    ) : (
                      <MdRepeat size={22} />
                    ),
                    () =>
                      store.setMode(
                        q.playMode === "repeat-all"
                          ? "repeat-one"
                          : q.playMode === "repeat-one"
                            ? "shuffle"
                            : "repeat-all",
                      ),
                    !ready,
                  )}
                  {iconButton("上一首", <MdSkipPrevious size={28} />, () => void store.previous(), blocked)}
                  <button
                    type="button"
                    className="control-icon-btn ui-icon-button ui-icon-button--play accent-control"
                    disabled={blocked}
                    aria-label={
                      state.playback === "paused" ? "播放" : state.playback === "loading" ? "取消加载" : "暂停"
                    }
                    onClick={() => (state.playback === "paused" ? void store.play() : store.pause())}
                  >
                    <div className="play-pause-button">
                      {state.playback === "paused" ? (
                        <span className="play-icon">
                          <FaPlay size={20} />
                        </span>
                      ) : (
                        <FaPause size={20} />
                      )}
                    </div>
                  </button>
                  {iconButton("下一首", <MdSkipNext size={28} />, () => void store.next(), !ready || !current)}
                  {current && <FavoriteButton track={current} />}
                </div>
                <PlayerTime current={current} />
              </div>
              <div className="player-right-section">
                <label className="volume">
                  <FaVolumeUp aria-hidden="true" />
                  <input
                    aria-label="音量"
                    type="range"
                    min="0"
                    max="1"
                    step="0.01"
                    value={state.settings?.volume ?? 1}
                    disabled={!state.settings}
                    onChange={(e) => store.setVolume(Number(e.target.value))}
                  />
                </label>
                <button
                  type="button"
                  className="control-button ui-icon-button"
                  aria-label={expanded ? "收起歌词" : "展开歌词"}
                  onClick={() => (expanded ? setExpanded(false) : openLyrics())}
                >
                  <img src="/lyric.svg" alt="" width="20" height="20" />
                </button>
                {iconButton("播放队列", <FaList size={18} />, () => openQueue())}
                <button
                  type="button"
                  className="control-button ui-icon-button"
                  aria-label="快捷键"
                  title="快捷键（?）"
                  onClick={openHelp}
                  disabled={expanded || queueOpen || helpOpen || store.isClosing()}
                >
                  <FaKeyboard size={18} />
                </button>
              </div>
            </div>
          </div>
        </div>
      </footer>
    </>
  );
}

function PlayerProgress({ current, ready }: { current?: TrackSnapshot; ready: boolean }) {
  const store = usePlayerStore();
  const positionMs = usePlayerSelector((state) => state.queue.positionMs);
  return (
    <div className="progress-control-container">
      <input
        aria-label="播放进度"
        type="range"
        min="0"
        max={Math.max(1, current?.durationMs || 0)}
        step="1000"
        value={Math.min(positionMs, current?.durationMs || 0)}
        disabled={!ready || !current}
        onChange={(e) => store.seek(Number(e.target.value))}
      />
    </div>
  );
}
function PlayerTime({ current }: { current?: TrackSnapshot }) {
  const positionMs = usePlayerSelector((state) => state.queue.positionMs);
  return (
    <small className="player-time">
      {time(positionMs)} / {time(current?.durationMs || 0)}
    </small>
  );
}
function PlayerLyrics() {
  const state = usePlayerSelector((state) => ({ lyrics: state.lyrics, lyricError: state.lyricError }), shallowEqual);
  const lyrics = useMemo(
    () => alignLyrics(state.lyrics?.original || "", state.lyrics?.translated || ""),
    [state.lyrics?.original, state.lyrics?.translated],
  );
  const positionMs = usePlayerSelector((state) => state.queue.positionMs);
  let active = -1;
  lyrics.forEach((line, index) => {
    if (line.time <= positionMs) active = index;
  });
  const lyricRef = useRef<HTMLDivElement>(null);
  // biome-ignore lint/correctness/useExhaustiveDependencies: active is the display trigger; the DOM class is the actual scroll input.
  useEffect(() => {
    lyricRef.current?.querySelector(".active")?.scrollIntoView({ block: "center", behavior: "smooth" });
  }, [active]);
  return (
    <div className="lyrics-section">
      <div className="lyrics-scroll-container" ref={lyricRef}>
        {state.lyricError ? (
          <div className="lyric-line no-lyrics">歌词暂时无法加载</div>
        ) : lyrics.length ? (
          lyrics.map((line, index) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: lyrics are immutable per track and never reordered; duplicate timestamps are possible, so the index is the only collision-free identity.
            <div key={index} className={`lyric-line ${index === active ? "active" : ""}`} data-time={line.time}>
              <div>{line.text || "♪"}</div>
              {line.translated && <div className="translated-lyric">{line.translated}</div>}
            </div>
          ))
        ) : (
          <div className="lyric-line no-lyrics plain-lyrics">
            {state.lyrics?.original || "播放歌曲后在这里查看歌词"}
            {state.lyrics?.translated && <p>{state.lyrics.translated}</p>}
          </div>
        )}
      </div>
    </div>
  );
}

function QueueTrackRow({
  entry,
  selected,
  disabled,
}: {
  entry: { queueEntryId: string; track: TrackSnapshot };
  selected: boolean;
  disabled: boolean;
}) {
  const store = usePlayerStore();
  const actions = useTrackActions({ track: entry.track, queueEntryId: entry.queueEntryId, disabled });
  return (
    <li className={selected ? "selected" : ""} onContextMenu={actions.onContextMenu} onKeyDown={actions.onKeyDown}>
      <button
        type="button"
        className="queue-track"
        disabled={disabled}
        onClick={() => void store.select(entry.queueEntryId)}
      >
        <strong>{entry.track.name}</strong>
        <small>{entry.track.artists.map((artist) => artist.name).join(" / ")}</small>
      </button>
      <FavoriteButton track={entry.track} />
      {actions.button}
      <button
        type="button"
        className="ui-icon-button"
        aria-label={`移除：${entry.track.name}`}
        disabled={disabled}
        onClick={() => store.remove(entry.queueEntryId)}
      >
        <FaTimes />
      </button>
      {actions.overlay}
    </li>
  );
}

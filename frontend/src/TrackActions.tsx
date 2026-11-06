import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import { createPortal } from "react-dom";
import { FaEllipsisH } from "react-icons/fa";
import type { TrackSnapshot } from "./contracts";
import { usePlayerSelector, usePlayerStore } from "./PlayerContext";
import { containOverlayFocus } from "./overlayFocus";
import { copyTrackShare, neteasePageURL, openNeteasePage } from "./trackSharing";

type Options = {
  track: TrackSnapshot;
  disabled?: boolean;
  queueEntryId?: string;
  onPlay?: (track: TrackSnapshot) => void;
  onAdd?: (track: TrackSnapshot) => void;
};
export function clampTrackMenu(
  x: number,
  y: number,
  width: number,
  height: number,
  viewportWidth: number,
  viewportHeight: number,
) {
  return {
    left: Math.max(8, Math.min(x, viewportWidth - width - 8)),
    top: Math.max(8, Math.min(y, viewportHeight - height - 8)),
  };
}
export function useTrackActions({ track, disabled = false, queueEntryId, onPlay, onAdd }: Options) {
  const [position, setPosition] = useState<{ left: number; top: number } | null>(null);
  const [message, setMessage] = useState("");
  const store = usePlayerStore();
  const available = usePlayerSelector((state) => state.hydration === "ready" && !!state.settings);
  const libraryReady = usePlayerSelector((state) => state.libraryReady);
  const liked = usePlayerSelector((state) => state.favoriteIDs.has(track.neteaseSongId));
  // A closed menu cannot show stale state, so it must not scan the queue on every
  // playback tick; only the open menu has to notice that its target disappeared.
  const targetExists = usePlayerSelector(
    (state) =>
      !position ||
      !queueEntryId ||
      state.queue.entries.some(
        (entry) => entry.queueEntryId === queueEntryId && entry.track.neteaseSongId === track.neteaseSongId,
      ),
  );
  const menuRef = useRef<HTMLDivElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const generation = useRef(0);
  const close = useCallback(() => setPosition(null), []);
  // biome-ignore lint/correctness/useExhaustiveDependencies: replacing the target invalidates an open menu and its asynchronous feedback.
  useLayoutEffect(() => {
    generation.current++;
    setPosition(null);
    setMessage("");
    return () => {
      generation.current++;
    };
  }, [track, queueEntryId]);
  useEffect(() => {
    if (!targetExists) close();
  }, [targetExists, close]);
  useLayoutEffect(() => {
    const menu = menuRef.current;
    if (!position || !menu) return;
    const rect = menu.getBoundingClientRect();
    const clamped = clampTrackMenu(
      position.left,
      position.top,
      rect.width,
      rect.height,
      window.innerWidth,
      window.innerHeight,
    );
    menu.style.left = `${clamped.left}px`;
    menu.style.top = `${clamped.top}px`;
    const release = containOverlayFocus(
      [menu],
      document.body,
      menu.querySelector<HTMLElement>("button") || menu,
      close,
    );
    const dismiss = () => close();
    window.addEventListener("resize", dismiss);
    return () => {
      window.removeEventListener("resize", dismiss);
      release();
      if (opener.current?.isConnected) opener.current.focus();
    };
  }, [position, close]);
  useEffect(() => {
    if (!message) return;
    const timer = setTimeout(() => setMessage(""), 4000);
    return () => clearTimeout(timer);
  }, [message]);
  function valid() {
    const state = store.getSnapshot();
    return (
      !store.isClosing() &&
      (!queueEntryId ||
        state.queue.entries.some(
          (entry) => entry.queueEntryId === queueEntryId && entry.track.neteaseSongId === track.neteaseSongId,
        ))
    );
  }
  function open(event: MouseEvent<HTMLElement> | KeyboardEvent<HTMLElement>) {
    if (!valid()) return;
    event.preventDefault();
    event.stopPropagation();
    generation.current++;
    setMessage("");
    opener.current = event.currentTarget.matches("button")
      ? event.currentTarget
      : event.currentTarget.querySelector<HTMLElement>('[aria-haspopup="dialog"]');
    const rect = event.currentTarget.getBoundingClientRect();
    setPosition(
      "clientX" in event && event.clientX
        ? { left: event.clientX, top: event.clientY }
        : { left: rect.left, top: rect.bottom },
    );
  }
  function run(command: () => void | Promise<void>, success = "") {
    if (!valid()) {
      close();
      return;
    }
    const gen = generation.current;
    close();
    try {
      Promise.resolve(command())
        .then(() => {
          if (generation.current === gen && valid()) setMessage(success);
        })
        .catch(() => {
          if (generation.current === gen && valid()) setMessage("操作失败，请重试。");
        });
    } catch {
      if (generation.current === gen) setMessage("操作失败，请重试。");
    }
  }
  function queueCommand(command: () => void) {
    run(() => {
      const state = store.getSnapshot();
      if (disabled || state.hydration !== "ready" || !state.settings) return;
      command();
    });
  }
  const blocked = disabled || !available;
  const menu =
    position && targetExists
      ? createPortal(
          <>
            <button
              type="button"
              className="track-menu-backdrop"
              data-overlay-backdrop=""
              tabIndex={-1}
              aria-label="关闭歌曲操作"
              onClick={close}
            />
            <div
              ref={menuRef}
              className="track-action-menu"
              role="dialog"
              aria-modal="true"
              aria-label={`歌曲操作：${track.name}`}
              tabIndex={-1}
              style={position}
            >
              <strong>{track.name}</strong>
              <button
                type="button"
                disabled={blocked}
                onClick={() =>
                  queueCommand(() => {
                    if (queueEntryId) void store.select(queueEntryId);
                    else if (onPlay) onPlay(track);
                    else {
                      const id = store.add(track);
                      if (id) void store.select(id);
                    }
                  })
                }
              >
                播放
              </button>
              <button
                type="button"
                disabled={blocked}
                onClick={() =>
                  queueCommand(() => {
                    store.playNext([track]);
                  })
                }
              >
                下一首播放
              </button>
              <button
                type="button"
                disabled={blocked}
                onClick={() =>
                  queueCommand(() => {
                    if (onAdd) onAdd(track);
                    else store.add(track);
                  })
                }
              >
                加入队列
              </button>
              <button
                type="button"
                disabled={!libraryReady}
                onClick={() =>
                  run(() => {
                    if (store.getSnapshot().libraryReady) store.toggleFavorite(track);
                  })
                }
              >
                {liked ? "取消收藏" : "收藏"}
              </button>
              <button
                type="button"
                disabled={!neteasePageURL("song", track.neteaseSongId)}
                onClick={() => run(() => copyTrackShare(track), "已复制歌曲分享。")}
              >
                复制分享
              </button>
              <button
                type="button"
                disabled={!neteasePageURL("song", track.neteaseSongId)}
                onClick={() => run(() => openNeteasePage("song", track.neteaseSongId), "已打开网易云歌曲页面。")}
              >
                在网易云打开歌曲
              </button>
              {neteasePageURL("album", track.album?.id) && (
                <button
                  type="button"
                  onClick={() => run(() => openNeteasePage("album", track.album?.id), "已打开网易云专辑页面。")}
                >
                  打开专辑：{track.album?.name}
                </button>
              )}
              {track.artists
                .filter((artist) => neteasePageURL("artist", artist.id))
                .map((artist) => (
                  <button
                    key={artist.id}
                    type="button"
                    onClick={() => run(() => openNeteasePage("artist", artist.id), "已打开网易云歌手页面。")}
                  >
                    打开歌手：{artist.name}
                  </button>
                ))}
              {queueEntryId && (
                <button type="button" disabled={blocked} onClick={() => queueCommand(() => store.remove(queueEntryId))}>
                  从队列移除
                </button>
              )}
              <button type="button" onClick={close}>
                关闭
              </button>
            </div>
          </>,
          document.body,
        )
      : null;
  return {
    onContextMenu: open,
    onKeyDown: (event: KeyboardEvent<HTMLElement>) => {
      if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) open(event);
    },
    button: (
      <button
        type="button"
        className="ui-icon-button"
        aria-label={`更多操作：${track.name}`}
        aria-haspopup="dialog"
        aria-expanded={!!position}
        onClick={open}
      >
        <FaEllipsisH />
      </button>
    ),
    overlay: (
      <>
        {menu}
        {message && (
          <span role="status" className="track-action-feedback">
            {message}
          </span>
        )}
      </>
    ),
  };
}

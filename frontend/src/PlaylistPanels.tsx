// Local playlist controls reuse the adapted OTONEI music-card and icon-button presentation.
import { useCallback, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { FaListUl } from "react-icons/fa";
import type { TrackSnapshot } from "./contracts";
import { usePlayerSelector, usePlayerStore } from "./PlayerContext";
import { TrackCard } from "./OtoneiPresentation";
import { containOverlayFocus } from "./overlayFocus";

export function usePlaylists() {
  const store = usePlayerStore();
  return useSyncExternalStore(store.playlists.subscribe, store.playlists.getSnapshot, store.playlists.getSnapshot);
}
export function PlaylistPanel() {
  const store = usePlayerStore();
  const playable = usePlayerSelector((state) => state.hydration === "ready" && !!state.settings);
  const state = usePlaylists();
  const playTrack = useCallback(
    (track: TrackSnapshot) => {
      const id = store.add(track);
      if (id) void store.select(id);
    },
    [store],
  );
  const addTrack = useCallback(
    (track: TrackSnapshot) => {
      store.add(track);
    },
    [store],
  );
  const [selected, setSelected] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [rename, setRename] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const playlist = selected ? state.details[selected] : undefined;
  // biome-ignore lint/correctness/useExhaustiveDependencies: selected resets the form even when two playlists share a name; the name itself is also a trigger.
  useEffect(() => {
    setRename(playlist?.name ?? "");
    setConfirmDelete(false);
  }, [selected, playlist?.name]);
  const busy = state.pending > 0;
  const ready = state.status === "ready";
  const message = (
    <>
      {state.status === "loading" && <p className="muted">正在读取本地歌单…</p>}
      {state.error && (
        <p role="alert" className="error">
          {state.error}
        </p>
      )}
      {busy && (
        <p role="status" className="muted">
          正在保存或读取歌单…
        </p>
      )}
    </>
  );
  if (selected)
    return (
      <section aria-label="本地歌单详情">
        <button type="button" onClick={() => setSelected(null)}>
          返回歌单
        </button>
        {message}
        {playlist ? (
          <>
            <div className="section-heading">
              <h2>{playlist.name}</h2>
              <span className="muted">{playlist.tracks.length} 首歌曲</span>
            </div>
            <form
              className="playlist-form"
              onSubmit={(e) => {
                e.preventDefault();
                void store.playlists.rename(selected, rename);
              }}
            >
              <input
                aria-label="歌单新名称"
                value={rename}
                maxLength={256}
                onChange={(e) => setRename(e.target.value)}
                disabled={!ready || busy}
              />
              <button type="submit" disabled={!ready || busy || !rename.trim()}>
                重命名
              </button>
            </form>
            <div className="playlist-toolbar">
              <button
                type="button"
                disabled={!playable || !playlist.tracks.length}
                onClick={() => store.addAll(playlist.tracks, true)}
              >
                播放全部
              </button>
              <button
                type="button"
                disabled={!playable || !playlist.tracks.length}
                onClick={() => store.addAll(playlist.tracks)}
              >
                全部加入队列
              </button>
              <button type="button" disabled={!ready || busy} onClick={() => setConfirmDelete(true)}>
                删除歌单
              </button>
            </div>
            {confirmDelete && (
              // biome-ignore lint/a11y/useSemanticElements: this is a confirmation panel, not a form field group; fieldset would add border/legend semantics.
              <div className="alert-light" role="group" aria-label="确认删除歌单">
                <p>确定删除“{playlist.name}”？播放队列中的歌曲会继续保留。</p>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() =>
                    void store.playlists.delete(selected).then((deleted) => {
                      if (deleted) setSelected(null);
                    })
                  }
                >
                  确认删除
                </button>{" "}
                <button type="button" disabled={busy} onClick={() => setConfirmDelete(false)}>
                  取消
                </button>
              </div>
            )}
            <div className="music-list">
              {playlist.tracks.map((track, index) => (
                <div key={track.neteaseSongId} className="playlist-track">
                  <TrackCard track={track} disabled={!playable} onPlay={playTrack} onAdd={addTrack} />
                  <div className="playlist-track-actions">
                    <button
                      type="button"
                      aria-label={`上移：${track.name}`}
                      disabled={!ready || busy || index === 0}
                      onClick={() => void store.playlists.move(selected, track.neteaseSongId, -1)}
                    >
                      上移
                    </button>
                    <button
                      type="button"
                      aria-label={`下移：${track.name}`}
                      disabled={!ready || busy || index === playlist.tracks.length - 1}
                      onClick={() => void store.playlists.move(selected, track.neteaseSongId, 1)}
                    >
                      下移
                    </button>
                    <button
                      type="button"
                      aria-label={`从歌单移除：${track.name}`}
                      disabled={!ready || busy}
                      onClick={() => void store.playlists.remove(selected, track.neteaseSongId)}
                    >
                      移除
                    </button>
                  </div>
                </div>
              ))}
            </div>
            {!playlist.tracks.length && <p className="empty">歌单还是空的。点击歌曲旁的“加入歌单”来添加音乐。</p>}
          </>
        ) : (
          ready && !busy && <p className="empty">歌单已更新或删除，请返回列表重新打开。</p>
        )}
      </section>
    );
  return (
    <section aria-label="本地歌单">
      <p className="muted">整理这台电脑上的歌单，按你喜欢的顺序听歌。</p>
      <form
        className="playlist-form"
        onSubmit={(e) => {
          e.preventDefault();
          void store.playlists.create(name).then((p) => {
            if (p) setName("");
          });
        }}
      >
        <input
          aria-label="新歌单名称"
          placeholder="新歌单名称"
          value={name}
          maxLength={256}
          disabled={!ready || busy}
          onChange={(e) => setName(e.target.value)}
        />
        <button type="submit" disabled={!ready || busy || !name.trim() || state.items.length >= 100}>
          创建歌单
        </button>
      </form>
      {message}
      <div className="music-list">
        {state.items.map((p) => (
          <div className="music-card" key={p.playlistId}>
            <div className="music-card-row">
              <button
                type="button"
                className="music-card-info"
                disabled={busy}
                onClick={() => {
                  setSelected(p.playlistId);
                  void store.playlists.load(p.playlistId);
                }}
              >
                <h6>{p.name}</h6>
                <small>{p.trackCount} 首歌曲</small>
              </button>
            </div>
          </div>
        ))}
      </div>
      {ready && !state.items.length && <p className="empty">还没有本地歌单，创建一个开始整理音乐。</p>}
    </section>
  );
}

export function AddToPlaylistButton({ track }: { track: TrackSnapshot }) {
  const state = usePlaylists();
  const [open, setOpen] = useState(false);
  const opener = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button
        type="button"
        ref={opener}
        className="ui-icon-button"
        aria-label={`加入歌单：${track.name}`}
        title="加入歌单"
        disabled={state.status !== "ready"}
        onClick={() => setOpen(true)}
      >
        <FaListUl />
      </button>
      {open && <AddToPlaylistDialog track={track} close={() => setOpen(false)} opener={opener.current} />}
    </>
  );
}
function AddToPlaylistDialog({
  track,
  close,
  opener,
}: {
  track: TrackSnapshot;
  close: () => void;
  opener: HTMLButtonElement | null;
}) {
  const store = usePlayerStore();
  const state = usePlaylists();
  const [name, setName] = useState("");
  const root = useRef<HTMLDivElement>(null);
  const closeRef = useRef(close);
  closeRef.current = close;
  const container = document.querySelector<HTMLElement>(".app-container");
  useLayoutEffect(() => {
    if (!root.current || !container) return;
    const release = containOverlayFocus(
      [root.current],
      container,
      root.current.querySelector<HTMLElement>("button") ?? root.current,
      () => closeRef.current(),
    );
    return () => {
      release();
      if (opener?.isConnected) opener.focus();
    };
  }, [container, opener]);
  if (!container) return null;
  const busy = state.pending > 0;
  return createPortal(
    <div className="playlist-dialog-layer" ref={root}>
      <section
        className="playlist-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="playlist-dialog-title"
        tabIndex={-1}
      >
        <div className="section-heading">
          <h2 id="playlist-dialog-title">加入歌单</h2>
          <button type="button" aria-label="关闭加入歌单" onClick={close}>
            关闭
          </button>
        </div>
        <p className="muted">{track.name}</p>
        {state.error && (
          <p className="error" role="alert">
            {state.error}
          </p>
        )}
        <div className="playlist-choices">
          {state.items.map((p) => (
            <button
              type="button"
              key={p.playlistId}
              disabled={busy || state.status !== "ready"}
              onClick={() =>
                void store.playlists.add(p.playlistId, track).then((saved) => {
                  if (saved) close();
                })
              }
            >
              {p.name} · {p.trackCount} 首
            </button>
          ))}
        </div>
        {!state.items.length && <p className="muted">创建第一个歌单来收藏这首歌。</p>}
        <form
          className="playlist-form"
          onSubmit={(e) => {
            e.preventDefault();
            void store.playlists.create(name, track).then((saved) => {
              if (saved) close();
            });
          }}
        >
          <input
            aria-label="新歌单名称"
            placeholder="新歌单名称"
            value={name}
            maxLength={256}
            disabled={busy || state.status !== "ready"}
            onChange={(e) => setName(e.target.value)}
          />
          <button
            type="submit"
            disabled={busy || state.status !== "ready" || !name.trim() || state.items.length >= 100}
          >
            创建并加入
          </button>
        </form>
        {busy && (
          <p role="status" className="muted">
            正在保存…
          </p>
        )}
      </section>
    </div>,
    container,
  );
}

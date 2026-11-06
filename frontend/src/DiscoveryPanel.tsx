import { useCallback, useEffect, useRef, useState } from "react";
import type { DesktopBridge, PublicPlaylistPage, ToplistResult, TrackSnapshot } from "./contracts";
import { usePlayerSelector, usePlayerStore } from "./PlayerContext";
import { TrackCard } from "./OtoneiPresentation";

export function discoveryErrorMessage(error: unknown): string {
  const code = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  if (/LOGIN_REQUIRED|UNAUTHORIZED|NEED_LOGIN|loginRequired/i.test(code))
    return "此榜单暂需网易云登录，当前客户端不提供账号登录。";
  if (/RATE_LIMITED|rateLimited|429/i.test(code)) return "请求较频繁，请稍后重试。";
  return "排行榜暂时无法加载，请检查网络后重试。";
}
export function DiscoveryPanel({ bridge }: { bridge: DesktopBridge }) {
  const store = usePlayerStore();
  const ready = usePlayerSelector((state) => state.hydration === "ready" && !!state.settings);
  const [toplists, setToplists] = useState<ToplistResult | null>(null);
  const [page, setPage] = useState<PublicPlaylistPage | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [offset, setOffset] = useState(0);
  const [retry, setRetry] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: retry restarts a failed request; the fetched list stays cached until navigation.
  useEffect(() => {
    const controller = new AbortController();
    const gen = ++generation.current;
    setError("");
    // Keep the fetched list while inspecting a playlist; no background prefetch.
    if (!selected && toplists) {
      setLoading(false);
      return () => {
        controller.abort();
        generation.current++;
      };
    }
    if (store.isClosing()) return;
    const request = selected
      ? bridge.GetPublicPlaylist?.(selected, offset, controller.signal)
      : bridge.GetToplists?.(controller.signal);
    if (!request) {
      setError("当前版本暂不支持排行榜，请更新客户端。");
      setLoading(false);
      return;
    }
    setLoading(true);
    void request
      .then((result) => {
        if (gen !== generation.current || controller.signal.aborted) return;
        if (selected) setPage(result as PublicPlaylistPage);
        else setToplists(result as ToplistResult);
      })
      .catch((failure: unknown) => {
        if (gen === generation.current && !controller.signal.aborted) setError(discoveryErrorMessage(failure));
      })
      .finally(() => {
        if (gen === generation.current) setLoading(false);
      });
    return () => {
      controller.abort();
      generation.current++;
    };
  }, [bridge, store, selected, offset, retry]);
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
  function select(id: string | null) {
    generation.current++;
    setSelected(id);
    setOffset(0);
    setPage(null);
    setError("");
  }
  function changePage(value: number) {
    generation.current++;
    setOffset(value);
    setPage(null);
  }
  const current = page?.playlistId === selected && page.offset === offset ? page : null;
  const blocked = !ready || store.isClosing() || loading || !current?.tracks.length;
  return (
    <section aria-label="排行榜" className="discovery-panel">
      {selected && (
        <button type="button" onClick={() => select(null)}>
          返回排行榜
        </button>
      )}
      <div className="section-heading">
        <h1>
          {selected
            ? current?.name || toplists?.items.find((item) => item.playlistId === selected)?.name || "榜单详情"
            : "排行榜"}
        </h1>
      </div>
      <p className="muted">网易云公共榜单 · 歌曲是否可播放取决于当前服务与播放权益。</p>
      {loading && <p role="status">正在加载{selected ? "榜单歌曲" : "排行榜"}…</p>}
      {error && (
        <div className="alert-light">
          <p role="alert">{error}</p>
          <button type="button" onClick={() => setRetry((value) => value + 1)}>
            重试
          </button>
        </div>
      )}
      {!selected && toplists && (
        <>
          {toplists.partial && <p className="muted">部分榜单资料暂不可用。</p>}
          {!toplists.items.length && <p className="alert-light">暂时没有可显示的排行榜。</p>}
          <div className="music-list ranking-list">
            {toplists.items.map((item) => (
              <button
                type="button"
                className="music-card ranking-item"
                key={item.playlistId}
                onClick={() => select(item.playlistId)}
              >
                <strong>{item.name}</strong>
                <span className="muted">{item.trackCount} 首</span>
                {item.description && <small>{item.description}</small>}
              </button>
            ))}
          </div>
        </>
      )}
      {selected && current && (
        <>
          {current.description && <p className="ranking-description muted">{current.description}</p>}
          <p className="muted">
            共 {current.total} 首 · 当前可浏览 {current.availableCount} 首 · 本页 {current.tracks.length} 首
          </p>
          {current.partial && <p className="alert-light">仅显示部分歌曲资料；部分曲目暂不可用或超过本次浏览范围。</p>}
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
          <nav className="pagination" aria-label="榜单翻页">
            <button
              type="button"
              disabled={loading || offset <= 0}
              onClick={() => changePage(Math.max(0, offset - 30))}
            >
              上一页
            </button>
            <span>第 {Math.floor(offset / 30) + 1} 页</span>
            <button type="button" disabled={loading || !current.hasMore} onClick={() => changePage(current.nextOffset)}>
              下一页
            </button>
          </nav>
        </>
      )}
    </section>
  );
}

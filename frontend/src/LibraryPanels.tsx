// Track rows and heart treatment adapted from OTONEI SearchResultItem.tsx and HeartButton.jsx (MIT).
import { useEffect, useRef, useState } from "react";
import type { TrackSnapshot, LocalSettings } from "./contracts";
import { normalizeLocalQuery } from "./contracts";
import { usePlayer } from "./PlayerContext";
import { PlaylistPanel } from "./PlaylistPanels";
import { TrackCard } from "./OtoneiPresentation";
import { FaHeart, FaRegHeart } from "react-icons/fa";
import { DesktopSettingsPanel } from "./DesktopSettingsPanel";
import { startSettingsCachePolling } from "./diagnosticPolling";
import type { DesktopBridge } from "./contracts";

export function FavoriteButton({ track }: { track: TrackSnapshot }) {
  const { state, store } = usePlayer();
  const liked = store.isFavorite(track);
  return (
    <button
      type="button"
      className={`favorite ui-icon-button ${liked ? "liked" : ""}`}
      disabled={!state.libraryReady}
      aria-pressed={liked}
      aria-label={`${liked ? "取消收藏" : "收藏"}：${track.name}`}
      onClick={() => store.toggleFavorite(track)}
    >
      {liked ? <FaHeart /> : <FaRegHeart />}
    </button>
  );
}

export function LibraryPanel({ kind }: { kind: "favorites" | "history" }) {
  const { state, store } = usePlayer();
  const [filter, setFilter] = useState(kind === "favorites" ? state.favoritesQuery : state.historyQuery);
  const [tab, setTab] = useState<"tracks" | "playlists">("tracks");
  const tracks =
    kind === "favorites"
      ? state.favorites.map((track) => ({
          id: track.neteaseSongId,
          track,
          date: "",
        }))
      : state.history.map((entry) => ({
          id: entry.historyEntryId,
          track: entry.track,
          date: new Date(entry.playedAt).toLocaleString("zh-CN"),
        }));
  useEffect(() => {
    store.resumeLibraryQuery(kind);
    return () => store.cancelLibraryQuery(kind);
  }, [store, kind]);
  let query = filter;
  try {
    query = normalizeLocalQuery(filter);
  } catch {
    /* Store reports the invalid byte boundary. */
  }
  const loading = kind === "favorites" ? state.favoritesLoading : state.historyLoading || state.historyClearing;
  const page = kind === "favorites" ? state.favoritesPage : state.historyPage;
  const hasMore = kind === "favorites" ? state.favoritesHasMore : state.historyHasMore;
  const navigate = (direction: -1 | 1) =>
    kind === "favorites" ? store.navigateFavorites(direction) : store.navigateHistory(direction);
  useEffect(() => {
    const saved = kind === "favorites" ? state.favoritesQuery : state.historyQuery;
    if (query === saved || (kind === "history" && state.historyClearing)) return;
    const timer = setTimeout(() => {
      void (kind === "favorites" ? store.queryFavorites(query) : store.queryHistory(query));
    }, 250);
    return () => clearTimeout(timer);
  }, [query, kind, store, state.favoritesQuery, state.historyQuery, state.historyClearing]);
  return (
    <section className="search-panel" aria-label={kind === "favorites" ? "本地收藏" : "播放历史"}>
      <div className="section-heading">
        <h1>{kind === "favorites" ? "本地收藏" : "播放历史"}</h1>
        {kind === "history" && (
          <button
            type="button"
            disabled={!state.libraryReady || state.historyClearing}
            onClick={() => store.clearHistory()}
          >
            清空历史
          </button>
        )}
      </div>
      {kind === "favorites" && (
        // biome-ignore lint/a11y/useSemanticElements: segmented button group; fieldset would introduce form/legend semantics and default styling.
        <div className="library-tabs" role="group" aria-label="本地收藏分类">
          <button type="button" aria-pressed={tab === "tracks"} onClick={() => setTab("tracks")}>
            收藏歌曲
          </button>
          <button type="button" aria-pressed={tab === "playlists"} onClick={() => setTab("playlists")}>
            本地歌单
          </button>
        </div>
      )}
      {kind === "favorites" && tab === "playlists" ? (
        <PlaylistPanel />
      ) : (
        <>
          <p className="muted">
            {kind === "favorites"
              ? "喜欢的音乐保存在这台电脑，离线也可以查看。"
              : "记录实际听过的音乐，保留至手动清空。"}
          </p>
          <input
            disabled={!state.libraryReady || (kind === "history" && state.historyClearing)}
            className="local-filter"
            aria-label="筛选本地音乐"
            placeholder="筛选歌曲、歌手或专辑"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
          {!state.libraryReady && !state.libraryError && <p className="muted">正在读取本地音乐资料…</p>}
          <div className="music-list">
            {tracks.map(({ id, track, date }) => (
              <TrackCard
                key={id}
                track={track}
                date={date}
                disabled={state.hydration !== "ready" || !state.settings}
                onPlay={() => {
                  const entry = store.add(track);
                  if (entry) void store.select(entry);
                }}
                onAdd={() => store.add(track)}
              />
            ))}
          </div>
          <p role="status" aria-live="polite">
            {loading ? "正在读取本地音乐资料…" : ""}
          </p>
          {state.libraryError && (
            <p role="alert">
              {state.libraryError}
              <button type="button" onClick={() => void store.retryLibrary(kind)}>
                重试
              </button>
            </p>
          )}
          <nav className="pagination" aria-label={kind === "favorites" ? "收藏翻页" : "历史翻页"}>
            <button type="button" disabled={loading || page <= 1} onClick={() => void navigate(-1)}>
              上一页
            </button>
            <span>第 {page} 页</span>
            <button type="button" disabled={loading || !hasMore} onClick={() => void navigate(1)}>
              下一页
            </button>
          </nav>
          {state.libraryReady && !loading && !tracks.length && (
            <p className="empty">
              {query
                ? "没有符合筛选条件的歌曲"
                : kind === "favorites"
                  ? "点击歌曲旁的爱心，收藏喜欢的音乐。"
                  : "开始听歌后，播放记录会出现在这里。"}
            </p>
          )}
        </>
      )}
    </section>
  );
}

export function SettingsPanel({ bridge }: { bridge?: DesktopBridge } = {}) {
  const { state, store } = usePlayer();
  useEffect(() => startSettingsCachePolling(store), [store]);
  const [refreshing, setRefreshing] = useState(false);
  const refreshRequest = useRef<AbortController | null>(null);
  useEffect(() => () => refreshRequest.current?.abort(), []);
  async function refresh() {
    const controller = new AbortController();
    refreshRequest.current = controller;
    setRefreshing(true);
    try {
      await store.refreshRegion(controller.signal);
    } finally {
      if (!controller.signal.aborted) setRefreshing(false);
    }
  }
  const settings = state.settings;
  const region = state.region;
  if (!settings)
    return (
      <section className="search-panel">
        <h1>设置</h1>
        <p className="muted">{state.settingsError ? "设置尚未读取。" : "正在读取设置…"}</p>
      </section>
    );
  return (
    <section className="search-panel settings" aria-label="设置">
      <h1>设置</h1>
      <p className="muted">更改会自动保存到这台电脑，并用于下一次播放。</p>
      <label>
        首选音质
        <select
          value={settings.preferredQuality}
          onChange={(e) =>
            store.setSettings({
              preferredQuality: e.target.value as LocalSettings["preferredQuality"],
            })
          }
        >
          {(state.qualityPolicy?.selectableQualities ?? ["exhigh"]).map((quality) => (
            <option key={quality} value={quality}>
              {
                {
                  exhigh: "320 kbps（极高）",
                  lossless: "无损",
                  hires: "Hi-Res",
                  jyeffect: "高清环绕声",
                  sky: "沉浸环绕声",
                  jymaster: "超清母带",
                }[quality]
              }
            </option>
          ))}
        </select>
      </label>
      <p className="muted">
        320 kbps 优先；192/128 仅自动降级。
        {!state.qualityPolicy?.premiumAllowed && "更高音质需登录并确认会员。"}
        实际音质取决于歌曲和可用的播放权限，以播放栏显示为准。
      </p>
      <label>
        海外播放兼容
        <select
          value={settings.regionMode}
          onChange={(e) =>
            store.setSettings({
              regionMode: e.target.value as LocalSettings["regionMode"],
            })
          }
        >
          <option value="auto">自动（推荐）</option>
          <option value="always">始终启用</option>
          <option value="off">关闭</option>
        </select>
      </label>
      <p className="muted">
        自动模式按出口地区选择直连方式：中国大陆使用普通请求，海外直接启用兼容；地区未知时保留兜底重试。更改不会打断正在播放的歌曲。
      </p>
      <div className="region-status" aria-live="polite">
        <strong>
          当前地区：
          {region?.classification === "CN"
            ? "中国大陆"
            : region?.classification === "overseas"
              ? `海外${region.country ? `（${region.country}）` : ""}`
              : "尚未确定"}
        </strong>
        <small>
          {region?.detecting
            ? "正在检测…"
            : region?.observedAt
              ? `最近检测：${new Date(region.observedAt).toLocaleString("zh-CN")}`
              : "地区检测不影响浏览本地音乐。"}
        </small>
        <button type="button" disabled={refreshing || region?.detecting} onClick={() => void refresh()}>
          {refreshing ? "正在检测…" : "手动重新检测"}
        </button>
        {(state.regionError || region?.errorCode) && (
          <p className="muted">{state.regionError || "本次检测未能确定地区，可稍后重试。"}</p>
        )}
      </div>
      <label>
        音量
        <input
          aria-label="设置音量"
          type="range"
          min="0"
          max="1"
          step="0.01"
          value={settings.volume}
          onChange={(e) => store.setVolume(Number(e.target.value))}
        />
        <span>{Math.round(settings.volume * 100)}%</span>
      </label>
      <label>
        <input
          type="checkbox"
          checked={settings.audioCache?.enabled ?? true}
          onChange={(e) =>
            store.setSettings({
              audioCache: { enabled: e.target.checked, maxBytes: settings.audioCache?.maxBytes ?? 20 * 1024 ** 3 },
            })
          }
        />
        启用音频缓存
      </label>
      <label>
        缓存容量上限（GB）
        <input
          aria-label="缓存容量上限（GB）"
          type="number"
          min="1"
          max="1024"
          step="1"
          value={(settings.audioCache?.maxBytes ?? 20 * 1024 ** 3) / 1024 ** 3}
          onChange={(e) => {
            const gb = Number(e.target.value);
            if (Number.isInteger(gb) && gb >= 1 && gb <= 1024)
              store.setSettings({
                audioCache: { enabled: settings.audioCache?.enabled ?? true, maxBytes: gb * 1024 ** 3 },
              });
          }}
        />
      </label>
      <div aria-live="polite">
        {state.cacheStats?.available ? (
          <>
            <p className="muted">
              已使用 {(state.cacheStats.usedBytes / 1024 ** 3).toFixed(2)} GB /{" "}
              {(state.cacheStats.maxBytes / 1024 ** 3).toFixed(0)} GB · {state.cacheStats.cachedTracks} 首 · 正在缓存{" "}
              {state.cacheStats.activeDownloads} 首
            </p>
            <progress
              aria-label="音频缓存使用量"
              value={state.cacheStats.usedBytes}
              max={Math.max(1, state.cacheStats.maxBytes)}
            />
          </>
        ) : (
          <p className="muted">音频缓存暂不可用。</p>
        )}
        <button type="button" disabled={!state.cacheStats?.available} onClick={() => void store.clearAudioCache()}>
          清理音频缓存
        </button>
        <p className="muted">清理或关闭缓存不会打断当前播放；正在播放的文件将在播放结束后清理。</p>
        {state.cacheMessage && <p role="status">{state.cacheMessage}</p>}
      </div>
      <h2>封面与歌词缓存</h2>
      <label>
        <input
          type="checkbox"
          checked={settings.resourceCache?.enabled ?? true}
          onChange={(e) =>
            store.setSettings({
              resourceCache: { enabled: e.target.checked, maxBytes: settings.resourceCache?.maxBytes ?? 1024 ** 3 },
            })
          }
        />
        启用封面与歌词缓存
      </label>
      <label>
        封面与歌词容量上限（GB）
        <input
          aria-label="封面与歌词容量上限（GB）"
          type="number"
          min="0.0625"
          max="1024"
          step="0.0625"
          value={(settings.resourceCache?.maxBytes ?? 1024 ** 3) / 1024 ** 3}
          onChange={(e) => {
            const gb = Number(e.target.value);
            if (Number.isFinite(gb) && gb >= 0.0625 && gb <= 1024)
              store.setSettings({
                resourceCache: {
                  enabled: settings.resourceCache?.enabled ?? true,
                  maxBytes: Math.round(gb * 1024 ** 3),
                },
              });
          }}
        />
      </label>
      <div aria-live="polite">
        {state.resourceCacheStats?.available ? (
          <>
            <p className="muted">
              已使用 {(state.resourceCacheStats.usedBytes / 1024 ** 3).toFixed(2)} GB ·{" "}
              {state.resourceCacheStats.cachedCovers} 张封面 · {state.resourceCacheStats.cachedLyrics} 首歌词
            </p>
            <progress
              aria-label="封面与歌词缓存使用量"
              value={state.resourceCacheStats.usedBytes}
              max={Math.max(1, state.resourceCacheStats.maxBytes)}
            />
          </>
        ) : (
          <p className="muted">封面与歌词缓存暂不可用。</p>
        )}
        <button
          type="button"
          disabled={!state.resourceCacheStats?.available}
          onClick={() => void store.clearResourceCache()}
        >
          清理封面与歌词缓存
        </button>
        <p className="muted">与音频缓存分开管理；清理不会删除收藏、歌单或音频缓存。</p>
        {state.resourceCacheMessage && <p role="status">{state.resourceCacheMessage}</p>}
      </div>
      <label>
        <input
          type="checkbox"
          checked={settings.closeToTray ?? false}
          onChange={(e) => store.setSettings({ closeToTray: e.target.checked })}
        />
        关闭窗口时隐藏到托盘
      </label>
      <button type="button" onClick={() => void store.minimizeToTray()}>
        隐藏到托盘
      </button>
      {bridge && <DesktopSettingsPanel bridge={bridge} />}
      <p className="muted font-notice">界面字体 MiSans，版权归小米及相关权利人所有。</p>
      {state.capabilities?.gd && (
        <label>
          <input
            type="checkbox"
            checked={settings.gdEnabled}
            onChange={(e) => store.setSettings({ gdEnabled: e.target.checked })}
          />
          启用 GD 网易云备用音源
        </label>
      )}
    </section>
  );
}

import { it, expect, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import type { DesktopBridge, ResolvedStream, TrackSnapshot } from "./contracts";
import type { AudioExecutor } from "./AudioEngine";
import { PlayerStore } from "./PlayerStore";
import { PlayerProvider } from "./PlayerContext";
import { LibraryPanel, SettingsPanel } from "./LibraryPanels";
const track: TrackSnapshot = {
  neteaseSongId: "123456789012345678",
  name: "离线收藏歌曲",
  artists: [{ name: "歌手" }],
  album: { name: "专辑" },
  durationMs: 61000,
};
function setup() {
  const bridge: DesktopBridge = {
    ListLocalPlaylists: vi.fn(async () => []),
    GetLocalPlaylist: vi.fn(async () => {
      throw new Error("PLAYLIST_NOT_FOUND");
    }),
    CreateLocalPlaylist: vi.fn(async () => {
      throw new Error("not configured");
    }),
    UpdateLocalPlaylist: vi.fn(async () => {
      throw new Error("not configured");
    }),
    DeleteLocalPlaylist: vi.fn(async () => {}),
    GetPlaybackQualityPolicy: async () => ({
      premiumAllowed: false,
      selectableQualities: ["exhigh"],
      maxBitrate: 320000,
    }),
    GetSettings: async () => ({
      preferredQuality: "exhigh",
      regionMode: "auto",
      gdEnabled: false,
      volume: 0.4,
    }),
    SetSettings: async (s) => s,
    GetCapabilities: async () => ({
      regionCompatibility: true,
      gd: false,
      account: false,
    }),
    GetRegionStatus: async () => ({
      classification: "unknown",
      observedAt: 0,
      networkGeneration: 1,
      detecting: false,
    }),
    RefreshRegion: vi.fn<DesktopBridge["RefreshRegion"]>(async () => ({
      classification: "unknown",
      observedAt: 0,
      networkGeneration: 1,
      detecting: false,
    })),
    GetLocalFavorites: async () => [track],
    SetLocalFavorite: vi.fn(async () => {}),
    ListLocalHistory: async () => [{ historyEntryId: "history", track, playedAt: 1000 }],
    AddLocalHistory: async () => {},
    ClearLocalHistory: vi.fn(async () => {}),
    LoadQueue: async () => null,
    SaveQueue: async () => {},
    SearchNetease: vi.fn(async () => {
      throw new Error("offline");
    }),
    GetLyrics: async () => {
      throw new Error("offline");
    },
    ResolveStream: async () => {
      throw new Error("offline");
    },
  };
  const engine: AudioExecutor = {
    on: () => () => {},
    setSource: async () => {},
    pause: () => {},
    seek: () => {},
    setVolume: () => {},
    currentTime: 0,
    duration: 0,
  };
  const store = new PlayerStore(bridge, engine);
  return { store, bridge };
}
it("renders saved favorites and history offline and reflects successful local removal/clear", async () => {
  const { store, bridge } = setup();
  await store.hydrate();
  const render = (kind: "favorites" | "history") =>
    renderToStaticMarkup(
      <PlayerProvider store={store}>
        <LibraryPanel kind={kind} />
      </PlayerProvider>,
    );
  expect(render("favorites")).toContain("离线收藏歌曲");
  expect(render("favorites")).toContain("取消收藏：离线收藏歌曲");
  expect(render("history")).toContain("离线收藏歌曲");
  expect(render("favorites")).toContain("筛选本地音乐");
  store.toggleFavorite(track);
  store.clearHistory();
  await store.flush();
  expect(render("favorites")).not.toContain("离线收藏歌曲");
  expect(render("history")).not.toContain("离线收藏歌曲");
  expect(bridge.SearchNetease).not.toHaveBeenCalled();
  expect(bridge.RefreshRegion).not.toHaveBeenCalled();
});
it("shows saved settings and honest unknown region while hiding unverified GD and account controls", async () => {
  const { store } = setup();
  await store.hydrate();
  const html = renderToStaticMarkup(
    <PlayerProvider store={store}>
      <SettingsPanel />
    </PlayerProvider>,
  );
  expect(html).not.toContain('<option value="standard"');
  expect(html).not.toContain('<option value="higher"');
  expect(html).not.toContain('<option value="lossless"');
  expect(html).toContain("192/128 仅自动降级");
  expect(html).toContain('<option value="exhigh" selected="">320 kbps（极高）</option>');
  expect(html).toContain("自动（推荐）");
  expect(html).toContain("尚未确定");
  expect(html).toContain("40%");
  expect(html).toContain("手动重新检测");
  expect(html).not.toContain("GD");
  expect(html).toContain("更高音质需登录并确认会员");
});

it("shows a library failure and retry without an active loading message", async () => {
  const { store, bridge } = setup();
  bridge.GetLocalFavorites = async () => {
    throw new Error("LOCAL_STORAGE_UNAVAILABLE");
  };
  await store.hydrate();
  for (const kind of ["favorites", "history"] as const) {
    const html = renderToStaticMarkup(
      <PlayerProvider store={store}>
        <LibraryPanel kind={kind} />
      </PlayerProvider>,
    );
    expect(html).toContain("本地音乐资料读取失败");
    expect(html).toContain("重试");
    expect(html).not.toContain("正在读取本地音乐资料…");
  }
});

it("keeps original OTONEI shell and player controls without an always-visible queue inspector", async () => {
  const { App } = await import("./App");
  const { store, bridge } = setup();
  await store.hydrate();
  const html = renderToStaticMarkup(
    <PlayerProvider store={store}>
      <App bridge={bridge} />
    </PlayerProvider>,
  );
  for (const token of [
    "navigation-sidebar",
    "app-header-desktop",
    "header-search-field-wrapper",
    "audio-player collapsed",
    "player-left-section",
    "player-center-section",
    "player-right-section",
    "搜索歌曲或歌手",
    "播放进度",
    "音量",
  ])
    expect(html).toContain(token);
  expect(html).not.toContain("queue-drawer");
  expect(html).not.toContain("<audio");
  expect(html).not.toContain("登录");
  expect(html).not.toContain("player-time");
  expect(html).toContain("progress-time-bubble");
  expect(html).toContain('aria-valuetext="0:00 / 0:00"');
});

it("prefers the visible query over the last submitted one when switching search kinds", async () => {
  const { searchTermFor } = await import("./App");
  expect(searchTermFor("新关键词", "旧关键词")).toBe("新关键词");
  expect(searchTermFor("   ", "旧关键词")).toBe("旧关键词");
  expect(searchTermFor("   ", "")).toBe("");
});

it("offers all four search kinds and an in-app entity detail panel", async () => {
  const { App } = await import("./App");
  const { EntityPanel } = await import("./EntityPanel");
  const { store, bridge } = setup();
  await store.hydrate();
  const html = renderToStaticMarkup(
    <PlayerProvider store={store}>
      <App bridge={bridge} />
    </PlayerProvider>,
  );
  for (const label of ["歌曲", "专辑", "歌手", "歌单"]) expect(html).toContain(`>${label}</button>`);
  const panel = renderToStaticMarkup(
    <PlayerProvider store={store}>
      <EntityPanel bridge={bridge} request={{ kind: "album", id: "123", name: "专辑详情" }} onBack={() => {}} />
    </PlayerProvider>,
  );
  expect(panel).toContain("返回");
  expect(panel).toContain("专辑详情");
});

it("keeps progress time on the bar as a thumb-following bubble, not under the controls", async () => {
  const { App } = await import("./App");
  const { store, bridge } = setup();
  bridge.LoadQueue = async () => ({
    entries: [{ queueEntryId: "entry", track }],
    currentEntryId: "entry",
    currentIndex: 0,
    playMode: "repeat-all",
    positionMs: 30000,
    revision: 1,
  });
  await store.hydrate();
  const html = renderToStaticMarkup(
    <PlayerProvider store={store}>
      <App bridge={bridge} />
    </PlayerProvider>,
  );
  expect(html).toContain('aria-label="播放进度"');
  expect(html).toContain('aria-valuetext="0:30 / 1:01"');
  expect(html).toContain("--progress-time-left:");
  expect(html).toContain('class="progress-time-bubble"');
  expect(html).toContain("0:30 / 1:01</div>");
  expect(html).not.toContain("player-time");
});

it("parses repeated LRC timestamps, fractions and ordering without treating plain lyrics as timed", async () => {
  const { parseLyrics } = await import("./OtoneiPresentation");
  expect(parseLyrics("[00:10.50][00:20.5]副歌\n[ar:歌手]\n[00:02.005]开场")).toEqual([
    { time: 2005, text: "开场" },
    { time: 10500, text: "副歌" },
    { time: 20500, text: "副歌" },
  ]);
  expect(parseLyrics("没有时间标签的歌词")).toEqual([]);
});

it("keeps favorite songs as the default tab and renders playlist hydration protection", async () => {
  const { PlaylistPanel } = await import("./PlaylistPanels");
  const { store } = setup();
  const render = (element: React.ReactNode) =>
    renderToStaticMarkup(<PlayerProvider store={store}>{element}</PlayerProvider>);
  expect(render(<LibraryPanel kind="favorites" />)).toContain("收藏歌曲");
  expect(render(<LibraryPanel kind="favorites" />)).toContain("本地歌单");
  expect(render(<PlaylistPanel />)).toContain("正在读取本地歌单");
  await store.hydrate();
  expect(render(<LibraryPanel kind="favorites" />)).toContain("离线收藏歌曲");
  expect(render(<LibraryPanel kind="favorites" />)).toContain("加入歌单：离线收藏歌曲");
  expect(render(<PlaylistPanel />)).toContain("创建歌单");
  expect(render(<PlaylistPanel />)).toContain("还没有本地歌单");
});

it.each<{ metadata: Partial<ResolvedStream>; expected: string }>([
  {
    metadata: { actualQuality: "standard", actualBitrate: 128000, actualFormat: "mp3" },
    expected: "网易云直连 · 128 kbps · mp3",
  },
  {
    metadata: {
      provider: "gd-netease",
      actualQuality: "standard",
      actualBitrate: 127900,
      actualFormat: "mp3",
      status: "trial",
    },
    expected: "GD 网易云备用音源 · 128 kbps · mp3 · 试听片段",
  },
  { metadata: { status: "unknown" }, expected: "网易云直连 · 实际音质未知 · 完整性未确认" },
  { metadata: { actualQuality: "higher", actualBitrate: 0 }, expected: "网易云直连 · 较高" },
  { metadata: { actualQuality: "lossless", actualBitrate: NaN }, expected: "网易云直连 · 无损" },
  { metadata: { actualBitrate: Infinity }, expected: "网易云直连 · 实际音质未知" },
  { metadata: { actualBitrate: -128000 }, expected: "网易云直连 · 实际音质未知" },
  { metadata: { actualQuality: "unrecognized" }, expected: "网易云直连 · 实际音质未知" },
])(
  "renders returned stream metadata without substituting the 320 kbps preference: $expected",
  async ({ metadata, expected }) => {
    const { OtoneiPlayer } = await import("./OtoneiPresentation");
    const { store, bridge } = setup();
    bridge.GetSettings = async () => ({ preferredQuality: "exhigh", regionMode: "off", gdEnabled: false, volume: 0.4 });
    bridge.ResolveStream = vi.fn<DesktopBridge["ResolveStream"]>(async () => ({
      songId: track.neteaseSongId,
      provider: "netease-direct",
      url: "https://media.example/audio",
      requestedQuality: "exhigh",
      status: "full",
      ...metadata,
    }));
    await store.hydrate();
    store.add(track);
    await store.play();
    expect(bridge.ResolveStream).toHaveBeenCalledWith(track.neteaseSongId, "exhigh", expect.any(AbortSignal));
    const html = renderToStaticMarkup(
      <PlayerProvider store={store}>
        <OtoneiPlayer current={track} />
      </PlayerProvider>,
    );
    expect(html).toContain(`<small class="stream-source">${expected}</small>`);
    expect(html).not.toContain("320 kbps");
    await store.flush();
  },
);

it("renders premium manual tiers only from the trusted server policy without enabling account controls", async () => {
  const { store, bridge } = setup();
  bridge.GetPlaybackQualityPolicy = async () => ({
    premiumAllowed: true,
    selectableQualities: ["exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster"],
    maxBitrate: 0,
  });
  await store.hydrate();
  const html = renderToStaticMarkup(
    <PlayerProvider store={store}>
      <SettingsPanel />
    </PlayerProvider>,
  );
  for (const quality of ["exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster"])
    expect(html).toContain(`<option value="${quality}"`);
  expect(html).not.toContain('<option value="standard"');
  expect(html).not.toContain('<option value="higher"');
  expect(html).not.toContain("更高音质需登录并确认会员");
  expect(store.getSnapshot().capabilities?.account).toBe(false);
});

it("renders accessible cache and desktop controls plus cursor history paging", async () => {
  const { store, bridge } = setup();
  bridge.GetAudioCacheStats = async () => ({
    available: true,
    enabled: true,
    maxBytes: 20 * 1024 ** 3,
    usedBytes: 1024 ** 3,
    cachedTracks: 3,
    activeDownloads: 1,
  });
  bridge.ListLocalHistoryBefore = async () => ({
    entries: [{ historyEntryId: "h", playedAt: 1, track }],
    hasMore: true,
  });
  await store.hydrate();
  await store.refreshCacheStats();
  const render = (element: React.ReactNode) =>
    renderToStaticMarkup(<PlayerProvider store={store}>{element}</PlayerProvider>);
  const settings = render(<SettingsPanel bridge={bridge} />);
  for (const text of [
    "启用音频缓存",
    "缓存容量上限（GB）",
    "音频缓存使用量",
    "清理音频缓存",
    "隐藏到托盘",
    "关闭窗口时隐藏到托盘",
    "1.00 GB / 20 GB",
    "正在缓存 1 首",
    "打开迷你播放器",
    "打开桌面歌词",
    "锁定桌面歌词（鼠标穿透）",
    "歌词字号",
    "歌词不透明度",
    "主窗口隐藏时提示新歌曲",
    "在任务栏显示播放进度",
    "重置桌面窗口位置",
  ])
    expect(settings).toContain(text);
  expect(settings).not.toContain("127.0.0.1");
  expect(render(<LibraryPanel kind="history" />)).toContain("下一页");
  expect(render(<LibraryPanel kind="history" />)).not.toContain("最近 200");
});
it("labels complete cache hits without changing returned source quality or membership", async () => {
  const { OtoneiPlayer } = await import("./OtoneiPresentation");
  const { store, bridge } = setup();
  const token = "b".repeat(32);
  bridge.ResolveStream = async () => ({
    songId: track.neteaseSongId,
    provider: "netease-direct",
    url: `http://127.0.0.1:19394/media/${token}`,
    cacheToken: token,
    cacheHit: true,
    requestedQuality: "exhigh",
    actualBitrate: 128000,
    actualFormat: "mp3",
    status: "full",
  });
  await store.hydrate();
  store.add(track);
  await store.play();
  const html = renderToStaticMarkup(
    <PlayerProvider store={store}>
      <OtoneiPlayer current={track} />
    </PlayerProvider>,
  );
  expect(html).toContain("本地缓存 · 网易云直连 · 128 kbps · mp3");
  expect(store.getSnapshot().qualityPolicy?.premiumAllowed).toBe(false);
});

it("shows separate accessible resource-cache policy, usage, clear result and original audio cache controls", async () => {
  const { store, bridge } = setup();
  bridge.GetResourceCacheStats = async () => ({
    available: true,
    enabled: true,
    maxBytes: 1024 ** 3,
    usedBytes: 10,
    cachedCovers: 2,
    cachedLyrics: 3,
  });
  bridge.ClearResourceCache = vi.fn(async () => ({ removedCovers: 2, removedLyrics: 3 }));
  await store.hydrate();
  await store.refreshResourceCacheStats();
  const render = () =>
    renderToStaticMarkup(
      <PlayerProvider store={store}>
        <SettingsPanel />
      </PlayerProvider>,
    );
  for (const text of [
    "启用封面与歌词缓存",
    "封面与歌词容量上限（GB）",
    "封面与歌词缓存使用量",
    "清理封面与歌词缓存",
    "启用音频缓存",
    "清理音频缓存",
  ])
    expect(render()).toContain(text);
  store.setSettings({ resourceCache: { enabled: false, maxBytes: 2 * 1024 ** 3 } });
  await store.flush();
  expect(store.getSnapshot().settings?.resourceCache).toEqual({ enabled: false, maxBytes: 2 * 1024 ** 3 });
  await store.clearResourceCache();
  expect(render()).toContain("已清理 2 张封面和 3 首歌词");
  expect(bridge.ClearLocalHistory).not.toHaveBeenCalled();
  expect(bridge.SetLocalFavorite).not.toHaveBeenCalled();
});

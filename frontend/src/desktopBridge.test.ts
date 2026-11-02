import { defaultDesktopSettings } from "./desktopWire";
import { it, expect, vi } from "vitest";
vi.mock("@wailsio/runtime", () => ({}));
vi.mock(
  "../bindings/github.com/voici5986/otoneigo/internal/appservice/appservice",
  () => ({
    GetPlaybackQualityPolicy: vi.fn(async () => ({ premiumAllowed: false, selectableQualities: ["exhigh"], maxBitrate: 320000 })),
    GetSettings: vi.fn(async () => ({
      preferredQuality: "exhigh",
      regionMode: "auto",
      gdEnabled: false,
      volume: 1,
    })),
    SetSettings: vi.fn(async (settings) => settings),
    GetCapabilities: vi.fn(async () => ({
      regionCompatibility: true,
      gd: false,
      account: false,
    })),
    GetRegionStatus: vi.fn(async () => ({
      classification: "unknown",
      observedAt: 0,
      networkGeneration: 0,
      detecting: false,
    })),
    RefreshRegion: vi.fn(() => {
      const promise = Promise.resolve({
        classification: "overseas",
        country: "TH",
        observedAt: 1,
        networkGeneration: 1,
        detecting: false,
      });
      return Object.assign(promise, { cancelOn: vi.fn(() => promise) });
    }),
    ListLocalPlaylists: vi.fn(async () => null),
    GetLocalPlaylist: vi.fn(async () => ({ playlistId: "local-1", name: "歌单", tracks: null, revision: 1, createdAt: 1, updatedAt: 1 })),
    CreateLocalPlaylist: vi.fn(async (name) => ({ playlistId: "local-1", name, tracks: null, revision: 1, createdAt: 1, updatedAt: 1 })),
    UpdateLocalPlaylist: vi.fn(async (update) => ({ playlistId: update.playlistId, name: update.name, tracks: update.tracks, revision: update.expectedRevision + 1, createdAt: 1, updatedAt: 2 })),
    DeleteLocalPlaylist: vi.fn(async () => {}),
    SearchLocalFavorites: vi.fn(() => { const promise = Promise.resolve({ tracks: [], hasMore: false, nextAddedOrder: 0, nextSongId: "" }); return Object.assign(promise, { cancelOn: vi.fn(() => promise) }); }),
    SearchLocalHistory: vi.fn(() => { const promise = Promise.resolve({ entries: [], hasMore: false }); return Object.assign(promise, { cancelOn: vi.fn(() => promise) }); }),
    GetLocalFavoriteIDs: vi.fn(async () => ["123"]),
    ListSearchHistory: vi.fn(async () => [{ query: "关键词", searchedAt: 1 }]),
    RecordSearchQuery: vi.fn(async () => {}), DeleteSearchQuery: vi.fn(async () => {}), ClearSearchHistory: vi.fn(async () => {}),
    ResolveCover: vi.fn(() => { const promise = Promise.resolve("/artwork/" + "a".repeat(64)); return Object.assign(promise, { cancelOn: vi.fn(() => promise) }); }),
    GetResourceCacheStats: vi.fn(async () => ({ available: true, enabled: true, maxBytes: 1024 ** 3, usedBytes: 0, cachedCovers: 0, cachedLyrics: 0 })),
    ClearResourceCache: vi.fn(async () => ({ removedCovers: 1, removedLyrics: 2 })),
    GetLocalFavorites: vi.fn(async () => null),
    SetLocalFavorite: vi.fn(async () => {}),
    AddLocalHistory: vi.fn(async () => {}),
    ListLocalHistory: vi.fn(async () => null),
    ClearLocalHistory: vi.fn(async () => {}),
    SearchNetease: vi.fn(() => {
      const promise = Promise.resolve({
        tracks: null,
        total: 0,
        hasMore: false,
      });
      return Object.assign(promise, { cancelOn: vi.fn(() => promise) });
    }),
  }),
);
import * as service from "../bindings/github.com/voici5986/otoneigo/internal/appservice/appservice";
import { desktopBridge } from "./desktopBridge";
it("maps displayed page one to native page zero and forwards cancellation", async () => {
  const controller = new AbortController();
  expect(
    (await desktopBridge.SearchNetease("歌手", 1, controller.signal)).tracks,
  ).toEqual([]);
  expect(service.SearchNetease).toHaveBeenCalledWith("歌手", 0);
  const call = vi.mocked(service.SearchNetease).mock.results[0].value;
  expect(call.cancelOn).toHaveBeenCalledWith(controller.signal);
  await desktopBridge.SearchNetease("歌手", 2);
  expect(service.SearchNetease).toHaveBeenLastCalledWith("歌手", 1);
});
it("normalizes local list slices and sends only metadata to favorites/history", async () => {
  expect(await desktopBridge.GetLocalFavorites()).toEqual([]);
  expect(await desktopBridge.ListLocalHistory(200, 0)).toEqual([]);
  const track = {
    neteaseSongId: "123456789012345678",
    name: "歌曲",
    artists: [{ name: "歌手" }],
    durationMs: 1000,
    url: "https://media.example/private?token=secret",
  };
  await desktopBridge.SetLocalFavorite(track, true);
  await desktopBridge.AddLocalHistory({
    historyEntryId: "entry",
    track,
    playedAt: 123.6,
    listenedMs: 600.3,
  });
  expect(service.SetLocalFavorite).toHaveBeenCalledWith(
    expect.not.objectContaining({ url: expect.anything() }),
    true,
  );
  expect(service.AddLocalHistory).toHaveBeenCalledWith({
    historyEntryId: "entry",
    track: expect.not.objectContaining({ url: expect.anything() }),
    playedAt: 124,
    listenedMs: 600,
  });
  expect(
    JSON.stringify(vi.mocked(service.AddLocalHistory).mock.calls),
  ).not.toContain("secret");
});
it("uses cancellable refresh only when requested and returns validated settings", async () => {
  expect(await desktopBridge.GetSettings()).toEqual({
    desktop: defaultDesktopSettings(),
    audioCache: { enabled: true, maxBytes: 20 * 1024 ** 3 },
    resourceCache: { enabled: true, maxBytes: 1024 ** 3 },
    closeToTray: false,
    preferredQuality: "exhigh",
    regionMode: "auto",
    gdEnabled: false,
    volume: 1,
  });
  const controller = new AbortController();
  expect(
    (await desktopBridge.RefreshRegion(controller.signal)).classification,
  ).toBe("overseas");
  const call = vi.mocked(service.RefreshRegion).mock.results[0].value;
  expect(call.cancelOn).toHaveBeenCalledWith(controller.signal);
});


it("maps local playlist operations and strips arbitrary fields before update", async () => {
  expect(await desktopBridge.ListLocalPlaylists()).toEqual([]);
  expect((await desktopBridge.GetLocalPlaylist("local-1")).tracks).toEqual([]);
  expect((await desktopBridge.CreateLocalPlaylist("歌单")).revision).toBe(1);
  const track = { neteaseSongId: "123456789012345678", name: "歌曲", artists: [], durationMs: 1000,
    url: "https://media.example/?token=secret", cookie: "secret" };
  const update = { playlistId: "local-1", name: "歌单", expectedRevision: 1, tracks: [track], token: "secret" };
  expect((await desktopBridge.UpdateLocalPlaylist(update)).revision).toBe(2);
  expect(JSON.stringify(vi.mocked(service.UpdateLocalPlaylist).mock.calls)).not.toMatch(/secret|cookie|token/);
  await desktopBridge.DeleteLocalPlaylist("local-1", 2);
  expect(service.DeleteLocalPlaylist).toHaveBeenCalledWith("local-1", 2);
});

it("reads the native quality policy through strict wire validation", async () => {
  expect(await desktopBridge.GetPlaybackQualityPolicy()).toEqual({ premiumAllowed: false, selectableQualities: ["exhigh"], maxBitrate: 320000 });
  expect(service.GetPlaybackQualityPolicy).toHaveBeenCalled();
});

it("sends fully normalized cache and tray settings even for a legacy caller", async () => {
  await desktopBridge.SetSettings({ preferredQuality: "exhigh", regionMode: "auto", gdEnabled: false, volume: 1 });
  expect(service.SetSettings).toHaveBeenLastCalledWith({ desktop: defaultDesktopSettings(), preferredQuality: "exhigh", regionMode: "auto", gdEnabled: false, volume: 1, closeToTray: false, resourceCache: { enabled: true, maxBytes: 1024 ** 3 }, audioCache: { enabled: true, maxBytes: 20 * 1024 ** 3 } });
});


it("uses paged SQLite/search-history/resource APIs and forwards independent cancellation", async () => {
  const signal = new AbortController().signal;
  expect(await desktopBridge.SearchLocalFavorites!("中文%_", 200, 42, "123", signal)).toEqual({ tracks: [], hasMore: false, nextAddedOrder: 0, nextSongId: "" });
  expect(service.SearchLocalFavorites).toHaveBeenCalledWith("中文%_", 200, 42, "123");
  expect(vi.mocked(service.SearchLocalFavorites).mock.results[0].value.cancelOn).toHaveBeenCalledWith(signal);
  expect(await desktopBridge.SearchLocalHistory!("日本語", 200, 12, "entry", signal)).toEqual({ entries: [], hasMore: false });
  expect(service.SearchLocalHistory).toHaveBeenCalledWith("日本語", 200, 12, "entry");
  expect(await desktopBridge.GetLocalFavoriteIDs!()).toEqual(["123"]);
  expect(await desktopBridge.ListSearchHistory!(20)).toEqual([{ query: "关键词", searchedAt: 1 }]);
  await desktopBridge.RecordSearchQuery!("关键词"); await desktopBridge.DeleteSearchQuery!("关键词"); await desktopBridge.ClearSearchHistory!();
  expect(service.RecordSearchQuery).toHaveBeenCalledWith("关键词"); expect(service.DeleteSearchQuery).toHaveBeenCalledWith("关键词"); expect(service.ClearSearchHistory).toHaveBeenCalledTimes(1);
  expect(await desktopBridge.ResolveCover!("https://p1.music.126.net/cover.jpg", signal)).toMatch(/^\/artwork\/[a-f0-9]{64}$/);
  expect(await desktopBridge.GetResourceCacheStats!()).toMatchObject({ maxBytes: 1024 ** 3, cachedCovers: 0, cachedLyrics: 0 });
  expect(await desktopBridge.ClearResourceCache!()).toEqual({ removedCovers: 1, removedLyrics: 2 });
});

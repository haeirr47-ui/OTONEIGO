import { defaultDesktopSettings } from "./desktopWire";
import { it, expect } from "vitest";
import { fromQueue, fromTrack, fromStream, fromPlaybackQualityPolicy } from "./wire";
it("normalizes nullable Go slices without weakening ID types", () => {
  expect(
    fromTrack({
      neteaseSongId: "123456789012345678",
      name: "歌曲",
      artists: null,
      album: null,
      durationMs: 1,
    }).artists,
  ).toEqual([]);
  expect(
    fromQueue({
      entries: null,
      currentEntryId: null,
      currentIndex: -1,
      playMode: "repeat-all",
      positionMs: 0,
      revision: 0,
    })?.entries,
  ).toEqual([]);
  expect(fromQueue(null)).toBeNull();
});
it("rejects malformed restored snapshots and unsupported provider/status", () => {
  expect(() =>
    fromQueue({
      entries: null,
      currentEntryId: null,
      currentIndex: 0,
      playMode: "repeat-all",
      positionMs: 0,
      revision: 1,
    }),
  ).toThrow();
  expect(() =>
    fromQueue({
      entries: null,
      currentEntryId: null,
      currentIndex: -1,
      playMode: "invalid",
      positionMs: 0,
      revision: 1,
    }),
  ).toThrow();
  expect(() =>
    fromStream({
      songId: "1",
      url: "",
      provider: "other",
      requestedQuality: "standard",
      status: "full",
    }),
  ).toThrow();
});

import { fromSettings, fromRegion, fromCapabilities, fromHistory, fromFavorites } from "./wire";
it("validates unknown settings, region and capability payloads strictly", () => {
  expect(
    fromSettings({
      preferredQuality: "hires",
      regionMode: "auto",
      gdEnabled: false,
      volume: 0.4,
    }),
  ).toEqual({
    desktop: defaultDesktopSettings(),
    audioCache: { enabled: true, maxBytes: 20 * 1024 ** 3 },
    resourceCache: { enabled: true, maxBytes: 1024 ** 3 },
    closeToTray: false,
    preferredQuality: "hires",
    regionMode: "auto",
    gdEnabled: false,
    volume: 0.4,
  });
  for (const change of [
    { preferredQuality: "ultra" },
    { regionMode: "detect" },
    { volume: 2 },
    { volume: NaN },
    { gdEnabled: "false" },
  ]) {
    expect(() =>
      fromSettings({
        preferredQuality: "standard",
        regionMode: "auto",
        volume: 1,
        gdEnabled: false,
        ...change,
      }),
    ).toThrow();
  }
  expect(() =>
    fromRegion({
      classification: "unknown",
      observedAt: 0,
      networkGeneration: 0,
      detecting: false,
      country: "192.0.2.1",
    }),
  ).toThrow();
  expect(() =>
    fromRegion({
      classification: "EU",
      observedAt: 0,
      networkGeneration: 0,
      detecting: false,
    }),
  ).toThrow();
  expect(() => fromCapabilities({ regionCompatibility: true, gd: "true", account: false })).toThrow();
  expect(() => fromFavorites([{ neteaseSongId: 123, name: "track", durationMs: 1 }])).toThrow();
  expect(() =>
    fromHistory({
      historyEntryId: "1",
      playedAt: -1,
      track: { neteaseSongId: "1", name: "track", durationMs: 1 },
    }),
  ).toThrow();
  expect(() =>
    fromStream({
      songId: "1",
      url: "https://media.example/audio",
      provider: "netease-direct",
      requestedQuality: "standard",
      status: "full",
      accessStrategy: "other",
    }),
  ).toThrow();
});

it("accepts the distinct optional GD netease provider without mislabeling its source", () => {
  expect(
    fromStream({
      songId: "1",
      url: "https://media.example/audio",
      provider: "gd-netease",
      requestedQuality: "lossless",
      actualQuality: "standard",
      status: "unknown",
      accessStrategy: "normal",
      actualBitrate: 128000,
    }),
  ).toMatchObject({
    provider: "gd-netease",
    actualQuality: "standard",
    actualBitrate: 128000,
    status: "unknown",
  });
});

it("validates shared resolution deadline as an optional safe epoch integer", () => {
  const stream = {
    songId: "1",
    url: "https://media.example/audio",
    provider: "netease-direct",
    requestedQuality: "standard",
    status: "full",
  };
  expect(fromStream({ ...stream, resolutionDeadlineAt: 1234567890000 }).resolutionDeadlineAt).toBe(1234567890000);
  for (const deadline of [-1, NaN, Infinity, 123.5, Number.MAX_SAFE_INTEGER + 1, "123"]) {
    expect(() => fromStream({ ...stream, resolutionDeadlineAt: deadline })).toThrow();
  }
});

import { fromPlaylist, fromPlaylistList } from "./wire";
it("validates playlist IDs, revisions, names, uniqueness and bounded metadata while picking track fields", () => {
  const track = { neteaseSongId: "123456789012345678", name: "歌曲", artists: [], durationMs: 1000, url: "secret" };
  const playlist = {
    playlistId: "local-1",
    name: "我的歌单",
    revision: 1,
    createdAt: 1,
    updatedAt: 2,
    tracks: [track],
  };
  expect(fromPlaylist(playlist).tracks[0].neteaseSongId).toBe(track.neteaseSongId);
  expect(JSON.stringify(fromPlaylist(playlist))).not.toContain("secret");
  expect(fromPlaylistList(null)).toEqual([]);
  const summary = { ...playlist, trackCount: 1 };
  for (const change of [
    { playlistId: 1 },
    { playlistId: "../bad" },
    { name: " " },
    { name: "bad\nname" },
    { revision: 0 },
    { revision: Number.MAX_SAFE_INTEGER + 1 },
    { createdAt: 0 },
    { updatedAt: 0 },
    { trackCount: 1001 },
  ])
    expect(() => fromPlaylistList([{ ...summary, ...change }])).toThrow();
  for (const change of [
    { neteaseSongId: "0" },
    { neteaseSongId: "1".repeat(31) },
    { durationMs: 1.5 },
    { durationMs: 86400001 },
    { name: " " },
    { coverRef: "https://media.example/?token=secret" },
    { artists: [{ id: "bad", name: "歌手" }] },
  ])
    expect(() => fromPlaylist({ ...playlist, tracks: [{ ...track, ...change }] })).toThrow();
  expect(() => fromPlaylist({ ...playlist, tracks: [track, track] })).toThrow();
  expect(() => fromPlaylist({ ...playlist, tracks: [{ ...track, neteaseSongId: 123 }] })).toThrow();
  expect(() =>
    fromPlaylist({
      ...playlist,
      tracks: Array.from({ length: 1001 }, (_, i) => ({ ...track, neteaseSongId: String(i + 1) })),
    }),
  ).toThrow();
  expect(() => fromPlaylist({ ...playlist, tracks: [{ ...track, name: "a".repeat(2 * 1024 * 1024) }] })).toThrow();
  expect(() => fromPlaylistList([summary, summary])).toThrow();
});

it("validates quality policy membership, selectable tiers and bitrate caps", () => {
  const anonymous = { premiumAllowed: false, selectableQualities: ["exhigh"], maxBitrate: 320000 };
  const member = {
    premiumAllowed: true,
    selectableQualities: ["exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster"],
    maxBitrate: 0,
  };
  expect(fromPlaybackQualityPolicy(anonymous)).toEqual(anonymous);
  expect(fromPlaybackQualityPolicy(member)).toEqual(member);
  for (const policy of [
    null,
    {},
    { ...anonymous, premiumAllowed: "true" },
    { ...anonymous, maxBitrate: 0 },
    { ...anonymous, selectableQualities: ["standard"] },
    { ...anonymous, selectableQualities: ["exhigh", "hires"] },
    { ...member, maxBitrate: 320000 },
    { ...member, selectableQualities: ["exhigh", "lossless", "hires", "jyeffect", "sky", "sky"] },
    { ...member, selectableQualities: ["exhigh", "lossless", "hires", "jyeffect", "sky", "unknown"] },
  ])
    expect(() => fromPlaybackQualityPolicy(policy)).toThrow();
});
it.each(["standard", "higher"])("normalizes legacy saved %s before exposing settings", (quality) => {
  expect(
    fromSettings({ preferredQuality: quality, regionMode: "off", gdEnabled: false, volume: 1 }).preferredQuality,
  ).toBe("exhigh");
});

it("accepts only cache URLs matching a bounded private lease and rejects private sources without leases", () => {
  const token = "a".repeat(32);
  const stream = {
    songId: "1",
    provider: "netease-direct",
    url: `http://127.0.0.1:18392/media/${token}`,
    requestedQuality: "exhigh",
    status: "full",
    cacheToken: token,
    cacheHit: true,
    contentLengthBytes: 42,
    accessStrategy: "cache",
  };
  expect(fromStream(stream)).toMatchObject({ cacheToken: token, cacheHit: true, contentLengthBytes: 42 });
  for (const change of [
    { cacheToken: "bad" },
    { cacheToken: undefined },
    { url: `${stream.url}?x=1` },
    { url: `${stream.url}#x` },
    { url: `${stream.url}/x` },
    { url: stream.url.replace("18392", "99999") },
    { url: stream.url.replace("127.0.0.1", "localhost") },
    { url: "file:///C:/private.mp3" },
  ])
    expect(() => fromStream({ ...stream, ...change })).toThrow();
  for (const url of [
    stream.url,
    "http://localhost/private",
    "http://localhost./private",
    "http://10.0.0.2/private",
    "http://[::1]/private",
    "cache:private",
    "file:///C:/private.mp3",
  ])
    expect(() => fromStream({ ...stream, url, cacheToken: undefined, cacheHit: false })).toThrow();
  expect(
    fromStream({
      ...stream,
      url: "https://media.example/audio?secret=signed",
      cacheToken: undefined,
      cacheHit: undefined,
    }).url,
  ).toContain("https:");
});
it("normalizes legacy cache defaults and rejects invalid settings limits", () => {
  const base = { preferredQuality: "exhigh", regionMode: "auto", gdEnabled: false, volume: 1 };
  expect(fromSettings({ ...base, audioCache: { enabled: false, maxBytes: 0 } }).audioCache).toEqual({
    enabled: false,
    maxBytes: 20 * 1024 ** 3,
  });
  for (const maxBytes of [1, 1024 ** 4 + 1, NaN, -1])
    expect(() => fromSettings({ ...base, audioCache: { enabled: true, maxBytes } })).toThrow();
});

it("strictly decodes resource sources and paged local search responses", async () => {
  const {
    fromFavoritePage,
    fromFavoriteIDs,
    fromSearchHistory,
    fromResolvedCover,
    fromResourceCacheStats,
    normalizeResourceCache,
  } = await import("./wire");
  expect(normalizeResourceCache(undefined)).toEqual({ enabled: true, maxBytes: 1024 ** 3 });
  expect(() => normalizeResourceCache({ enabled: true, maxBytes: 63 * 1024 ** 2 })).toThrow();
  expect(fromFavoriteIDs(["123456789012345678"])).toEqual(["123456789012345678"]);
  expect(() => fromFavoriteIDs([123])).toThrow();
  expect(() => fromFavoritePage({ tracks: [], hasMore: true, nextAddedOrder: 1, nextSongId: "1" })).toThrow();
  expect(() => fromSearchHistory([{ query: " ", searchedAt: 1 }])).toThrow();
  expect(() =>
    fromSearchHistory([
      { query: "词", searchedAt: 1 },
      { query: "词", searchedAt: 1 },
    ]),
  ).toThrow();
  expect(fromResolvedCover(`/artwork/${"a".repeat(64)}`)).toContain("/artwork/");
  expect(fromResolvedCover("https://img.y.music.126.net/test.webp")).toContain("test.webp");
  for (const value of [
    "http://p1.music.126.net/a.jpg",
    "https://p1.music.126.net/a.jpg?token=secret",
    "/artwork/../secret",
    "https://evil.example/a.jpg",
    "https://p1.music.126.net/a.svg",
    "//evil.example/a.jpg",
  ])
    expect(() => fromResolvedCover(value)).toThrow();
  expect(() =>
    fromResourceCacheStats({
      available: true,
      enabled: true,
      maxBytes: 1,
      usedBytes: -1,
      cachedCovers: 0,
      cachedLyrics: 0,
    }),
  ).toThrow();
});

it("matches the native 512-byte query boundary without imposing a 200-character limit", async () => {
  const { fromSearchHistory } = await import("./wire");
  const { normalizeLocalQuery } = await import("./contracts");
  for (const query of ["a".repeat(201), "a".repeat(512), `${"中".repeat(170)}ab`, "😀".repeat(128)]) {
    expect(fromSearchHistory([{ query, searchedAt: 1 }])[0].query).toBe(query);
    expect(normalizeLocalQuery(query)).toBe(query);
  }
  for (const query of ["a".repeat(513), "中".repeat(171), `${"😀".repeat(128)}a`, "\ud800"]) {
    expect(() => fromSearchHistory([{ query, searchedAt: 1 }])).toThrow();
    expect(() => normalizeLocalQuery(query)).toThrow();
  }
  expect(normalizeLocalQuery("\u0085中文\u3000")).toBe("中文");
  expect(normalizeLocalQuery("\ufeff词")).toBe("\ufeff词");
});

it("accepts numeric-prefixed public CDN names without admitting private IP literals", () => {
  const base = { songId: "1", provider: "netease-direct", requestedQuality: "exhigh", status: "full" };
  for (const host of [
    "127.music.126.net",
    "10.music.126.net",
    "192.168.music.126.net",
    "172.16.music.126.net",
    "0.music.126.net",
  ])
    expect(() => fromStream({ ...base, url: `https://${host}/audio.mp3` })).not.toThrow();
  for (const host of ["127.1", "0x7f000001", "10.0.0.1", "192.168.0.1", "172.16.1.1", "localhost.", "[::1]"])
    expect(() => fromStream({ ...base, url: `https://${host}/audio.mp3` })).toThrow();
});

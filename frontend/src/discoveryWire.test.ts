import { expect, it } from "vitest";
import { fromEntityDetail, fromEntitySearch, fromPublicPlaylist, fromToplists } from "./discoveryWire";

const entityItem = {
  id: "12345678901234567890",
  name: "专辑",
  subtitle: "歌手",
  coverRef: "",
  trackCount: 10,
};
const entitySearch = { kind: "album", items: [entityItem], total: 1, hasMore: false, partial: false };

const summary = { playlistId: "12345678901234567890", name: "榜单", description: "", coverRef: "" };
const track = { neteaseSongId: "22345678901234567890", name: "歌曲", artists: [], durationMs: 1000 };
const entityDetail = {
  kind: "album",
  id: "12345678901234567890",
  name: "专辑",
  subtitle: "歌手",
  description: "简介",
  coverRef: "",
  tracks: [track],
  total: 1,
  partial: false,
};
const page = {
  ...summary,
  tracks: [track],
  total: 100,
  availableCount: 100,
  offset: 0,
  nextOffset: 30,
  hasMore: true,
  partial: true,
};

it("preserves large string IDs and explicitly partial pages while stripping unrelated response fields", () => {
  const decoded = fromPublicPlaylist({
    ...page,
    cookie: "private",
    tracks: [{ ...track, url: "https://media.example/private" }],
  });
  expect(decoded.playlistId).toBe(summary.playlistId);
  expect(decoded.tracks[0].neteaseSongId).toBe(track.neteaseSongId);
  expect(decoded.partial).toBe(true);
  expect(decoded).not.toHaveProperty("cookie");
  expect(decoded.tracks[0]).not.toHaveProperty("url");
  expect(fromToplists({ items: [{ ...summary, trackCount: 100 }], partial: false }).items[0].trackCount).toBe(100);
});

it("rejects numeric identities, over-limit lists, duplicate rows and unsafe cover references", () => {
  for (const value of [
    { ...page, playlistId: 123 },
    { ...page, tracks: [{ ...track, neteaseSongId: 123 }] },
    { ...page, tracks: [track, track] },
    { ...page, tracks: Array(31).fill(track) },
    { ...page, coverRef: "https://example.com/cover.jpg" },
    { ...page, name: "x".repeat(513) },
    { ...page, total: Number.MAX_SAFE_INTEGER + 1 },
  ])
    expect(() => fromPublicPlaylist(value)).toThrow();
  expect(() => fromToplists({ items: Array(101).fill({ ...summary, trackCount: 1 }), partial: false })).toThrow();
  expect(() =>
    fromToplists({
      items: [
        { ...summary, trackCount: 1 },
        { ...summary, trackCount: 2 },
      ],
      partial: false,
    }),
  ).toThrow();
});

it("rejects inconsistent pagination instead of admitting loops or unbounded page requests", () => {
  for (const changes of [
    { offset: 1 },
    { offset: 1020 },
    { nextOffset: 0 },
    { nextOffset: 60 },
    { availableCount: 101 },
    { total: 10 },
    { hasMore: "true" },
    { tracks: [{ ...track, durationMs: Infinity }] },
  ])
    expect(() => fromPublicPlaylist({ ...page, ...changes })).toThrow();
  expect(fromPublicPlaylist({ ...page, total: 1, availableCount: 1, nextOffset: 1, hasMore: false }).hasMore).toBe(
    false,
  );
});

it("keeps entity identities as strings and strips fields outside the search/detail contract", () => {
  const decoded = fromEntitySearch(
    { ...entitySearch, cookie: "private", items: [{ ...entityItem, url: "https://media.example/private" }] },
    "album",
  );
  expect(decoded.items[0].id).toBe(entityItem.id);
  expect(decoded).not.toHaveProperty("cookie");
  expect(decoded.items[0]).not.toHaveProperty("url");
  const detail = fromEntityDetail({ ...entityDetail, token: "private" }, "album", entityDetail.id);
  expect(detail.tracks[0].neteaseSongId).toBe(track.neteaseSongId);
  expect(detail).not.toHaveProperty("token");
});

it("rejects mismatched kinds, duplicate rows, over-limit lists and unsafe entity covers", () => {
  expect(() => fromEntitySearch(entitySearch, "artist")).toThrow();
  for (const value of [
    { ...entitySearch, items: [{ ...entityItem, id: 123 }] },
    { ...entitySearch, items: [entityItem, entityItem] },
    { ...entitySearch, items: Array(31).fill(entityItem) },
    { ...entitySearch, items: [{ ...entityItem, coverRef: "https://example.com/a.jpg" }] },
    { ...entitySearch, total: Number.MAX_SAFE_INTEGER + 1 },
  ])
    expect(() => fromEntitySearch(value, "album")).toThrow();
  expect(() => fromEntityDetail(entityDetail, "artist", entityDetail.id)).toThrow();
  expect(() => fromEntityDetail(entityDetail, "album", "999")).toThrow();
  for (const changes of [
    { id: 123 },
    { name: " " },
    { description: "x".repeat(4097) },
    { coverRef: "https://example.com/a.jpg" },
    { tracks: [track, track] },
    { tracks: Array(201).fill(track) },
    { total: 0 },
  ])
    expect(() => fromEntityDetail({ ...entityDetail, ...changes }, "album", entityDetail.id)).toThrow();
});

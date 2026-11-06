import { expect, it } from "vitest";
import { fromPublicPlaylist, fromToplists } from "./discoveryWire";

const summary = { playlistId: "12345678901234567890", name: "榜单", description: "", coverRef: "" };
const track = { neteaseSongId: "22345678901234567890", name: "歌曲", artists: [], durationMs: 1000 };
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

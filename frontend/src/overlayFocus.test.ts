import { describe, expect, it, vi } from "vitest";
import { isOverlayTabStop, nextOverlayTabStop } from "./overlayFocus";
import { alignLyrics } from "./OtoneiPresentation";

describe("overlay keyboard boundaries", () => {
  it("wraps both directions and redirects focus arriving from outside the scope", () => {
    const stops = ["close", "progress", "play", "queue"];
    expect(nextOverlayTabStop(stops, "queue", false)).toBe("close");
    expect(nextOverlayTabStop(stops, "close", true)).toBe("queue");
    expect(nextOverlayTabStop(stops, "play", true)).toBe("progress");
    expect(nextOverlayTabStop(stops, "background", false)).toBe("close");
    expect(nextOverlayTabStop(stops, null, true)).toBe("queue");
    expect(nextOverlayTabStop(["close"], "close", true)).toBe("close");
    expect(nextOverlayTabStop([], null, false)).toBeUndefined();
  });
  it("excludes disabled controls, hidden/inert ancestry, CSS-hidden controls and negative tab indices", () => {
    vi.stubGlobal("getComputedStyle", () => ({ visibility: "visible" }));
    const element = (overrides: object = {}) => ({
      tabIndex: 0, matches: () => false, closest: () => null,
      getClientRects: () => [{}], ...overrides,
    }) as unknown as HTMLElement;
    try {
      expect(isOverlayTabStop(element())).toBe(true);
      expect(isOverlayTabStop(element({ tabIndex: -1 }))).toBe(false);
      expect(isOverlayTabStop(element({ matches: () => true }))).toBe(false);
      expect(isOverlayTabStop(element({ closest: () => ({}) }))).toBe(false);
      expect(isOverlayTabStop(element({ getClientRects: () => [] }))).toBe(false);
      vi.stubGlobal("getComputedStyle", () => ({ visibility: "hidden" }));
      expect(isOverlayTabStop(element())).toBe(false);
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

it("aligns dense translated timestamps to the nearest original, preferring exact matches", () => {
  expect(alignLyrics(
    "[00:10.000]first\n[00:10.500]second\n[00:20.000]unmatched",
    "[00:10.000]第一\n[00:10.500]第二",
  )).toEqual([
    { time: 10000, text: "first", translated: "第一" },
    { time: 10500, text: "second", translated: "第二" },
    { time: 20000, text: "unmatched", translated: undefined },
  ]);
  expect(alignLyrics("[00:10.400]line", "[00:10.000]far\n[00:10.500]near")[0].translated).toBe("near");
});

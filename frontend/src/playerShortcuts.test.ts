import { describe, expect, it, vi } from "vitest";
import {
  handlePlayerShortcut,
  matchPlayerShortcut,
  playerShortcuts,
  shortcutContext,
  shortcutUnavailable,
} from "./playerShortcuts";
import type { PlayerState, PlayerStore } from "./PlayerStore";
import type { TrackSnapshot } from "./contracts";

const song: TrackSnapshot = {
  neteaseSongId: "123456789012345678",
  name: "One",
  artists: [{ name: "Artist" }],
  durationMs: 120000,
};
const other: TrackSnapshot = { ...song, neteaseSongId: "223456789012345678", name: "Two" };
const state = (): PlayerState => ({
  resourceCacheStats: null,
  resourceCacheMessage: "",
  coverRevision: 0,
  searchHistory: [],
  searchHistoryError: "",
  favoritesHasMore: false,
  favoritesLoading: false,
  favoritesPage: 1,
  historyPage: 1,
  favoritesQuery: "",
  historyQuery: "",
  cacheStats: null,
  cacheMessage: "",
  historyHasMore: false,
  historyLoading: false,
  historyClearing: false,
  queue: {
    entries: [{ queueEntryId: "one", track: song }],
    currentEntryId: "one",
    currentIndex: 0,
    playMode: "repeat-all",
    positionMs: 50000,
    revision: 0,
  },
  currentSongId: song.neteaseSongId,
  hydration: "ready",
  playback: "paused",
  error: "",
  saveError: "",
  lyrics: null,
  lyricError: false,
  stream: null,
  settings: { preferredQuality: "exhigh", regionMode: "auto", gdEnabled: false, volume: 0.5 },
  qualityPolicy: { premiumAllowed: false, selectableQualities: ["exhigh"], maxBitrate: 320000 },
  settingsError: "",
  favoriteIDs: new Set(),
  favorites: [],
  history: [],
  libraryReady: true,
  libraryError: "",
  region: null,
  regionError: "",
  capabilities: null,
});
function harness() {
  const current = state();
  const calls = {
    play: vi.fn(),
    pause: vi.fn(),
    previous: vi.fn(),
    next: vi.fn(),
    seek: vi.fn(),
    setVolume: vi.fn(),
    toggleFavorite: vi.fn(),
  };
  const store = {
    getSnapshot: () => current,
    getPlaybackPosition: () => current.queue.positionMs,
    isClosing: () => false,
    play: calls.play,
    pause: calls.pause,
    previous: calls.previous,
    next: calls.next,
    seek: calls.seek,
    setVolume: calls.setVolume,
    toggleFavorite: calls.toggleFavorite,
  } as unknown as PlayerStore;
  const doc = { hidden: false, hasFocus: () => true, querySelector: () => null } as unknown as Document;
  const views = { openQueue: vi.fn(), openLyrics: vi.fn(), openHelp: vi.fn() };
  return { current, calls, store, doc, views };
}
function key(key: string, options: Partial<KeyboardEvent> = {}) {
  let prevented = false;
  const event = {
    key,
    ctrlKey: false,
    altKey: false,
    metaKey: false,
    shiftKey: false,
    repeat: false,
    isComposing: false,
    keyCode: 0,
    defaultPrevented: false,
    target: null,
    preventDefault: () => {
      prevented = true;
    },
    ...options,
  } as KeyboardEvent;
  return { event, prevented: () => prevented };
}
describe("player shortcut registry and dispatch", () => {
  it("matches case-insensitive letters and '?' while rejecting extra modifiers", () => {
    expect(playerShortcuts.length).toBeGreaterThan(0);
    expect(
      matchPlayerShortcut({ key: "ArrowLeft", ctrlKey: true, altKey: false, metaKey: false, shiftKey: false })?.id,
    ).toBe("previous");
    expect(
      matchPlayerShortcut({ key: "ArrowLeft", ctrlKey: false, altKey: false, metaKey: false, shiftKey: false })?.id,
    ).toBe("seek-back");
    expect(
      matchPlayerShortcut({ key: "ArrowLeft", ctrlKey: true, altKey: true, metaKey: false, shiftKey: false }),
    ).toBeUndefined();
    expect(
      matchPlayerShortcut({ key: "f", ctrlKey: true, altKey: false, metaKey: false, shiftKey: false }),
    ).toBeUndefined();
    expect(
      matchPlayerShortcut({ key: "F", ctrlKey: false, altKey: false, metaKey: false, shiftKey: true }),
    ).toBeUndefined();
    expect(matchPlayerShortcut({ key: "F", ctrlKey: false, altKey: false, metaKey: false, shiftKey: false })?.id).toBe(
      "favorite",
    );
    expect(matchPlayerShortcut({ key: "?", ctrlKey: false, altKey: false, metaKey: false, shiftKey: true })?.id).toBe(
      "help",
    );
    expect(
      matchPlayerShortcut({ key: "?", ctrlKey: false, altKey: false, metaKey: true, shiftKey: true }),
    ).toBeUndefined();
  });

  it("dispatches seek and volume against the current values with finite bounds", () => {
    const h = harness();
    let k = key("ArrowRight");
    expect(handlePlayerShortcut(k.event, h.store, h.views, h.doc)).toBe(true);
    expect(h.calls.seek).toHaveBeenLastCalledWith(55000);
    h.current.queue.positionMs = 119000;
    k = key("ArrowRight");
    handlePlayerShortcut(k.event, h.store, h.views, h.doc);
    expect(h.calls.seek).toHaveBeenLastCalledWith(120000);
    h.current.queue.positionMs = 1000;
    k = key("ArrowLeft");
    handlePlayerShortcut(k.event, h.store, h.views, h.doc);
    expect(h.calls.seek).toHaveBeenLastCalledWith(0);
    h.current.settings!.volume = 0.98;
    k = key("ArrowUp", { ctrlKey: true });
    handlePlayerShortcut(k.event, h.store, h.views, h.doc);
    expect(h.calls.setVolume).toHaveBeenLastCalledWith(1);
    h.current.settings!.volume = 0.02;
    k = key("ArrowDown", { ctrlKey: true });
    handlePlayerShortcut(k.event, h.store, h.views, h.doc);
    expect(h.calls.setVolume).toHaveBeenLastCalledWith(0);
    expect(k.prevented()).toBe(true);
  });

  it("toggles playback, including cancellation while loading, and blocks closing or unavailable actions", () => {
    const h = harness();
    let k = key(" ");
    expect(handlePlayerShortcut(k.event, h.store, h.views, h.doc)).toBe(true);
    expect(h.calls.play).toHaveBeenCalledTimes(1);
    expect(k.prevented()).toBe(true);
    h.current.playback = "loading";
    k = key(" ");
    handlePlayerShortcut(k.event, h.store, h.views, h.doc);
    expect(h.calls.pause).toHaveBeenCalledTimes(1);
    let closing = true;
    const closingStore = { ...h.store, isClosing: () => closing } as PlayerStore;
    k = key("q");
    expect(handlePlayerShortcut(k.event, closingStore, h.views, h.doc)).toBe(false);
    expect(k.prevented()).toBe(false);
    closing = false;
    h.current.settings = null;
    k = key(" ");
    expect(handlePlayerShortcut(k.event, h.store, h.views, h.doc)).toBe(false);
    expect(k.prevented()).toBe(false);
    h.current.settings = { preferredQuality: "exhigh", regionMode: "auto", gdEnabled: false, volume: 0.5 };
    h.current.queue.currentEntryId = null;
    k = key("ArrowRight");
    expect(handlePlayerShortcut(k.event, h.store, h.views, h.doc)).toBe(false);
    expect(k.prevented()).toBe(false);
    h.current.queue.currentEntryId = "one";
    h.current.libraryReady = false;
    k = key("f");
    expect(handlePlayerShortcut(k.event, h.store, h.views, h.doc)).toBe(false);
    expect(k.prevented()).toBe(false);
    h.current.libraryReady = true;
    h.current.hydration = "loading";
    k = key("Control", { ctrlKey: true });
    expect(handlePlayerShortcut(k.event, h.store, h.views, h.doc)).toBe(false);
  });

  it("suppresses unsafe or unavailable events without preventing browser behavior", () => {
    const h = harness();
    for (const options of [{ defaultPrevented: true }, { repeat: true }, { isComposing: true }, { keyCode: 229 }]) {
      const k = key("f", options);
      expect(handlePlayerShortcut(k.event, h.store, h.views, h.doc)).toBe(false);
      expect(k.prevented()).toBe(false);
    }
    h.current.hydration = "loading";
    const unavailable = shortcutUnavailable(playerShortcuts[0], shortcutContext(h.current));
    expect(unavailable).toBeTruthy();
    const k = key(" ");
    expect(handlePlayerShortcut(k.event, h.store, h.views, h.doc)).toBe(false);
    expect(k.prevented()).toBe(false);
    expect(h.calls.play).not.toHaveBeenCalled();
  });

  it("respects focused editors, dialogs, hidden documents, and native button Space", () => {
    const h = harness();
    const editor = key("f", {
      target: { closest: (s: string) => (s.includes("input") ? {} : null) } as unknown as Element,
    });
    expect(handlePlayerShortcut(editor.event, h.store, h.views, h.doc)).toBe(false);
    const buttonSpace = key(" ", {
      target: { closest: (s: string) => (s.includes("button") ? {} : null) } as unknown as Element,
    });
    expect(handlePlayerShortcut(buttonSpace.event, h.store, h.views, h.doc)).toBe(false);
    const dialogDoc = { ...h.doc, querySelector: () => ({}) } as Document;
    expect(handlePlayerShortcut(key("q").event, h.store, h.views, dialogDoc)).toBe(false);
    const hiddenDoc = { ...h.doc, hidden: true } as Document;
    expect(handlePlayerShortcut(key("q").event, h.store, h.views, hiddenDoc)).toBe(false);
    const unfocusedDoc = { ...h.doc, hasFocus: () => false } as Document;
    expect(handlePlayerShortcut(key("q").event, h.store, h.views, unfocusedDoc)).toBe(false);
  });

  it("favorite follows the latest song and opens views only for their registered commands", () => {
    const h = harness();
    const k = key("f");
    handlePlayerShortcut(k.event, h.store, h.views, h.doc);
    expect(h.calls.toggleFavorite).toHaveBeenLastCalledWith(song);
    h.current.queue.entries[0].track = other;
    h.current.queue.currentEntryId = "one";
    handlePlayerShortcut(key("f").event, h.store, h.views, h.doc);
    expect(h.calls.toggleFavorite).toHaveBeenLastCalledWith(other);
    handlePlayerShortcut(key("q").event, h.store, h.views, h.doc);
    handlePlayerShortcut(key("l").event, h.store, h.views, h.doc);
    handlePlayerShortcut(key("?").event, h.store, h.views, h.doc);
    expect(h.views.openQueue).toHaveBeenCalledTimes(1);
    expect(h.views.openLyrics).toHaveBeenCalledTimes(1);
    expect(h.views.openHelp).toHaveBeenCalledTimes(1);
  });
});

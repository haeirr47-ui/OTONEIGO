import { it, expect, vi } from "vitest";
import { bindMediaSession, playerCommand } from "./mediaSession";
import type { PlayerStore, PlayerState } from "./PlayerStore";
function setup() {
  let listener = () => {};
  const track = { neteaseSongId: "1", name: "Title", artists: [{ name: "Artist" }], album: { name: "Album" }, durationMs: 120000 };
  const state = { queue: { entries: [{ queueEntryId: "entry", track }], currentEntryId: "entry", positionMs: 1000 }, playback: "paused", stream: { url: "http://127.0.0.1:18993/media/private", cacheToken: "secret" } } as PlayerState;
  const unsubscribe = vi.fn();
  const store = { isClosing: vi.fn(() => false), getSnapshot: () => state, subscribe: vi.fn(fn => { listener = fn; return unsubscribe; }), play: vi.fn(async () => {}), pause: vi.fn(), next: vi.fn(async () => {}), previous: vi.fn(async () => {}), seek: vi.fn() } as unknown as PlayerStore;
  const handlers = new Map<string, MediaSessionActionHandler | null>();
  const session = { metadata: null, playbackState: "none", setActionHandler: vi.fn((action, handler) => { handlers.set(action, handler); }), setPositionState: vi.fn() } as unknown as MediaSession;
  const Metadata = vi.fn(function (this: MediaMetadata, value: MediaMetadataInit) { Object.assign(this, value); }) as unknown as typeof MediaMetadata;
  return { store, state, session, Metadata, handlers, unsubscribe, update: () => listener() };
}
it("uses the existing store for exact tray commands and ignores unknown commands", () => {
  const { store, state } = setup();
  playerCommand(store, "toggle-play"); expect(store.play).toHaveBeenCalledOnce();
  state.playback = "playing"; playerCommand(store, "toggle-play"); expect(store.pause).toHaveBeenCalledOnce();
  playerCommand(store, "next"); playerCommand(store, "previous"); playerCommand(store, { command: "next" });
  expect(store.next).toHaveBeenCalledOnce(); expect(store.previous).toHaveBeenCalledOnce();
});
it("publishes only track metadata, guards position, routes actions, and cleans handlers", () => {
  const { store, state, session, Metadata, handlers, update, unsubscribe } = setup();
  const stop = bindMediaSession(store, session, Metadata);
  expect(Metadata).toHaveBeenCalledExactlyOnceWith({ title: "Title", artist: "Artist", album: "Album" });
  expect(JSON.stringify(session.metadata)).not.toMatch(/private|secret|url|cacheToken/);
  state.playback = "playing"; state.queue.positionMs = 3000; update();
  expect(Metadata).toHaveBeenCalledOnce(); expect(session.playbackState).toBe("playing");
  expect(session.setPositionState).toHaveBeenLastCalledWith({ duration: 120, position: 3, playbackRate: 1 });
  state.playback = "paused"; handlers.get("play")!({ action: "play" });
  state.playback = "playing"; handlers.get("pause")!({ action: "pause" });
  handlers.get("nexttrack")!({ action: "nexttrack" }); handlers.get("previoustrack")!({ action: "previoustrack" });
  handlers.get("seekto")!({ action: "seekto", seekTime: 12 }); expect(store.seek).toHaveBeenLastCalledWith(12000);
  handlers.get("seekforward")!({ action: "seekforward", seekOffset: 2 }); expect(store.seek).toHaveBeenLastCalledWith(5000);
  handlers.get("seekbackward")!({ action: "seekbackward", seekOffset: 1 }); expect(store.seek).toHaveBeenLastCalledWith(2000);
  expect(store.play).toHaveBeenCalledOnce(); expect(store.pause).toHaveBeenCalledOnce(); expect(store.next).toHaveBeenCalledOnce(); expect(store.previous).toHaveBeenCalledOnce();
  state.queue.positionMs = NaN; update(); expect(session.setPositionState).toHaveBeenLastCalledWith();
  stop(); expect(unsubscribe).toHaveBeenCalledOnce(); expect([...handlers.values()]).toEqual(Array(7).fill(null)); expect(session.metadata).toBeNull(); expect(session.playbackState).toBe("none");
});
it("tolerates unsupported media session methods and missing platform support", () => {
  const { store, session, Metadata } = setup();
  vi.mocked(session.setActionHandler).mockImplementation(() => { throw new Error("unsupported"); });
  vi.mocked(session.setPositionState).mockImplementation(() => { throw new Error("unsupported"); });
  expect(() => bindMediaSession(store, session, Metadata)()).not.toThrow();
  expect(() => bindMediaSession(store, undefined, Metadata)()).not.toThrow();
});

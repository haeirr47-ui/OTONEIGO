import { afterEach, expect, it, vi } from "vitest";
import { PlayerStore, type PlayerState } from "./PlayerStore";
import type { DesktopBridge, LocalSettings } from "./contracts";
import type { AudioExecutor } from "./AudioEngine";
import { bindDesktopPublisher, createDesktopProjector, handleDesktopCommand, truncateUTF8, updateDesktopSettings, type DesktopEvents } from "./desktopProjection";
import { acceptDesktopSnapshot, defaultDesktopSettings, fromDesktopSnapshot, normalizeDesktopSettings } from "./desktopWire";

function fixture() {
  const settings: LocalSettings = { preferredQuality: "exhigh", regionMode: "auto", gdEnabled: false, volume: .4, audioCache: { enabled: false, maxBytes: 1024 ** 3 }, resourceCache: { enabled: true, maxBytes: 1024 ** 3 }, desktop: defaultDesktopSettings() };
  const bridge = { SetSettings: vi.fn(async (s: LocalSettings) => s) } as unknown as DesktopBridge;
  const engine = { on: () => () => {}, currentTime: 12500, duration: 60000, setVolume: vi.fn(), pause: vi.fn() } as unknown as AudioExecutor;
  const store = new PlayerStore(bridge, engine);
  const state = store.getSnapshot();
  Object.assign(state, { hydration: "ready", libraryReady: true, settings, qualityPolicy: { premiumAllowed: false, selectableQualities: ["exhigh"], maxBitrate: 320000 },
    queue: { entries: [{ queueEntryId: "entry-1", track: { neteaseSongId: "123456789012345678", name: "歌😀".repeat(500), artists: [{ name: "歌手" }], album: { name: "专辑" }, durationMs: 60000, coverRef: "https://p1.music.126.net/cover.jpg" } }], currentEntryId: "entry-1", currentIndex: 0, playMode: "repeat-all", positionMs: 11000, revision: 1 },
    lyrics: { original: "[00:10]第一行\n[00:20]第二行\n[00:30]完整歌词不得传输", translated: "[00:10.2]First line\n[00:20.1]Second line" },
    stream: { songId: "123456789012345678", url: "https://media.example/secret?token=do-not-send", cacheToken: "private-cache-token", provider: "netease-direct", requestedQuality: "exhigh", actualBitrate: 320000, status: "full" } } satisfies Partial<PlayerState>);
  return { store, state, engine, bridge };
}
afterEach(() => vi.useRealTimers());
it("projects only bounded metadata and aligned visible lyrics, using the one actual audio clock", () => {
  const { store, state, engine } = fixture();
  const project = createDesktopProjector(store);
  expect(project(1, 1).positionMs).toBe(11000); // Paused restored position, not the unloaded executor.
  state.playback = "playing";
  const result = project(1, 2);
  expect(result).toMatchObject({ positionMs: 12500, currentLyric: "第一行", nextLyric: "第二行", translation: "First line", lyricProgress: .25, quality: "320 kbps" });
  state.playback = "loading";
  expect(project(1, 3).positionMs).toBe(engine.currentTime); // Rebuffering retains the executor clock.
  Object.assign(store, { starting: true });
  expect(project(1, 4).positionMs).toBe(11000);
  expect(new TextEncoder().encode(result.title).length).toBeLessThanOrEqual(1024);
  expect(fromDesktopSnapshot(JSON.stringify(result))).toEqual(result);
  const wire = JSON.stringify(result);
  for (const secret of ["do-not-send", "private-cache-token", "完整歌词不得传输", "entries", "original", "stream", "url"]) expect(wire).not.toContain(secret);
  expect(truncateUTF8("😀中a", 6)).toBe("😀");
  expect(truncateUTF8("😀中a", 7)).toBe("😀中");
});
it("guards commands by epoch, readiness, shutdown and seek/favorite track identity", () => {
  const { store, state } = fixture();
  const play = vi.spyOn(store, "play").mockResolvedValue(), pause = vi.spyOn(store, "pause").mockImplementation(() => {});
  const seek = vi.spyOn(store, "seek").mockImplementation(() => {}), next = vi.spyOn(store, "next").mockResolvedValue();
  const favorite = vi.spyOn(store, "toggleFavorite").mockImplementation(() => {});
  const send = (command: string, songId = "123456789012345678", epoch = 7, value = 1000) => handleDesktopCommand(store, 7, { epoch, command, songId, value });
  send("play", "", 6); expect(play).not.toHaveBeenCalled();
  state.hydration = "loading"; send("next"); expect(next).not.toHaveBeenCalled(); state.hydration = "ready";
  send("seek", "999"); send("toggle-favorite", "999"); expect(seek).not.toHaveBeenCalled(); expect(favorite).not.toHaveBeenCalled();
  send("seek"); expect(seek).toHaveBeenCalledWith(1000);
  send("next", "999"); expect(next).toHaveBeenCalledOnce();
  send("play"); expect(play).toHaveBeenCalledOnce();
  state.playback = "playing"; send("play"); expect(play).toHaveBeenCalledOnce();
  send("pause"); expect(pause).toHaveBeenCalledOnce(); state.playback = "paused"; send("pause"); expect(pause).toHaveBeenCalledOnce();
  send("seek", "123456789012345678", 7, NaN); expect(seek).toHaveBeenCalledOnce();
  Object.assign(store, { closing: true }); send("next"); expect(next).toHaveBeenCalledOnce();
});
it("coalesces ticks and refreshes, republishes after a late navigation epoch, then freezes at close", async () => {
  vi.useFakeTimers();
  const { store, state, bridge } = fixture();
  state.playback = "playing";
  let epoch = 1, changed: (() => void) | undefined;
  vi.spyOn(store, "subscribe").mockImplementation(fn => { changed = fn; return () => {}; });
  bridge.GetDesktopEpoch = vi.fn(async () => epoch);
  const callbacks = new Map<string, (e: { data: unknown }) => void>();
  const events: DesktopEvents = { On: (name, fn) => { callbacks.set(name, fn); return () => { callbacks.delete(name); }; }, Emit: vi.fn() };
  const stop = bindDesktopPublisher(store, bridge, events);
  await vi.advanceTimersByTimeAsync(0);
  const published = () => vi.mocked(events.Emit).mock.calls.map(c => JSON.parse(c[1] as string));
  expect(published()).toHaveLength(1);
  for (let i = 0; i < 30; i++) { changed?.(); callbacks.get("otoneigo:desktop-refresh")!({ data: null }); }
  await vi.advanceTimersByTimeAsync(249); expect(published()).toHaveLength(1);
  await vi.advanceTimersByTimeAsync(1); expect(published()).toHaveLength(2);
  epoch = 2; callbacks.get("otoneigo:desktop-epoch")!({ data: 2 });
  await vi.advanceTimersByTimeAsync(0);
  expect(published().at(-1)).toMatchObject({ epoch: 2, sequence: 1, ready: true, positionMs: 12500 });
  Object.assign(store, { closing: true }); changed?.(); await vi.advanceTimersByTimeAsync(250);
  expect(published().at(-1).closing).toBe(true);
  const count = published().length;
  changed?.(); callbacks.get("otoneigo:desktop-refresh")!({ data: null }); await vi.advanceTimersByTimeAsync(1000);
  expect(published()).toHaveLength(count);
  epoch = 3; callbacks.get("otoneigo:desktop-epoch")!({ data: 3 });
  await vi.advanceTimersByTimeAsync(0);
  changed?.(); callbacks.get("otoneigo:desktop-refresh")!({ data: null }); await vi.advanceTimersByTimeAsync(1000);
  expect(published()).toHaveLength(count); stop(); expect(callbacks.size).toBe(0);
});
it("rejects malformed wire payloads and stale observer snapshots including post-close commands", () => {
  const { store } = fixture(), snapshot = createDesktopProjector(store)(2, 3);
  expect(acceptDesktopSnapshot(snapshot, { ...snapshot, epoch: 1, sequence: 50 })).toBe(false);
  expect(acceptDesktopSnapshot(snapshot, { ...snapshot, sequence: 3 })).toBe(false);
  expect(acceptDesktopSnapshot(snapshot, { ...snapshot, sequence: 4 })).toBe(true);
  expect(acceptDesktopSnapshot({ ...snapshot, closing: true }, { ...snapshot, sequence: 4 })).toBe(false);
  expect(acceptDesktopSnapshot(snapshot, { ...snapshot, epoch: 3, sequence: 0, ready: false })).toBe(true);
  for (const update of [{ sequence: 0 }, { epoch: 0 }, { positionMs: NaN }, { volume: 2 }, { title: "😀".repeat(257) }, { coverRef: "https://evil.example/secret.jpg" }, { currentLyric: "x".repeat(4097) }, { stream: { url: "secret" } }, { queueEntryId: "bad/id" }, { songId: "0" }, { options: undefined }]) expect(() => fromDesktopSnapshot({ ...snapshot, ...update })).toThrow();
  expect(fromDesktopSnapshot({ ...snapshot, sequence: 0, ready: false }).sequence).toBe(0);
  expect(() => fromDesktopSnapshot(" ".repeat(65537))).toThrow();
  expect(() => normalizeDesktopSettings({ ...defaultDesktopSettings(), lyricsFontSize: 65 })).toThrow();
});
it("desktop preference updates preserve audio/source/cache preferences and merge current desktop options", async () => {
  const { store, state, bridge } = fixture();
  updateDesktopSettings(store, { lyricsLocked: true });
  updateDesktopSettings(store, { lyricsFontSize: 40 });
  await store.flush();
  expect(state.settings?.audioCache?.enabled).toBe(false);
  const saved = vi.mocked(bridge.SetSettings).mock.calls.at(-1)![0];
  expect(saved).toMatchObject({ preferredQuality: "exhigh", regionMode: "auto", gdEnabled: false, audioCache: { enabled: false }, resourceCache: { enabled: true }, desktop: { lyricsLocked: true, lyricsFontSize: 40, miniAlwaysOnTop: true } });
});

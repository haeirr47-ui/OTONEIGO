import { startSettingsCachePolling } from "./diagnosticPolling";
import { createCloseHandler } from "./prepareClose";
import { playerCommand, bindMediaSession } from "./mediaSession";
import { describe, it, expect, vi } from "vitest";
import { PlayerStore } from "./PlayerStore";
import type { AudioExecutor, EngineEvent } from "./AudioEngine";
import { AudioEngine } from "./AudioEngine";
import type {
  DesktopBridge,
  LocalSettings,
  QueueSnapshot,
  ResolvedStream,
  TrackSnapshot,
  PlaybackQualityPolicy,
} from "./contracts";
const track: TrackSnapshot = {
  neteaseSongId: "123456789012345678",
  name: "测试歌曲",
  artists: [{ name: "歌手" }],
  durationMs: 120000,
};
const stream: ResolvedStream = {
  songId: track.neteaseSongId,
  provider: "netease-direct",
  url: "https://media.example/audio?token=secret",
  requestedQuality: "exhigh",
  status: "full",
};
const saved: QueueSnapshot = {
  entries: [{ queueEntryId: "old-entry", track }],
  currentEntryId: "old-entry",
  currentIndex: 0,
  playMode: "repeat-all",
  positionMs: 30000,
  revision: 8,
};

it("hydrates favorite membership after page reads quarantine damaged records", async () => {
  const { store, bridge } = setup();
  let ids = [track.neteaseSongId];
  bridge.GetLocalFavoriteIDs = async () => ids.slice();
  bridge.SearchLocalFavorites = async () => {
    ids = [];
    return { tracks: [], hasMore: false, nextAddedOrder: 0, nextSongId: "" };
  };
  await store.hydrate();
  expect(store.getSnapshot().libraryReady).toBe(true);
  expect(store.isFavorite(track)).toBe(false);
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error?: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
class FakeAudio implements AudioExecutor {
  listeners = new Map<EngineEvent, () => void>();
  currentTime = 0;
  duration = 120000;
  on(event: EngineEvent, fn: () => void) {
    this.listeners.set(event, fn);
    return () => {
      this.listeners.delete(event);
    };
  }
  setSource = vi.fn(async (_url: string, _position: number) => {});
  pause = vi.fn((_preserveSource?: boolean) => {});
  seek = vi.fn();
  setVolume = vi.fn();
  emit(event: EngineEvent) {
    this.listeners.get(event)?.();
  }
}
const memberPolicy: PlaybackQualityPolicy = {
  premiumAllowed: true,
  selectableQualities: ["exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster"],
  maxBitrate: 0,
};
const anonymousPolicy: PlaybackQualityPolicy = {
  premiumAllowed: false,
  selectableQualities: ["exhigh"],
  maxBitrate: 320000,
};
function setup(load: Promise<QueueSnapshot | null> = Promise.resolve(null)) {
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
    GetPlaybackQualityPolicy: vi.fn(async () => memberPolicy),
    GetSettings: vi.fn<DesktopBridge["GetSettings"]>(async () => ({
      preferredQuality: "exhigh",
      regionMode: "auto",
      gdEnabled: false,
      volume: 1,
    })),
    SetSettings: vi.fn(async (s) => s),
    GetRegionStatus: vi.fn<DesktopBridge["GetRegionStatus"]>(async () => ({
      classification: "unknown",
      observedAt: 0,
      networkGeneration: 0,
      detecting: false,
    })),
    RefreshRegion: vi.fn<DesktopBridge["RefreshRegion"]>(async () => ({
      classification: "unknown",
      observedAt: 0,
      networkGeneration: 0,
      detecting: false,
    })),
    GetCapabilities: vi.fn(async () => ({
      regionCompatibility: true,
      gd: false,
      account: false,
    })),
    GetLocalFavorites: vi.fn(async () => []),
    SetLocalFavorite: vi.fn(async () => {}),
    AddLocalHistory: vi.fn(async () => {}),
    ListLocalHistory: vi.fn(async () => []),
    ClearLocalHistory: vi.fn(async () => {}),
    LoadQueue: vi.fn(() => load),
    SaveQueue: vi.fn(async () => {}),
    ResolveStream: vi.fn(async () => stream),
    GetLyrics: vi.fn(async () => ({ original: "歌词", translated: "" })),
    SearchNetease: vi.fn(async () => ({
      tracks: [],
      total: 0,
      hasMore: false,
    })),
  };
  const engine = new FakeAudio();
  const store = new PlayerStore(bridge, engine);
  return { bridge, engine, store };
}
describe("single queue owner and playback races", () => {
  it("serializes fractional progress, pause and seek as nonnegative integer milliseconds", async () => {
    const { store, engine, bridge } = setup(Promise.resolve(saved));
    await store.hydrate();
    await store.play();
    engine.emit("playing");
    engine.currentTime = 38792.239;
    engine.emit("timeupdate");
    store.pause();
    store.seek(45000.733);
    store.setMode("repeat-one");
    await store.flush();
    const positions = vi.mocked(bridge.SaveQueue).mock.calls.map(([q]) => q.positionMs);
    expect(positions).toContain(38792);
    expect(positions).toContain(45001);
    expect(positions.every((p) => Number.isSafeInteger(p) && p >= 0)).toBe(true);
  });
  it.each([NaN, Infinity, -Infinity, -100])("clamps invalid media position %s before saving", async (position) => {
    const { store, engine, bridge } = setup(Promise.resolve(saved));
    await store.hydrate();
    await store.play();
    engine.emit("playing");
    engine.currentTime = position;
    store.pause();
    await store.flush();
    expect(vi.mocked(bridge.SaveQueue).mock.lastCall?.[0].positionMs).toBe(0);
  });
  it("captures exact media position when pausing actual playback", async () => {
    const { store, engine } = setup(Promise.resolve(saved));
    await store.hydrate();
    await store.play();
    engine.emit("playing");
    engine.currentTime = 37892;
    store.pause();
    expect(store.getSnapshot().queue.positionMs).toBe(37892);
    await store.play();
    expect(engine.setSource).toHaveBeenLastCalledWith(
      stream.url,
      37892,
      expect.objectContaining({
        expectedDurationMs: track.durationMs,
        requireComplete: true,
      }),
    );
  });
  it("does not save or accept edits until hydration completes, then restores paused", async () => {
    const load = deferred<QueueSnapshot | null>();
    const { store, bridge, engine } = setup(load.promise);
    const hydration = store.hydrate();
    expect(store.add(track)).toBeNull();
    store.seek(100);
    store.pause();
    expect(bridge.SaveQueue).not.toHaveBeenCalled();
    load.resolve(saved);
    await hydration;
    expect(store.getSnapshot().queue).toEqual(saved);
    expect(store.getSnapshot().playback).toBe("paused");
    expect(engine.setSource).not.toHaveBeenCalled();
    expect(bridge.SaveQueue).not.toHaveBeenCalled();
    await store.play();
    expect(engine.setSource).toHaveBeenCalledWith(
      stream.url,
      30000,
      expect.objectContaining({
        expectedDurationMs: track.durationMs,
        requireComplete: true,
      }),
    );
    expect(store.getSnapshot().playback).toBe("loading");
    engine.emit("playing");
    expect(store.getSnapshot().playback).toBe("playing");
  });
  it("fails hydration visibly and never overwrites an unread queue", async () => {
    const { store, bridge } = setup(Promise.reject(new Error("database")));
    await store.hydrate();
    store.add(track);
    store.seek(5);
    store.pause();
    await store.flush();
    expect(store.getSnapshot().hydration).toBe("failed");
    expect(store.getSnapshot().error).toBeTruthy();
    expect(bridge.SaveQueue).not.toHaveBeenCalled();
  });
  it("accepts a fresh null snapshot without writing an empty default", async () => {
    const { store, bridge } = setup();
    await store.hydrate();
    expect(store.getSnapshot().queue.entries).toEqual([]);
    expect(bridge.SaveQueue).not.toHaveBeenCalled();
  });
  it("keeps duplicate songs as distinct queue entries and strips service fields", async () => {
    const { store, bridge } = setup();
    await store.hydrate();
    const a = store.add({ ...track, url: stream.url } as TrackSnapshot);
    const b = store.add(track);
    await store.flush();
    expect(a).not.toBe(b);
    expect(store.getSnapshot().queue.entries).toHaveLength(2);
    expect(JSON.stringify(vi.mocked(bridge.SaveQueue).mock.calls)).not.toContain("secret");
  });
  it("serializes full snapshot writes in revision order", async () => {
    const { store, bridge } = setup(Promise.resolve(saved));
    await store.hydrate();
    const first = deferred<void>();
    vi.mocked(bridge.SaveQueue).mockImplementationOnce(() => first.promise);
    store.add(track);
    store.setMode("repeat-one");
    await Promise.resolve();
    await Promise.resolve();
    expect(bridge.SaveQueue).toHaveBeenCalledTimes(1);
    first.resolve();
    await store.flush();
    expect(vi.mocked(bridge.SaveQueue).mock.calls.map((call) => call[0].revision)).toEqual([9, 10]);
    expect(vi.mocked(bridge.SaveQueue).mock.calls[1][0].playMode).toBe("repeat-one");
  });
  it("cancels a pending play and discards its late URL after pause", async () => {
    const { store, bridge, engine } = setup(Promise.resolve(saved));
    await store.hydrate();
    const resolve = deferred<ResolvedStream>();
    vi.mocked(bridge.ResolveStream).mockReturnValueOnce(resolve.promise);
    const play = store.play();
    const signal = vi.mocked(bridge.ResolveStream).mock.calls[0][2];
    store.pause();
    expect(signal?.aborted).toBe(true);
    resolve.resolve(stream);
    await play;
    expect(engine.setSource).not.toHaveBeenCalled();
    expect(store.getSnapshot().playback).toBe("paused");
  });
  it("a late older song cannot replace the selected song", async () => {
    const { store, bridge, engine } = setup(Promise.resolve(saved));
    await store.hydrate();
    const slow = deferred<ResolvedStream>();
    vi.mocked(bridge.ResolveStream).mockReturnValueOnce(slow.promise);
    const old = store.play();
    const id = store.add({ ...track, neteaseSongId: "2" })!;
    await store.select(id);
    slow.resolve({ ...stream, url: "https://old.example" });
    await old;
    expect(engine.setSource).toHaveBeenCalledTimes(1);
    expect(store.getSnapshot().queue.currentEntryId).toBe(id);
  });
  it("seek during URL resolution and paused seek survive first source load", async () => {
    const { store, bridge, engine } = setup(Promise.resolve(saved));
    await store.hydrate();
    store.seek(45000);
    expect(engine.seek).not.toHaveBeenCalled();
    const slow = deferred<ResolvedStream>();
    vi.mocked(bridge.ResolveStream).mockReturnValueOnce(slow.promise);
    const play = store.play();
    store.seek(60000);
    slow.resolve(stream);
    await play;
    expect(engine.setSource).toHaveBeenCalledWith(
      stream.url,
      60000,
      expect.objectContaining({
        expectedDurationMs: track.durationMs,
        requireComplete: true,
      }),
    );
  });
  it("does not advance or loop automatically on media error", async () => {
    const { store, bridge, engine } = setup(Promise.resolve(saved));
    await store.hydrate();
    await store.play();
    engine.emit("error");
    engine.emit("ended");
    expect(store.getSnapshot().playback).toBe("paused");
    expect(store.getSnapshot().error).toBeTruthy();
    expect(bridge.ResolveStream).toHaveBeenCalledTimes(1);
  });
  it("bounds progress persistence and saves edits immediately", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(10000);
    const { store, engine, bridge } = setup(Promise.resolve(saved));
    await store.hydrate();
    await store.play();
    engine.emit("playing");
    engine.currentTime = 32000;
    engine.emit("timeupdate");
    await store.flush();
    engine.currentTime = 33000;
    engine.emit("timeupdate");
    await store.flush();
    expect(bridge.SaveQueue).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(5000);
    engine.emit("timeupdate");
    await store.flush();
    expect(bridge.SaveQueue).toHaveBeenCalledTimes(2);
    store.seek(40000);
    await store.flush();
    expect(bridge.SaveQueue).toHaveBeenCalledTimes(3);
    vi.useRealTimers();
  });
});

describe("local library, settings and playback policy", () => {
  it("hydrates saved settings before playback and never overwrites unread settings", async () => {
    const { store, bridge, engine } = setup(Promise.resolve(saved));
    const settings = deferred<Awaited<ReturnType<DesktopBridge["GetSettings"]>>>();
    vi.mocked(bridge.GetSettings).mockReturnValue(settings.promise);
    const hydration = store.hydrate();
    await Promise.resolve();
    await store.play();
    store.setSettings({ preferredQuality: "hires" });
    expect(bridge.ResolveStream).not.toHaveBeenCalled();
    expect(bridge.SetSettings).not.toHaveBeenCalled();
    settings.resolve({
      preferredQuality: "lossless",
      regionMode: "always",
      gdEnabled: false,
      volume: 0.3,
    });
    await hydration;
    expect(engine.setVolume).toHaveBeenCalledWith(0.3);
    await store.play();
    expect(bridge.ResolveStream).toHaveBeenCalledWith(track.neteaseSongId, "lossless", expect.any(AbortSignal));
    expect(bridge.SetSettings).not.toHaveBeenCalled();
  });
  it("keeps a failed settings read protected", async () => {
    const { store, bridge } = setup(Promise.resolve(saved));
    vi.mocked(bridge.GetSettings).mockRejectedValue(new Error("sqlite"));
    await store.hydrate();
    store.setVolume(0.5);
    await store.play();
    expect(store.getSnapshot().settingsError).toBeTruthy();
    expect(bridge.SetSettings).not.toHaveBeenCalled();
    expect(bridge.ResolveStream).not.toHaveBeenCalled();
  });
  it("serializes settings writes and waits for the latest saved policy before resolving", async () => {
    const { store, bridge } = setup(Promise.resolve(saved));
    await store.hydrate();
    const first = deferred<Awaited<ReturnType<DesktopBridge["SetSettings"]>>>();
    vi.mocked(bridge.SetSettings).mockReturnValueOnce(first.promise);
    store.setSettings({ preferredQuality: "lossless" });
    store.setSettings({ preferredQuality: "hires", regionMode: "always" });
    const play = store.play();
    await Promise.resolve();
    await Promise.resolve();
    expect(bridge.SetSettings).toHaveBeenCalledTimes(1);
    expect(bridge.ResolveStream).not.toHaveBeenCalled();
    first.resolve({
      preferredQuality: "lossless",
      regionMode: "auto",
      gdEnabled: false,
      volume: 1,
    });
    await play;
    expect(vi.mocked(bridge.SetSettings).mock.calls.map(([s]) => s.preferredQuality)).toEqual(["lossless", "hires"]);
    expect(bridge.ResolveStream).toHaveBeenCalledWith(track.neteaseSongId, "hires", expect.any(AbortSignal));
  });
  it("records actual advancing audio once, excludes selection/failure/rebuffer and gives a repeat session a new ID", async () => {
    const { store, bridge, engine } = setup(Promise.resolve(saved));
    await store.hydrate();
    await store.select("old-entry");
    expect(bridge.AddLocalHistory).not.toHaveBeenCalled();
    engine.emit("error");
    engine.currentTime = 1000;
    engine.emit("timeupdate");
    await store.flush();
    expect(bridge.AddLocalHistory).not.toHaveBeenCalled();
    await store.play();
    engine.currentTime = 0;
    engine.emit("playing");
    engine.currentTime = 600;
    engine.emit("timeupdate");
    engine.emit("waiting");
    engine.emit("playing");
    engine.currentTime = 1200;
    engine.emit("timeupdate");
    await store.flush();
    expect(bridge.AddLocalHistory).toHaveBeenCalledTimes(1);
    const first = vi.mocked(bridge.AddLocalHistory).mock.calls[0][0];
    expect(first.track.neteaseSongId).toBe(track.neteaseSongId);
    expect(first.historyEntryId).toMatch(/^[0-9a-f-]{36}$/);
    expect(JSON.stringify(first)).not.toContain("secret");
    await store.select("old-entry");
    engine.currentTime = 0;
    engine.emit("playing");
    engine.currentTime = 600;
    engine.emit("timeupdate");
    await store.flush();
    expect(bridge.AddLocalHistory).toHaveBeenCalledTimes(2);
    expect(vi.mocked(bridge.AddLocalHistory).mock.calls[1][0].historyEntryId).not.toBe(first.historyEntryId);
  });
  it("does not mistake a seek or a stalled playhead for listened audio", async () => {
    const { store, bridge, engine } = setup(Promise.resolve(saved));
    await store.hydrate();
    await store.play();
    engine.currentTime = 0;
    engine.emit("playing");
    store.seek(50000);
    engine.currentTime = 50000;
    engine.emit("timeupdate");
    engine.emit("timeupdate");
    await store.flush();
    expect(bridge.AddLocalHistory).not.toHaveBeenCalled();
  });
  it("serializes favorites and clears history using only SQLite bridge operations", async () => {
    const { store, bridge } = setup();
    vi.mocked(bridge.ListLocalHistory).mockResolvedValue([{ historyEntryId: "prior", track, playedAt: 1 }]);
    await store.hydrate();
    const metadata = { ...track, url: stream.url } as TrackSnapshot;
    store.toggleFavorite(metadata);
    store.toggleFavorite(metadata);
    await store.flush();
    expect(vi.mocked(bridge.SetLocalFavorite).mock.calls.map(([, liked]) => liked)).toEqual([true, false]);
    expect(store.getSnapshot().favorites).toEqual([]);
    expect(JSON.stringify(vi.mocked(bridge.SetLocalFavorite).mock.calls)).not.toContain("secret");
    store.clearHistory();
    await store.flush();
    expect(bridge.ClearLocalHistory).toHaveBeenCalledOnce();
    expect(store.getSnapshot().history).toEqual([]);
  });
  it.each(["quality", "region", "gd", "network"])(
    "cancels pending media startup after %s changes even after URL resolution",
    async (kind) => {
      const { store, bridge, engine } = setup(Promise.resolve(saved));
      await store.hydrate();
      store.setSettings({ gdEnabled: true });
      await store.flush();
      const startup = deferred<void>();
      engine.setSource.mockReturnValueOnce(startup.promise);
      const play = store.play();
      await Promise.resolve();
      expect(engine.setSource).toHaveBeenCalledOnce();
      expect(store.getSnapshot().stream).toEqual(stream);
      const signal = vi.mocked(bridge.ResolveStream).mock.calls[0][2];
      const pauses = engine.pause.mock.calls.length;
      if (kind === "network") {
        vi.mocked(bridge.GetRegionStatus).mockResolvedValue({
          classification: "overseas",
          observedAt: 1,
          networkGeneration: 1,
          detecting: false,
        });
        await store.pollRegion();
      } else {
        const updates: Record<string, Partial<LocalSettings>> = {
          quality: { preferredQuality: "lossless" },
          region: { regionMode: "off" },
          gd: { gdEnabled: false },
        };
        store.setSettings(updates[kind]);
      }
      expect(signal?.aborted).toBe(true);
      expect(engine.pause).toHaveBeenCalledTimes(pauses + 1);
      expect(store.getSnapshot().playback).toBe("paused");
      expect(store.getSnapshot().error).toBeTruthy();
      // An already queued startup completion/event cannot revive the old request.
      startup.resolve();
      await play;
      engine.emit("playing");
      engine.currentTime = 600;
      engine.emit("timeupdate");
      await store.flush();
      expect(store.getSnapshot().playback).toBe("paused");
      expect(bridge.AddLocalHistory).not.toHaveBeenCalled();
      expect(bridge.ResolveStream).toHaveBeenCalledOnce();
    },
  );
  it.each(["playing", "rebuffering"])(
    "preserves %s audio when startup has emitted playing before setSource settles",
    async (phase) => {
      const { store, bridge, engine } = setup(Promise.resolve(saved));
      await store.hydrate();
      const startup = deferred<void>();
      engine.setSource.mockReturnValueOnce(startup.promise);
      const play = store.play();
      await Promise.resolve();
      engine.emit("playing");
      if (phase === "rebuffering") engine.emit("waiting");
      const pauses = engine.pause.mock.calls.length;
      store.setSettings({ preferredQuality: "lossless" });
      vi.mocked(bridge.GetRegionStatus).mockResolvedValue({
        classification: "overseas",
        observedAt: 1,
        networkGeneration: 1,
        detecting: false,
      });
      await store.pollRegion();
      expect(engine.pause).toHaveBeenCalledTimes(pauses);
      expect(store.getSnapshot().playback).toBe(phase === "playing" ? "playing" : "loading");
      startup.resolve();
      await play;
      engine.emit("playing");
      engine.currentTime = 600;
      engine.emit("timeupdate");
      await store.flush();
      expect(bridge.AddLocalHistory).toHaveBeenCalledOnce();
    },
  );
  it.each(["policy", "network"])(
    "cancels pending URL resolution after %s changes and discards late sources",
    async (kind) => {
      const { store, bridge, engine } = setup(Promise.resolve(saved));
      await store.hydrate();
      const slow = deferred<ResolvedStream>();
      vi.mocked(bridge.ResolveStream).mockReturnValueOnce(slow.promise);
      const play = store.play();
      const signal = vi.mocked(bridge.ResolveStream).mock.calls[0][2];
      if (kind === "policy") store.setSettings({ regionMode: "off" });
      else {
        vi.mocked(bridge.GetRegionStatus).mockResolvedValue({
          classification: "overseas",
          country: "TH",
          observedAt: 1,
          networkGeneration: 1,
          detecting: false,
        });
        await store.pollRegion();
      }
      expect(signal?.aborted).toBe(true);
      slow.resolve(stream);
      await play;
      await store.flush();
      expect(engine.setSource).not.toHaveBeenCalled();
      await store.play();
      engine.emit("playing");
      const pauses = engine.pause.mock.calls.length;
      store.setSettings({ preferredQuality: "lossless" });
      vi.mocked(bridge.GetRegionStatus).mockResolvedValue({
        classification: "CN",
        observedAt: 2,
        networkGeneration: 2,
        detecting: false,
      });
      await store.pollRegion();
      await store.flush();
      expect(engine.pause).toHaveBeenCalledTimes(pauses);
      expect(engine.setSource).toHaveBeenCalledTimes(1);
      expect(store.getSnapshot().playback).toBe("playing");
    },
  );
});

it("passes the shared deadline and full catalog duration to GD startup, with a readable blocked-trial error", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  await store.hydrate();
  const deadline = Date.now() + 500;
  vi.mocked(bridge.ResolveStream).mockResolvedValue({
    ...stream,
    provider: "gd-netease",
    resolutionDeadlineAt: deadline,
  });
  const { MediaStartupError } = await import("./AudioEngine");
  engine.setSource.mockRejectedValueOnce(new MediaStartupError("TRIAL_ONLY"));
  await store.play();
  expect(engine.setSource).toHaveBeenCalledWith(stream.url, 30000, {
    expectedDurationMs: track.durationMs,
    requireComplete: true,
    deadlineAt: deadline,
  });
  expect(store.getSnapshot().playback).toBe("paused");
  expect(store.getSnapshot().error).toContain("试听片段");
  expect(bridge.AddLocalHistory).not.toHaveBeenCalled();
});

it.each([120, 90, 30])(
  "checks direct full-source metadata before playback: actual duration %s seconds",
  async (duration) => {
    class Media extends EventTarget {
      ended = false;
      error: object | null = null;
      preload = "";
      src = "";
      paused = true;
      duration = 0;
      currentTime = 0;
      volume = 1;
      load = vi.fn();
      pause = vi.fn(() => {
        this.paused = true;
      });
      play = vi.fn(async () => {
        this.paused = false;
      });
      getAttribute(name: string) {
        return name === "src" ? this.src : null;
      }
      removeAttribute(name: string) {
        if (name === "src") this.src = "";
      }
    }
    const { bridge } = setup(Promise.resolve(saved));
    const media = new Media();
    const engine = new AudioEngine(media as unknown as HTMLAudioElement);
    const store = new PlayerStore(bridge, engine);
    await store.hydrate();
    const play = store.play();
    await Promise.resolve();
    expect(media.src).toBe(stream.url);
    expect(media.play).not.toHaveBeenCalled();
    media.duration = duration;
    media.dispatchEvent(new Event("loadedmetadata"));
    await play;
    if (duration === 120) {
      expect(media.play).toHaveBeenCalledOnce();
      expect(media.currentTime).toBe(30);
      media.dispatchEvent(new Event("playing"));
      expect(store.getSnapshot().playback).toBe("playing");
    } else {
      expect(media.play).not.toHaveBeenCalled();
      expect(media.src).toBe("");
      media.dispatchEvent(new Event("playing"));
      media.currentTime = 0.6;
      media.dispatchEvent(new Event("timeupdate"));
      await store.flush();
      expect(store.getSnapshot().playback).toBe("paused");
      expect(store.getSnapshot().error).toBeTruthy();
      expect(bridge.AddLocalHistory).not.toHaveBeenCalled();
    }
    store.dispose();
  },
);

it("commits playlist queue batches atomically, enforces capacity and isolates queue from later playlist deletion", async () => {
  const { store, bridge, engine } = setup();
  await store.hydrate();
  const tracks = [track, { ...track, neteaseSongId: "2", name: "另一首" }];
  expect(store.addAll(tracks, true)).toBe(true);
  await store.flush();
  expect(bridge.SaveQueue).toHaveBeenCalledTimes(1);
  expect(store.getSnapshot().queue.currentIndex).toBe(0);
  expect(bridge.ResolveStream).toHaveBeenCalledWith(track.neteaseSongId, "exhigh", expect.any(AbortSignal));
  const firstId = store.getSnapshot().queue.currentEntryId;
  expect(new Set(store.getSnapshot().queue.entries.map((e) => e.queueEntryId)).size).toBe(2);
  expect(store.getSnapshot().queue.entries[0].track).not.toBe(tracks[0]);
  tracks[0] = { ...track, name: "修改歌单对象" };
  expect(store.getSnapshot().queue.entries[0].track.name).toBe(track.name);
  const playlist = { playlistId: "local", name: "歌单", tracks: [track], revision: 1, createdAt: 1, updatedAt: 1 };
  vi.mocked(bridge.GetLocalPlaylist).mockResolvedValueOnce(playlist);
  engine.emit("playing");
  const pauses = engine.pause.mock.calls.length;
  await store.playlists.delete("local");
  expect(engine.pause).toHaveBeenCalledTimes(pauses);
  expect(store.getSnapshot().queue.currentEntryId).toBe(firstId);
  expect(store.addAll(Array.from({ length: 998 }, () => track))).toBe(true);
  await store.flush();
  expect(bridge.SaveQueue).toHaveBeenCalledTimes(2);
  expect(store.addAll([track])).toBe(false);
  expect(store.add(track)).toBe(null);
  expect(store.getSnapshot().queue.entries).toHaveLength(1000);
  expect(bridge.SaveQueue).toHaveBeenCalledTimes(2);
});

it.each([
  ["ordinary UTF-8", "n".repeat(1024), "a".repeat(960)],
  ["Go HTML and separator escaping", "<>&\u2028\u2029".repeat(64), "a".repeat(64)],
])("rejects a valid large playlist before replacement changes playback: %s", async (_label, name, artist) => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  await store.hydrate();
  await store.play();
  engine.emit("playing");
  const before = store.getSnapshot();
  const signal = vi.mocked(bridge.ResolveStream).mock.calls[0][2]!;
  const pauses = engine.pause.mock.calls.length;
  const tracks = Array.from({ length: 1000 }, (_, i) => ({
    neteaseSongId: String(i + 1),
    name,
    artists: [{ name: artist }],
    durationMs: 1000,
  }));
  const playlist = { playlistId: "large", name: "large", tracks, revision: 1, createdAt: 1, updatedAt: 1 };
  const goBytes = (value: unknown) =>
    new TextEncoder().encode(
      JSON.stringify(value)
        .replaceAll("<", "\\u003c")
        .replaceAll(">", "\\u003e")
        .replaceAll("&", "\\u0026")
        .replaceAll("\u2028", "\\u2028")
        .replaceAll("\u2029", "\\u2029"),
    ).length;
  expect(goBytes(playlist)).toBeLessThan(2 * 1024 * 1024);
  expect(store.addAll(tracks, true)).toBe(false);
  await store.flush();
  expect(store.getSnapshot().queue).toBe(before.queue);
  expect(store.getSnapshot().queue.revision).toBe(saved.revision);
  expect(store.getSnapshot().playback).toBe("playing");
  expect(store.getSnapshot().stream).toBe(before.stream);
  expect(store.getSnapshot().lyrics).toBe(before.lyrics);
  expect(signal.aborted).toBe(false);
  expect(engine.pause).toHaveBeenCalledTimes(pauses);
  expect(bridge.SaveQueue).not.toHaveBeenCalled();
  expect(bridge.ResolveStream).toHaveBeenCalledTimes(1);
  expect(store.getSnapshot().error).toContain("容量已满");
});

it("shares the byte guard with ordinary add and ignores non-snapshot input fields", async () => {
  const tracks = Array.from({ length: 980 }, (_, i) => ({
    queueEntryId: String(i + 1).padStart(36, "0"),
    track: {
      neteaseSongId: String(i + 1),
      name: "n".repeat(1024),
      artists: [{ name: "a".repeat(960) }],
      durationMs: 1000,
    },
  }));
  const initial: QueueSnapshot = { ...saved, entries: tracks, currentEntryId: tracks[0].queueEntryId, currentIndex: 0 };
  expect(new TextEncoder().encode(JSON.stringify(initial)).length).toBeLessThan(2 * 1024 * 1024);
  const { store, bridge, engine } = setup(Promise.resolve(initial));
  await store.hydrate();
  await store.play();
  engine.emit("playing");
  const before = store.getSnapshot().queue;
  const pauses = engine.pause.mock.calls.length;
  expect(store.add({ ...track, artists: Array.from({ length: 40 }, () => ({ name: "a".repeat(1024) })) })).toBeNull();
  await store.flush();
  expect(store.getSnapshot().queue).toBe(before);
  expect(store.getSnapshot().playback).toBe("playing");
  expect(engine.pause).toHaveBeenCalledTimes(pauses);
  expect(bridge.SaveQueue).not.toHaveBeenCalled();
  const unsafe = { ...track, url: "x".repeat(3 * 1024 * 1024), cookie: "private" };
  expect(store.add(unsafe)).toBeTruthy();
  await store.flush();
  expect(bridge.SaveQueue).toHaveBeenCalledTimes(1);
  expect(JSON.stringify(vi.mocked(bridge.SaveQueue).mock.calls)).not.toContain("private");
});

it.each(["standard", "higher", "hires"])(
  "normalizes legacy or unauthorized saved %s to anonymous exhigh",
  async (quality) => {
    const { store, bridge } = setup(Promise.resolve(saved));
    vi.mocked(bridge.GetPlaybackQualityPolicy).mockResolvedValue(anonymousPolicy);
    vi.mocked(bridge.GetSettings).mockResolvedValue({
      preferredQuality: quality as LocalSettings["preferredQuality"],
      regionMode: "off",
      gdEnabled: false,
      volume: 1,
    });
    await store.hydrate();
    expect(store.getSnapshot().settings?.preferredQuality).toBe("exhigh");
    await store.play();
    expect(bridge.ResolveStream).toHaveBeenCalledWith(track.neteaseSongId, "exhigh", expect.any(AbortSignal));
  },
);
it("waits for entitlement hydration and fails closed when the policy RPC fails", async () => {
  const { store, bridge } = setup(Promise.resolve(saved));
  const policy = deferred<PlaybackQualityPolicy>();
  vi.mocked(bridge.GetPlaybackQualityPolicy).mockReturnValue(policy.promise);
  vi.mocked(bridge.GetSettings).mockResolvedValue({
    preferredQuality: "hires",
    regionMode: "off",
    gdEnabled: false,
    volume: 1,
  });
  const hydration = store.hydrate();
  await Promise.resolve();
  await store.play();
  expect(bridge.ResolveStream).not.toHaveBeenCalled();
  policy.reject(new Error("offline"));
  await hydration;
  expect(store.getSnapshot().qualityPolicy).toEqual(anonymousPolicy);
  expect(store.getSnapshot().settings?.preferredQuality).toBe("exhigh");
  await store.play();
  expect(bridge.ResolveStream).toHaveBeenCalledWith(track.neteaseSongId, "exhigh", expect.any(AbortSignal));
});
it.each(["standard", "higher", "hires", "jymaster"])(
  "rejects anonymous manual %s without persisting and permits a valid retry",
  async (quality) => {
    const { store, bridge } = setup();
    vi.mocked(bridge.GetPlaybackQualityPolicy).mockResolvedValue(anonymousPolicy);
    await store.hydrate();
    store.setSettings({ preferredQuality: quality as LocalSettings["preferredQuality"] });
    await store.flush();
    expect(bridge.SetSettings).not.toHaveBeenCalled();
    expect(store.getSnapshot().settings?.preferredQuality).toBe("exhigh");
    expect(store.getSnapshot().settingsError).toBeTruthy();
    store.setSettings({ preferredQuality: "exhigh" });
    await store.flush();
    expect(store.getSnapshot().settingsError).toBe("");
  },
);
it("accepts the server normalized settings response without keeping an optimistic premium preference", async () => {
  const { store, bridge } = setup();
  await store.hydrate();
  vi.mocked(bridge.SetSettings).mockImplementation(async (s) => ({ ...s, preferredQuality: "exhigh" }));
  store.setSettings({ preferredQuality: "hires" });
  await store.flush();
  expect(store.getSnapshot().settings?.preferredQuality).toBe("exhigh");
});

it("permits every member tier but never manually permits the automatic fallback tiers", async () => {
  const { store, bridge } = setup(Promise.resolve(saved));
  await store.hydrate();
  for (const quality of memberPolicy.selectableQualities) {
    store.setSettings({ preferredQuality: quality });
    await store.flush();
    await store.play();
    expect(bridge.ResolveStream).toHaveBeenLastCalledWith(track.neteaseSongId, quality, expect.any(AbortSignal));
  }
  const writes = vi.mocked(bridge.SetSettings).mock.calls.length;
  for (const quality of ["standard", "higher"]) {
    store.setSettings({ preferredQuality: quality as LocalSettings["preferredQuality"] });
    await store.flush();
    expect(vi.mocked(bridge.SetSettings).mock.calls).toHaveLength(writes);
    expect(store.getSnapshot().settings?.preferredQuality).toBe("jymaster");
  }
});
it("does not replace newer settings with an older normalized save response and recovers from a failed save", async () => {
  const { store, bridge } = setup();
  await store.hydrate();
  const first = deferred<LocalSettings>();
  const second = deferred<LocalSettings>();
  vi.mocked(bridge.SetSettings).mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
  store.setSettings({ preferredQuality: "lossless" });
  store.setSettings({ preferredQuality: "hires" });
  first.resolve({ preferredQuality: "exhigh", regionMode: "auto", gdEnabled: false, volume: 1 });
  await vi.waitFor(() => expect(bridge.SetSettings).toHaveBeenCalledTimes(2));
  expect(store.getSnapshot().settings?.preferredQuality).toBe("hires");
  second.reject(new Error("sqlite"));
  await store.flush();
  expect(store.getSnapshot().settingsError).toContain("设置保存失败");
  store.setSettings({ preferredQuality: "exhigh" });
  await store.flush();
  expect(store.getSnapshot().settingsError).toBe("");
  expect(store.getSnapshot().settings?.preferredQuality).toBe("exhigh");
});

const cacheToken = "a".repeat(32);
const cachedStream: ResolvedStream = {
  ...stream,
  url: `http://127.0.0.1:18993/media/${cacheToken}`,
  cacheToken,
  cacheHit: true,
};
it("releases a cache lease exactly once after pause and flush waits for release", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  const done = deferred<void>();
  bridge.ReleasePlaybackSource = vi.fn(() => done.promise);
  bridge.ReportPlaybackSourceFailure = vi.fn(async () => {});
  vi.mocked(bridge.ResolveStream).mockResolvedValue(cachedStream);
  await store.hydrate();
  await store.play();
  engine.emit("playing");
  store.pause();
  store.pause();
  store.dispose();
  let flushed = false;
  const flush = store.flush().then(() => {
    flushed = true;
  });
  await Promise.resolve();
  expect(flushed).toBe(false);
  expect(bridge.ReleasePlaybackSource).toHaveBeenCalledExactlyOnceWith(cacheToken);
  expect(bridge.ReportPlaybackSourceFailure).not.toHaveBeenCalled();
  done.resolve();
  await flush;
});
it("reports source failures without racing a separate release", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  bridge.ReleasePlaybackSource = vi.fn(async () => {});
  bridge.ReportPlaybackSourceFailure = vi.fn(async () => {});
  vi.mocked(bridge.ResolveStream).mockResolvedValue(cachedStream);
  await store.hydrate();
  await store.play();
  engine.emit("error");
  store.pause();
  await store.flush();
  expect(bridge.ReportPlaybackSourceFailure).toHaveBeenCalledExactlyOnceWith(cacheToken);
  expect(bridge.ReleasePlaybackSource).not.toHaveBeenCalled();
});
it("releases stale resolver leases without using their media and preserves snapshot privacy", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  const pending = deferred<ResolvedStream>();
  bridge.ReleasePlaybackSource = vi.fn(async () => {});
  vi.mocked(bridge.ResolveStream).mockReturnValue(pending.promise);
  await store.hydrate();
  const play = store.play();
  store.pause();
  pending.resolve(cachedStream);
  await play;
  await store.flush();
  expect(engine.setSource).not.toHaveBeenCalled();
  expect(bridge.ReleasePlaybackSource).toHaveBeenCalledExactlyOnceWith(cacheToken);
  expect(JSON.stringify(vi.mocked(bridge.SaveQueue).mock.calls)).not.toContain(cacheToken);
  expect(JSON.stringify(store.getSnapshot().queue)).not.toContain("/media/");
});
it("clearing or disabling cache leaves active playback and lease intact", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  bridge.ReleasePlaybackSource = vi.fn(async () => {});
  bridge.ClearAudioCache = vi.fn(async () => ({ removedTracks: 2, retainedActiveTracks: 1 }));
  bridge.GetAudioCacheStats = vi.fn(async () => ({
    available: true,
    enabled: true,
    maxBytes: 20 * 1024 ** 3,
    usedBytes: 30,
    cachedTracks: 1,
    activeDownloads: 0,
  }));
  vi.mocked(bridge.ResolveStream).mockResolvedValue(cachedStream);
  await store.hydrate();
  await store.play();
  engine.emit("playing");
  engine.pause.mockClear();
  await store.clearAudioCache();
  store.setSettings({ audioCache: { enabled: false, maxBytes: 20 * 1024 ** 3 } });
  await store.flush();
  expect(engine.pause).not.toHaveBeenCalled();
  expect(bridge.ReleasePlaybackSource).not.toHaveBeenCalled();
  expect(store.getSnapshot().playback).toBe("playing");
  expect(store.getSnapshot().cacheMessage).toContain("播放结束后清理");
});
it("pages history by its original cursor, keeps new and loaded entries, and blocks stale pages after clear", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  const entries = Array.from({ length: 200 }, (_, i) => ({ historyEntryId: `h${200 - i}`, track, playedAt: 200 - i }));
  const pending = deferred<{ entries: typeof entries; hasMore: boolean }>();
  bridge.ListLocalHistoryBefore = vi
    .fn()
    .mockResolvedValueOnce({ entries, hasMore: true })
    .mockReturnValueOnce(pending.promise);
  await store.hydrate();
  const more = store.loadMoreHistory();
  await store.play();
  engine.emit("playing");
  engine.currentTime = 600;
  engine.emit("timeupdate");
  await store.flush();
  expect(store.getSnapshot().history).toHaveLength(201);
  pending.resolve({ entries: [entries[199], { historyEntryId: "older", track, playedAt: 0 }], hasMore: true });
  await more;
  expect(store.getSnapshot().history).toHaveLength(202);
  expect(bridge.ListLocalHistoryBefore).toHaveBeenNthCalledWith(2, 200, 1, "h1");
  const stale = deferred<{ entries: typeof entries; hasMore: boolean }>();
  vi.mocked(bridge.ListLocalHistoryBefore).mockReturnValueOnce(stale.promise);
  const staleLoad = store.loadMoreHistory();
  store.clearHistory();
  await store.flush();
  stale.resolve({ entries, hasMore: true });
  await staleLoad;
  expect(store.getSnapshot().history).toEqual([]);
  expect(store.getSnapshot().historyHasMore).toBe(false);
});

it("cancels pending startup when cache is disabled but ignores a limit-only update", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  const pending = deferred<ResolvedStream>();
  bridge.ReleasePlaybackSource = vi.fn(async () => {});
  vi.mocked(bridge.ResolveStream).mockReturnValue(pending.promise);
  await store.hydrate();
  const play = store.play();
  store.setSettings({ audioCache: { enabled: true, maxBytes: 10 * 1024 ** 3 } });
  expect(store.getSnapshot().playback).toBe("loading");
  store.setSettings({ audioCache: { enabled: false, maxBytes: 10 * 1024 ** 3 } });
  expect(store.getSnapshot().playback).toBe("paused");
  pending.resolve(cachedStream);
  await play;
  await store.flush();
  expect(engine.setSource).not.toHaveBeenCalled();
  expect(bridge.ReleasePlaybackSource).toHaveBeenCalledExactlyOnceWith(cacheToken);
});
it("releases the previous lease before replacing its source and releases ended playback", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  const nextToken = "b".repeat(32);
  const order: string[] = [];
  bridge.ReleasePlaybackSource = vi.fn(async (token) => {
    order.push(`release:${token}`);
  });
  vi.mocked(bridge.ResolveStream)
    .mockResolvedValueOnce(cachedStream)
    .mockResolvedValue({ ...cachedStream, cacheToken: nextToken, url: `http://127.0.0.1:18993/media/${nextToken}` });
  engine.setSource.mockImplementation(async (url) => {
    order.push(`source:${url}`);
  });
  await store.hydrate();
  await store.play();
  await store.play();
  expect(order.indexOf(`release:${cacheToken}`)).toBeLessThan(
    order.indexOf(`source:http://127.0.0.1:18993/media/${nextToken}`),
  );
  engine.emit("playing");
  engine.currentTime = 120000;
  engine.emit("ended");
  await store.flush();
  expect(bridge.ReleasePlaybackSource).toHaveBeenCalledWith(nextToken);
});

it("freezes transports during close, cancels settings-waiting playback and permits only the final position save", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  await store.hydrate();
  const settings = deferred<LocalSettings>();
  vi.mocked(bridge.SetSettings).mockReturnValue(settings.promise);
  store.setSettings({ volume: 0.5 });
  const pendingPlay = store.play();
  const handlers = new Map<string, MediaSessionActionHandler | null>();
  const session = {
    setActionHandler: (action: string, handler: MediaSessionActionHandler | null) => handlers.set(action, handler),
    setPositionState: vi.fn(),
    metadata: null,
    playbackState: "none",
  } as unknown as MediaSession;
  const cleanup = bindMediaSession(store, session);
  const ack = vi.fn(async () => {});
  const freeze = vi.fn();
  const close = createCloseHandler(store, freeze, ack);
  const closing = close("token");
  expect(store.isClosing()).toBe(true);
  expect(close("token")).toBe(closing);
  settings.resolve({ preferredQuality: "exhigh", regionMode: "auto", gdEnabled: false, volume: 0.5 });
  await pendingPlay;
  await closing;
  expect(engine.setSource).not.toHaveBeenCalled();
  expect(bridge.ResolveStream).not.toHaveBeenCalled();
  const savedCalls = vi.mocked(bridge.SaveQueue).mock.calls.length;
  const settingsCalls = vi.mocked(bridge.SetSettings).mock.calls.length;
  for (const command of ["toggle-play", "next", "previous"]) playerCommand(store, command);
  for (const [action, handler] of handlers)
    handler?.({ action: action as MediaSessionAction, seekTime: 42, seekOffset: 10 });
  await store.play();
  await store.select("old-entry");
  await store.next();
  await store.previous();
  store.seek(42000);
  store.add(track);
  store.addAll([track], true);
  store.remove("old-entry");
  store.setMode("shuffle");
  store.setSettings({ volume: 1 });
  store.beginClose();
  await store.flush();
  expect(vi.mocked(bridge.SaveQueue).mock.calls.length).toBe(savedCalls);
  expect(vi.mocked(bridge.SetSettings).mock.calls.length).toBe(settingsCalls);
  expect(store.getSnapshot().queue.entries).toHaveLength(1);
  expect(store.getSnapshot().queue.positionMs).toBe(saved.positionMs);
  expect(store.getSnapshot().queue.playMode).toBe("repeat-all");
  expect(engine.seek).not.toHaveBeenCalled();
  expect(ack).toHaveBeenCalledExactlyOnceWith("token");
  expect(freeze).toHaveBeenCalledOnce();
  cleanup();
});
it("closing before hydration still saves the restored paused queue", async () => {
  const loaded = deferred<QueueSnapshot | null>();
  const { store, bridge, engine } = setup(loaded.promise);
  const close = createCloseHandler(
    store,
    () => {},
    async () => {},
  );
  const closing = close("loading-close");
  expect(store.isClosing()).toBe(true);
  loaded.resolve(saved);
  await closing;
  expect(bridge.SaveQueue).toHaveBeenCalledExactlyOnceWith(
    expect.objectContaining({ currentEntryId: "old-entry", positionMs: saved.positionMs }),
  );
  expect(engine.setSource).not.toHaveBeenCalled();
});
it("invalidates a full cached stream that ends far before its expected duration without advancing", async () => {
  const shortTrackQueue = {
    ...saved,
    entries: [{ queueEntryId: "old-entry", track: { ...track, durationMs: 30000 } }],
  };
  const { store, bridge, engine } = setup(Promise.resolve(shortTrackQueue));
  bridge.ReleasePlaybackSource = vi.fn(async () => {});
  bridge.ReportPlaybackSourceFailure = vi.fn(async () => {});
  vi.mocked(bridge.ResolveStream).mockResolvedValue({ ...cachedStream, durationMs: 30000 });
  await store.hydrate();
  await store.play();
  engine.emit("playing");
  engine.currentTime = 10000;
  const next = vi.spyOn(store, "next");
  engine.emit("ended");
  engine.emit("ended");
  store.pause();
  await store.flush();
  expect(next).not.toHaveBeenCalled();
  expect(bridge.ReportPlaybackSourceFailure).toHaveBeenCalledExactlyOnceWith(cacheToken);
  expect(bridge.ReleasePlaybackSource).not.toHaveBeenCalled();
  expect(store.getSnapshot().playback).toBe("paused");
  expect(store.getSnapshot().error).toContain("缓存音频提前结束");
});
it("preserves unknown and uncached ended behavior and tolerates natural cache endings", async () => {
  for (const resolved of [stream, { ...cachedStream, status: "unknown" as const }, cachedStream]) {
    const { store, bridge, engine } = setup(Promise.resolve(saved));
    bridge.ReportPlaybackSourceFailure = vi.fn(async () => {});
    bridge.ReleasePlaybackSource = vi.fn(async () => {});
    vi.mocked(bridge.ResolveStream).mockResolvedValue(resolved);
    await store.hydrate();
    await store.play();
    engine.emit("playing");
    engine.currentTime = resolved === cachedStream ? 118000 : 10000;
    const next = vi.spyOn(store, "next").mockResolvedValue();
    engine.emit("ended");
    await store.flush();
    expect(next).toHaveBeenCalledExactlyOnceWith(true);
    expect(bridge.ReportPlaybackSourceFailure).not.toHaveBeenCalled();
    store.dispose();
    await store.flush();
  }
});

it("ignores repeated media-session play while loading or playing and pause while already paused", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  await store.hydrate();
  const pending = deferred<ResolvedStream>();
  vi.mocked(bridge.ResolveStream).mockReturnValue(pending.promise);
  const handlers = new Map<string, MediaSessionActionHandler | null>();
  const session = {
    setActionHandler: (action: string, handler: MediaSessionActionHandler | null) => handlers.set(action, handler),
    setPositionState: vi.fn(),
    metadata: null,
    playbackState: "none",
  } as unknown as MediaSession;
  const cleanup = bindMediaSession(store, session);
  const play = () => handlers.get("play")?.({ action: "play" });
  const pause = () => handlers.get("pause")?.({ action: "pause" });
  play();
  play();
  expect(store.getSnapshot().playback).toBe("loading");
  expect(bridge.ResolveStream).toHaveBeenCalledOnce();
  pending.resolve(stream);
  await Promise.resolve();
  await Promise.resolve();
  engine.emit("playing");
  play();
  expect(bridge.ResolveStream).toHaveBeenCalledOnce();
  expect(engine.setSource).toHaveBeenCalledOnce();
  const before = vi.mocked(bridge.SaveQueue).mock.calls.length;
  pause();
  pause();
  await store.flush();
  expect(vi.mocked(bridge.SaveQueue).mock.calls.length).toBe(before + 1);
  cleanup();
});

it("preserves only ordinary established pauses, releases their lease, and resolves fresh on resume", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  bridge.ReleasePlaybackSource = vi.fn(async () => {});
  vi.mocked(bridge.ResolveStream).mockResolvedValue(cachedStream);
  await store.hydrate();
  await store.play();
  engine.emit("playing");
  engine.currentTime = 42000;
  const signal = vi.mocked(bridge.ResolveStream).mock.calls[0][2];
  engine.pause.mockClear();
  store.pause();
  store.pause();
  await store.flush();
  expect(engine.pause.mock.calls).toEqual([[true], [true]]);
  expect(signal?.aborted).toBe(true);
  expect(bridge.ReleasePlaybackSource).toHaveBeenCalledExactlyOnceWith(cacheToken);
  expect(store.getSnapshot().queue.positionMs).toBe(42000);
  await store.play();
  expect(engine.pause).toHaveBeenLastCalledWith();
  expect(bridge.ResolveStream).toHaveBeenCalledTimes(2);
  expect(engine.setSource).toHaveBeenLastCalledWith(cachedStream.url, 42000, expect.any(Object));
});
it.each(["select", "remove", "replace", "close", "dispose"])("fully resets retained media on %s", async (action) => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  await store.hydrate();
  await store.play();
  engine.emit("playing");
  store.pause();
  engine.pause.mockClear();
  vi.mocked(bridge.ResolveStream).mockResolvedValue({ ...stream, url: "", status: "unavailable" });
  if (action === "select") await store.select("old-entry");
  if (action === "remove") store.remove("old-entry");
  if (action === "replace") store.addAll([track], true);
  if (action === "close") store.beginClose();
  if (action === "dispose") store.dispose();
  expect(engine.pause).toHaveBeenCalled();
  expect(engine.pause.mock.calls.every((args) => args[0] !== true)).toBe(true);
  await store.flush();
});
it("never preserves a source when cancelling pending startup", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  const pending = deferred<ResolvedStream>();
  vi.mocked(bridge.ResolveStream).mockReturnValue(pending.promise);
  await store.hydrate();
  const play = store.play();
  store.pause();
  expect(engine.pause).toHaveBeenLastCalledWith(false);
  pending.resolve(stream);
  await play;
  expect(engine.setSource).not.toHaveBeenCalled();
});

it("preserves established media when pausing during buffering", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  bridge.ReleasePlaybackSource = vi.fn(async () => {});
  vi.mocked(bridge.ResolveStream).mockResolvedValue(cachedStream);
  await store.hydrate();
  await store.play();
  engine.emit("playing");
  engine.emit("waiting");
  expect(store.getSnapshot().playback).toBe("loading");
  engine.pause.mockClear();
  store.pause();
  await store.flush();
  expect(engine.pause).toHaveBeenCalledExactlyOnceWith(true);
  expect(store.getSnapshot().playback).toBe("paused");
  expect(bridge.ReleasePlaybackSource).toHaveBeenCalledExactlyOnceWith(cacheToken);
});

describe("SQLite library pages and lightweight membership", () => {
  it("uses all favorite IDs independently of loaded metadata and serializes rapid toggles", async () => {
    const { store, bridge } = setup();
    bridge.GetLocalFavoriteIDs = vi.fn(async () => [track.neteaseSongId]);
    bridge.SearchLocalFavorites = vi.fn(async () => ({
      tracks: [],
      hasMore: false,
      nextAddedOrder: 0,
      nextSongId: "",
    }));
    await store.hydrate();
    expect(bridge.GetLocalFavorites).not.toHaveBeenCalled();
    expect(store.getSnapshot().favorites).toEqual([]);
    expect(store.isFavorite(track)).toBe(true);
    store.toggleFavorite(track);
    store.toggleFavorite(track);
    await store.flush();
    expect(vi.mocked(bridge.SetLocalFavorite).mock.calls.map((call) => call[1])).toEqual([false, true]);
    expect(store.isFavorite(track)).toBe(true);
  });
  it("queries the whole library, rejects late queries, and uses bounded stable pages", async () => {
    const { store, bridge } = setup();
    const old = deferred<import("./contracts").FavoritePage>();
    const first = { tracks: [track], hasMore: true, nextAddedOrder: 1000, nextSongId: track.neteaseSongId };
    bridge.GetLocalFavoriteIDs = async () => [track.neteaseSongId];
    bridge.SearchLocalFavorites = vi.fn(async () => first);
    await store.hydrate();
    vi.mocked(bridge.SearchLocalFavorites).mockReturnValueOnce(old.promise);
    const stale = store.queryFavorites("中文%_");
    await Promise.resolve();
    vi.mocked(bridge.SearchLocalFavorites).mockResolvedValueOnce({ ...first, tracks: [{ ...track, name: "日本語" }] });
    await store.queryFavorites("日本語");
    old.resolve(first);
    await stale;
    expect(store.getSnapshot().favorites[0].name).toBe("日本語");
    expect(bridge.SearchLocalFavorites).toHaveBeenCalledWith("日本語", 200, 0, "", expect.any(AbortSignal));
    vi.mocked(bridge.SearchLocalFavorites).mockResolvedValueOnce({
      tracks: [{ ...track, neteaseSongId: "2" }],
      hasMore: false,
      nextAddedOrder: 0,
      nextSongId: "",
    });
    await store.navigateFavorites(1);
    expect(bridge.SearchLocalFavorites).toHaveBeenLastCalledWith(
      "日本語",
      200,
      1000,
      track.neteaseSongId,
      expect.any(AbortSignal),
    );
    expect(store.getSnapshot().favorites.map((t) => t.neteaseSongId)).toEqual(["2"]);
    await store.navigateFavorites(-1);
    expect(bridge.SearchLocalFavorites).toHaveBeenLastCalledWith("日本語", 200, 0, "", expect.any(AbortSignal));
  });
  it("blocks history query completion after clear and resets filtered writes to the first page", async () => {
    const { store, bridge, engine } = setup(Promise.resolve(saved));
    bridge.SearchLocalHistory = vi.fn(async () => ({ entries: [], hasMore: false }));
    await store.hydrate();
    const pending = deferred<import("./contracts").HistoryPage>();
    vi.mocked(bridge.SearchLocalHistory).mockReturnValueOnce(pending.promise);
    const read = store.queryHistory("歌");
    await Promise.resolve();
    store.clearHistory();
    await store.flush();
    pending.resolve({ entries: [{ historyEntryId: "stale", track, playedAt: 1 }], hasMore: true });
    await read;
    expect(store.getSnapshot().history).toEqual([]);
    await store.play();
    engine.emit("playing");
    engine.currentTime = 600;
    engine.emit("timeupdate");
    await store.flush();
    await Promise.resolve();
    expect(bridge.SearchLocalHistory).toHaveBeenLastCalledWith("歌", 200, 0, "", expect.any(AbortSignal));
  });
});

describe("local search keyword write ordering", () => {
  it("serializes record/delete/clear and ignores successes from before deletion", async () => {
    const { store, bridge } = setup();
    let entries = [{ query: "旧词", searchedAt: 1 }];
    const pending = deferred<void>();
    const order: string[] = [];
    bridge.ListSearchHistory = vi.fn(async () => [...entries]);
    bridge.RecordSearchQuery = vi.fn(async (query) => {
      order.push("record");
      await pending.promise;
      entries.unshift({ query, searchedAt: 2 });
    });
    bridge.DeleteSearchQuery = vi.fn(async (query) => {
      order.push("delete");
      entries = entries.filter((e) => e.query !== query);
    });
    bridge.ClearSearchHistory = vi.fn(async () => {
      order.push("clear");
      entries = [];
    });
    await store.hydrate();
    expect(bridge.ListSearchHistory).toHaveBeenCalledWith(20);
    const gen = store.getSearchHistoryGeneration();
    store.recordSearchQuery("新词", gen);
    await Promise.resolve();
    await Promise.resolve();
    store.deleteSearchQuery("旧词");
    store.clearSearchHistory();
    store.recordSearchQuery("迟到搜索", gen);
    pending.resolve();
    await store.flush();
    expect(order).toEqual(["record", "delete", "clear"]);
    expect(store.getSnapshot().searchHistory).toEqual([]);
    expect(bridge.RecordSearchQuery).toHaveBeenCalledTimes(1);
  });
  it("flushes accepted records during close and refuses new writes after close freeze", async () => {
    const { store, bridge } = setup();
    const pending = deferred<void>();
    bridge.RecordSearchQuery = vi.fn(() => pending.promise);
    bridge.ListSearchHistory = async () => [];
    await store.hydrate();
    store.recordSearchQuery("成功搜索", store.getSearchHistoryGeneration());
    store.beginClose();
    let flushed = false;
    const flush = store.flush().then(() => {
      flushed = true;
    });
    await Promise.resolve();
    await Promise.resolve();
    expect(flushed).toBe(false);
    store.recordSearchQuery("退出后的响应", store.getSearchHistoryGeneration());
    pending.resolve();
    await flush;
    expect(bridge.RecordSearchQuery).toHaveBeenCalledExactlyOnceWith("成功搜索");
  });
});

it("coalesces cover resolution without mutating persisted metadata and evicts after clear/policy changes", async () => {
  const { store, bridge } = setup();
  const original = "https://p1.music.126.net/cover.jpg";
  const artwork = `/artwork/${"a".repeat(64)}`;
  const pending = deferred<string>();
  bridge.ResolveCover = vi.fn(() => pending.promise);
  bridge.ClearResourceCache = async () => ({ removedCovers: 1, removedLyrics: 2 });
  await store.hydrate();
  const first = store.resolveCover(original);
  const second = store.resolveCover(original);
  pending.resolve(artwork);
  expect(await first).toBe(artwork);
  expect(await second).toBe(artwork);
  expect(bridge.ResolveCover).toHaveBeenCalledTimes(1);
  store.add({ ...track, coverRef: original });
  await store.flush();
  expect(vi.mocked(bridge.SaveQueue).mock.calls.at(-1)![0].entries[0].track.coverRef).toBe(original);
  await store.clearResourceCache();
  await store.resolveCover(original);
  expect(bridge.ResolveCover).toHaveBeenCalledTimes(2);
  store.setSettings({ resourceCache: { enabled: false, maxBytes: 1024 ** 3 } });
  await store.resolveCover(original);
  expect(bridge.ResolveCover).toHaveBeenCalledTimes(3);
  expect(await store.resolveCover("https://evil.example/cover.jpg")).toBe("/default_cover.svg");
  vi.mocked(bridge.ResolveCover).mockRejectedValueOnce(new Error("offline"));
  expect(await store.resolveCover("https://p1.music.126.net/other.jpg")).toBe("https://p1.music.126.net/other.jpg");
});

it("drops runtime artwork references from snapshot persistence", async () => {
  const { store, bridge } = setup();
  await store.hydrate();
  store.add({ ...track, coverRef: `/artwork/${"a".repeat(64)}` });
  await store.flush();
  expect(vi.mocked(bridge.SaveQueue).mock.calls.at(-1)![0].entries[0].track.coverRef).toBeUndefined();
});

it("resumes a cancelled page when its library view returns", async () => {
  const { store, bridge } = setup();
  bridge.GetLocalFavoriteIDs = async () => [];
  bridge.SearchLocalFavorites = vi.fn(async () => ({ tracks: [], hasMore: false, nextAddedOrder: 0, nextSongId: "" }));
  await store.hydrate();
  const pending = deferred<import("./contracts").FavoritePage>();
  vi.mocked(bridge.SearchLocalFavorites).mockReturnValueOnce(pending.promise);
  const read = store.queryFavorites("歌");
  await Promise.resolve();
  store.cancelLibraryQuery("favorites");
  pending.resolve({ tracks: [track], hasMore: false, nextAddedOrder: 0, nextSongId: "" });
  await read;
  expect(store.getSnapshot().favorites).toEqual([]);
  vi.mocked(bridge.SearchLocalFavorites).mockResolvedValueOnce({
    tracks: [track],
    hasMore: false,
    nextAddedOrder: 0,
    nextSongId: "",
  });
  store.resumeLibraryQuery("favorites");
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
  expect(store.getSnapshot().favorites).toEqual([track]);
});

it("recovers an LRU-evicted artwork once for shared consumers and keeps runtime URLs out of snapshots", async () => {
  const { store, bridge } = setup();
  const original = "https://p1.music.126.net/cover.jpg";
  const artwork = `/artwork/${"a".repeat(64)}`;
  const refreshed = `/artwork/${"b".repeat(64)}`;
  const pending = deferred<string>();
  bridge.ResolveCover = vi.fn().mockResolvedValueOnce(artwork).mockReturnValueOnce(pending.promise);
  await store.hydrate();
  expect(await store.resolveCover(original)).toBe(artwork);
  const first = store.recoverCover(original, artwork);
  const second = store.recoverCover(original, artwork);
  pending.resolve(refreshed);
  expect(await first).toBe(refreshed);
  expect(await second).toBe(refreshed);
  expect(bridge.ResolveCover).toHaveBeenCalledTimes(2);
  expect(await store.recoverCover(original, artwork)).toBe(refreshed);
  expect(bridge.ResolveCover).toHaveBeenCalledTimes(2);
  store.add({ ...track, coverRef: original });
  store.toggleFavorite({ ...track, coverRef: original });
  await store.flush();
  expect(JSON.stringify(vi.mocked(bridge.SaveQueue).mock.calls)).not.toContain("/artwork/");
  expect(JSON.stringify(vi.mocked(bridge.SetLocalFavorite).mock.calls)).not.toContain("/artwork/");
  store.invalidateCover(original);
  vi.mocked(bridge.ResolveCover).mockRejectedValueOnce(new Error("offline"));
  expect(await store.resolveCover(original)).toBe(original);
});

it("validates search inputs using UTF-8 bytes before calling SQLite", async () => {
  const { store, bridge } = setup();
  bridge.GetLocalFavoriteIDs = async () => [];
  bridge.SearchLocalFavorites = vi.fn(async () => ({ tracks: [], hasMore: false, nextAddedOrder: 0, nextSongId: "" }));
  bridge.RecordSearchQuery = vi.fn(async () => {});
  await store.hydrate();
  await store.queryFavorites("a".repeat(512));
  expect(bridge.SearchLocalFavorites).toHaveBeenLastCalledWith("a".repeat(512), 200, 0, "", expect.any(AbortSignal));
  const count = vi.mocked(bridge.SearchLocalFavorites).mock.calls.length;
  await store.queryFavorites("中".repeat(171));
  expect(bridge.SearchLocalFavorites).toHaveBeenCalledTimes(count);
  expect(store.getSnapshot().libraryError).toContain("512");
  const gen = store.getSearchHistoryGeneration();
  store.recordSearchQuery("😀".repeat(128), gen);
  store.recordSearchQuery("😀".repeat(129), gen);
  await store.flush();
  expect(bridge.RecordSearchQuery).toHaveBeenCalledExactlyOnceWith("😀".repeat(128));
});

it("can recover the same deterministic artwork hash after a later independent eviction", async () => {
  const { store, bridge } = setup();
  const original = "https://p1.music.126.net/cover.jpg";
  const artwork = `/artwork/${"a".repeat(64)}`;
  bridge.ResolveCover = vi.fn(async () => artwork);
  await store.hydrate();
  await store.resolveCover(original);
  await store.recoverCover(original, artwork);
  await store.recoverCover(original, artwork);
  expect(bridge.ResolveCover).toHaveBeenCalledTimes(3);
});

it("freezes page-three history navigation and query changes while clear is pending", async () => {
  const { store, bridge } = setup();
  bridge.SearchLocalHistory = vi.fn(async (_query, _limit, playedAt) => ({
    entries: [{ historyEntryId: `h${playedAt || 300}`, track, playedAt: (playedAt || 400) - 100 }],
    hasMore: true,
  }));
  const clear = deferred<void>();
  bridge.ClearLocalHistory = vi.fn(() => clear.promise);
  await store.hydrate();
  await store.navigateHistory(1);
  await store.navigateHistory(1);
  expect(store.getSnapshot().historyPage).toBe(3);
  const reads = vi.mocked(bridge.SearchLocalHistory).mock.calls.length;
  store.clearHistory();
  await Promise.resolve();
  await Promise.resolve();
  expect(store.getSnapshot()).toMatchObject({
    historyPage: 1,
    historyLoading: true,
    historyClearing: true,
    historyHasMore: false,
  });
  await store.navigateHistory(-1);
  await store.navigateHistory(1);
  await store.queryHistory("清空期间查询");
  store.clearHistory();
  expect(bridge.SearchLocalHistory).toHaveBeenCalledTimes(reads);
  expect(bridge.ClearLocalHistory).toHaveBeenCalledTimes(1);
  expect(store.getSnapshot().historyQuery).toBe("");
  clear.resolve();
  await store.flush();
  expect(store.getSnapshot()).toMatchObject({
    history: [],
    historyPage: 1,
    historyLoading: false,
    historyClearing: false,
    historyHasMore: false,
    libraryError: "",
  });
  await store.navigateHistory(-1);
  expect(bridge.SearchLocalHistory).toHaveBeenCalledTimes(reads);
  await store.queryHistory("清空后查询");
  expect(bridge.SearchLocalHistory).toHaveBeenLastCalledWith("清空后查询", 200, 0, "", expect.any(AbortSignal));
});

it("restores the previous stable history page if clearing fails", async () => {
  const { store, bridge } = setup();
  bridge.SearchLocalHistory = vi.fn(async (_query, _limit, playedAt) => ({
    entries: [{ historyEntryId: `h${playedAt || 300}`, track, playedAt: (playedAt || 400) - 100 }],
    hasMore: true,
  }));
  bridge.ClearLocalHistory = vi.fn(async () => {
    throw new Error("storage unavailable");
  });
  await store.hydrate();
  await store.navigateHistory(1);
  await store.navigateHistory(1);
  const previous = store.getSnapshot().history;
  store.clearHistory();
  await store.flush();
  expect(store.getSnapshot()).toMatchObject({
    history: previous,
    historyPage: 3,
    historyHasMore: true,
    historyLoading: false,
    historyClearing: false,
  });
  expect(store.getSnapshot().libraryError).toContain("清除失败");
  await store.navigateHistory(-1);
  expect(store.getSnapshot().historyPage).toBe(2);
  expect(store.getSnapshot().libraryError).toBe("");
});

it("retries all library hydration before enabling writes", async () => {
  const { store, bridge } = setup();
  const ids = (bridge.GetLocalFavoriteIDs = vi
    .fn()
    .mockRejectedValueOnce(new Error("offline"))
    .mockResolvedValue([track.neteaseSongId]));
  await store.hydrate();
  expect(store.getSnapshot().libraryReady).toBe(false);
  store.toggleFavorite(track);
  await store.flush();
  expect(bridge.SetLocalFavorite).not.toHaveBeenCalled();
  await store.retryLibrary("favorites");
  expect(ids).toHaveBeenCalledTimes(2);
  expect(store.getSnapshot().libraryReady).toBe(true);
  expect(store.isFavorite(track)).toBe(true);
  expect(store.getSnapshot().libraryError).toBe("");
});
it("favorite writes retain filtered page and rendered rows while refreshing its cursor", async () => {
  const { store, bridge } = setup();
  bridge.GetLocalFavoriteIDs = vi.fn(async () => [track.neteaseSongId]);
  bridge.SearchLocalFavorites = vi.fn(async (_query, _limit, order) => ({
    tracks: [track],
    hasMore: true,
    nextAddedOrder: order + 1,
    nextSongId: track.neteaseSongId,
  }));
  await store.hydrate();
  await store.queryFavorites("test");
  await store.navigateFavorites(1);
  const pages: number[] = [],
    counts: number[] = [];
  const stop = store.subscribe(() => {
    pages.push(store.getSnapshot().favoritesPage);
    counts.push(store.getSnapshot().favorites.length);
  });
  store.toggleFavorite({ ...track, neteaseSongId: "999" });
  await store.flush();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(pages.every((page) => page === 2)).toBe(true);
  expect(counts.every((count) => count > 0)).toBe(true);
  expect(vi.mocked(bridge.SearchLocalFavorites).mock.calls.at(-1)?.slice(0, 4)).toEqual([
    "test",
    200,
    1,
    track.neteaseSongId,
  ]);
  stop();
});
it("coalesces slider seeks, flushes the old track on switch and keeps revision order", async () => {
  vi.useFakeTimers();
  try {
    const { store, bridge, engine } = setup(Promise.resolve(saved));
    await store.hydrate();
    await store.play();
    engine.emit("playing");
    for (let i = 1; i <= 50; i++) store.seek(i * 1000);
    await Promise.resolve();
    await Promise.resolve();
    expect(engine.seek).toHaveBeenCalledTimes(50);
    expect(bridge.SaveQueue).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(250);
    expect(bridge.SaveQueue).toHaveBeenCalledTimes(1);
    expect(vi.mocked(bridge.SaveQueue).mock.calls[0][0]).toMatchObject({
      currentEntryId: "old-entry",
      positionMs: 50000,
      revision: 9,
    });
    store.seek(51000);
    store.add(track);
    const other = store.getSnapshot().queue.entries[1].queueEntryId;
    await store.select(other);
    await store.flush();
    const calls = vi.mocked(bridge.SaveQueue).mock.calls.map(([q]) => q);
    expect(calls.some((q) => q.currentEntryId === "old-entry" && q.positionMs === 51000)).toBe(true);
    expect(calls.at(-1)?.currentEntryId).toBe(other);
    expect(calls.map((q) => q.revision)).toEqual([...new Set(calls.map((q) => q.revision))].sort((a, b) => a - b));
    store.dispose();
  } finally {
    vi.useRealTimers();
  }
});

it("recording listened audio keeps the history filter, page and rows", async () => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  bridge.SearchLocalHistory = vi.fn(async (_q, _n, before) => ({
    entries: [{ historyEntryId: before ? "older" : "first", track, playedAt: before ? 50 : 100 }],
    hasMore: true,
  }));
  await store.hydrate();
  await store.queryHistory("filter");
  await store.navigateHistory(1);
  const snapshots: ReturnType<PlayerStore["getSnapshot"]>[] = [];
  const stop = store.subscribe(() => snapshots.push(store.getSnapshot()));
  await store.play();
  engine.emit("playing");
  engine.currentTime = 600;
  engine.emit("timeupdate");
  await store.flush();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(bridge.AddLocalHistory).toHaveBeenCalledOnce();
  expect(
    snapshots.every(
      (s) => s.historyPage === 2 && s.historyQuery === "filter" && s.history[0]?.historyEntryId === "older",
    ),
  ).toBe(true);
  expect(vi.mocked(bridge.SearchLocalHistory).mock.calls.at(-1)?.slice(0, 4)).toEqual(["filter", 200, 100, "first"]);
  stop();
  store.dispose();
});
it("deleting the final favorite on a page backs up to a valid cursor without clearing rows", async () => {
  const { store, bridge } = setup();
  let removed = false;
  bridge.GetLocalFavoriteIDs = vi.fn(async () => [track.neteaseSongId]);
  bridge.SetLocalFavorite = vi.fn(async () => {
    removed = true;
  });
  bridge.SearchLocalFavorites = vi.fn(async (_q, _n, order) => ({
    tracks: removed && order ? [] : [track],
    hasMore: !removed,
    nextAddedOrder: 1,
    nextSongId: track.neteaseSongId,
  }));
  await store.hydrate();
  await store.navigateFavorites(1);
  const counts: number[] = [];
  const stop = store.subscribe(() => counts.push(store.getSnapshot().favorites.length));
  store.toggleFavorite(track);
  await store.flush();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(store.getSnapshot()).toMatchObject({ favoritesPage: 1, favoritesHasMore: false });
  expect(counts.every((n) => n === 1)).toBe(true);
  expect(
    vi
      .mocked(bridge.SearchLocalFavorites)
      .mock.calls.slice(-2)
      .map((c) => c[2]),
  ).toEqual([1, 0]);
  stop();
});
it.each(["pause", "beginClose"] as const)("%s flushes the latest seek before its trailing timer", async (command) => {
  vi.useFakeTimers();
  try {
    const { store, bridge, engine } = setup(Promise.resolve(saved));
    await store.hydrate();
    await store.play();
    engine.emit("playing");
    store.seek(45678);
    store[command]();
    await store.flush();
    expect(vi.mocked(bridge.SaveQueue).mock.calls.at(-1)?.[0]).toMatchObject({
      currentEntryId: "old-entry",
      positionMs: 45678,
    });
    const count = vi.mocked(bridge.SaveQueue).mock.calls.length;
    await vi.advanceTimersByTimeAsync(1000);
    expect(bridge.SaveQueue).toHaveBeenCalledTimes(count);
    store.dispose();
  } finally {
    vi.useRealTimers();
  }
});

it.each(["ids", "favorites", "history"] as const)(
  "keeps library writes disabled until every retried %s read succeeds",
  async (failed) => {
    const { store, bridge } = setup();
    const ids = (bridge.GetLocalFavoriteIDs = vi.fn(async () => [track.neteaseSongId]));
    const favorites = (bridge.SearchLocalFavorites = vi.fn(async () => ({
      tracks: [track],
      hasMore: false,
      nextAddedOrder: 0,
      nextSongId: "",
    })));
    const history = (bridge.SearchLocalHistory = vi.fn(async () => ({
      entries: [{ historyEntryId: "prior", track, playedAt: 1 }],
      hasMore: false,
    })));
    const failing = { ids, favorites, history }[failed];
    failing.mockRejectedValueOnce(new Error("unavailable")).mockRejectedValueOnce(new Error("still unavailable"));
    await store.hydrate();
    await store.retryLibrary("history");
    expect(store.getSnapshot().libraryReady).toBe(false);
    expect(store.getSnapshot().libraryError).not.toBe("");
    if (failed !== "ids") expect(ids).not.toHaveBeenCalled();
    store.toggleFavorite(track);
    store.clearHistory();
    await store.flush();
    expect(bridge.SetLocalFavorite).not.toHaveBeenCalled();
    expect(bridge.ClearLocalHistory).not.toHaveBeenCalled();
    await store.retryLibrary("history");
    expect(store.getSnapshot()).toMatchObject({ libraryReady: true, libraryError: "", favorites: [track] });
    expect(store.getSnapshot().history[0].historyEntryId).toBe("prior");
    expect(store.isFavorite(track)).toBe(true);
    expect(ids).toHaveBeenCalledTimes(failed === "ids" ? 3 : 1);
    expect(favorites).toHaveBeenCalledTimes(3);
    expect(history).toHaveBeenCalledTimes(3);
  },
);

it.each([
  ["GD_BUDGET_EXHAUSTED", "GD 备用音源解析时间已用尽，请稍后重试。"],
  ["GD_RATE_LIMITED", "GD 备用音源请求过于频繁，请稍后重试。"],
])("translates a rejected %s resolution into a fixed user message", async (code, message) => {
  const { store, bridge, engine } = setup(Promise.resolve(saved));
  vi.mocked(bridge.ResolveStream).mockRejectedValueOnce(new Error(code));
  await store.hydrate();
  await store.play();
  expect(store.getSnapshot()).toMatchObject({ playback: "paused", error: message });
  expect(engine.setSource).not.toHaveBeenCalled();
});
it("does not expose arbitrary or code-prefixed Wails rejection payloads", async () => {
  for (const raw of [
    "GD_BUDGET_EXHAUSTED https://secret.example/?token=private",
    "cookies=private",
    "GD_RATE_LIMITED: private",
  ]) {
    const { store, bridge } = setup(Promise.resolve(saved));
    vi.mocked(bridge.ResolveStream).mockRejectedValueOnce(new Error(raw));
    await store.hydrate();
    await store.play();
    expect(store.getSnapshot().error).toBe("播放失败，请重试或选择其他歌曲。");
    expect(store.getSnapshot().error).not.toContain(raw);
  }
});

it("global region polling never requests settings-only cache statistics", async () => {
  const { store, bridge } = setup();
  bridge.GetAudioCacheStats = vi.fn();
  bridge.GetResourceCacheStats = vi.fn();
  await store.hydrate();
  await store.pollRegion();
  await store.pollRegion();
  expect(bridge.GetAudioCacheStats).not.toHaveBeenCalled();
  expect(bridge.GetResourceCacheStats).not.toHaveBeenCalled();
});

it("settings exit and reentry serialize actual cache reads and discard retired store updates", async () => {
  vi.useFakeTimers();
  const { store, bridge } = setup();
  const audio = (usedBytes: number) => ({
    available: true,
    enabled: true,
    maxBytes: 1000,
    usedBytes,
    cachedTracks: 1,
    activeDownloads: 0,
  });
  const resources = (usedBytes: number) => ({
    available: true,
    enabled: true,
    maxBytes: 1000,
    usedBytes,
    cachedCovers: 1,
    cachedLyrics: 1,
  });
  const oldAudio = deferred<ReturnType<typeof audio>>();
  const oldResources = deferred<ReturnType<typeof resources>>();
  const nextAudio = deferred<ReturnType<typeof audio>>();
  const nextResources = deferred<ReturnType<typeof resources>>();
  bridge.GetAudioCacheStats = vi.fn().mockReturnValueOnce(oldAudio.promise).mockReturnValueOnce(nextAudio.promise);
  bridge.GetResourceCacheStats = vi
    .fn()
    .mockReturnValueOnce(oldResources.promise)
    .mockReturnValueOnce(nextResources.promise);
  let stop = () => {};
  try {
    stop = startSettingsCachePolling(store);
    stop();
    stop = startSettingsCachePolling(store);
    expect(bridge.GetAudioCacheStats).toHaveBeenCalledTimes(1);
    expect(bridge.GetResourceCacheStats).toHaveBeenCalledTimes(1);
    oldAudio.resolve(audio(100));
    oldResources.resolve(resources(100));
    await vi.advanceTimersByTimeAsync(0);
    expect(store.getSnapshot().cacheStats).toBeNull();
    expect(store.getSnapshot().resourceCacheStats).toBeNull();
    expect(bridge.GetAudioCacheStats).toHaveBeenCalledTimes(2);
    expect(bridge.GetResourceCacheStats).toHaveBeenCalledTimes(2);
    stop();
    nextAudio.resolve(audio(200));
    nextResources.reject(new Error("late failure"));
    await vi.advanceTimersByTimeAsync(60_000);
    expect(store.getSnapshot().cacheStats).toBeNull();
    expect(store.getSnapshot().resourceCacheStats).toBeNull();
    expect(bridge.GetAudioCacheStats).toHaveBeenCalledTimes(2);
    expect(vi.getTimerCount()).toBe(0);
  } finally {
    stop();
    vi.useRealTimers();
  }
});

it("newer manual cache refreshes survive stale in-flight settings results", async () => {
  const { store, bridge } = setup();
  const audio = (usedBytes: number) => ({
    available: true,
    enabled: true,
    maxBytes: 1000,
    usedBytes,
    cachedTracks: 1,
    activeDownloads: 0,
  });
  const resources = (usedBytes: number) => ({
    available: true,
    enabled: true,
    maxBytes: 1000,
    usedBytes,
    cachedCovers: 1,
    cachedLyrics: 1,
  });
  const oldAudio = deferred<ReturnType<typeof audio>>();
  const oldResources = deferred<ReturnType<typeof resources>>();
  bridge.GetAudioCacheStats = vi.fn().mockReturnValueOnce(oldAudio.promise).mockResolvedValue(audio(200));
  bridge.GetResourceCacheStats = vi.fn().mockReturnValueOnce(oldResources.promise).mockResolvedValue(resources(200));
  const stop = startSettingsCachePolling(store);
  try {
    await store.refreshCacheStats();
    await store.refreshResourceCacheStats();
    oldAudio.resolve(audio(100));
    oldResources.reject(new Error("old failure"));
    await Promise.all([oldAudio.promise, oldResources.promise.catch(() => {})]);
    await Promise.resolve();
    expect(store.getSnapshot().cacheStats?.usedBytes).toBe(200);
    expect(store.getSnapshot().resourceCacheStats?.usedBytes).toBe(200);
  } finally {
    stop();
  }
});

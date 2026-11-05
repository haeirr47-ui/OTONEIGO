// biome-ignore-all lint/correctness/noComponentHookFactories: this fixture names mock bridge methods after Wails APIs, not React components.
import { createRoot, type Root } from "react-dom/client";
import { renderToString } from "react-dom/server";
import type { AudioExecutor, EngineEvent } from "./AudioEngine";
import type { DesktopBridge, TrackSnapshot } from "./contracts";
import { PlayerStore } from "./PlayerStore";
import { PlayerProvider, usePlayer, usePlayerSelector, usePlayerStore, shallowEqual } from "./PlayerContext";
import { TrackCard } from "./OtoneiPresentation";
import { App as ProductApp } from "./App";

declare global {
  interface Window {
    __playerSelectorAcceptance?: { run: () => Promise<void>; dispose: () => void };
    __selectorProductRenders: {
      App: number;
      OtoneiPlayer: number;
      TrackCard: number;
      Cover: number;
      FavoriteButton: number;
    };
  }
}

const tracks: TrackSnapshot[] = [
  { neteaseSongId: "track-A", name: "Track A", artists: [{ name: "Artist" }], durationMs: 180000 },
  { neteaseSongId: "track-B", name: "Track B", artists: [{ name: "Artist" }], durationMs: 180000 },
];
const emptyPage = { tracks: [] as TrackSnapshot[], hasMore: false, nextAddedOrder: 0, nextSongId: "" };
function fixture() {
  let rejectFavorite = false;
  const metrics = { queueSaves: 0 };
  const bridge = new Proxy(
    {
      LoadQueue: async () => ({
        entries: [
          { queueEntryId: "entry-A", track: tracks[0] },
          { queueEntryId: "entry-B", track: tracks[1] },
        ],
        currentEntryId: "entry-A",
        currentIndex: 0,
        playMode: "repeat-all",
        positionMs: 0,
        revision: 0,
      }),
      GetSettings: async () => ({ preferredQuality: "exhigh", regionMode: "auto", gdEnabled: false, volume: 0.5 }),
      GetPlaybackQualityPolicy: async () => ({
        premiumAllowed: false,
        selectableQualities: ["exhigh"],
        maxBitrate: 320000,
      }),
      GetCapabilities: async () => ({ regionCompatibility: true, gd: false, account: false }),
      GetRegionStatus: async () => ({
        classification: "unknown",
        observedAt: 0,
        networkGeneration: 0,
        detecting: false,
      }),
      RefreshRegion: async () => ({ classification: "unknown", observedAt: 0, networkGeneration: 0, detecting: false }),
      ListLocalPlaylists: async () => [],
      GetLocalFavorites: async () => [],
      GetLocalFavoriteIDs: async () => [],
      GetLocalHistory: async () => [],
      ListLocalHistory: async () => [],
      SearchLocalFavorites: async () => emptyPage,
      SearchLocalHistory: async () => ({ entries: [], hasMore: false, nextPlayedAt: 0, nextHistoryEntryId: "" }),
      SearchNetease: async () => ({ tracks, total: tracks.length, hasMore: false }),
      GetSearchHistory: async () => [],
      SetSettings: async (value: unknown) => value,
      ResolveStream: async () => ({
        songId: "track-A",
        provider: "netease-direct",
        url: "https://media.example/audio",
        requestedQuality: "exhigh",
        status: "full",
      }),
      SaveQueue: async () => {
        metrics.queueSaves++;
      },
      SetLocalFavorite: async () => {
        if (rejectFavorite) throw new Error("write rejected");
      },
      setRejectFavorite: (value: boolean) => {
        rejectFavorite = value;
      },
    } as Record<string, unknown>,
    {
      get(target, key: string) {
        if (key in target) return target[key];
        return async () => undefined;
      },
    },
  ) as unknown as DesktopBridge & { setRejectFavorite: (value: boolean) => void };
  const handlers = new Map<EngineEvent, () => void>();
  const engine: AudioExecutor & { currentTime: number } = {
    currentTime: 0,
    duration: 180000,
    on(event, fn) {
      handlers.set(event, fn);
      return () => handlers.delete(event);
    },
    setSource: async () => {},
    pause: () => {},
    seek: () => {},
    setVolume: () => {},
  };
  return { store: new PlayerStore(bridge, engine), bridge, handlers, engine, metrics };
}

const counts: Record<string, number> = {};
function count(key: string) {
  counts[key] = (counts[key] ?? 0) + 1;
}
function FavoriteView({ id }: { id: string }) {
  const favorite = usePlayerSelector((s) => s.favoriteIDs.has(id));
  count(`favorite:${id}`);
  return <div data-favorite-id={id} data-favorite={String(favorite)} />;
}
function ProgressOnly() {
  const value = usePlayerSelector((s) => s.queue.positionMs);
  count("progress-only");
  return <span data-testid="progress-position">{value}</span>;
}
function CompositeOnly() {
  const value = usePlayerSelector(
    (s) => ({ hydration: s.hydration, settingsReady: Boolean(s.settings) }),
    shallowEqual,
  );
  count("composite");
  return (
    <span>
      {value.hydration}
      {String(value.settingsReady)}
    </span>
  );
}
function LegacyOnly() {
  const { state } = usePlayer();
  count("legacy");
  return <span>{state.playback}</span>;
}
function Actions({ track }: { track: TrackSnapshot }) {
  const store = usePlayerStore();
  return (
    <>
      <button type="button" onClick={() => store.toggleFavorite(track)}>
        toggle
      </button>
      <button type="button" onClick={() => store.seek(4321)}>
        seek
      </button>
    </>
  );
}
const callbackIDs = { play: [] as string[], add: [] as string[] };
function ProbeTree({ id }: { id: string }) {
  const track = tracks.find((item) => item.neteaseSongId === id) ?? tracks[0];
  return (
    <>
      <FavoriteView id={id} />
      <FavoriteView id="track-B" />
      <ProgressOnly />
      <CompositeOnly />
      <LegacyOnly />
      <Actions track={tracks[0]} />
      <div data-testid="probe-track-card">
        <TrackCard
          track={track}
          onPlay={(selected) => callbackIDs.play.push(selected.neteaseSongId)}
          onAdd={(selected) => callbackIDs.add.push(selected.neteaseSongId)}
          disabled={false}
        />
      </div>
    </>
  );
}

let root: Root | undefined;
let mounted: ReturnType<typeof fixture> | undefined;
function assert(condition: unknown, message: string) {
  if (!condition) throw new Error(message);
}
function required<T>(value: T | null | undefined, message: string): T {
  if (value == null) throw new Error(message);
  return value;
}
function resetCounts() {
  for (const key of Object.keys(counts)) delete counts[key];
}
const tick = () => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));

async function run() {
  const report: string[] = [];
  const check = (condition: unknown, message: string) => {
    assert(condition, message);
    report.push(message);
  };
  mounted = fixture();
  const original = mounted;
  root = createRoot(required(document.getElementById("root"), "acceptance root element missing"));
  root.render(
    <PlayerProvider store={original.store}>
      <ProductApp bridge={original.bridge} />
      <ProbeTree id="track-A" />
    </PlayerProvider>,
  );
  await original.store.hydrate();
  await tick();
  resetCounts();
  const input = required(
    document.querySelector<HTMLInputElement>('input[aria-label="搜索歌曲或歌手"]'),
    "product search input missing",
  );
  const setter = required(
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set,
    "native input value setter missing",
  );
  setter.call(input, "test");
  input.dispatchEvent(new Event("input", { bubbles: true }));
  input.dispatchEvent(new Event("change", { bubbles: true }));
  await tick();
  required(document.querySelector<HTMLButtonElement>("button.search-submit"), "product search button missing").click();
  await new Promise((resolve) => setTimeout(resolve, 30));
  await tick();
  assert(
    document.querySelectorAll(".music-list > .music-card").length === 2,
    "App search rendered two real TrackCard rows",
  );
  for (const name of ["App", "OtoneiPlayer", "TrackCard", "Cover", "FavoriteButton"] as const) {
    assert(window.__selectorProductRenders[name] > 0, `render instrumentation did not observe ${name}`);
    report.push(`render instrumentation observed ${name}`);
  }
  resetCounts();
  window.__selectorProductRenders.App = 0;
  window.__selectorProductRenders.OtoneiPlayer = 0;
  window.__selectorProductRenders.TrackCard = 0;
  window.__selectorProductRenders.FavoriteButton = 0;
  window.__selectorProductRenders.Cover = 0;
  check(
    document.querySelector(".music-card.is-active h6")?.textContent === "Track A",
    "TrackCard highlights the current queue item",
  );

  original.store.seek(1200);
  await tick();
  check(counts["progress-only"] === 1, `progress selector renders once for seek update (${JSON.stringify(counts)})`);
  check(
    !counts["favorite:track-A"] && !counts["favorite:track-B"],
    "progress leaves per-track favorite consumers unchanged",
  );
  check(counts.legacy > 0, `compatibility usePlayer rerenders on store updates (${JSON.stringify(counts)})`);
  check(!counts.composite, "shallow equal composite selector ignores unrelated progress changes");
  check(window.__selectorProductRenders.App === 0, "App does not rerender for a queue position update");
  check(
    window.__selectorProductRenders.TrackCard === 0,
    "TrackCard does not rerender when current track identity stays the same",
  );
  window.__selectorProductRenders.OtoneiPlayer = 0;
  const savesBeforeSeekFlush = original.metrics.queueSaves;
  await new Promise((resolve) => setTimeout(resolve, 275));
  await original.store.flush();
  await tick();
  check(original.metrics.queueSaves > savesBeforeSeekFlush, "seek debounce reached SaveQueue");
  check(
    window.__selectorProductRenders.OtoneiPlayer === 0,
    "OtoneiPlayer does not rerender when a seek flush saves the same queue rows",
  );
  check(
    document.querySelector(".music-card.is-active h6")?.textContent === "Track A",
    "seek flush preserves current TrackCard highlight",
  );

  resetCounts();
  await original.store.toggleFavorite(tracks[0]);
  await new Promise((resolve) => setTimeout(resolve, 20));
  await tick();
  check(
    document.querySelector('[data-favorite-id="track-A"]')?.getAttribute("data-favorite") === "true",
    "favorite state reaches visible track consumer",
  );
  check(
    document.querySelector('[aria-label="取消收藏：Track A"]')?.getAttribute("aria-pressed") === "true",
    "TrackCard favorite button reflects the persisted heart state",
  );
  check(
    (counts["favorite:track-A"] ?? 0) > 0 && !counts["favorite:track-B"],
    "favorite update renders only the matching track membership selector",
  );
  check(
    (original.store.getSnapshot().favoriteIDs as ReadonlySet<string>).has("track-A"),
    "favorite IDs are exposed in current store snapshot",
  );
  check(
    window.__selectorProductRenders.FavoriteButton > 0 && window.__selectorProductRenders.App === 0,
    "favorite action updates the heart without rerendering App",
  );

  original.bridge.setRejectFavorite(true);
  resetCounts();
  await original.store.toggleFavorite(tracks[0]);
  await new Promise((resolve) => setTimeout(resolve, 20));
  await tick();
  check(
    (original.store.getSnapshot().favoriteIDs as ReadonlySet<string>).has("track-A"),
    "failed favorite write preserves confirmed membership",
  );
  check(
    document.querySelector('[data-favorite-id="track-A"]')?.getAttribute("data-favorite") === "true",
    "failed favorite write leaves rendered heart unchanged",
  );

  const ssr = renderToString(
    <PlayerProvider store={original.store}>
      <ProbeTree id="track-A" />
    </PlayerProvider>,
  );
  check(ssr.includes("Track A"), "selector snapshot supports server rendering");

  await original.store.play();
  original.handlers.get("playing")?.();
  await tick();
  check(original.store.getSnapshot().playback === "playing", "fake audio emitted the playing transition");
  window.__selectorProductRenders.OtoneiPlayer = 0;
  const savesBeforeProgress = original.metrics.queueSaves;
  original.engine.currentTime = 9000;
  original.handlers.get("timeupdate")?.();
  await original.store.flush();
  await tick();
  check(original.metrics.queueSaves > savesBeforeProgress, "timeupdate reached the queue persistence path");
  check(
    window.__selectorProductRenders.OtoneiPlayer === 0,
    "OtoneiPlayer does not rerender after a timeupdate save clones persisted queue state",
  );

  resetCounts();
  root.render(
    <PlayerProvider store={original.store}>
      <ProductApp bridge={original.bridge} />
      <ProbeTree id="track-B" />
    </PlayerProvider>,
  );
  await tick();
  check(
    document.querySelector('[data-favorite-id="track-B"]')?.getAttribute("data-favorite") === "false",
    "selector follows changed track ID",
  );
  check((counts["favorite:track-B"] ?? 0) > 0, "changed track ID obtains its own selected value");
  const probe = required(document.querySelector('[data-testid="probe-track-card"]'), "probe TrackCard missing");
  probe.querySelector<HTMLButtonElement>('[aria-label="播放 Track B - Artist"]')?.click();
  probe.querySelector<HTMLButtonElement>('[aria-label="加入队列：Track B"]')?.click();
  check(callbackIDs.play.at(-1) === "track-B", "TrackCard onPlay callback receives the updated track prop");
  check(callbackIDs.add.at(-1) === "track-B", "TrackCard onAdd callback receives the updated track prop");

  const replacement = fixture();
  root.render(
    <PlayerProvider store={replacement.store}>
      <ProductApp bridge={replacement.bridge} />
      <ProbeTree id="track-B" />
    </PlayerProvider>,
  );
  await replacement.store.hydrate();
  replacement.store.seek(9876);
  await tick();
  resetCounts();
  original.store.seek(7777);
  await tick();
  check(!counts["progress-only"], "provider replacement unsubscribes consumers from prior store");
  check(
    document.querySelector('[data-testid="progress-position"]')?.textContent === "9876",
    "provider replacement reads new store snapshot",
  );
  root.unmount();
  root = undefined;
  await new Promise((resolve) => setTimeout(resolve, 275));
  await replacement.store.flush();
  original.store.dispose();
  replacement.store.dispose();
  document.body.dataset.result = "passed";
  document.body.dataset.report = JSON.stringify(report);
}
window.__playerSelectorAcceptance = {
  run: async () => {
    try {
      await run();
    } catch (error) {
      document.body.dataset.result = "failed";
      document.body.dataset.error = String(error);
      root?.unmount();
      root = undefined;
    }
  },
  dispose: () => {
    root?.unmount();
    root = undefined;
    mounted?.store.dispose();
    mounted = undefined;
  },
};

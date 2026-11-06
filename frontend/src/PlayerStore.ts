import {
  normalizeAudioCache,
  normalizeResourceCache,
  fromResolvedCover,
  validRemoteCover,
  validateStreamURL,
} from "./wire";
import type { AudioCacheStats, ResourceCacheStats, SearchHistoryEntry } from "./contracts";
import { PlaylistStore } from "./PlaylistStore";
import { normalizeDesktopSettings } from "./desktopWire";
import type {
  DesktopBridge,
  LyricData,
  QueueSnapshot,
  ResolvedStream,
  TrackSnapshot,
  LocalSettings,
  PlaybackQualityPolicy,
  RegionStatus,
  Capabilities,
  HistoryEntry,
} from "./contracts";
import { trackSnapshot, normalizeLocalQuery } from "./contracts";
import type { AudioExecutor } from "./AudioEngine";
import { MediaStartupError } from "./AudioEngine";
export interface PlayerState {
  resourceCacheStats: ResourceCacheStats | null;
  resourceCacheMessage: string;
  coverRevision: number;
  searchHistory: SearchHistoryEntry[];
  searchHistoryError: string;
  favoritesHasMore: boolean;
  favoritesLoading: boolean;
  favoritesPage: number;
  historyPage: number;
  favoritesQuery: string;
  historyQuery: string;
  cacheStats: AudioCacheStats | null;
  cacheMessage: string;
  historyHasMore: boolean;
  historyLoading: boolean;
  historyClearing: boolean;
  queue: QueueSnapshot;
  // Derived from `queue` on every queue replacement: the current entry's song ID,
  // or "" when no entry is current. Card highlight selectors read this instead of
  // scanning `entries`, which kept every mounted card O(queue length) per patch.
  currentSongId: string;
  hydration: "loading" | "ready" | "failed";
  playback: "paused" | "loading" | "playing";
  error: string;
  saveError: string;
  lyrics: LyricData | null;
  lyricError: boolean;
  stream: ResolvedStream | null;
  settings: LocalSettings | null;
  qualityPolicy: PlaybackQualityPolicy | null;
  settingsError: string;
  favoriteIDs: ReadonlySet<string>;
  favorites: TrackSnapshot[];
  history: HistoryEntry[];
  libraryReady: boolean;
  libraryError: string;
  region: RegionStatus | null;
  regionError: string;
  capabilities: Capabilities | null;
}
const historySnapshot = (entry: HistoryEntry): HistoryEntry => ({
  historyEntryId: entry.historyEntryId,
  track: trackSnapshot(entry.track),
  playedAt: entry.playedAt,
  listenedMs: entry.listenedMs,
});
const emptyQueue = (): QueueSnapshot => ({
  entries: [],
  currentEntryId: null,
  currentIndex: -1,
  playMode: "repeat-all",
  positionMs: 0,
  revision: 0,
});
// Resolved by entry ID rather than index so it matches what the previous per-card
// selector computed; a duplicate song in the queue still highlights by song ID.
const currentSongOf = (queue: QueueSnapshot) =>
  queue.entries.find((entry) => entry.queueEntryId === queue.currentEntryId)?.track.neteaseSongId ?? "";
export class PlayerStore {
  private state: PlayerState = {
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
    queue: emptyQueue(),
    currentSongId: "",
    hydration: "loading",
    playback: "paused",
    error: "",
    saveError: "",
    lyrics: null,
    lyricError: false,
    stream: null,
    settings: null,
    qualityPolicy: null,
    settingsError: "",
    favoriteIDs: new Set<string>(),
    favorites: [],
    history: [],
    libraryReady: false,
    libraryError: "",
    region: null,
    regionError: "",
    capabilities: null,
  };
  readonly playlists: PlaylistStore;
  private listeners = new Set<() => void>();
  private saves: Promise<unknown> = Promise.resolve();
  private sourceToken?: string;
  private sourceReleases = new Set<Promise<unknown>>();
  private favoritePageInterrupted = false;
  private historyPageInterrupted = false;
  private favoritesGeneration = 0;
  private favoritesRequest?: AbortController;
  private historyRequest?: AbortController;
  private favoriteCursors: { order: number; id: string }[] = [{ order: 0, id: "" }];
  private historyCursors: { playedAt: number; id: string }[] = [{ playedAt: 0, id: "" }];
  private favoriteNext = { order: 0, id: "" };
  private searchHistorySaves: Promise<unknown> = Promise.resolve();
  private searchHistoryGeneration = 0;
  private coverRecoveries = new Map<string, Promise<string>>();
  private covers = new Map<string, Promise<string>>();
  private historyGeneration = 0;
  private historyClearing = false;
  private historyCursor?: HistoryEntry;
  private generation = 0;
  // Session-only overrides; queue entries themselves use the existing persisted snapshot.
  private pendingNextIds: string[] = [];
  private cacheStatsRevision = 0;
  private resourceCacheStatsRevision = 0;
  private starting = false;
  private closing = false;
  private settingsSaves: Promise<unknown> = Promise.resolve();
  private pendingSettings = 0;
  private settingsRevision = 0;
  private librarySaves: Promise<unknown> = Promise.resolve();
  private historyId = "";
  private historyRecorded = false;
  private listenedMs = 0;
  private progressAnchor = 0;
  private controller?: AbortController;
  private lyricsController?: AbortController;
  private lastProgressSave = 0;
  private hydrationPromise?: Promise<void>;
  private libraryHydration?: Promise<void>;
  private seekTimer?: ReturnType<typeof setTimeout>;
  private unbind: (() => void)[];
  constructor(
    private bridge: DesktopBridge,
    private engine: AudioExecutor,
  ) {
    this.playlists = new PlaylistStore(bridge);
    this.unbind = [
      engine.on("playing", () => {
        if (this.state.playback === "loading") {
          this.starting = false;
          this.progressAnchor = this.engine.currentTime;
          this.patch({ playback: "playing" });
        }
      }),
      engine.on("waiting", () => {
        if (this.state.playback === "playing") this.patch({ playback: "loading" });
      }),
      engine.on("timeupdate", () => this.progress()),
      engine.on("ended", () => {
        if (this.closing || this.state.playback === "paused") return;
        const stream = this.state.stream;
        const track = this.state.queue.entries[this.state.queue.currentIndex]?.track;
        const expected = Math.max(stream?.durationMs || 0, track?.durationMs || 0);
        const position = this.engine.currentTime;
        if (
          this.sourceToken &&
          stream?.cacheToken === this.sourceToken &&
          stream.status === "full" &&
          Number.isFinite(expected) &&
          expected > 0 &&
          Number.isFinite(position) &&
          position >= 0 &&
          expected - position > Math.max(2000, expected * 0.02)
        ) {
          this.failPlayback("缓存音频提前结束，已将本次缓存标记为无效。请重新播放。");
          return;
        }
        void this.next(true);
      }),
      engine.on("error", () => {
        if (this.state.playback !== "paused") this.failPlayback();
      }),
    ];
  }
  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => {
      this.listeners.delete(fn);
    };
  };
  getSnapshot = () => this.state;
  isClosing = () => this.closing;
  // Read the one existing executor. Pending startup retains the restored queue position.
  getPlaybackPosition = () => {
    const position =
      this.state.playback === "paused" || this.starting ? this.state.queue.positionMs : this.engine.currentTime;
    return Number.isFinite(position) ? Math.max(0, Math.min(86400000, Math.round(position))) : 0;
  };
  beginClose() {
    if (this.closing) return;
    this.closing = true;
    this.pendingNextIds = [];
    this.stop();
  }
  private patch(patch: Partial<PlayerState>) {
    const next = { ...this.state, ...patch };
    // Only a queue replacement can change the current song, so the one scan per
    // queue change replaces the per-card scan on every notification.
    if (patch.queue) next.currentSongId = currentSongOf(next.queue);
    this.state = next;
    this.listeners.forEach((fn) => {
      fn();
    });
  }
  hydrate() {
    if (this.hydrationPromise) return this.hydrationPromise;
    const queueLoad = this.bridge
      .LoadQueue()
      .then((saved) => {
        const queue = saved ?? emptyQueue();
        this.pendingNextIds = [];
        this.patch({
          queue: {
            ...queue,
            entries: queue.entries.map((e) => ({
              ...e,
              track: trackSnapshot(e.track),
            })),
          },
          hydration: "ready",
        });
      })
      .catch(() => {
        this.patch({
          hydration: "failed",
          error: "播放队列读取失败。为保护已有队列，暂时无法修改；请重新启动后重试。",
        });
      });
    const policyLoad = Promise.resolve()
      .then(() => this.bridge.GetPlaybackQualityPolicy())
      .catch(
        (): PlaybackQualityPolicy => ({
          premiumAllowed: false,
          selectableQualities: ["exhigh"],
          maxBitrate: 320000,
        }),
      );
    const settingsLoad = Promise.all([this.bridge.GetSettings(), policyLoad])
      .then(([saved, qualityPolicy]) => {
        const settings = this.normalizeSettings(saved, qualityPolicy);
        this.engine.setVolume(settings.volume);
        this.patch({ settings, qualityPolicy, settingsError: "" });
      })
      .catch(() =>
        this.patch({
          settingsError: "设置读取失败，请重新启动后重试。为保护已保存的设置，暂时无法播放或修改设置。",
        }),
      );
    const libraryLoad = this.loadLibrary();
    const searchHistoryLoad = this.refreshSearchHistory(this.searchHistoryGeneration);
    const capabilities = this.bridge
      .GetCapabilities()
      .then((capabilities) => this.patch({ capabilities }))
      .catch(() => {});
    this.hydrationPromise = Promise.all([
      this.playlists.hydrate(),
      queueLoad,
      settingsLoad,
      libraryLoad,
      searchHistoryLoad,
      capabilities,
    ]).then(() => {});
    void this.pollRegion();
    return this.hydrationPromise;
  }
  private loadLibrary() {
    if (this.libraryHydration) return this.libraryHydration;
    this.libraryHydration = Promise.all([this.fetchFavoritePage("", 0, ""), this.fetchHistoryPage("", 0, "")])
      .then(async ([favorites, history]) => {
        // Page reads may quarantine damaged rows. Read membership afterwards so
        // the initial heart state cannot retain IDs removed by that recovery.
        const ids = this.bridge.GetLocalFavoriteIDs
          ? await this.bridge.GetLocalFavoriteIDs()
          : (await this.bridge.GetLocalFavorites()).map((t) => t.neteaseSongId);
        this.favoriteNext = { order: favorites.nextAddedOrder, id: favorites.nextSongId };
        this.historyCursor = history.entries.at(-1);
        this.patch({
          favoriteIDs: new Set(ids),
          favorites: favorites.tracks.map(trackSnapshot),
          favoritesHasMore: favorites.hasMore,
          history: history.entries.map(historySnapshot),
          historyHasMore: history.hasMore,
          libraryReady: true,
          libraryError: "",
        });
      })
      .catch(() => this.patch({ libraryError: "本地音乐资料读取失败，请重试。" }));
    void this.libraryHydration.finally(() => {
      this.libraryHydration = undefined;
    });
    return this.libraryHydration;
  }
  retryLibrary(kind: "favorites" | "history") {
    if (this.closing) return Promise.resolve();
    if (!this.state.libraryReady) return this.loadLibrary();
    return kind === "favorites"
      ? this.readFavorites(this.state.favoritesPage, true)
      : this.readHistory(this.state.historyPage, true);
  }
  private flushSeek() {
    if (this.seekTimer === undefined) return;
    this.commit(this.state.queue);
  }
  private queueSnapshot(queue: QueueSnapshot): QueueSnapshot {
    return {
      ...queue,
      // Wails decodes this field into Go int64; HTML media times can be fractional.
      positionMs: Number.isFinite(queue.positionMs)
        ? Math.max(0, Math.min(Number.MAX_SAFE_INTEGER, Math.round(queue.positionMs)))
        : 0,
      revision: this.state.queue.revision + 1,
      entries: queue.entries.map((e) => ({
        queueEntryId: e.queueEntryId,
        track: trackSnapshot(e.track),
      })),
    };
  }
  private canAddQueue(queue: QueueSnapshot) {
    if (queue.entries.length > 1000) {
      this.patch({ error: "播放队列最多容纳 1000 首歌曲，请先移除部分歌曲。" });
      return false;
    }
    // Match encoding/json.Marshal's HTML and JavaScript-separator escaping before UTF-8 counting.
    // Pick and normalize exactly the same metadata that commit sends to native persistence.
    const json = JSON.stringify(this.queueSnapshot(queue)).replace(
      /[<>&\u2028\u2029]/g,
      (character) => `\\u${character.charCodeAt(0).toString(16).padStart(4, "0")}`,
    );
    if (new TextEncoder().encode(json).length > 2 * 1024 * 1024) {
      this.patch({ error: "播放队列资料容量已满，请先移除部分歌曲。" });
      return false;
    }
    return true;
  }
  private commit(queue: QueueSnapshot) {
    if (this.state.hydration !== "ready") return;
    if (this.seekTimer !== undefined) clearTimeout(this.seekTimer);
    this.seekTimer = undefined;
    const snapshot = this.queueSnapshot(queue);
    // All callers normalize incoming tracks; retain runtime metadata identities for selectors.
    // Persistence still receives its own detached, normalized snapshot.
    const entries = queue.entries;
    this.patch({ queue: { ...snapshot, entries } });
    this.saves = this.saves
      .catch(() => {})
      .then(() => this.bridge.SaveQueue(snapshot))
      .then(() => this.patch({ saveError: "" }))
      .catch(() => this.patch({ saveError: "队列保存失败，最近的更改可能尚未保存。" }));
  }
  flush() {
    this.flushSeek();
    return Promise.all([
      this.saves,
      this.settingsSaves,
      this.librarySaves,
      this.searchHistorySaves,
      this.playlists.flush(),
      ...this.sourceReleases,
    ]);
  }
  add(track: TrackSnapshot) {
    if (this.closing) return null;
    if (this.state.hydration !== "ready") return null;
    const id = crypto.randomUUID();
    const q = this.state.queue;
    const next = {
      ...q,
      entries: [...q.entries, { queueEntryId: id, track: trackSnapshot(track) }],
      currentEntryId: q.currentEntryId ?? id,
      currentIndex: Math.max(q.currentIndex, 0),
    };
    if (!this.canAddQueue(next)) return null;
    this.commit(next);
    return id;
  }
  addAll(tracks: TrackSnapshot[], replace = false) {
    if (this.closing) return false;
    if (this.state.hydration !== "ready" || !tracks.length) return false;
    const q = this.state.queue;
    if (tracks.length + (replace ? 0 : q.entries.length) > 1000) {
      this.patch({ error: "播放队列最多容纳 1000 首歌曲，请先移除部分歌曲。" });
      return false;
    }
    const added = tracks.map((track) => ({ queueEntryId: crypto.randomUUID(), track: trackSnapshot(track) }));
    const next = {
      ...q,
      entries: replace ? added : [...q.entries, ...added],
      currentEntryId: replace ? added[0].queueEntryId : (q.currentEntryId ?? added[0].queueEntryId),
      currentIndex: replace || q.currentIndex < 0 ? 0 : q.currentIndex,
      positionMs: replace ? 0 : q.positionMs,
    };
    // A rejected replacement must not pause audio or invalidate pending stream resolution.
    if (!this.canAddQueue(next)) return false;
    if (replace) {
      this.pendingNextIds = [];
      this.generation++;
      this.controller?.abort();
      this.lyricsController?.abort();
      this.starting = false;
      this.engine.pause();
      this.releaseSource();
      this.patch({ playback: "paused", stream: null, lyrics: null });
    }
    this.commit(next);
    if (replace) void this.play();
    return true;
  }
  playNext(tracks: TrackSnapshot[]): boolean {
    if (this.closing || this.state.hydration !== "ready" || !tracks.length) return false;
    const q = this.state.queue;
    if (tracks.length + q.entries.length > 1000) {
      this.patch({ error: "播放队列最多容纳 1000 首歌曲，请先移除部分歌曲。" });
      return false;
    }
    if (
      tracks.some((track) => typeof track.neteaseSongId !== "string" || !/^[1-9][0-9]{0,29}$/.test(track.neteaseSongId))
    ) {
      this.patch({ error: "歌曲资料无效，无法加入播放队列。" });
      return false;
    }
    const added = tracks.map((track) => ({ queueEntryId: crypto.randomUUID(), track: trackSnapshot(track) }));
    const currentIndex = q.entries.findIndex((entry) => entry.queueEntryId === q.currentEntryId);
    const insertion = currentIndex + 1;
    const next = {
      ...q,
      entries: [...q.entries.slice(0, insertion), ...added, ...q.entries.slice(insertion)],
      currentEntryId: currentIndex < 0 ? added[0].queueEntryId : q.currentEntryId,
      currentIndex: Math.max(currentIndex, 0),
    };
    if (!this.canAddQueue(next)) return false;
    const priority = currentIndex < 0 ? added.slice(1) : added;
    this.pendingNextIds = [...priority.map((entry) => entry.queueEntryId), ...this.pendingNextIds];
    this.commit(next);
    return true;
  }
  remove(id: string) {
    if (this.closing) return;
    if (this.state.hydration !== "ready") return;
    const q = this.state.queue;
    const entries = q.entries.filter((e) => e.queueEntryId !== id);
    this.pendingNextIds = this.pendingNextIds.filter((pending) => pending !== id);
    const current = q.currentEntryId === id;
    if (current) this.stop();
    const index = current
      ? Math.min(q.currentIndex, entries.length - 1)
      : entries.findIndex((e) => e.queueEntryId === q.currentEntryId);
    this.commit({
      ...q,
      entries,
      currentIndex: index,
      currentEntryId: entries[index]?.queueEntryId ?? null,
      positionMs: current ? 0 : q.positionMs,
    });
    if (current) this.patch({ lyrics: null, stream: null });
  }
  setMode(playMode: QueueSnapshot["playMode"]) {
    if (this.closing) return;
    if (this.state.hydration === "ready") this.commit({ ...this.state.queue, playMode });
  }
  async select(id: string) {
    this.pendingNextIds = [];
    await this.selectEntry(id);
  }
  private async selectEntry(id: string) {
    if (this.closing) return;
    if (this.state.hydration !== "ready") return;
    const index = this.state.queue.entries.findIndex((e) => e.queueEntryId === id);
    if (index < 0) return;
    this.stop();
    this.commit({
      ...this.state.queue,
      currentIndex: index,
      currentEntryId: id,
      positionMs: 0,
    });
    await this.play();
  }
  pause() {
    this.stop(!this.starting);
  }
  private stop(preserveSource = false) {
    this.starting = false;
    const positionMs =
      this.state.playback === "playing" && this.seekTimer === undefined
        ? this.engine.currentTime
        : this.state.queue.positionMs;
    this.generation++;
    this.controller?.abort();
    this.lyricsController?.abort();
    this.engine.pause(preserveSource);
    this.releaseSource();
    this.patch({ playback: "paused" });
    if (this.state.hydration === "ready" && this.state.queue.currentEntryId)
      this.commit({ ...this.state.queue, positionMs });
  }
  async play() {
    if (this.closing) return;
    if (this.state.hydration !== "ready" || !this.state.settings) return;
    if (this.state.settingsError && !this.pendingSettings) return;
    if (this.pendingSettings) {
      const requestedGeneration = this.generation;
      await this.settingsSaves;
      if (this.closing || requestedGeneration !== this.generation || this.pendingSettings || this.state.settingsError)
        return;
    }
    const q = this.state.queue;
    const entry = q.entries.find((e) => e.queueEntryId === q.currentEntryId);
    if (!entry) return;
    const generation = ++this.generation;
    this.historyId = crypto.randomUUID();
    this.historyRecorded = false;
    this.listenedMs = 0;
    this.starting = true;
    this.controller?.abort();
    this.lyricsController?.abort();
    this.engine.pause();
    this.releaseSource();
    const controller = new AbortController();
    this.controller = controller;
    const lyricController = new AbortController();
    this.lyricsController = lyricController;
    this.patch({
      playback: "loading",
      error: "",
      stream: null,
      lyrics: null,
      lyricError: false,
    });
    void this.bridge
      .GetLyrics(entry.track.neteaseSongId, lyricController.signal)
      .then((lyrics) => {
        if (generation === this.generation) this.patch({ lyrics });
      })
      .catch(() => {
        if (generation === this.generation) this.patch({ lyricError: true });
      });
    try {
      const stream = await this.bridge.ResolveStream(
        entry.track.neteaseSongId,
        this.state.settings.preferredQuality,
        controller.signal,
      );
      if (generation !== this.generation) {
        this.releaseToken(stream.cacheToken);
        return;
      }
      this.sourceToken = stream.cacheToken;
      validateStreamURL(stream);
      this.patch({ stream });
      if (!stream.url || stream.status === "unavailable" || stream.status === "unauthorized") {
        this.starting = false;
        this.releaseSource();
        this.patch({
          playback: "paused",
          error:
            stream.status === "unauthorized"
              ? "这首歌需要账号或播放权限，目前无法播放。"
              : playbackError(stream.errorCode),
        });
        return;
      }
      // A seek while resolution was pending must win over the original saved position.
      await this.engine.setSource(stream.url, this.state.queue.positionMs, {
        expectedDurationMs: entry.track.durationMs,
        requireComplete: stream.provider === "gd-netease" || stream.status === "full",
        deadlineAt: stream.resolutionDeadlineAt,
      });
      if (generation !== this.generation) return;
      this.starting = false;
    } catch (error) {
      if (generation === this.generation) {
        this.starting = false;
        this.failPlayback(
          error instanceof MediaStartupError
            ? playbackError(error.code)
            : error instanceof Error && ["GD_BUDGET_EXHAUSTED", "GD_RATE_LIMITED"].includes(error.message)
              ? playbackError(error.message)
              : undefined,
        );
      }
    }
  }
  private failPlayback(message = "播放失败，请重试或选择其他歌曲。") {
    this.starting = false;
    this.generation++;
    this.controller?.abort();
    this.engine.pause();
    this.releaseSource(true);
    this.patch({
      playback: "paused",
      error: message,
    });
  }
  seek(positionMs: number) {
    if (this.closing) return;
    if (this.state.hydration !== "ready" || !this.state.queue.currentEntryId) return;
    const track = this.state.queue.entries[this.state.queue.currentIndex]?.track;
    const maximum = this.engine.duration || track?.durationMs || 0;
    const position = Number.isFinite(positionMs) ? Math.round(Math.max(0, Math.min(positionMs, maximum))) : 0;
    if (this.state.playback !== "paused") this.engine.seek(position);
    this.progressAnchor = position;
    this.patch({ queue: { ...this.state.queue, positionMs: position } });
    if (this.seekTimer !== undefined) clearTimeout(this.seekTimer);
    this.seekTimer = setTimeout(() => this.flushSeek(), 250);
  }
  private progress() {
    if (this.closing || this.state.playback === "paused" || this.state.hydration !== "ready") return;
    const position = this.engine.currentTime;
    if (this.state.playback === "playing") {
      const delta = position - this.progressAnchor;
      if (Number.isFinite(delta) && delta > 0 && delta <= 5000) this.listenedMs += delta;
      this.progressAnchor = position;
      if (!this.historyRecorded && this.listenedMs >= 500) this.recordHistory();
    }
    const queue = { ...this.state.queue, positionMs: position };
    if (Date.now() - this.lastProgressSave >= 5000) {
      this.lastProgressSave = Date.now();
      this.commit(queue);
    } else this.patch({ queue });
  }
  async next(ended = false) {
    if (this.closing) return;
    const q = this.state.queue;
    if (!q.entries.length || this.state.hydration !== "ready") return;
    while (this.pendingNextIds.length) {
      const id = this.pendingNextIds.shift();
      if (!id || id === q.currentEntryId) continue;
      if (q.entries.some((entry) => entry.queueEntryId === id)) {
        await this.selectEntry(id);
        return;
      }
    }
    let index = (q.currentIndex + 1) % q.entries.length;
    if (ended && q.playMode === "repeat-one") index = q.currentIndex;
    if (q.playMode === "shuffle" && q.entries.length > 1)
      index = (q.currentIndex + 1 + Math.floor(Math.random() * (q.entries.length - 1))) % q.entries.length;
    await this.selectEntry(q.entries[index].queueEntryId);
  }
  setVolume(volume: number) {
    this.setSettings({ volume });
  }
  previous() {
    if (this.closing) return;
    this.pendingNextIds = [];
    const q = this.state.queue;
    if (q.entries.length)
      return this.select(q.entries[(q.currentIndex - 1 + q.entries.length) % q.entries.length].queueEntryId);
  }
  setSettings(update: Partial<LocalSettings>) {
    if (this.closing) return;
    const policy = this.state.qualityPolicy;
    const current = this.state.settings;
    if (!current || !policy) return;
    if (update.preferredQuality !== undefined && !policy.selectableQualities.includes(update.preferredQuality)) {
      this.patch({ settingsError: "该音质不可手动选择。320 kbps 以上音质需登录并确认会员。" });
      return;
    }
    const settings = { ...current, ...update };
    const revision = ++this.settingsRevision;
    if (
      settings.preferredQuality !== current.preferredQuality ||
      settings.regionMode !== current.regionMode ||
      settings.gdEnabled !== current.gdEnabled ||
      settings.audioCache?.enabled !== current.audioCache?.enabled
    )
      this.cancelPendingStartup();
    if (update.resourceCache) this.invalidateCovers();
    this.patch({ settings });
    this.engine.setVolume(settings.volume);
    this.pendingSettings++;
    this.settingsSaves = this.settingsSaves
      .catch(() => {})
      .then(() => this.bridge.SetSettings(settings))
      .then((saved) => {
        if (revision === this.settingsRevision) {
          const settings = this.normalizeSettings(saved, this.state.qualityPolicy ?? policy);
          this.engine.setVolume(settings.volume);
          this.patch({ settings, settingsError: "" });
          if (update.resourceCache) {
            this.invalidateCovers();
            void this.refreshResourceCacheStats();
          }
        }
      })
      .catch(() =>
        this.patch({
          settingsError: "设置保存失败，最近的更改尚未保存。请再次修改以重试。",
        }),
      )
      .finally(() => {
        this.pendingSettings--;
      });
  }
  private normalizeSettings(settings: LocalSettings, policy: PlaybackQualityPolicy): LocalSettings {
    return {
      ...settings,
      desktop: normalizeDesktopSettings(settings.desktop),
      resourceCache: normalizeResourceCache(settings.resourceCache),
      audioCache: normalizeAudioCache(settings.audioCache),
      closeToTray: settings.closeToTray ?? false,
      preferredQuality: policy.selectableQualities.includes(settings.preferredQuality)
        ? settings.preferredQuality
        : "exhigh",
    };
  }
  private cancelPendingStartup() {
    if (!this.starting) return;
    this.starting = false;
    this.generation++;
    this.controller?.abort();
    this.lyricsController?.abort();
    this.engine.pause();
    this.releaseSource();
    this.patch({
      playback: "paused",
      error: "播放条件已更新，请重新点击播放。",
    });
  }
  private acceptRegion(region: RegionStatus) {
    if (this.state.region && region.networkGeneration < this.state.region.networkGeneration) return;
    if (this.state.region && region.networkGeneration !== this.state.region.networkGeneration)
      this.cancelPendingStartup();
    this.patch({ region, regionError: "" });
  }
  async pollRegion() {
    try {
      this.acceptRegion(await this.bridge.GetRegionStatus());
    } catch {
      this.patch({ regionError: "暂时无法读取地区状态。" });
    }
  }
  async refreshRegion(signal?: AbortSignal) {
    try {
      this.acceptRegion(await this.bridge.RefreshRegion(signal));
    } catch {
      if (!signal?.aborted) this.patch({ regionError: "地区检测未完成，请稍后重试。" });
    }
  }
  isFavorite(track: TrackSnapshot) {
    return this.state.favoriteIDs.has(track.neteaseSongId);
  }
  toggleFavorite(track: TrackSnapshot) {
    if (this.closing) return;
    if (!this.state.libraryReady) return;
    const snapshot = trackSnapshot(track);
    this.favoritesGeneration++;
    this.favoritesRequest?.abort();
    // Read the latest state inside the serialized operation, including rapid repeated clicks.
    this.librarySaves = this.librarySaves
      .catch(() => {})
      .then(async () => {
        const liked = !this.isFavorite(snapshot);
        await this.bridge.SetLocalFavorite(snapshot, liked);
        const favoriteIDs = new Set(this.state.favoriteIDs);
        if (liked) favoriteIDs.add(snapshot.neteaseSongId);
        else favoriteIDs.delete(snapshot.neteaseSongId);
        if (this.bridge.SearchLocalFavorites) {
          this.patch({ favoriteIDs, libraryError: "" });
          void this.readFavorites(this.state.favoritesPage, true);
        } else
          this.patch({
            favoriteIDs,
            favorites: liked
              ? [snapshot, ...this.state.favorites.filter((t) => t.neteaseSongId !== snapshot.neteaseSongId)].slice(
                  0,
                  200,
                )
              : this.state.favorites.filter((t) => t.neteaseSongId !== snapshot.neteaseSongId),
            libraryError: "",
          });
      })
      .catch((error) =>
        this.patch({
          favoritesLoading: false,
          libraryError:
            error instanceof Error && error.message === "LOCAL_METADATA_REQUIRES_NEWER_VERSION"
              ? "此记录包含较新版本的数据，请更新客户端后修改。"
              : "收藏保存失败，请重试。",
        }),
      );
  }
  private recordHistory() {
    const track = this.state.queue.entries[this.state.queue.currentIndex]?.track;
    if (!track || !this.state.libraryReady) return;
    this.historyRecorded = true;
    const entry: HistoryEntry = {
      historyEntryId: this.historyId,
      track: trackSnapshot(track),
      playedAt: Date.now(),
      listenedMs: Math.round(this.listenedMs),
    };
    this.librarySaves = this.librarySaves
      .catch(() => {})
      .then(async () => {
        await this.bridge.AddLocalHistory(entry);
        this.patch({
          history: this.bridge.SearchLocalHistory
            ? this.state.history
            : [entry, ...this.state.history.filter((e) => e.historyEntryId !== entry.historyEntryId)],
          libraryError: "",
        });
        if (this.bridge.SearchLocalHistory) void this.readHistory(this.state.historyPage, true);
      })
      .catch(() => this.patch({ libraryError: "播放历史保存失败。" }));
  }
  clearHistory() {
    if (this.closing || this.historyClearing) return;
    if (!this.state.libraryReady) return;
    const previous = {
      cursors: this.historyCursors,
      cursor: this.historyCursor,
      page: this.state.historyPage,
      hasMore: this.state.historyHasMore,
    };
    this.historyGeneration++;
    this.historyRequest?.abort();
    this.historyCursors = [{ playedAt: 0, id: "" }];
    this.historyClearing = true;
    this.patch({ historyLoading: true, historyClearing: true, historyPage: 1, historyHasMore: false });
    this.librarySaves = this.librarySaves
      .catch(() => {})
      .then(async () => {
        await this.bridge.ClearLocalHistory();
        this.historyCursor = undefined;
        this.patch({ history: [], historyPage: 1, historyHasMore: false, libraryError: "" });
      })
      .catch(() => {
        this.historyCursors = previous.cursors;
        this.historyCursor = previous.cursor;
        this.patch({
          historyPage: previous.page,
          historyHasMore: previous.hasMore,
          libraryError: "播放历史清除失败，请重试。",
        });
      })
      .finally(() => {
        this.historyClearing = false;
        this.patch({ historyLoading: false, historyClearing: false });
      });
  }
  private releaseToken(token?: string, failed = false) {
    if (!token || !/^[a-f0-9]{32}$/.test(token)) return;
    const operation = failed ? this.bridge.ReportPlaybackSourceFailure : this.bridge.ReleasePlaybackSource;
    if (!operation) return;
    const call = Promise.resolve()
      .then(() => operation.call(this.bridge, token))
      .catch(() => {});
    this.sourceReleases.add(call);
    void call.finally(() => this.sourceReleases.delete(call));
  }
  private releaseSource(failed = false) {
    const token = this.sourceToken;
    this.sourceToken = undefined;
    this.releaseToken(token, failed);
  }
  async refreshCacheStats(accept: () => boolean = () => true) {
    if (!this.bridge.GetAudioCacheStats) return;
    const revision = ++this.cacheStatsRevision;
    try {
      const cacheStats = await this.bridge.GetAudioCacheStats();
      if (accept() && revision === this.cacheStatsRevision) this.patch({ cacheStats });
    } catch {
      if (accept() && revision === this.cacheStatsRevision) this.patch({ cacheStats: null });
    }
  }
  async clearAudioCache() {
    if (this.closing) return;
    try {
      if (!this.bridge.ClearAudioCache) throw new Error();
      const result = await this.bridge.ClearAudioCache();
      this.patch({ cacheMessage: `已清理 ${result.removedTracks} 首音频缓存。正在播放的文件将在播放结束后清理。` });
      await this.refreshCacheStats();
    } catch {
      this.patch({ cacheMessage: "音频缓存暂时无法清理，请稍后重试。" });
    }
  }
  async minimizeToTray() {
    if (this.closing) return;
    try {
      if (!this.bridge.MinimizeToTray) throw new Error();
      await this.bridge.MinimizeToTray();
    } catch {
      this.patch({ cacheMessage: "暂时无法隐藏到托盘，窗口将保持打开。" });
    }
  }
  async loadMoreHistory() {
    if (this.bridge.SearchLocalHistory) return this.navigateHistory(1);
    if (this.closing) return;
    if (
      !this.state.libraryReady ||
      this.historyClearing ||
      this.state.historyLoading ||
      !this.state.historyHasMore ||
      !this.bridge.ListLocalHistoryBefore
    )
      return;
    const generation = this.historyGeneration;
    const cursor = this.historyCursor;
    this.patch({ historyLoading: true });
    try {
      const page = await this.bridge.ListLocalHistoryBefore(200, cursor?.playedAt ?? 0, cursor?.historyEntryId ?? "");
      if (generation !== this.historyGeneration) return;
      const seen = new Set(this.state.history.map((e) => e.historyEntryId));
      this.historyCursor = page.entries.at(-1) ?? cursor;
      this.patch({
        history: [
          ...this.state.history,
          ...page.entries
            .filter((e) => {
              if (seen.has(e.historyEntryId)) return false;
              seen.add(e.historyEntryId);
              return true;
            })
            .map(historySnapshot),
        ],
        historyHasMore: page.hasMore,
        libraryError: "",
      });
    } catch {
      if (generation === this.historyGeneration) this.patch({ libraryError: "播放历史读取失败，请重试。" });
    } finally {
      if (generation === this.historyGeneration) this.patch({ historyLoading: false });
    }
  }
  resumeLibraryQuery(kind: "favorites" | "history") {
    if (kind === "favorites" && this.favoritePageInterrupted) {
      this.favoritePageInterrupted = false;
      void this.readFavorites(this.state.favoritesPage);
    } else if (kind === "history" && this.historyPageInterrupted) {
      this.historyPageInterrupted = false;
      void this.readHistory(this.state.historyPage);
    }
  }
  cancelLibraryQuery(kind: "favorites" | "history") {
    if (kind === "favorites") {
      this.favoritePageInterrupted = this.state.favoritesLoading;
      this.favoritesGeneration++;
      this.favoritesRequest?.abort();
      this.patch({ favoritesLoading: false });
    } else {
      this.historyPageInterrupted = this.state.historyLoading;
      this.historyGeneration++;
      this.historyRequest?.abort();
      this.patch({ historyLoading: false });
    }
  }
  private fetchFavoritePage(query: string, order: number, id: string, signal?: AbortSignal) {
    if (this.bridge.SearchLocalFavorites) return this.bridge.SearchLocalFavorites(query, 200, order, id, signal);
    return this.bridge.GetLocalFavorites().then((tracks) => ({
      tracks: tracks.filter((t) => this.matches(t, query)),
      hasMore: false,
      nextAddedOrder: 0,
      nextSongId: "",
    }));
  }
  private fetchHistoryPage(query: string, playedAt: number, id: string, signal?: AbortSignal) {
    if (this.bridge.SearchLocalHistory) return this.bridge.SearchLocalHistory(query, 200, playedAt, id, signal);
    if (this.bridge.ListLocalHistoryBefore) return this.bridge.ListLocalHistoryBefore(200, playedAt, id);
    return this.bridge
      .ListLocalHistory(200, 0)
      .then((entries) => ({ entries: entries.filter((e) => this.matches(e.track, query)), hasMore: false }));
  }
  private matches(track: TrackSnapshot, query: string) {
    return `${track.name} ${track.artists.map((a) => a.name).join(" ")} ${track.album?.name ?? ""}`
      .toLocaleLowerCase()
      .includes(query.toLocaleLowerCase());
  }
  queryFavorites(query: string) {
    if (this.closing) return Promise.resolve();
    try {
      query = normalizeLocalQuery(query);
    } catch {
      this.cancelLibraryQuery("favorites");
      this.patch({ libraryError: "搜索关键词不能超过 512 字节。" });
      return Promise.resolve();
    }
    this.favoriteCursors = [{ order: 0, id: "" }];
    this.patch({ favoritesQuery: query });
    return this.readFavorites(1);
  }
  async navigateFavorites(direction: -1 | 1) {
    if (this.state.favoritesLoading || (direction === 1 && !this.state.favoritesHasMore)) return;
    const page = this.state.favoritesPage + direction;
    if (page < 1) return;
    if (direction === 1) this.favoriteCursors[page - 1] = this.favoriteNext;
    await this.readFavorites(page);
  }
  private async readFavorites(page: number, preserve = false) {
    if (this.closing) return;
    const gen = ++this.favoritesGeneration;
    this.favoritesRequest?.abort();
    const request = new AbortController();
    this.favoritesRequest = request;
    const cursor = this.favoriteCursors[page - 1];
    const query = this.state.favoritesQuery;
    this.patch({
      favoritesLoading: true,
      ...(preserve ? {} : { favoritesHasMore: false, favorites: [], favoritesPage: page }),
    });
    try {
      await this.librarySaves;
      if (gen !== this.favoritesGeneration) return;
      const result = await this.fetchFavoritePage(query, cursor.order, cursor.id, request.signal);
      if (gen !== this.favoritesGeneration || this.closing) return;
      if (!result.tracks.length && page > 1) {
        await this.readFavorites(page - 1, true);
        return;
      }
      this.favoriteCursors.length = page;
      this.favoriteNext = { order: result.nextAddedOrder, id: result.nextSongId };
      this.patch({
        favoritesPage: page,
        favorites: result.tracks.map(trackSnapshot),
        favoritesHasMore: result.hasMore,
        libraryError: "",
      });
    } catch {
      if (gen === this.favoritesGeneration) this.patch({ libraryError: "收藏读取失败，请重试。" });
    } finally {
      if (gen === this.favoritesGeneration) this.patch({ favoritesLoading: false });
    }
  }
  queryHistory(query: string) {
    if (this.closing || this.historyClearing) return Promise.resolve();
    try {
      query = normalizeLocalQuery(query);
    } catch {
      this.cancelLibraryQuery("history");
      this.patch({ libraryError: "搜索关键词不能超过 512 字节。" });
      return Promise.resolve();
    }
    this.historyCursors = [{ playedAt: 0, id: "" }];
    this.patch({ historyQuery: query });
    return this.readHistory(1);
  }
  async navigateHistory(direction: -1 | 1) {
    if (this.historyClearing || this.state.historyLoading || (direction === 1 && !this.state.historyHasMore)) return;
    const page = this.state.historyPage + direction;
    if (page < 1) return;
    if (direction === 1)
      this.historyCursors[page - 1] = {
        playedAt: this.historyCursor?.playedAt ?? 0,
        id: this.historyCursor?.historyEntryId ?? "",
      };
    await this.readHistory(page);
  }
  private async readHistory(page: number, preserve = false) {
    if (this.closing) return;
    const gen = ++this.historyGeneration;
    this.historyRequest?.abort();
    const request = new AbortController();
    this.historyRequest = request;
    const cursor = this.historyCursors[page - 1];
    if (!cursor || this.historyClearing) return;
    const query = this.state.historyQuery;
    this.patch({
      historyLoading: true,
      ...(preserve ? {} : { historyHasMore: false, history: [], historyPage: page }),
    });
    try {
      await this.librarySaves;
      if (gen !== this.historyGeneration) return;
      const result = await this.fetchHistoryPage(query, cursor.playedAt, cursor.id, request.signal);
      if (gen !== this.historyGeneration || this.closing) return;
      if (!result.entries.length && page > 1) {
        await this.readHistory(page - 1, true);
        return;
      }
      this.historyCursors.length = page;
      this.historyCursor = result.entries.at(-1);
      this.patch({
        historyPage: page,
        history: result.entries.map(historySnapshot),
        historyHasMore: result.hasMore,
        libraryError: "",
      });
    } catch {
      if (gen === this.historyGeneration) this.patch({ libraryError: "播放历史读取失败，请重试。" });
    } finally {
      if (gen === this.historyGeneration) this.patch({ historyLoading: false });
    }
  }
  getSearchHistoryGeneration() {
    return this.searchHistoryGeneration;
  }
  private async refreshSearchHistory(gen: number) {
    if (!this.bridge.ListSearchHistory) return;
    try {
      const entries = await this.bridge.ListSearchHistory(20);
      if (gen === this.searchHistoryGeneration)
        this.patch({ searchHistory: entries.slice(0, 20), searchHistoryError: "" });
    } catch {
      if (gen === this.searchHistoryGeneration) this.patch({ searchHistoryError: "搜索记录读取失败，请重试。" });
    }
  }
  recordSearchQuery(query: string, gen: number) {
    try {
      query = normalizeLocalQuery(query);
    } catch {
      this.patch({ searchHistoryError: "搜索关键词不能超过 512 字节。" });
      return;
    }
    const recordSearchQuery = this.bridge.RecordSearchQuery;
    if (this.closing || !query || gen !== this.searchHistoryGeneration || !recordSearchQuery) return;
    this.searchHistorySaves = this.searchHistorySaves
      .catch(() => {})
      .then(async () => {
        if (gen !== this.searchHistoryGeneration) return;
        await recordSearchQuery(query);
        await this.refreshSearchHistory(gen);
      })
      .catch(() => this.patch({ searchHistoryError: "搜索记录保存失败。" }));
  }
  deleteSearchQuery(query: string) {
    this.mutateSearchHistory(() => this.bridge.DeleteSearchQuery?.(query));
  }
  clearSearchHistory() {
    this.mutateSearchHistory(() => this.bridge.ClearSearchHistory?.());
  }
  private mutateSearchHistory(write: () => Promise<void> | undefined) {
    if (this.closing) return;
    const gen = ++this.searchHistoryGeneration;
    this.patch({ searchHistory: [], searchHistoryError: "" });
    this.searchHistorySaves = this.searchHistorySaves
      .catch(() => {})
      .then(async () => {
        await write();
        await this.refreshSearchHistory(gen);
      })
      .catch(() => this.patch({ searchHistoryError: "搜索记录删除失败，请重试。" }));
  }
  resolveCover(ref: string) {
    if (!validRemoteCover(ref)) return Promise.resolve("/default_cover.svg");
    let pending = this.covers.get(ref);
    if (!pending) {
      pending = Promise.resolve()
        .then(() => (this.bridge.ResolveCover ? this.bridge.ResolveCover(ref) : ref))
        .then(fromResolvedCover)
        .catch(() => ref);
      this.covers.set(ref, pending);
      if (this.covers.size > 256) {
        const oldest = this.covers.keys().next().value;
        if (oldest !== undefined) this.covers.delete(oldest);
      }
    }
    return pending;
  }
  invalidateCover(ref: string) {
    this.covers.delete(ref);
  }
  recoverCover(ref: string, failedSource: string) {
    if (!validRemoteCover(ref) || !/^\/artwork\/[a-f0-9]{64}$/.test(failedSource))
      return Promise.resolve(validRemoteCover(ref) ? ref : "/default_cover.svg");
    const key = `${ref}|${failedSource}`;
    let recovery = this.coverRecoveries.get(key);
    if (!recovery) {
      const remembered = this.covers.get(ref);
      recovery = Promise.resolve(remembered).then((current) => {
        // Another visible consumer may already have refreshed this failed asset.
        if (current && current !== failedSource) return current;
        this.invalidateCover(ref);
        return this.resolveCover(ref);
      });
      this.coverRecoveries.set(key, recovery);
      const shared = recovery;
      void recovery.finally(() => {
        if (this.coverRecoveries.get(key) === shared) this.coverRecoveries.delete(key);
      });
      if (this.coverRecoveries.size > 256) {
        const oldest = this.coverRecoveries.keys().next().value;
        if (oldest !== undefined) this.coverRecoveries.delete(oldest);
      }
    }
    return recovery;
  }
  private invalidateCovers() {
    this.covers.clear();
    this.coverRecoveries.clear();
    this.patch({ coverRevision: this.state.coverRevision + 1 });
  }
  async refreshResourceCacheStats(accept: () => boolean = () => true) {
    if (!this.bridge.GetResourceCacheStats) return;
    const revision = ++this.resourceCacheStatsRevision;
    try {
      const resourceCacheStats = await this.bridge.GetResourceCacheStats();
      if (accept() && revision === this.resourceCacheStatsRevision) this.patch({ resourceCacheStats });
    } catch {
      if (accept() && revision === this.resourceCacheStatsRevision) this.patch({ resourceCacheStats: null });
    }
  }
  async clearResourceCache() {
    if (this.closing || !this.bridge.ClearResourceCache) return;
    try {
      const result = await this.bridge.ClearResourceCache();
      this.invalidateCovers();
      this.patch({ resourceCacheMessage: `已清理 ${result.removedCovers} 张封面和 ${result.removedLyrics} 首歌词。` });
      await this.refreshResourceCacheStats();
    } catch {
      this.patch({ resourceCacheMessage: "封面与歌词缓存暂时无法清理，请重试。" });
    }
  }
  dispose() {
    this.pendingNextIds = [];
    this.favoritesRequest?.abort();
    this.historyRequest?.abort();
    this.stop();
    this.unbind.forEach((fn) => {
      fn();
    });
    this.listeners.clear();
  }
}
function playbackError(code?: string) {
  if (code === "GD_BUDGET_EXHAUSTED") return "GD 备用音源解析时间已用尽，请稍后重试。";
  if (code === "GD_RATE_LIMITED") return "GD 备用音源请求过于频繁，请稍后重试。";
  if (code === "COMPATIBILITY_COOLDOWN") return "海外兼容请求暂时冷却，请稍后重试。";
  if (code === "TRIAL_ONLY") return "当前音源仅提供试听片段，未开始播放。请选择其他歌曲。";
  if (code === "STREAM_DURATION_MISMATCH") return "当前音源的完整时长无法确认，未开始播放。请选择其他歌曲。";
  if (code === "REGION_RESTRICTED") return "这首歌在当前地区暂时无法播放。";
  if (code === "NETWORK_ERROR" || code === "RESOLVE_TIMEOUT") return "网络连接未完成，请稍后重试。";
  return "暂时无法获取这首歌的播放地址，请重试或选择其他歌曲。";
}

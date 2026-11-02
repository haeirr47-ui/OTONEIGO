export type DesktopView = "mini" | "lyrics";
export interface WindowPlacement {
  valid: boolean;
  x: number;
  y: number;
  width: number;
  height: number;
}
export interface DesktopSettings {
  miniAlwaysOnTop: boolean;
  lyricsAlwaysOnTop: boolean;
  lyricsLocked: boolean;
  lyricsFontSize: number;
  lyricsOpacity: number;
  notifyTrackChanges: boolean;
  taskbarProgress: boolean;
  miniPlacement: WindowPlacement;
  lyricsPlacement: WindowPlacement;
}
export interface DesktopSnapshot {
  epoch: number;
  sequence: number;
  ready: boolean;
  closing: boolean;
  queueEntryId: string;
  songId: string;
  title: string;
  artist: string;
  album: string;
  coverRef: string;
  playback: "paused" | "loading" | "playing";
  positionMs: number;
  durationMs: number;
  volume: number;
  favorite: boolean;
  canPrevious: boolean;
  canNext: boolean;
  cacheHit: boolean;
  provider: "" | "netease-direct" | "gd-netease";
  quality: string;
  currentLyric: string;
  nextLyric: string;
  translation: string;
  lyricProgress: number;
  error: string;
  options: DesktopSettings;
}
export type DesktopCommandName =
  | "play"
  | "pause"
  | "toggle-play"
  | "previous"
  | "next"
  | "seek"
  | "volume"
  | "toggle-favorite"
  | "toggle-lyrics-lock"
  | "toggle-mini-topmost"
  | "toggle-lyrics-topmost";
export interface DesktopCommand {
  epoch: number;
  command: DesktopCommandName;
  songId: string;
  value: number;
}
export interface DesktopNativeState {
  mainVisible: boolean;
  mainMinimized: boolean;
  miniVisible: boolean;
  lyricsVisible: boolean;
  lyricsLocked: boolean;
  taskbarAvailable: boolean;
  taskbarState: string;
  taskbarPositionMs: number;
  taskbarDurationMs: number;
  notificationRequests: number;
  notificationAccepted: number;
  lastError: string;
}
export interface TrackSnapshot {
  neteaseSongId: string;
  name: string;
  artists: { id?: string; name: string }[];
  album?: { id?: string; name: string };
  durationMs: number;
  coverRef?: string;
}
export interface QueueSnapshot {
  entries: { queueEntryId: string; track: TrackSnapshot }[];
  currentEntryId: string | null;
  currentIndex: number;
  playMode: "repeat-all" | "repeat-one" | "shuffle";
  positionMs: number;
  revision: number;
}
export interface SearchResult {
  tracks: TrackSnapshot[];
  total: number;
  hasMore: boolean;
}
export interface LyricData {
  original: string;
  translated: string;
}
export interface ResolvedStream {
  cacheHit?: boolean;
  cacheToken?: string;
  contentLengthBytes?: number;
  songId: string;
  provider: "netease-direct" | "gd-netease";
  url: string;
  requestedQuality: string;
  actualQuality?: string;
  actualFormat?: string;
  status: "full" | "trial" | "unavailable" | "unauthorized" | "unknown";
  expiresAt?: number;
  durationMs?: number;
  accessStrategy?: "normal" | "region-header" | "cdn-compat" | "cache";
  regionSuspected?: boolean;
  errorCode?: string;
  actualBitrate?: number;
  resolutionDeadlineAt?: number;
}
export const playbackQualities = ["exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster"] as const;
export type PlaybackQuality = (typeof playbackQualities)[number];
export interface PlaybackQualityPolicy {
  premiumAllowed: boolean;
  selectableQualities: PlaybackQuality[];
  maxBitrate: number;
}
export interface AudioCacheSettings {
  enabled: boolean;
  maxBytes: number;
}
export interface AudioCacheStats {
  available: boolean;
  enabled: boolean;
  maxBytes: number;
  usedBytes: number;
  cachedTracks: number;
  activeDownloads: number;
  errorCode?: string;
}
export interface HistoryPage {
  entries: HistoryEntry[];
  hasMore: boolean;
}
export interface FavoritePage {
  tracks: TrackSnapshot[];
  hasMore: boolean;
  nextAddedOrder: number;
  nextSongId: string;
}
export interface SearchHistoryEntry {
  query: string;
  searchedAt: number;
}
export interface ResourceCacheStats {
  available: boolean;
  enabled: boolean;
  maxBytes: number;
  usedBytes: number;
  cachedCovers: number;
  cachedLyrics: number;
  errorCode?: string;
}
export interface StorageRecoveryStatus {
  quarantinedFavorites: number;
  quarantinedHistory: number;
  settingsRecovered: boolean;
}
export interface LocalSettings {
  desktop?: DesktopSettings;
  resourceCache?: AudioCacheSettings;
  audioCache?: AudioCacheSettings;
  closeToTray?: boolean;
  preferredQuality: PlaybackQuality;
  regionMode: "auto" | "always" | "off";
  gdEnabled: boolean;
  volume: number;
}
export interface RegionStatus {
  classification: "CN" | "overseas" | "unknown";
  country?: string;
  observedAt: number;
  networkGeneration: number;
  detecting: boolean;
  errorCode?: string;
}
export interface Capabilities {
  regionCompatibility: boolean;
  gd: boolean;
  account: boolean;
}
export interface HistoryEntry {
  historyEntryId: string;
  track: TrackSnapshot;
  playedAt: number;
  listenedMs?: number;
}
export interface LocalPlaylistSummary {
  playlistId: string;
  name: string;
  trackCount: number;
  revision: number;
  createdAt: number;
  updatedAt: number;
}
export interface LocalPlaylist extends Omit<LocalPlaylistSummary, "trackCount"> {
  tracks: TrackSnapshot[];
}
export interface PlaylistUpdate {
  playlistId: string;
  expectedRevision: number;
  name: string;
  tracks: TrackSnapshot[];
}
export interface DesktopBridge {
  GetStorageRecoveryStatus?(): Promise<StorageRecoveryStatus>;
  GetDesktopEpoch?(): Promise<number>;
  GetDesktopSnapshot?(): Promise<DesktopSnapshot>;
  GetDesktopNativeState?(): Promise<DesktopNativeState>;
  ShowDesktopView?(kind: DesktopView): Promise<void>;
  HideDesktopView?(kind: DesktopView): Promise<void>;
  DispatchDesktopCommand?(command: DesktopCommand): Promise<void>;
  ResetDesktopViewPositions?(): Promise<void>;
  SearchLocalFavorites?(
    query: string,
    limit: number,
    beforeAddedOrder: number,
    beforeSongID: string,
    signal?: AbortSignal,
  ): Promise<FavoritePage>;
  SearchLocalHistory?(
    query: string,
    limit: number,
    beforePlayedAt: number,
    beforeEntryID: string,
    signal?: AbortSignal,
  ): Promise<HistoryPage>;
  GetLocalFavoriteIDs?(): Promise<string[]>;
  ListSearchHistory?(limit: number): Promise<SearchHistoryEntry[]>;
  RecordSearchQuery?(query: string): Promise<void>;
  DeleteSearchQuery?(query: string): Promise<void>;
  ClearSearchHistory?(): Promise<void>;
  ResolveCover?(coverRef: string, signal?: AbortSignal): Promise<string>;
  GetResourceCacheStats?(): Promise<ResourceCacheStats>;
  ClearResourceCache?(): Promise<{ removedCovers: number; removedLyrics: number }>;
  GetAudioCacheStats?(): Promise<AudioCacheStats>;
  ClearAudioCache?(): Promise<{ removedTracks: number; retainedActiveTracks: number }>;
  ReleasePlaybackSource?(token: string): Promise<void>;
  ReportPlaybackSourceFailure?(token: string): Promise<void>;
  MinimizeToTray?(): Promise<void>;
  ShowMainWindow?(): Promise<void>;
  ListLocalHistoryBefore?(limit: number, beforePlayedAt: number, beforeEntryID: string): Promise<HistoryPage>;
  ListLocalPlaylists(): Promise<LocalPlaylistSummary[]>;
  GetLocalPlaylist(id: string): Promise<LocalPlaylist>;
  CreateLocalPlaylist(name: string): Promise<LocalPlaylist>;
  UpdateLocalPlaylist(update: PlaylistUpdate): Promise<LocalPlaylist>;
  DeleteLocalPlaylist(id: string, expectedRevision: number): Promise<void>;
  GetPlaybackQualityPolicy(): Promise<PlaybackQualityPolicy>;
  GetSettings(): Promise<LocalSettings>;
  SetSettings(settings: LocalSettings): Promise<LocalSettings>;
  GetRegionStatus(): Promise<RegionStatus>;
  RefreshRegion(signal?: AbortSignal): Promise<RegionStatus>;
  GetCapabilities(): Promise<Capabilities>;
  GetLocalFavorites(): Promise<TrackSnapshot[]>;
  SetLocalFavorite(track: TrackSnapshot, liked: boolean): Promise<void>;
  AddLocalHistory(entry: HistoryEntry): Promise<void>;
  ListLocalHistory(limit: number, offset: number): Promise<HistoryEntry[]>;
  ClearLocalHistory(): Promise<void>;
  SearchNetease(query: string, page: number, signal?: AbortSignal): Promise<SearchResult>;
  GetLyrics(songID: string, signal?: AbortSignal): Promise<LyricData>;
  ResolveStream(songID: string, quality: string, signal?: AbortSignal): Promise<ResolvedStream>;
  LoadQueue(): Promise<QueueSnapshot | null>;
  SaveQueue(snapshot: QueueSnapshot): Promise<unknown>;
}
// Pick metadata explicitly: signed URLs and arbitrary service fields never enter snapshots.
export function trackSnapshot(t: TrackSnapshot): TrackSnapshot {
  return {
    neteaseSongId: t.neteaseSongId,
    name: t.name,
    artists: t.artists.map((a) => ({ id: a.id, name: a.name })),
    album: t.album ? { id: t.album.id, name: t.album.name } : undefined,
    durationMs: t.durationMs,
    coverRef: t.coverRef && validRemoteCover(t.coverRef) ? t.coverRef : undefined,
  };
}

export function validRemoteCover(value: string) {
  try {
    const u = new URL(value);
    return (
      u.protocol === "https:" &&
      !u.username &&
      !u.password &&
      !u.port &&
      !u.search &&
      !u.hash &&
      u.hostname.endsWith(".music.126.net") &&
      /\.(?:jpe?g|png|webp|gif|avif)$/i.test(u.pathname) &&
      new TextEncoder().encode(value).length <= 4096
    );
  } catch {
    return false;
  }
}

// Match Go strings.TrimSpace and the native UTF-8 byte boundary, without truncating code points.
export function normalizeLocalQuery(value: string) {
  const query = value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
  for (const character of query) {
    const code = character.codePointAt(0)!;
    if (code >= 0xd800 && code <= 0xdfff) throw new Error("Invalid search query");
  }
  if (new TextEncoder().encode(query).length > 512) throw new Error("Invalid search query");
  return query;
}

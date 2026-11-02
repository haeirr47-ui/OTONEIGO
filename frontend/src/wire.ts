import { playbackQualities, validRemoteCover, normalizeLocalQuery, type StorageRecoveryStatus } from "./contracts";
import { normalizeDesktopSettings } from "./desktopWire";
import type {
  QueueSnapshot,
  ResolvedStream,
  TrackSnapshot,
  LocalSettings,
  PlaybackQualityPolicy,
  RegionStatus,
  Capabilities,
  HistoryEntry,
  LocalPlaylist,
  LocalPlaylistSummary,
} from "./contracts";
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Invalid desktop response");
  return value as Record<string, unknown>;
}

export function fromStorageRecoveryStatus(value: unknown): StorageRecoveryStatus {
  const v=object(value);
  return {quarantinedFavorites: integer(v.quarantinedFavorites), quarantinedHistory: integer(v.quarantinedHistory), settingsRecovered: boolean(v.settingsRecovered)};
}
function string(value: unknown): string {
  if (typeof value !== "string") throw new Error("Invalid text");
  return value;
}
function optionalString(value: unknown) {
  return value == null ? undefined : string(value);
}
function number(value: unknown): number {
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0)
    throw new Error("Invalid number");
  return value;
}
function integer(value: unknown): number {
  const n = number(value);
  if (!Number.isSafeInteger(n)) throw new Error("Invalid integer");
  return n;
}
function boolean(value: unknown): boolean {
  if (typeof value !== "boolean") throw new Error("Invalid flag");
  return value;
}
function array(value: unknown): unknown[] {
  if (value == null) return [];
  if (!Array.isArray(value)) throw new Error("Invalid list");
  return value;
}
export function fromTrack(value: unknown): TrackSnapshot {
  const track = object(value);
  const album = track.album == null ? undefined : object(track.album);
  return {
    neteaseSongId: string(track.neteaseSongId),
    name: string(track.name),
    artists: array(track.artists).map((value) => {
      const a = object(value);
      return { id: optionalString(a.id), name: string(a.name) };
    }),
    album: album
      ? { id: optionalString(album.id), name: string(album.name) }
      : undefined,
    durationMs: number(track.durationMs),
    coverRef: optionalString(track.coverRef),
  };
}
export function fromQueue(value: unknown): QueueSnapshot | null {
  if (value === null) return null;
  const queue = object(value);
  const mode = queue.playMode;
  if (mode !== "repeat-all" && mode !== "repeat-one" && mode !== "shuffle")
    throw new Error("Invalid queue mode");
  const entries = array(queue.entries).map((value) => {
    const e = object(value);
    return { queueEntryId: string(e.queueEntryId), track: fromTrack(e.track) };
  });
  const currentIndex = queue.currentIndex;
  const currentEntryId =
    queue.currentEntryId === null ? null : string(queue.currentEntryId);
  if (
    typeof currentIndex !== "number" ||
    !Number.isInteger(currentIndex) ||
    (entries.length === 0
      ? currentEntryId !== null || currentIndex !== -1
      : entries[currentIndex]?.queueEntryId !== currentEntryId) ||
    new Set(entries.map((e) => e.queueEntryId)).size !== entries.length
  )
    throw new Error("Invalid queue selection");
  return {
    entries,
    currentEntryId,
    currentIndex,
    playMode: mode,
    positionMs: number(queue.positionMs),
    revision: integer(queue.revision),
  };
}
export function fromStream(value: unknown): ResolvedStream {
  const stream = object(value);
  if (stream.provider !== "netease-direct" && stream.provider !== "gd-netease")
    throw new Error("Unsupported playback provider");
  const status = stream.status;
  if (
    status !== "full" &&
    status !== "trial" &&
    status !== "unavailable" &&
    status !== "unauthorized" &&
    status !== "unknown"
  )
    throw new Error("Invalid playback status");
  const accessStrategy = stream.accessStrategy;
  if (
    accessStrategy !== undefined &&
    accessStrategy !== "normal" &&
    accessStrategy !== "region-header" &&
    accessStrategy !== "cdn-compat" &&
    accessStrategy !== "cache"
  )
    throw new Error("Invalid access strategy");
  validateStreamURL(stream);
  return {
    cacheToken: optionalString(stream.cacheToken),
    cacheHit: stream.cacheHit == null ? undefined : boolean(stream.cacheHit),
    contentLengthBytes: stream.contentLengthBytes == null ? undefined : integer(stream.contentLengthBytes),
    songId: string(stream.songId),
    url: string(stream.url),
    provider: stream.provider,
    requestedQuality: string(stream.requestedQuality),
    actualQuality: optionalString(stream.actualQuality),
    actualFormat: optionalString(stream.actualFormat),
    status,
    expiresAt: stream.expiresAt == null ? undefined : number(stream.expiresAt),
    durationMs:
      stream.durationMs == null ? undefined : number(stream.durationMs),
    accessStrategy,
    errorCode: optionalString(stream.errorCode),
    regionSuspected:
      stream.regionSuspected == null
        ? undefined
        : boolean(stream.regionSuspected),
    actualBitrate:
      stream.actualBitrate == null ? undefined : number(stream.actualBitrate),
    resolutionDeadlineAt:
      stream.resolutionDeadlineAt == null
        ? undefined
        : integer(stream.resolutionDeadlineAt),
  };
}
export function fromSettings(value: unknown): LocalSettings {
  const s = object(value);
  const quality = s.preferredQuality === "standard" || s.preferredQuality === "higher"
    ? "exhigh" : s.preferredQuality;
  if (
    quality !== "exhigh" &&
    quality !== "lossless" &&
    quality !== "hires" &&
    quality !== "jyeffect" &&
    quality !== "sky" &&
    quality !== "jymaster"
  )
    throw new Error("Invalid quality");
  const mode = s.regionMode;
  if (mode !== "auto" && mode !== "always" && mode !== "off")
    throw new Error("Invalid region mode");
  const volume = number(s.volume);
  if (volume > 1) throw new Error("Invalid volume");
  return {
    desktop: normalizeDesktopSettings(s.desktop),
    audioCache: normalizeAudioCache(s.audioCache),
    resourceCache: normalizeResourceCache(s.resourceCache),
    closeToTray: s.closeToTray == null ? false : boolean(s.closeToTray),
    preferredQuality: quality,
    regionMode: mode,
    gdEnabled: boolean(s.gdEnabled),
    volume,
  };
}
export function fromRegion(value: unknown): RegionStatus {
  const s = object(value);
  const classification = s.classification;
  if (
    classification !== "CN" &&
    classification !== "overseas" &&
    classification !== "unknown"
  )
    throw new Error("Invalid region");
  const country = optionalString(s.country);
  if (country && !/^[A-Z]{2}$/.test(country))
    throw new Error("Invalid country");
  return {
    classification,
    country,
    observedAt: integer(s.observedAt),
    networkGeneration: integer(s.networkGeneration),
    detecting: boolean(s.detecting),
    errorCode: optionalString(s.errorCode),
  };
}
export function fromCapabilities(value: unknown): Capabilities {
  const c = object(value);
  return {
    regionCompatibility: boolean(c.regionCompatibility),
    gd: boolean(c.gd),
    account: boolean(c.account),
  };
}
export function fromHistory(value: unknown): HistoryEntry {
  const h = object(value);
  return {
    historyEntryId: string(h.historyEntryId),
    track: fromTrack(h.track),
    playedAt: integer(h.playedAt),
    listenedMs: h.listenedMs == null ? undefined : integer(h.listenedMs),
  };
}
export const fromFavorites = (value: unknown) => array(value).map(fromTrack);
export const fromHistoryList = (value: unknown) =>
  array(value).map(fromHistory);

function playlistName(value: unknown): string {
  const name = string(value);
  if (name !== name.trim() || [...name].length < 1 || [...name].length > 128 || /[\p{Cc}]/u.test(name))
    throw new Error("Invalid playlist name");
  return name;
}
export function fromPlaylistSummary(value: unknown): LocalPlaylistSummary {
  const p = object(value);
  const playlistId = string(p.playlistId);
  const revision = integer(p.revision);
  const trackCount = integer(p.trackCount);
  const createdAt = integer(p.createdAt);
  const updatedAt = integer(p.updatedAt);
  if (!/^[a-zA-Z0-9_-]{1,128}$/.test(playlistId) || revision < 1 || trackCount > 1000 || createdAt < 1 || updatedAt < createdAt)
    throw new Error("Invalid playlist metadata");
  return { playlistId, name: playlistName(p.name), revision, trackCount, createdAt, updatedAt };
}
function playlistTrack(value: unknown): TrackSnapshot {
  const track = fromTrack(value);
  const validId = (id: string) => /^[1-9][0-9]{0,29}$/.test(id);
  const bytes = (text: string) => new TextEncoder().encode(text).length;
  const validOptionalId = (id?: string) => !id || validId(id);
  let validCover = !track.coverRef;
  if (track.coverRef && bytes(track.coverRef) <= 4096) {
    try {
      const url = new URL(track.coverRef);
      validCover = url.protocol === "https:" && !url.username && !url.password && !url.port &&
        !url.search && !url.hash && url.hostname.endsWith(".music.126.net") &&
        /\.(jpg|jpeg|png|webp|gif|avif)$/i.test(url.pathname);
    } catch { /* Reject malformed cover references. */ }
  }
  if (!validId(track.neteaseSongId) || !track.name.trim() || bytes(track.name) > 1024 ||
      track.artists.length > 100 || track.artists.some(a => !validOptionalId(a.id) || bytes(a.name) > 1024) ||
      (track.album && (!validOptionalId(track.album.id) || bytes(track.album.name) > 1024)) ||
      !Number.isSafeInteger(track.durationMs) || track.durationMs > 86400000 || !validCover)
    throw new Error("Invalid playlist track");
  return track;
}
export function fromPlaylist(value: unknown): LocalPlaylist {
  const p = object(value);
  const tracks = array(p.tracks).map(playlistTrack);
  const summary = fromPlaylistSummary({ ...p, trackCount: tracks.length });
  if (new Set(tracks.map(t => t.neteaseSongId)).size !== tracks.length)
    throw new Error("Invalid playlist tracks");
  const { trackCount: _, ...metadata } = summary;
  const result = { ...metadata, tracks };
  if (new TextEncoder().encode(JSON.stringify(result)).length > 2 * 1024 * 1024)
    throw new Error("Playlist too large");
  return result;
}
export function fromPlaylistList(value: unknown): LocalPlaylistSummary[] {
  const result = array(value).map(fromPlaylistSummary);
  if (result.length > 100 || new Set(result.map(p => p.playlistId)).size !== result.length)
    throw new Error("Invalid playlist list");
  return result;
}

export function fromPlaybackQualityPolicy(value: unknown): PlaybackQualityPolicy {
  const policy = object(value);
  const premiumAllowed = boolean(policy.premiumAllowed);
  const maxBitrate = integer(policy.maxBitrate);
  const expected: PlaybackQualityPolicy["selectableQualities"] = premiumAllowed ? [...playbackQualities] : ["exhigh"];
  const selectable = policy.selectableQualities;
  if (!Array.isArray(selectable) ||
      selectable.length !== expected.length ||
      new Set(selectable).size !== expected.length ||
      expected.some(quality => !selectable.includes(quality)) ||
      maxBitrate !== (premiumAllowed ? 0 : 320000))
    throw new Error("Invalid playback quality policy");
  return { premiumAllowed, selectableQualities: expected, maxBitrate };
}

export function normalizeAudioCache(value: unknown) {
  if (value == null) return { enabled: true, maxBytes: 20 * 1024 ** 3 };
  const c = object(value);
  const maxBytes = c.maxBytes === 0 || c.maxBytes == null ? 20 * 1024 ** 3 : integer(c.maxBytes);
  if (maxBytes < 64 * 1024 ** 2 || maxBytes > 1024 ** 4) throw new Error("Invalid cache limit");
  return { enabled: c.enabled == null ? true : boolean(c.enabled), maxBytes };
}
export function validateStreamURL(stream: { url?: unknown; cacheToken?: unknown; cacheHit?: unknown }) {
  const token = stream.cacheToken;
  if (token !== undefined || stream.cacheHit === true) {
    if (typeof token !== "string" || !/^[a-f0-9]{32}$/.test(token)) throw new Error("Invalid cache lease");
    if (typeof stream.url !== "string" || !new RegExp("^http://127\\.0\\.0\\.1:[1-9][0-9]{0,4}/media/" + token + "$").test(stream.url)) throw new Error("Invalid cache source");
    const url = new URL(stream.url);
    if (!url.port || Number(url.port) > 65535) throw new Error("Invalid cache port");
  } else if (stream.url) {
    const url = new URL(string(stream.url));
    const host = url.hostname.toLowerCase().replace(/\.$/, "");
    // URL canonicalizes short/hex IPv4 forms. Numeric DNS labels are not IP literals.
    const ipv4 = /^\d+\.\d+\.\d+\.\d+$/.test(host);
    if (!["https:", "http:"].includes(url.protocol) || url.username || url.password ||
        host === "localhost" || host.endsWith(".localhost") || host.includes(":") ||
        (ipv4 && (/^(127|10|0)\./.test(host) || /^192\.168\./.test(host) || /^169\.254\./.test(host) ||
        /^172\.(1[6-9]|2[0-9]|3[01])\./.test(host)))) throw new Error("Invalid playback source");
  }
}
export function fromHistoryPage(value: unknown) {
  const p = object(value);
  const entries = fromHistoryList(p.entries);
  if (entries.length > 200 || new Set(entries.map(e => e.historyEntryId)).size !== entries.length) throw new Error("Invalid history page");
  return { entries, hasMore: boolean(p.hasMore) };
}
export function fromAudioCacheStats(value: unknown) {
  const c = object(value);
  return { available: boolean(c.available), enabled: boolean(c.enabled), maxBytes: integer(c.maxBytes),
    usedBytes: integer(c.usedBytes), cachedTracks: integer(c.cachedTracks), activeDownloads: integer(c.activeDownloads), errorCode: optionalString(c.errorCode) };
}
export function fromCacheClear(value: unknown) {
  const c = object(value);
  return { removedTracks: integer(c.removedTracks), retainedActiveTracks: integer(c.retainedActiveTracks) };
}

export function normalizeResourceCache(value: unknown) {
  if (value == null) return { enabled: true, maxBytes: 1024 ** 3 };
  const c = object(value);
  const maxBytes = c.maxBytes === 0 || c.maxBytes == null ? 1024 ** 3 : integer(c.maxBytes);
  if (maxBytes < 64 * 1024 ** 2 || maxBytes > 1024 ** 4) throw new Error("Invalid resource cache limit");
  return { enabled: c.enabled == null ? true : boolean(c.enabled), maxBytes };
}
export function fromFavoriteIDs(value: unknown): string[] {
  const ids = array(value).map(string);
  if (ids.some(id => !/^[1-9][0-9]*$/.test(id)) || new Set(ids).size !== ids.length) throw new Error("Invalid favorite IDs");
  return ids;
}
export function fromFavoritePage(value: unknown) {
  const p = object(value);
  const tracks = fromFavorites(p.tracks);
  if (tracks.length > 200 || new Set(tracks.map(t => t.neteaseSongId)).size !== tracks.length) throw new Error("Invalid favorite page");
  const result = { tracks, hasMore: boolean(p.hasMore), nextAddedOrder: integer(p.nextAddedOrder), nextSongId: string(p.nextSongId) };
  if (result.hasMore && (!tracks.length || !/^[1-9][0-9]*$/.test(result.nextSongId) || !result.nextAddedOrder)) throw new Error("Invalid favorite cursor");
  return result;
}
export function fromSearchHistory(value: unknown) {
  const entries = array(value).map(value => { const e = object(value); return { query: string(e.query), searchedAt: integer(e.searchedAt) }; });
  if (entries.length > 100 || entries.some(e => !e.query || normalizeLocalQuery(e.query) !== e.query) || new Set(entries.map(e => e.query)).size !== entries.length) throw new Error("Invalid search history");
  return entries;
}
export { validRemoteCover };
export function fromResolvedCover(value: unknown) {
  const ref = string(value);
  if (!/^\/artwork\/[a-f0-9]{64}$/.test(ref) && !validRemoteCover(ref)) throw new Error("Invalid cover source");
  return ref;
}
export function fromResourceCacheStats(value: unknown) {
  const c = object(value);
  return { available: boolean(c.available), enabled: boolean(c.enabled), maxBytes: integer(c.maxBytes), usedBytes: integer(c.usedBytes), cachedCovers: integer(c.cachedCovers), cachedLyrics: integer(c.cachedLyrics), errorCode: optionalString(c.errorCode) };
}
export function fromResourceCacheClear(value: unknown) {
  const c = object(value);
  return { removedCovers: integer(c.removedCovers), removedLyrics: integer(c.removedLyrics) };
}

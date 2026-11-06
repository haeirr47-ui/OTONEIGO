import {
  fromDesktopEpoch,
  fromDesktopSnapshot,
  fromDesktopNativeState,
  fromDesktopCommand,
  normalizeDesktopSettings,
} from "./desktopWire";
import "@wailsio/runtime";
import * as service from "../bindings/github.com/voici5986/otoneigo/internal/appservice/appservice";
import type { DesktopBridge } from "./contracts";
import {
  fromStorageRecoveryStatus,
  normalizeResourceCache,
  fromFavoritePage,
  fromFavoriteIDs,
  fromSearchHistory,
  fromResolvedCover,
  fromResourceCacheStats,
  fromResourceCacheClear,
  normalizeAudioCache,
  fromAudioCacheStats,
  fromCacheClear,
  fromHistoryPage,
  fromTrack,
  fromPlaylist,
  fromPlaylistList,
  fromQueue,
  fromStream,
  fromSettings,
  fromPlaybackQualityPolicy,
  fromRegion,
  fromCapabilities,
  fromFavorites,
  fromHistoryList,
} from "./wire";
import { trackSnapshot } from "./contracts";
import { fromPublicPlaylist, fromToplists } from "./discoveryWire";
export const desktopBridge: DesktopBridge = {
  async GetToplists(signal) {
    const call = service.GetToplists();
    return fromToplists(await (signal ? call.cancelOn(signal) : call));
  },
  async GetPublicPlaylist(id, offset, signal) {
    if (
      typeof id !== "string" ||
      !/^[1-9][0-9]{0,29}$/.test(id) ||
      !Number.isSafeInteger(offset) ||
      offset < 0 ||
      offset > 990 ||
      offset % 30 !== 0
    )
      throw new Error("INVALID_PLAYLIST_REQUEST");
    const call = service.GetPublicPlaylist(id, offset);
    const page = fromPublicPlaylist(await (signal ? call.cancelOn(signal) : call));
    if (page.playlistId !== id || page.offset !== offset) throw new Error("MISMATCHED_PLAYLIST_RESPONSE");
    return page;
  },
  async GetStorageRecoveryStatus() {
    return fromStorageRecoveryStatus(await service.GetStorageRecoveryStatus());
  },
  async GetDesktopEpoch() {
    return fromDesktopEpoch(await service.GetDesktopEpoch());
  },
  async GetDesktopSnapshot() {
    return fromDesktopSnapshot(await service.GetDesktopSnapshot());
  },
  async GetDesktopNativeState() {
    return fromDesktopNativeState(await service.GetDesktopNativeState());
  },
  async ShowDesktopView(kind) {
    await service.ShowDesktopView(kind);
  },
  async HideDesktopView(kind) {
    await service.HideDesktopView(kind);
  },
  async DispatchDesktopCommand(command) {
    await service.DispatchDesktopCommand(fromDesktopCommand(command));
  },
  async ResetDesktopViewPositions() {
    await service.ResetDesktopViewPositions();
  },
  async SearchLocalFavorites(query, limit, order, id, signal) {
    const call = service.SearchLocalFavorites(query, limit, order, id);
    return fromFavoritePage(await (signal ? call.cancelOn(signal) : call));
  },
  async SearchLocalHistory(query, limit, playedAt, id, signal) {
    const call = service.SearchLocalHistory(query, limit, playedAt, id);
    return fromHistoryPage(await (signal ? call.cancelOn(signal) : call));
  },
  async GetLocalFavoriteIDs() {
    return fromFavoriteIDs(await service.GetLocalFavoriteIDs());
  },
  async ListSearchHistory(limit) {
    return fromSearchHistory(await service.ListSearchHistory(limit));
  },
  async RecordSearchQuery(query) {
    await service.RecordSearchQuery(query);
  },
  async DeleteSearchQuery(query) {
    await service.DeleteSearchQuery(query);
  },
  async ClearSearchHistory() {
    await service.ClearSearchHistory();
  },
  async ResolveCover(ref, signal) {
    const call = service.ResolveCover(ref);
    return fromResolvedCover(await (signal ? call.cancelOn(signal) : call));
  },
  async GetResourceCacheStats() {
    return fromResourceCacheStats(await service.GetResourceCacheStats());
  },
  async ClearResourceCache() {
    return fromResourceCacheClear(await service.ClearResourceCache());
  },
  async GetAudioCacheStats() {
    return fromAudioCacheStats(await service.GetAudioCacheStats());
  },
  async ClearAudioCache() {
    return fromCacheClear(await service.ClearAudioCache());
  },
  async ReleasePlaybackSource(token) {
    await service.ReleasePlaybackSource(token);
  },
  async ReportPlaybackSourceFailure(token) {
    await service.ReportPlaybackSourceFailure(token);
  },
  async MinimizeToTray() {
    await service.MinimizeToTray();
  },
  async ShowMainWindow() {
    await service.ShowMainWindow();
  },
  async ListLocalHistoryBefore(limit, playedAt, id) {
    return fromHistoryPage(await service.ListLocalHistoryBefore(limit, playedAt, id));
  },
  async ListLocalPlaylists() {
    return fromPlaylistList(await service.ListLocalPlaylists());
  },
  async GetLocalPlaylist(id) {
    return fromPlaylist(await service.GetLocalPlaylist(id));
  },
  async CreateLocalPlaylist(name) {
    return fromPlaylist(await service.CreateLocalPlaylist(name));
  },
  async UpdateLocalPlaylist(update) {
    return fromPlaylist(
      await service.UpdateLocalPlaylist({
        playlistId: update.playlistId,
        expectedRevision: update.expectedRevision,
        name: update.name,
        tracks: update.tracks.map(trackSnapshot),
      }),
    );
  },
  async DeleteLocalPlaylist(id, revision) {
    await service.DeleteLocalPlaylist(id, revision);
  },
  async GetPlaybackQualityPolicy() {
    return fromPlaybackQualityPolicy(await service.GetPlaybackQualityPolicy());
  },
  async GetSettings() {
    return fromSettings(await service.GetSettings());
  },
  async SetSettings(settings) {
    const normalized = fromSettings(settings);
    return fromSettings(
      await service.SetSettings({
        ...normalized,
        desktop: normalizeDesktopSettings(normalized.desktop),
        audioCache: normalizeAudioCache(normalized.audioCache),
        resourceCache: normalizeResourceCache(normalized.resourceCache),
        closeToTray: normalized.closeToTray ?? false,
      }),
    );
  },
  async GetCapabilities() {
    return fromCapabilities(await service.GetCapabilities());
  },
  async GetRegionStatus() {
    return fromRegion(await service.GetRegionStatus());
  },
  async RefreshRegion(signal) {
    const call = service.RefreshRegion();
    return fromRegion(await (signal ? call.cancelOn(signal) : call));
  },
  async GetLocalFavorites() {
    return fromFavorites(await service.GetLocalFavorites());
  },
  async SetLocalFavorite(track, liked) {
    await service.SetLocalFavorite(trackSnapshot(track), liked);
  },
  async AddLocalHistory(entry) {
    await service.AddLocalHistory({
      historyEntryId: entry.historyEntryId,
      track: trackSnapshot(entry.track),
      playedAt: Math.round(entry.playedAt),
      listenedMs: entry.listenedMs === undefined ? undefined : Math.round(entry.listenedMs),
    });
  },
  async ListLocalHistory(limit, offset) {
    return fromHistoryList(await service.ListLocalHistory(limit, offset));
  },
  async ClearLocalHistory() {
    await service.ClearLocalHistory();
  },
  async SearchNetease(query, page, signal) {
    // The interface displays pages starting at 1; the Go service uses zero-based offsets.
    const call = service.SearchNetease(query, page - 1);
    const result = await (signal ? call.cancelOn(signal) : call);
    return { ...result, tracks: (result.tracks ?? []).map(fromTrack) };
  },
  async GetLyrics(id, signal) {
    const call = service.GetLyrics(id);
    return await (signal ? call.cancelOn(signal) : call);
  },
  async ResolveStream(id, quality, signal) {
    const call = service.ResolveStream(id, quality);
    return fromStream(await (signal ? call.cancelOn(signal) : call));
  },
  async LoadQueue() {
    return fromQueue(await service.LoadQueue());
  },
  async SaveQueue(snapshot) {
    await service.SaveQueue(snapshot);
  },
};

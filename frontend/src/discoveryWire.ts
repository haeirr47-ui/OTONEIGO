import {
  validRemoteCover,
  type EntityDetail,
  type EntityDetailKind,
  type EntitySearchKind,
  type EntitySearchResult,
  type EntitySummary,
  type PublicPlaylistPage,
  type Toplist,
  type ToplistResult,
} from "./contracts";
import { fromTrack } from "./wire";

const invalid = () => new Error("INVALID_DISCOVERY_RESPONSE");
const decimalID = /^[1-9][0-9]{0,29}$/;
function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid();
  return value as Record<string, unknown>;
}
function text(value: unknown, maximum: number, required = false): string {
  if (typeof value !== "string" || new TextEncoder().encode(value).length > maximum || (required && !value.trim()))
    throw invalid();
  return value;
}
function id(value: unknown): string {
  if (typeof value !== "string" || !decimalID.test(value)) throw invalid();
  return value;
}
function integer(value: unknown, maximum = Number.MAX_SAFE_INTEGER): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0 || value > maximum) throw invalid();
  return value;
}
function flag(value: unknown): boolean {
  if (typeof value !== "boolean") throw invalid();
  return value;
}
function list(value: unknown, maximum: number): unknown[] {
  if (!Array.isArray(value) || value.length > maximum) throw invalid();
  return value;
}
function cover(value: unknown): string {
  const result = text(value, 4096);
  if (result && !validRemoteCover(result)) throw invalid();
  return result;
}
function summary(value: unknown): Omit<Toplist, "trackCount"> {
  const v = record(value);
  return {
    playlistId: id(v.playlistId),
    name: text(v.name, 512, true),
    description: text(v.description, 4096),
    coverRef: cover(v.coverRef),
  };
}
export function fromToplists(value: unknown): ToplistResult {
  const v = record(value);
  const items = list(v.items, 100).map((item) => ({ ...summary(item), trackCount: integer(record(item).trackCount) }));
  if (new Set(items.map((item) => item.playlistId)).size !== items.length) throw invalid();
  return { items, partial: flag(v.partial) };
}
export function fromPublicPlaylist(value: unknown): PublicPlaylistPage {
  const v = record(value);
  const result = {
    ...summary(v),
    total: integer(v.total),
    availableCount: integer(v.availableCount, 1000),
    offset: integer(v.offset, 990),
    nextOffset: integer(v.nextOffset, 1000),
    hasMore: flag(v.hasMore),
    partial: flag(v.partial),
    tracks: list(v.tracks, 30).map((raw) => {
      const track = fromTrack(raw);
      id(track.neteaseSongId);
      text(track.name, 1024, true);
      integer(track.durationMs, 86400000);
      if (track.artists.length > 100) throw invalid();
      for (const artist of track.artists) {
        if (artist.id) id(artist.id);
        text(artist.name, 1024);
      }
      if (track.album) {
        if (track.album.id) id(track.album.id);
        text(track.album.name, 1024);
      }
      if (track.coverRef) cover(track.coverRef);
      return track;
    }),
  };
  if (
    result.offset % 30 !== 0 ||
    result.availableCount > result.total ||
    (result.hasMore && (result.nextOffset !== result.offset + 30 || result.nextOffset >= result.availableCount)) ||
    new Set(result.tracks.map((track) => track.neteaseSongId)).size !== result.tracks.length
  )
    throw invalid();
  return result;
}
export function fromEntitySearch(value: unknown, expected: EntitySearchKind): EntitySearchResult {
  const v = record(value);
  if (v.kind !== expected) throw invalid();
  const items: EntitySummary[] = list(v.items, 30).map((raw) => {
    const item = record(raw);
    return {
      id: id(item.id),
      name: text(item.name, 512, true),
      subtitle: item.subtitle === undefined ? "" : text(item.subtitle, 512),
      coverRef: item.coverRef === undefined ? "" : cover(item.coverRef),
      trackCount: item.trackCount === undefined ? 0 : integer(item.trackCount, 2147483647),
    };
  });
  if (new Set(items.map((item) => item.id)).size !== items.length) throw invalid();
  return {
    kind: expected,
    items,
    total: integer(v.total, 2147483647),
    hasMore: flag(v.hasMore),
    partial: flag(v.partial),
  };
}
export function fromEntityDetail(value: unknown, expectedKind: EntityDetailKind, expectedId: string): EntityDetail {
  const v = record(value);
  if (v.kind !== expectedKind || id(v.id) !== expectedId) throw invalid();
  const tracks = list(v.tracks, 200).map((raw) => {
    const track = fromTrack(raw);
    id(track.neteaseSongId);
    text(track.name, 1024, true);
    integer(track.durationMs, 86400000);
    if (track.coverRef) cover(track.coverRef);
    return track;
  });
  if (new Set(tracks.map((track) => track.neteaseSongId)).size !== tracks.length) throw invalid();
  const total = integer(v.total, 200);
  if (total < tracks.length) throw invalid();
  return {
    kind: expectedKind,
    id: expectedId,
    name: text(v.name, 512, true),
    subtitle: v.subtitle === undefined ? "" : text(v.subtitle, 512),
    description: v.description === undefined ? "" : text(v.description, 4096),
    coverRef: v.coverRef === undefined ? "" : cover(v.coverRef),
    tracks,
    total,
    partial: flag(v.partial),
  };
}

import type {
  DesktopCommand,
  DesktopNativeState,
  DesktopSettings,
  DesktopSnapshot,
  WindowPlacement,
} from "./contracts";
import { validRemoteCover } from "./contracts";

export function defaultDesktopSettings(): DesktopSettings {
  return {
    miniAlwaysOnTop: true,
    lyricsAlwaysOnTop: true,
    lyricsLocked: false,
    lyricsFontSize: 28,
    lyricsOpacity: 0.9,
    notifyTrackChanges: false,
    taskbarProgress: true,
    miniPlacement: { valid: false, x: 0, y: 0, width: 420, height: 176 },
    lyricsPlacement: { valid: false, x: 0, y: 0, width: 820, height: 156 },
  };
}
function object(v: unknown): Record<string, unknown> {
  if (!v || typeof v !== "object" || Array.isArray(v)) throw new Error("Invalid desktop response");
  return v as Record<string, unknown>;
}
function flag(v: unknown): boolean {
  if (typeof v !== "boolean") throw new Error("Invalid desktop flag");
  return v;
}
function num(v: unknown, min: number, max: number, integer = false): number {
  if (typeof v !== "number" || !Number.isFinite(v) || v < min || v > max || (integer && !Number.isSafeInteger(v)))
    throw new Error("Invalid desktop number");
  return v;
}
function text(v: unknown, bytes: number): string {
  if (
    typeof v !== "string" ||
    v.includes("\0") ||
    /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(v) ||
    new TextEncoder().encode(v).length > bytes
  )
    throw new Error("Invalid desktop text");
  return v;
}
function placement(v: unknown): WindowPlacement {
  const p = object(v);
  return {
    valid: flag(p.valid),
    x: num(p.x, -100000, 100000, true),
    y: num(p.y, -100000, 100000, true),
    width: num(p.width, 100, 8192, true),
    height: num(p.height, 80, 8192, true),
  };
}
export function normalizeDesktopSettings(v?: unknown): DesktopSettings {
  if (v == null) return defaultDesktopSettings();
  const s = object(v);
  // Old fixtures/settings may omit the entire desktop section, but malformed supplied fields fail closed.
  return {
    miniAlwaysOnTop: flag(s.miniAlwaysOnTop),
    lyricsAlwaysOnTop: flag(s.lyricsAlwaysOnTop),
    lyricsLocked: flag(s.lyricsLocked),
    lyricsFontSize: num(s.lyricsFontSize, 16, 64, true),
    lyricsOpacity: num(s.lyricsOpacity, 0.3, 1),
    notifyTrackChanges: flag(s.notifyTrackChanges),
    taskbarProgress: flag(s.taskbarProgress),
    miniPlacement: placement(s.miniPlacement),
    lyricsPlacement: placement(s.lyricsPlacement),
  };
}
export const fromDesktopEpoch = (v: unknown) => num(v, 1, Number.MAX_SAFE_INTEGER, true);
function wireObject(v: unknown) {
  if (typeof v === "string") {
    if (new TextEncoder().encode(v).length > 65536) throw new Error("Desktop payload too large");
    v = JSON.parse(v);
  }
  const result = object(v);
  if (new TextEncoder().encode(JSON.stringify(result)).length > 65536) throw new Error("Desktop payload too large");
  return result;
}
function knownFields(value: Record<string, unknown>, names: string) {
  const allowed = new Set(names.split(" "));
  if (Object.keys(value).some((key) => !allowed.has(key))) throw new Error("Unexpected desktop field");
}
export function fromDesktopSnapshot(v: unknown): DesktopSnapshot {
  const s = wireObject(v);
  knownFields(
    s,
    "epoch sequence ready closing queueEntryId songId title artist album coverRef playback positionMs durationMs volume favorite canPrevious canNext cacheHit provider quality currentLyric nextLyric translation lyricProgress error options",
  );
  object(s.options);
  const playback = s.playback,
    provider = s.provider;
  if (playback !== "paused" && playback !== "loading" && playback !== "playing")
    throw new Error("Invalid desktop playback");
  if (provider !== "" && provider !== "netease-direct" && provider !== "gd-netease")
    throw new Error("Invalid desktop provider");
  const coverRef = text(s.coverRef, 4096),
    songId = text(s.songId, 128),
    queueEntryId = text(s.queueEntryId, 128);
  if (
    (coverRef && !validRemoteCover(coverRef)) ||
    (songId && !/^[1-9][0-9]{0,29}$/.test(songId)) ||
    (queueEntryId && !/^[A-Za-z0-9_-]{1,128}$/.test(queueEntryId))
  )
    throw new Error("Invalid desktop identity");
  const sequence = num(s.sequence, 0, Number.MAX_SAFE_INTEGER, true),
    ready = flag(s.ready);
  if (sequence === 0 && ready) throw new Error("Invalid initial desktop snapshot");
  return {
    epoch: fromDesktopEpoch(s.epoch),
    sequence,
    ready,
    closing: flag(s.closing),
    queueEntryId,
    songId,
    title: text(s.title, 1024),
    artist: text(s.artist, 1024),
    album: text(s.album, 1024),
    coverRef,
    playback,
    provider,
    positionMs: num(s.positionMs, 0, 86400000, true),
    durationMs: num(s.durationMs, 0, 86400000, true),
    volume: num(s.volume, 0, 1),
    favorite: flag(s.favorite),
    canPrevious: flag(s.canPrevious),
    canNext: flag(s.canNext),
    cacheHit: flag(s.cacheHit),
    quality: text(s.quality, 128),
    currentLyric: text(s.currentLyric, 4096),
    nextLyric: text(s.nextLyric, 4096),
    translation: text(s.translation, 4096),
    lyricProgress: num(s.lyricProgress, 0, 1),
    error: text(s.error, 1024),
    options: normalizeDesktopSettings(s.options),
  };
}
const commands = new Set([
  "play",
  "pause",
  "toggle-play",
  "previous",
  "next",
  "seek",
  "volume",
  "toggle-favorite",
  "toggle-lyrics-lock",
  "toggle-mini-topmost",
  "toggle-lyrics-topmost",
]);
export function fromDesktopCommand(v: unknown): DesktopCommand {
  const s = wireObject(v),
    command = text(s.command, 64),
    songId = text(s.songId, 128);
  knownFields(s, "epoch command songId value");
  if (
    !commands.has(command) ||
    ((command === "seek" || command === "toggle-favorite") && !/^[1-9][0-9]{0,29}$/.test(songId))
  )
    throw new Error("Invalid desktop command");
  return {
    epoch: fromDesktopEpoch(s.epoch),
    command: command as DesktopCommand["command"],
    songId,
    value: num(s.value, 0, command === "volume" ? 1 : 86400000),
  };
}
export function fromDesktopNativeState(v: unknown): DesktopNativeState {
  const s = wireObject(v);
  return {
    mainVisible: flag(s.mainVisible),
    mainMinimized: flag(s.mainMinimized),
    miniVisible: flag(s.miniVisible),
    lyricsVisible: flag(s.lyricsVisible),
    lyricsLocked: flag(s.lyricsLocked),
    taskbarAvailable: flag(s.taskbarAvailable),
    taskbarState: text(s.taskbarState, 128),
    taskbarPositionMs: num(s.taskbarPositionMs, 0, 86400000, true),
    taskbarDurationMs: num(s.taskbarDurationMs, 0, 86400000, true),
    notificationRequests: num(s.notificationRequests, 0, Number.MAX_SAFE_INTEGER, true),
    notificationAccepted: num(s.notificationAccepted, 0, Number.MAX_SAFE_INTEGER, true),
    lastError: text(s.lastError, 1024),
  };
}
export function acceptDesktopSnapshot(previous: DesktopSnapshot | null, next: DesktopSnapshot): boolean {
  if (!previous) return true;
  if (next.epoch !== previous.epoch) return next.epoch > previous.epoch;
  return !previous.closing && next.sequence > previous.sequence;
}

import type { DesktopBridge, DesktopSettings, DesktopSnapshot } from "./contracts";
import type { PlayerStore } from "./PlayerStore";
import { validRemoteCover } from "./contracts";
import { actualStreamQuality } from "./format";
import { alignLyrics } from "./lyrics";
import { fromDesktopCommand, fromDesktopEpoch, normalizeDesktopSettings } from "./desktopWire";

export interface DesktopEvents {
  On(name: string, callback: (event: { data: unknown }) => void): () => void;
  Emit(name: string, data: unknown): unknown;
}
export function truncateUTF8(value: string, maximum: number): string {
  let result = "",
    bytes = 0;
  for (const character of value) {
    if (character === "\0") continue;
    const code = character.codePointAt(0) ?? 0;
    const safe = code >= 0xd800 && code <= 0xdfff ? "\uFFFD" : character;
    const size = new TextEncoder().encode(safe).length;
    if (bytes + size > maximum) break;
    result += safe;
    bytes += size;
  }
  return result;
}
// The lyric cache is local to the main projection, never part of a wire snapshot.
export function createDesktopProjector(store: PlayerStore) {
  let original = "",
    translated = "",
    lines: ReturnType<typeof alignLyrics> = [];
  return (epoch: number, sequence: number): DesktopSnapshot => {
    const state = store.getSnapshot(),
      q = state.queue,
      entry = q.entries[q.currentIndex],
      track = entry?.track;
    if (original !== (state.lyrics?.original ?? "") || translated !== (state.lyrics?.translated ?? "")) {
      original = state.lyrics?.original ?? "";
      translated = state.lyrics?.translated ?? "";
      lines = alignLyrics(original, translated);
    }
    const positionMs = store.getPlaybackPosition();
    let index = -1;
    for (let i = 0; i < lines.length && lines[i].time <= positionMs; i++) index = i;
    const line = lines[index],
      next = lines[index + 1];
    const durationMs = Math.max(0, Math.min(86400000, Math.round(state.stream?.durationMs || track?.durationMs || 0)));
    return {
      epoch,
      sequence,
      ready: state.hydration === "ready",
      closing: store.isClosing(),
      queueEntryId: entry?.queueEntryId ?? "",
      songId: track?.neteaseSongId ?? "",
      title: truncateUTF8(track?.name ?? "", 1024),
      artist: truncateUTF8(track?.artists.map((a) => a.name).join(" / ") ?? "", 1024),
      album: truncateUTF8(track?.album?.name ?? "", 1024),
      coverRef: track?.coverRef && validRemoteCover(track.coverRef) ? track.coverRef : "",
      playback: state.playback,
      positionMs,
      durationMs,
      volume: state.settings?.volume ?? 0.8,
      favorite: !!track && store.isFavorite(track),
      canPrevious: q.entries.length > 0,
      canNext: q.entries.length > 0,
      cacheHit: state.stream?.cacheHit ?? false,
      provider: state.stream?.provider ?? "",
      quality: truncateUTF8(state.stream ? actualStreamQuality(state.stream) : "", 128),
      currentLyric: truncateUTF8(line?.text ?? "", 4096),
      nextLyric: truncateUTF8(next?.text ?? "", 4096),
      translation: truncateUTF8(line?.translated ?? "", 4096),
      lyricProgress:
        line && next && next.time > line.time
          ? Math.max(0, Math.min(1, (positionMs - line.time) / (next.time - line.time)))
          : line
            ? 1
            : 0,
      error: truncateUTF8(state.error, 1024),
      options: normalizeDesktopSettings(state.settings?.desktop),
    };
  };
}
export function toggleDesktopSetting(
  store: PlayerStore,
  key: "lyricsLocked" | "miniAlwaysOnTop" | "lyricsAlwaysOnTop",
) {
  if (store.isClosing() || store.getSnapshot().hydration !== "ready") return;
  const desktop = normalizeDesktopSettings(store.getSnapshot().settings?.desktop);
  store.setSettings({ desktop: { ...desktop, [key]: !desktop[key] } });
}
export function updateDesktopSettings(store: PlayerStore, update: Partial<DesktopSettings>) {
  store.setSettings({ desktop: { ...normalizeDesktopSettings(store.getSnapshot().settings?.desktop), ...update } });
}
export function handleDesktopCommand(store: PlayerStore, epoch: number, value: unknown) {
  const c = (() => {
    try {
      return fromDesktopCommand(value);
    } catch {
      return null;
    }
  })();
  if (!c) return;
  const state = store.getSnapshot();
  if (c.epoch !== epoch || store.isClosing() || state.hydration !== "ready") return;
  const track = state.queue.entries[state.queue.currentIndex]?.track;
  if ((c.command === "seek" || c.command === "toggle-favorite") && (!track || c.songId !== track.neteaseSongId)) return;
  switch (c.command) {
    case "play":
      if (state.playback === "paused") void store.play();
      break;
    case "pause":
      if (state.playback !== "paused") store.pause();
      break;
    case "toggle-play":
      if (state.playback === "paused") void store.play();
      else store.pause();
      break;
    case "previous":
      void store.previous();
      break;
    case "next":
      void store.next();
      break;
    case "seek":
      store.seek(c.value);
      break;
    case "volume":
      store.setVolume(c.value);
      break;
    case "toggle-favorite":
      if (track) store.toggleFavorite(track);
      break;
    case "toggle-lyrics-lock":
      toggleDesktopSetting(store, "lyricsLocked");
      break;
    case "toggle-mini-topmost":
      toggleDesktopSetting(store, "miniAlwaysOnTop");
      break;
    case "toggle-lyrics-topmost":
      toggleDesktopSetting(store, "lyricsAlwaysOnTop");
      break;
  }
}
export function bindDesktopPublisher(store: PlayerStore, bridge: DesktopBridge, events: DesktopEvents) {
  const project = createDesktopProjector(store);
  let epoch = 0,
    sequence = 0,
    generation = 0,
    stopped = false,
    frozen = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let last = -Infinity;
  const publish = () => {
    timer = undefined;
    if (stopped || frozen || !epoch) return;
    const snapshot = project(epoch, ++sequence);
    const json = JSON.stringify(snapshot);
    if (new TextEncoder().encode(json).length > 65536) return;
    last = Date.now();
    events.Emit("otoneigo:desktop-publish", json);
    if (snapshot.closing) frozen = true;
  };
  const schedule = () => {
    if (!timer && !stopped && !frozen) timer = setTimeout(publish, Math.max(0, 250 - (Date.now() - last)));
  };
  const acquire = async () => {
    const request = ++generation;
    epoch = 0;
    try {
      if (!bridge.GetDesktopEpoch) return;
      const next = fromDesktopEpoch(await bridge.GetDesktopEpoch());
      if (stopped || request !== generation) return;
      epoch = next;
      sequence = 0;
      last = -Infinity;
      if (timer) clearTimeout(timer);
      timer = undefined;
      // NavigationCompleted may arrive after startup: republish the current main state for its new epoch.
      schedule();
    } catch {
      /* The native view can be retried after the next epoch event. */
    }
  };
  const stops = [
    store.subscribe(schedule),
    events.On("otoneigo:desktop-refresh", schedule),
    events.On("otoneigo:desktop-epoch", () => {
      void acquire();
    }),
    events.On("otoneigo:desktop-command", (e) => handleDesktopCommand(store, epoch, e.data)),
  ];
  void acquire();
  return () => {
    stopped = true;
    generation++;
    if (timer) clearTimeout(timer);
    stops.forEach((stop) => {
      stop();
    });
  };
}

import type { PlayerStore } from "./PlayerStore";

export function playerCommand(store: PlayerStore, command: unknown) {
  if (store.isClosing()) return;
  if (command === "toggle-play") {
    if (store.getSnapshot().playback === "paused") void store.play(); else store.pause();
  } else if (command === "previous") void store.previous();
  else if (command === "next") void store.next();
}

// Browser Media Session support is optional; native Windows transport acceptance is separate.
export function bindMediaSession(store: PlayerStore, session: MediaSession | undefined = globalThis.navigator?.mediaSession,
  Metadata: typeof MediaMetadata | undefined = globalThis.MediaMetadata) {
  if (!session) return () => {};
  const safe = (fn: () => void) => { try { fn(); } catch { /* Unsupported platform feature. */ } };
  const seekRelative = (offset: number) => { if (Number.isFinite(offset)) store.seek(store.getSnapshot().queue.positionMs + offset * 1000); };
  const handlers: Partial<Record<MediaSessionAction, MediaSessionActionHandler>> = {
    play: () => { if (store.getSnapshot().playback === "paused") void store.play(); },
    pause: () => { if (store.getSnapshot().playback !== "paused") store.pause(); },
    nexttrack: () => { void store.next(); }, previoustrack: () => { void store.previous(); },
    seekto: details => { if (Number.isFinite(details.seekTime)) store.seek(details.seekTime! * 1000); },
    seekforward: details => seekRelative(details.seekOffset ?? 10),
    seekbackward: details => seekRelative(-(details.seekOffset ?? 10)),
  };
  for (const [action, handler] of Object.entries(handlers)) safe(() => session.setActionHandler(action as MediaSessionAction, details => { if (!store.isClosing()) handler(details); }));
  let metadataKey = "";
  const update = () => {
    const state = store.getSnapshot();
    const track = state.queue.entries.find(e => e.queueEntryId === state.queue.currentEntryId)?.track;
    const metadata = track ? { title: track.name, artist: track.artists.map(a => a.name).join(" / "), album: track.album?.name ?? "" } : null;
    const key = JSON.stringify([track?.neteaseSongId, metadata]);
    if (key !== metadataKey) {
      metadataKey = key;
      safe(() => { session.metadata = metadata && Metadata ? new Metadata(metadata) : null; });
    }
    safe(() => { session.playbackState = state.playback === "playing" ? "playing" : "paused"; });
    const duration = (state.stream?.durationMs || track?.durationMs || 0) / 1000;
    const position = state.queue.positionMs / 1000;
    if (Number.isFinite(duration) && duration > 0 && Number.isFinite(position) && position >= 0)
      safe(() => session.setPositionState?.({ duration, position: Math.min(position, duration), playbackRate: 1 }));
    else safe(() => session.setPositionState?.());
  };
  const unsubscribe = store.subscribe(update);
  update();
  return () => {
    unsubscribe();
    for (const action of Object.keys(handlers)) safe(() => session.setActionHandler(action as MediaSessionAction, null));
    safe(() => { session.metadata = null; session.playbackState = "none"; });
    safe(() => session.setPositionState?.());
  };
}

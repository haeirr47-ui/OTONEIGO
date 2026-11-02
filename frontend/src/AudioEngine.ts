// Adapted from OTONEI src/services/AudioEngine.ts, MIT, Copyright (c) 2026 voici5986.
// See frontend/THIRD_PARTY_NOTICES.md. One HTMLAudioElement; no WebAudio/CORS mode.
export type EngineEvent =
  "playing" | "pause" | "ended" | "timeupdate" | "error" | "waiting";
export interface AudioExecutor {
  on(event: EngineEvent, callback: () => void): () => void;
  setSource(
    url: string,
    positionMs: number,
    options?: SourceOptions,
  ): Promise<void>;
  pause(preserveSource?: boolean): void;
  seek(positionMs: number): void;
  setVolume(volume: number): void;
  readonly currentTime: number;
  readonly duration: number;
}
export interface SourceOptions {
  expectedDurationMs?: number;
  requireComplete?: boolean;
  deadlineAt?: number;
}
export class PlaybackRequestReplacedError extends Error {}
export class MediaStartupError extends Error {
  constructor(
    readonly code:
      "TRIAL_ONLY" | "STREAM_DURATION_MISMATCH" | "RESOLVE_TIMEOUT",
  ) {
    super(code);
  }
}
export class AudioEngine implements AudioExecutor {
  readonly audio: HTMLAudioElement;
  private generation = 0;
  private active = false;
  private playbackRequested = false;
  private pendingPosition: number | null = null;
  private cancelStartup?: () => void;
  private listeners = new Map<EngineEvent, Set<() => void>>();
  constructor(audio = new Audio()) {
    this.audio = audio;
    audio.preload = "metadata";
    for (const event of [
      "playing",
      "pause",
      "ended",
      "timeupdate",
      "error",
      "waiting",
    ] as const) {
      audio.addEventListener(event, () => {
        if (!this.active || !audio.getAttribute("src")) return;
        if (event === "playing" && (audio.paused || !this.playbackRequested))
          return;
        // Events already queued for a removed resource must agree with current media state.
        if (event === "ended" && !audio.ended) return;
        if (event === "error" && !audio.error) return;
        this.listeners.get(event)?.forEach((fn) => fn());
      });
    }
    audio.addEventListener("loadedmetadata", () => {
      if (this.active && this.pendingPosition !== null)
        this.seek(this.pendingPosition);
    });
  }
  on(event: EngineEvent, callback: () => void) {
    const set = this.listeners.get(event) ?? new Set();
    set.add(callback);
    this.listeners.set(event, set);
    return () => {
      set.delete(callback);
    };
  }
  setSource(
    url: string,
    positionMs: number,
    options: SourceOptions = {},
  ): Promise<void> {
    this.pause();
    const generation = this.generation;
    const deadlineAt = options.deadlineAt ?? Date.now() + 12000;
    return new Promise<void>((resolve, reject) => {
      let settled = false;
      let timer: ReturnType<typeof setTimeout> | undefined;
      const cleanup = () => {
        if (timer !== undefined) clearTimeout(timer);
        this.audio.removeEventListener("loadedmetadata", metadata);
        this.audio.removeEventListener("playing", playing);
        this.audio.removeEventListener("error", failed);
        if (generation === this.generation) this.cancelStartup = undefined;
      };
      const finish = (error?: unknown) => {
        if (settled) return;
        settled = true;
        cleanup();
        if (error) {
          if (generation === this.generation) this.pause();
          reject(error);
        } else resolve();
      };
      const validRequest = () => !settled && generation === this.generation;
      const start = () => {
        if (!validRequest()) return;
        if (Date.now() >= deadlineAt) {
          finish(new MediaStartupError("RESOLVE_TIMEOUT"));
          return;
        }
        try {
          this.playbackRequested = true;
          void this.audio.play().then(
            () => {
              if (validRequest())
                finish(
                  Date.now() >= deadlineAt
                    ? new MediaStartupError("RESOLVE_TIMEOUT")
                    : undefined,
                );
            },
            (error) => {
              if (validRequest())
                finish(
                  (error as Error)?.name === "AbortError"
                    ? new PlaybackRequestReplacedError()
                    : error,
                );
            },
          );
        } catch (error) {
          finish(error);
        }
      };
      const metadata = () => {
        if (!validRequest()) return;
        const expected = options.expectedDurationMs ?? 0;
        const actual = this.audio.duration * 1000;
        if (
          !Number.isFinite(expected) ||
          expected <= 0 ||
          !Number.isFinite(actual) ||
          actual <= 0
        ) {
          finish(new MediaStartupError("STREAM_DURATION_MISMATCH"));
          return;
        }
        if (Math.abs(actual - expected) > Math.max(2000, expected * 0.02)) {
          finish(
            new MediaStartupError(
              actual <= 31000 && actual < expected
                ? "TRIAL_ONLY"
                : "STREAM_DURATION_MISMATCH",
            ),
          );
          return;
        }
        // Validate this element's actual metadata before its first play call.
        this.audio.removeEventListener("loadedmetadata", metadata);
        start();
      };
      const playing = () => {
        if (validRequest() && this.playbackRequested && !this.audio.paused)
          finish(
            Date.now() >= deadlineAt
              ? new MediaStartupError("RESOLVE_TIMEOUT")
              : undefined,
          );
      };
      const failed = () => {
        if (validRequest() && this.audio.error)
          finish(new Error("MEDIA_ERROR"));
      };
      this.cancelStartup = () => finish(new PlaybackRequestReplacedError());
      if (!Number.isFinite(deadlineAt) || Date.now() >= deadlineAt) {
        finish(new MediaStartupError("RESOLVE_TIMEOUT"));
        return;
      }
      if (
        options.requireComplete &&
        (!Number.isFinite(options.expectedDurationMs) ||
          (options.expectedDurationMs ?? 0) <= 0)
      ) {
        finish(new MediaStartupError("STREAM_DURATION_MISMATCH"));
        return;
      }
      timer = setTimeout(
        () => finish(new MediaStartupError("RESOLVE_TIMEOUT")),
        Math.min(2147483647, deadlineAt - Date.now()),
      );
      this.audio.addEventListener("playing", playing);
      this.audio.addEventListener("error", failed);
      if (options.requireComplete)
        this.audio.addEventListener("loadedmetadata", metadata);
      this.pendingPosition = positionMs;
      this.audio.src = url;
      this.active = true;
      this.audio.load();
      if (!options.requireComplete) start();
    });
  }
  pause(preserveSource = false) {
    const cancel = this.cancelStartup;
    // A settled paused resource keeps WebView2's system media session available.
    // Pending startup must still unload, so no late play can restart that resource.
    const keepSource = preserveSource && !cancel && !!this.audio.getAttribute("src");
    this.cancelStartup = undefined;
    this.generation++;
    this.active = false;
    this.playbackRequested = false;
    this.pendingPosition = null;
    this.audio.pause();
    if (!keepSource) {
      this.audio.removeAttribute("src");
      this.audio.load();
    }
    cancel?.();
  }
  seek(positionMs: number) {
    const seconds = Math.max(0, positionMs / 1000);
    if (Number.isFinite(this.audio.duration) && this.audio.duration > 0) {
      this.audio.currentTime = Math.min(seconds, this.audio.duration);
      this.pendingPosition = null;
    } else this.pendingPosition = positionMs;
  }
  setVolume(volume: number) {
    this.audio.volume = Math.max(0, Math.min(1, volume));
  }
  get currentTime() {
    return Number.isFinite(this.audio.currentTime)
      ? Math.max(0, Math.round(this.audio.currentTime * 1000))
      : 0;
  }
  get duration() {
    return Number.isFinite(this.audio.duration)
      ? this.audio.duration * 1000
      : 0;
  }
}

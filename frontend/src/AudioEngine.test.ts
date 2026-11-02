import { it, expect, vi } from "vitest";
import { AudioEngine, PlaybackRequestReplacedError } from "./AudioEngine";
class Media extends EventTarget {
  ended = false;
  error: object | null = null;
  preload = "";
  src = "";
  paused = true;
  duration = 0;
  currentTime = 0;
  volume = 1;
  load = vi.fn();
  pause = vi.fn(() => {
    this.paused = true;
  });
  play = vi.fn(async () => {
    this.paused = false;
  });
  getAttribute(name: string) {
    return name === "src" ? this.src : null;
  }
  removeAttribute(name: string) {
    if (name === "src") this.src = "";
  }
}
it("uses one plain media element and applies restored seek after metadata", async () => {
  const media = new Media();
  const engine = new AudioEngine(media as unknown as HTMLAudioElement);
  await engine.setSource("https://audio.example", 42000);
  expect(media.currentTime).toBe(0);
  media.duration = 120;
  media.dispatchEvent(new Event("loadedmetadata"));
  expect(media.currentTime).toBe(42);
  expect("crossOrigin" in media).toBe(false);
});
it("ignores stale ended/error events after replacing a media resource", async () => {
  const media = new Media();
  const engine = new AudioEngine(media as unknown as HTMLAudioElement);
  const ended = vi.fn();
  const error = vi.fn();
  engine.on("ended", ended);
  engine.on("error", error);
  await engine.setSource("https://audio.example/first", 0);
  await engine.setSource("https://audio.example/second", 0);
  media.dispatchEvent(new Event("ended"));
  media.dispatchEvent(new Event("error"));
  expect(ended).not.toHaveBeenCalled();
  expect(error).not.toHaveBeenCalled();
  media.ended = true;
  media.dispatchEvent(new Event("ended"));
  expect(ended).toHaveBeenCalledTimes(1);
  media.error = { code: 4 };
  media.dispatchEvent(new Event("error"));
  expect(error).toHaveBeenCalledTimes(1);
});
it("pause invalidates pending native play, clears src and ignores late playing events", async () => {
  const media = new Media();
  let finish!: () => void;
  media.play.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  const engine = new AudioEngine(media as unknown as HTMLAudioElement);
  const playing = vi.fn();
  engine.on("playing", playing);
  const play = engine.setSource("https://audio.example", 0);
  engine.pause();
  finish();
  await expect(play).rejects.toBeInstanceOf(PlaybackRequestReplacedError);
  media.dispatchEvent(new Event("playing"));
  expect(playing).not.toHaveBeenCalled();
  expect(media.src).toBe("");
});

it("waits for actual GD metadata, restores fractional seek, then starts the same media element", async () => {
  const media = new Media();
  const engine = new AudioEngine(media as unknown as HTMLAudioElement);
  const promise = engine.setSource("https://audio.example/gd", 42567.8, {
    requireComplete: true,
    expectedDurationMs: 120000,
  });
  expect(media.play).not.toHaveBeenCalled();
  expect(media.currentTime).toBe(0);
  media.duration = 120.25;
  media.dispatchEvent(new Event("loadedmetadata"));
  await promise;
  expect(media.play).toHaveBeenCalledTimes(1);
  expect(media.currentTime).toBeCloseTo(42.5678);
  expect(engine.audio).toBe(media);
});
it.each([
  [30, 120000, "TRIAL_ONLY"],
  [90, 120000, "STREAM_DURATION_MISMATCH"],
  [NaN, 120000, "STREAM_DURATION_MISMATCH"],
  [Infinity, 120000, "STREAM_DURATION_MISMATCH"],
  [0, 120000, "STREAM_DURATION_MISMATCH"],
  [120, 0, "STREAM_DURATION_MISMATCH"],
])(
  "blocks incomplete/unknown GD metadata: actual %s expected %s",
  async (duration, expectedDurationMs, code) => {
    const media = new Media();
    const engine = new AudioEngine(media as unknown as HTMLAudioElement);
    const promise = engine.setSource("https://audio.example/gd", 0, {
      requireComplete: true,
      expectedDurationMs,
    });
    const rejection = expect(promise).rejects.toMatchObject({ code });
    media.duration = duration;
    media.dispatchEvent(new Event("loadedmetadata"));
    await rejection;
    expect(media.play).not.toHaveBeenCalled();
    expect(media.src).toBe("");
  },
);
it("rejects an already expired shared startup deadline without loading or playing a new source", async () => {
  const media = new Media();
  const engine = new AudioEngine(media as unknown as HTMLAudioElement);
  await expect(
    engine.setSource("https://audio.example", 0, {
      deadlineAt: Date.now() - 1,
    }),
  ).rejects.toMatchObject({ code: "RESOLVE_TIMEOUT" });
  expect(media.play).not.toHaveBeenCalled();
  expect(media.src).toBe("");
});
it("metadata timeout removes listeners and never starts on late metadata", async () => {
  vi.useFakeTimers();
  try {
    const media = new Media();
    const engine = new AudioEngine(media as unknown as HTMLAudioElement);
    const promise = engine.setSource("https://audio.example/gd", 0, {
      requireComplete: true,
      expectedDurationMs: 120000,
      deadlineAt: Date.now() + 500,
    });
    const rejection = expect(promise).rejects.toMatchObject({
      code: "RESOLVE_TIMEOUT",
    });
    await vi.advanceTimersByTimeAsync(500);
    await rejection;
    media.duration = 120;
    media.dispatchEvent(new Event("loadedmetadata"));
    expect(media.play).not.toHaveBeenCalled();
    expect(media.src).toBe("");
    expect(vi.getTimerCount()).toBe(0);
  } finally {
    vi.useRealTimers();
  }
});
it("shared remaining deadline bounds a pending normal native play promise", async () => {
  vi.useFakeTimers();
  try {
    const media = new Media();
    let finish!: () => void;
    media.play.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    const engine = new AudioEngine(media as unknown as HTMLAudioElement);
    const promise = engine.setSource("https://audio.example", 0, {
      deadlineAt: Date.now() + 250,
    });
    const rejection = expect(promise).rejects.toMatchObject({
      code: "RESOLVE_TIMEOUT",
    });
    expect(media.play).toHaveBeenCalledOnce();
    await vi.advanceTimersByTimeAsync(250);
    await rejection;
    finish();
    await Promise.resolve();
    expect(media.src).toBe("");
    expect(media.paused).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
  } finally {
    vi.useRealTimers();
  }
});
it("playing clears startup deadline even while the play promise remains pending", async () => {
  vi.useFakeTimers();
  try {
    const media = new Media();
    let finish!: () => void;
    media.play.mockImplementation(() => {
      media.paused = false;
      return new Promise<void>((resolve) => {
        finish = resolve;
      });
    });
    const engine = new AudioEngine(media as unknown as HTMLAudioElement);
    const promise = engine.setSource("https://audio.example", 0, {
      deadlineAt: Date.now() + 500,
    });
    media.dispatchEvent(new Event("playing"));
    await promise;
    const pauses = media.pause.mock.calls.length;
    await vi.advanceTimersByTimeAsync(12000);
    expect(media.pause).toHaveBeenCalledTimes(pauses);
    expect(media.src).toBe("https://audio.example");
    expect(vi.getTimerCount()).toBe(0);
    finish();
  } finally {
    vi.useRealTimers();
  }
});
it("replacement and pause cancel GD metadata waits and remove their deadlines", async () => {
  vi.useFakeTimers();
  try {
    const media = new Media();
    const engine = new AudioEngine(media as unknown as HTMLAudioElement);
    const old = engine.setSource("https://audio.example/old", 0, {
      requireComplete: true,
      expectedDurationMs: 120000,
    });
    const oldRejection = expect(old).rejects.toBeInstanceOf(
      PlaybackRequestReplacedError,
    );
    await engine.setSource("https://audio.example/new", 0);
    await oldRejection;
    media.duration = 120;
    media.dispatchEvent(new Event("loadedmetadata"));
    expect(media.play).toHaveBeenCalledOnce();
    expect(media.src).toBe("https://audio.example/new");
    const pending = engine.setSource("https://audio.example/pending", 0, {
      requireComplete: true,
      expectedDurationMs: 120000,
    });
    const pauseRejection = expect(pending).rejects.toBeInstanceOf(
      PlaybackRequestReplacedError,
    );
    engine.pause();
    await pauseRejection;
    media.dispatchEvent(new Event("loadedmetadata"));
    expect(media.play).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
  } finally {
    vi.useRealTimers();
  }
});

it("retains settled paused media without reloading or forwarding stale events, then resets fully", async () => {
  const media = new Media();
  const engine = new AudioEngine(media as unknown as HTMLAudioElement);
  const playing = vi.fn();
  const ended = vi.fn();
  engine.on("playing", playing);
  engine.on("ended", ended);
  await engine.setSource("https://audio.example/loaded", 0);
  media.duration = 120;
  media.currentTime = 42;
  const loads = media.load.mock.calls.length;
  engine.pause(true);
  engine.pause(true);
  expect(media.paused).toBe(true);
  expect(media.src).toBe("https://audio.example/loaded");
  expect(engine.duration).toBe(120000);
  expect(engine.currentTime).toBe(42000);
  expect(media.load).toHaveBeenCalledTimes(loads);
  expect(media.play).toHaveBeenCalledOnce();
  media.dispatchEvent(new Event("playing"));
  media.ended = true;
  media.dispatchEvent(new Event("ended"));
  expect(playing).not.toHaveBeenCalled();
  expect(ended).not.toHaveBeenCalled();
  engine.pause();
  expect(media.src).toBe("");
  expect(media.load).toHaveBeenCalledTimes(loads + 1);
});
it("a preserve request still unloads pending native play and rejects late startup", async () => {
  const media = new Media();
  let finish!: () => void;
  media.play.mockImplementation(() => new Promise<void>(resolve => { finish = resolve; }));
  const engine = new AudioEngine(media as unknown as HTMLAudioElement);
  const startup = engine.setSource("https://audio.example/pending", 0);
  const rejected = expect(startup).rejects.toBeInstanceOf(PlaybackRequestReplacedError);
  engine.pause(true);
  finish();
  await rejected;
  expect(media.src).toBe("");
  expect(media.paused).toBe(true);
  expect(media.play).toHaveBeenCalledOnce();
});
it("a late settled play promise cannot restart preserved paused media", async () => {
  const media = new Media();
  let finish!: () => void;
  media.play.mockImplementation(() => {
    media.paused = false;
    return new Promise<void>(resolve => { finish = resolve; });
  });
  const engine = new AudioEngine(media as unknown as HTMLAudioElement);
  const startup = engine.setSource("https://audio.example/playing", 0);
  media.dispatchEvent(new Event("playing"));
  await startup;
  engine.pause(true);
  finish();
  await Promise.resolve();
  expect(media.paused).toBe(true);
  expect(media.src).toBe("https://audio.example/playing");
  expect(media.play).toHaveBeenCalledOnce();
});

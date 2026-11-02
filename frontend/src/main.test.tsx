import { afterEach, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({
  render: vi.fn(),
  audio: vi.fn(),
  store: vi.fn(),
  publisher: vi.fn(() => () => {}),
  media: vi.fn(() => () => {}),
  close: vi.fn(() => vi.fn()),
  on: vi.fn((_name: string, _callback: unknown) => () => {}),
}));
vi.mock("react-dom/client", () => ({ createRoot: () => ({ render: mocks.render }) }));
vi.mock("@wailsio/runtime", () => ({ Events: { On: mocks.on, Emit: vi.fn() } }));
vi.mock("./AudioEngine", () => ({
  AudioEngine: class {
    constructor() {
      mocks.audio();
    }
  },
}));
vi.mock("./PlayerStore", () => ({
  PlayerStore: class {
    constructor() {
      mocks.store();
    }
  },
}));
vi.mock("./desktopBridge", () => ({ desktopBridge: {} }));
vi.mock("./desktopProjection", () => ({ bindDesktopPublisher: mocks.publisher, toggleDesktopSetting: vi.fn() }));
vi.mock("./mediaSession", () => ({ bindMediaSession: mocks.media, playerCommand: vi.fn() }));
vi.mock("./prepareClose", () => ({ createCloseHandler: mocks.close }));
vi.mock("./App", () => ({ App: () => null }));
vi.mock("./PlayerContext", () => ({ PlayerProvider: () => null }));
vi.mock("./DesktopViews", () => ({ DesktopAuxView: () => null }));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});
it.each(["?view=mini", "?view=lyrics", "?view=unknown", "?view=", "?view=mini&view=lyrics"])(
  "bootstrap %s never creates an audio executor/store, media session or close acknowledgement",
  async (search) => {
    vi.resetModules();
    vi.stubGlobal("document", { getElementById: () => ({}), documentElement: { dataset: {} } });
    vi.stubGlobal("window", { location: { search }, addEventListener: vi.fn() });
    await import("./main");
    expect(mocks.render).toHaveBeenCalledOnce();
    for (const spy of [mocks.audio, mocks.store, mocks.publisher, mocks.media, mocks.close])
      expect(spy).not.toHaveBeenCalled();
    expect(mocks.on).not.toHaveBeenCalled();
  },
);
it("main bootstrap alone owns one store/executor, native publisher and close handshake", async () => {
  vi.resetModules();
  vi.stubGlobal("document", { getElementById: () => ({}) });
  vi.stubGlobal("window", { location: { search: "" }, addEventListener: vi.fn() });
  await import("./main");
  for (const spy of [mocks.audio, mocks.store, mocks.publisher, mocks.media, mocks.close])
    expect(spy).toHaveBeenCalledOnce();
  expect(mocks.on.mock.calls.map((c) => c[0])).toEqual(["otoneigo:player-command", "otoneigo:prepare-close"]);
});

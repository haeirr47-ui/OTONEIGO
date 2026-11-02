import { expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => {}), Emit: vi.fn() } }));
import { DesktopViewContent } from "./DesktopViews";
import type { DesktopBridge, DesktopSnapshot } from "./contracts";
import { defaultDesktopSettings } from "./desktopWire";
const snapshot: DesktopSnapshot = {
  epoch: 1,
  sequence: 1,
  ready: true,
  closing: false,
  queueEntryId: "entry-1",
  songId: "123",
  title: "歌曲",
  artist: "歌手",
  album: "专辑",
  coverRef: "",
  playback: "playing",
  positionMs: 1000,
  durationMs: 60000,
  volume: 0.4,
  favorite: true,
  canPrevious: true,
  canNext: true,
  cacheHit: false,
  provider: "netease-direct",
  quality: "320 kbps",
  currentLyric: "当前歌词",
  nextLyric: "下一行",
  translation: "Translation",
  lyricProgress: 0.5,
  error: "",
  options: defaultDesktopSettings(),
};
it("mini observer exposes keyboard native ranges, labeled transport and hide/return controls", () => {
  const html = renderToStaticMarkup(
    <DesktopViewContent kind="mini" snapshot={snapshot} bridge={{} as DesktopBridge} onError={() => {}} />,
  );
  for (const label of [
    "迷你播放器",
    "播放进度",
    "音量",
    "上一首",
    "下一首",
    "暂停",
    "取消收藏",
    "返回主窗口",
    "隐藏迷你播放器",
    "窗口置顶",
  ])
    expect(html).toContain(`aria-label="${label}"`);
  expect(html).toContain('type="range"');
  expect(html).toContain('role="status"');
  expect(html).toContain('aria-pressed="true"');
});
it("lyrics observer displays precomputed visible lines and hides controls when natively locked", () => {
  const render = (locked: boolean) =>
    renderToStaticMarkup(
      <DesktopViewContent
        kind="lyrics"
        snapshot={{
          ...snapshot,
          options: { ...snapshot.options, lyricsLocked: locked, lyricsFontSize: 32, lyricsOpacity: 0.7 },
        }}
        bridge={{} as DesktopBridge}
        onError={() => {}}
      />,
    );
  expect(render(false)).toContain("锁定桌面歌词");
  const locked = render(true);
  expect(locked).toContain("当前歌词");
  expect(locked).toContain("Translation");
  expect(locked).not.toContain("下一行");
  expect(locked).not.toContain("<button ");
  expect(locked).toContain("--lyrics-size:32px");
  expect(locked).toContain("--lyrics-opacity:0.7");
});
it("unready and closing projections disable transport instead of creating another player", () => {
  for (const s of [null, { ...snapshot, ready: false }, { ...snapshot, closing: true }]) {
    const html = renderToStaticMarkup(
      <DesktopViewContent kind="mini" snapshot={s} bridge={{} as DesktopBridge} onError={() => {}} />,
    );
    expect(html).toContain('aria-label="下一首" disabled=""');
    expect(html).not.toContain("<audio");
  }
});
it("terminal unready close snapshot shows exit status rather than reconnecting", () => {
  for (const kind of ["mini", "lyrics"] as const) {
    const html = renderToStaticMarkup(
      <DesktopViewContent
        kind={kind}
        snapshot={{ ...snapshot, ready: false, closing: true }}
        bridge={{} as DesktopBridge}
        onError={() => {}}
      />,
    );
    expect(html).toContain("播放器正在退出…");
    expect(html).not.toContain("正在连接播放器…");
  }
});

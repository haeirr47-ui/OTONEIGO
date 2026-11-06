import type { PlayerState, PlayerStore } from "./PlayerStore";
import { playerCommand } from "./mediaSession";

export function shortcutContext(state: PlayerState, closing = false) {
  return {
    state,
    closing,
    current: state.queue.entries.find((entry) => entry.queueEntryId === state.queue.currentEntryId)?.track,
  };
}
export type ShortcutContext = ReturnType<typeof shortcutContext>;
type ShortcutId =
  | "toggle-play"
  | "previous"
  | "next"
  | "volume-up"
  | "volume-down"
  | "seek-back"
  | "seek-forward"
  | "favorite"
  | "queue"
  | "lyrics"
  | "help";
interface PlayerShortcut {
  id: ShortcutId;
  key: string;
  ctrl?: boolean;
  keys: string;
  label: string;
  unavailable: (context: ShortcutContext) => string | null;
}
const playable = ({ state, current }: ShortcutContext) =>
  state.hydration !== "ready" ? "队列尚未就绪" : !current ? "请先选择歌曲" : null;
const withSettings = (context: ShortcutContext) =>
  playable(context) || (!context.state.settings ? "设置尚未就绪" : null);
const volume = ({ state }: ShortcutContext) => (!state.settings || !state.qualityPolicy ? "设置尚未就绪" : null);
const seek = (context: ShortcutContext) =>
  playable(context) ||
  (!Number.isFinite(context.current?.durationMs) || (context.current?.durationMs ?? 0) <= 0
    ? "歌曲时长尚未就绪"
    : null);
const available = () => null;

// This registry owns both dispatch and the help labels; it does not subscribe to playback.
export const playerShortcuts: readonly PlayerShortcut[] = [
  { id: "toggle-play", key: " ", keys: "空格", label: "播放 / 暂停（加载中可取消）", unavailable: withSettings },
  { id: "previous", key: "ArrowLeft", ctrl: true, keys: "Ctrl + ←", label: "上一首", unavailable: withSettings },
  { id: "next", key: "ArrowRight", ctrl: true, keys: "Ctrl + →", label: "下一首", unavailable: playable },
  { id: "volume-up", key: "ArrowUp", ctrl: true, keys: "Ctrl + ↑", label: "音量增加 5%", unavailable: volume },
  { id: "volume-down", key: "ArrowDown", ctrl: true, keys: "Ctrl + ↓", label: "音量减少 5%", unavailable: volume },
  { id: "seek-back", key: "ArrowLeft", keys: "←", label: "后退 5 秒", unavailable: seek },
  { id: "seek-forward", key: "ArrowRight", keys: "→", label: "前进 5 秒", unavailable: seek },
  {
    id: "favorite",
    key: "f",
    keys: "F",
    label: "收藏 / 取消收藏当前歌曲",
    unavailable: ({ current, state }) => (!current ? "请先选择歌曲" : !state.libraryReady ? "音乐库尚未就绪" : null),
  },
  { id: "queue", key: "q", keys: "Q", label: "打开播放队列", unavailable: available },
  { id: "lyrics", key: "l", keys: "L", label: "打开歌词", unavailable: available },
  { id: "help", key: "?", keys: "?", label: "打开快捷键帮助", unavailable: available },
];

export function shortcutUnavailable(shortcut: PlayerShortcut, context: ShortcutContext): string | null {
  return context.closing ? "正在关闭播放器" : shortcut.unavailable(context);
}

export function matchPlayerShortcut(event: Pick<KeyboardEvent, "key" | "ctrlKey" | "altKey" | "metaKey" | "shiftKey">) {
  if (event.altKey || event.metaKey || (event.shiftKey && event.key !== "?")) return undefined;
  const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
  return playerShortcuts.find((shortcut) => shortcut.key === key && !!shortcut.ctrl === event.ctrlKey);
}

export interface ShortcutViews {
  openQueue: () => void;
  openLyrics: () => void;
  openHelp: () => void;
}

export function handlePlayerShortcut(
  event: KeyboardEvent,
  store: PlayerStore,
  views: ShortcutViews,
  document: Document,
): boolean {
  if (
    event.defaultPrevented ||
    event.repeat ||
    event.isComposing ||
    event.keyCode === 229 ||
    document.hidden ||
    !document.hasFocus() ||
    store.isClosing()
  )
    return false;
  const shortcut = matchPlayerShortcut(event);
  if (!shortcut) return false;
  // Every active dialog owns its keyboard scope, including dialogs outside this player.
  if (document.querySelector('[role="dialog"], dialog[open]')) return false;
  const target = event.target as Element | null;
  if (target?.closest) {
    if (
      target.closest(
        '[hidden], [inert], input, select, textarea, [contenteditable]:not([contenteditable="false"]), [role="textbox"], [role="searchbox"], [role="combobox"], [role="slider"], [role="spinbutton"], [role="listbox"], [role="tree"], [role="grid"], [role="menu"], [role="tablist"]',
      )
    )
      return false;
    if (
      !shortcut.ctrl &&
      target.closest(
        'button, a[href], summary, audio, video, [role="button"], [role="link"], [role="checkbox"], [role="radio"], [role="switch"], [role="tab"], [role="menuitem"]',
      )
    )
      return false;
  }
  const context = shortcutContext(store.getSnapshot(), store.isClosing());
  if (shortcutUnavailable(shortcut, context)) return false;
  event.preventDefault();
  const { state, current } = context;
  switch (shortcut.id) {
    case "toggle-play":
    case "previous":
    case "next":
      playerCommand(store, shortcut.id);
      break;
    case "volume-up":
    case "volume-down":
      store.setVolume(
        Math.min(1, Math.max(0, (state.settings?.volume ?? 1) + (shortcut.id === "volume-up" ? 0.05 : -0.05))),
      );
      break;
    case "seek-back":
    case "seek-forward":
      store.seek(
        Math.min(
          current?.durationMs ?? 0,
          Math.max(0, store.getPlaybackPosition() + (shortcut.id === "seek-forward" ? 5000 : -5000)),
        ),
      );
      break;
    case "favorite":
      if (current) store.toggleFavorite(current);
      break;
    case "queue":
      views.openQueue();
      break;
    case "lyrics":
      views.openLyrics();
      break;
    case "help":
      views.openHelp();
      break;
  }
  return true;
}

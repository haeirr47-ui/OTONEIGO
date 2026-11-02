import { useState } from "react";
import { usePlayer } from "./PlayerContext";
import { normalizeDesktopSettings } from "./desktopWire";
import { updateDesktopSettings } from "./desktopProjection";
import type { DesktopBridge, DesktopSettings } from "./contracts";

export function DesktopSettingsPanel({ bridge }: { bridge: DesktopBridge }) {
  const { state, store } = usePlayer();
  const options = normalizeDesktopSettings(state.settings?.desktop);
  const [message, setMessage] = useState("");
  const update = (value: Partial<DesktopSettings>) => updateDesktopSettings(store, value);
  const run = async (operation: (() => Promise<void>) | undefined) => {
    if (!operation || store.isClosing()) return;
    try {
      await operation();
      setMessage("");
    } catch {
      setMessage("窗口操作暂未完成，请稍后重试。");
    }
  };
  return (
    <fieldset className="desktop-settings">
      <legend>桌面窗口</legend>
      <div className="desktop-settings-actions">
        <button
          type="button"
          disabled={!bridge.ShowDesktopView}
          onClick={() => void run(() => bridge.ShowDesktopView!("mini"))}
        >
          打开迷你播放器
        </button>
        <button
          type="button"
          disabled={!bridge.ShowDesktopView}
          onClick={() => void run(() => bridge.ShowDesktopView!("lyrics"))}
        >
          打开桌面歌词
        </button>
      </div>
      <label>
        <input
          type="checkbox"
          checked={options.miniAlwaysOnTop}
          onChange={(e) => update({ miniAlwaysOnTop: e.target.checked })}
        />
        迷你播放器始终置顶
      </label>
      <label>
        <input
          type="checkbox"
          checked={options.lyricsAlwaysOnTop}
          onChange={(e) => update({ lyricsAlwaysOnTop: e.target.checked })}
        />
        桌面歌词始终置顶
      </label>
      <label>
        <input
          type="checkbox"
          checked={options.lyricsLocked}
          onChange={(e) => update({ lyricsLocked: e.target.checked })}
        />
        锁定桌面歌词（鼠标穿透）
      </label>
      <p className="muted">锁定后可在这里或托盘菜单解锁。</p>
      <label>
        歌词字号
        <input
          aria-label="歌词字号"
          type="range"
          min="16"
          max="64"
          step="1"
          value={options.lyricsFontSize}
          onChange={(e) => update({ lyricsFontSize: Number(e.target.value) })}
        />
        <span>{options.lyricsFontSize}</span>
      </label>
      <label>
        歌词不透明度
        <input
          aria-label="歌词不透明度"
          type="range"
          min="0.3"
          max="1"
          step="0.05"
          value={options.lyricsOpacity}
          onChange={(e) => update({ lyricsOpacity: Number(e.target.value) })}
        />
        <span>{Math.round(options.lyricsOpacity * 100)}%</span>
      </label>
      <label>
        <input
          type="checkbox"
          checked={options.notifyTrackChanges}
          onChange={(e) => update({ notifyTrackChanges: e.target.checked })}
        />
        主窗口隐藏时提示新歌曲
      </label>
      <label>
        <input
          type="checkbox"
          checked={options.taskbarProgress}
          onChange={(e) => update({ taskbarProgress: e.target.checked })}
        />
        在任务栏显示播放进度
      </label>
      <button
        type="button"
        disabled={!bridge.ResetDesktopViewPositions}
        onClick={() => void run(bridge.ResetDesktopViewPositions?.bind(bridge))}
      >
        重置桌面窗口位置
      </button>
      {message && <p role="status">{message}</p>}
    </fieldset>
  );
}

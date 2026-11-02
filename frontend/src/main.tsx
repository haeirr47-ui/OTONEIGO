import { DesktopAuxView } from "./DesktopViews";
import { bindDesktopPublisher, toggleDesktopSetting } from "./desktopProjection";
import { windowRole } from "./windowRole";
import { createRoot } from "react-dom/client";
import { Events } from "@wailsio/runtime";
import { App } from "./App";
import { AudioEngine } from "./AudioEngine";
import { desktopBridge } from "./desktopBridge";
import { PlayerStore } from "./PlayerStore";
import { PlayerProvider } from "./PlayerContext";
import { bindMediaSession, playerCommand } from "./mediaSession";
import { createCloseHandler } from "./prepareClose";
import "./styles.css";
const root = document.getElementById("root")!;
const role = windowRole(window.location.search);
if (role === "unknown") {
  createRoot(root).render(<main role="alert">无法打开此窗口，请从主窗口重试。</main>);
} else if (role !== "main") {
  document.documentElement.dataset.desktopView = role;
  createRoot(root).render(<DesktopAuxView kind={role} bridge={desktopBridge} />);
} else {
  const store = new PlayerStore(desktopBridge, new AudioEngine());
  const stopDesktop = bindDesktopPublisher(store, desktopBridge, Events);
  const stopMediaSession = bindMediaSession(store);
  const stopPlayerCommands = Events.On("otoneigo:player-command", (event) => {
    if (event.data === "toggle-lyrics-lock") toggleDesktopSetting(store, "lyricsLocked");
    else playerCommand(store, event.data);
  });
  window.addEventListener(
    "pagehide",
    () => {
      stopDesktop();
      stopMediaSession();
      stopPlayerCommands();
    },
    { once: true },
  );
  const prepareClose = createCloseHandler(
    store,
    () => {
      root.inert = true;
    },
    (token) => Events.Emit("otoneigo:close-ready", token),
  );
  Events.On("otoneigo:prepare-close", (event) => {
    void prepareClose(event.data);
  });
  createRoot(root).render(
    <PlayerProvider store={store}>
      <App bridge={desktopBridge} />
    </PlayerProvider>,
  );
}

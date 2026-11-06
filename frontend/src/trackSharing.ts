import { Browser, Clipboard } from "@wailsio/runtime";
import type { TrackSnapshot } from "./contracts";

export type NeteasePage = "song" | "album" | "artist";
export function neteasePageURL(kind: NeteasePage, id: string | undefined): string | null {
  if (!id || !/^[1-9][0-9]{0,29}$/.test(id) || !["song", "album", "artist"].includes(kind)) return null;
  return `https://music.163.com/${kind}?id=${id}`;
}
export interface ShareRuntime {
  openURL(url: string): Promise<void>;
  setText(text: string): Promise<void>;
}
// Static import: the same runtime is already in the main chunk (bindings and
// desktopBridge import it), so a dynamic import only produced an ineffective
// chunk split and a build warning. Tests inject their own ShareRuntime.
async function nativeRuntime(): Promise<ShareRuntime> {
  return { openURL: (url) => Browser.OpenURL(url), setText: (text) => Clipboard.SetText(text) };
}
export function trackShareText(track: TrackSnapshot): string {
  const url = neteasePageURL("song", track.neteaseSongId);
  if (!url) throw new Error("INVALID_NETEASE_ID");
  return `${track.name} - ${track.artists.map((artist) => artist.name).join(" / ")}\n${url}`;
}
export async function copyTrackShare(track: TrackSnapshot, runtime?: ShareRuntime): Promise<void> {
  const text = trackShareText(track);
  await (runtime ?? (await nativeRuntime())).setText(text);
}
export async function openNeteasePage(
  kind: NeteasePage,
  id: string | undefined,
  runtime?: ShareRuntime,
): Promise<void> {
  const url = neteasePageURL(kind, id);
  if (!url) throw new Error("INVALID_NETEASE_ID");
  await (runtime ?? (await nativeRuntime())).openURL(url);
}

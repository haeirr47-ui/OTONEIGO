import type { DesktopBridge, LocalPlaylist, LocalPlaylistSummary, TrackSnapshot } from "./contracts";
import { trackSnapshot } from "./contracts";

export interface PlaylistState {
  status: "loading" | "ready" | "failed";
  items: LocalPlaylistSummary[];
  details: Record<string, LocalPlaylist>;
  pending: number;
  error: string;
}
// Metadata only. All components share one serialized mutation tail; playback remains in PlayerStore.
export class PlaylistStore {
  private state: PlaylistState = { status: "loading", items: [], details: {}, pending: 0, error: "" };
  private listeners = new Set<() => void>();
  private tail: Promise<unknown> = Promise.resolve();
  private epoch = 0;
  private hydration?: Promise<void>;
  constructor(private bridge: DesktopBridge) {}
  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => {
      this.listeners.delete(fn);
    };
  };
  getSnapshot = () => this.state;
  private patch(update: Partial<PlaylistState>) {
    this.state = { ...this.state, ...update };
    this.listeners.forEach((fn) => {
      fn();
    });
  }
  hydrate() {
    if (!this.hydration) this.hydration = this.reload();
    return this.hydration;
  }
  private async reload() {
    try {
      const items = await this.bridge.ListLocalPlaylists();
      this.patch({ items, details: {}, status: "ready" });
    } catch {
      this.patch({ status: "failed", error: "本地歌单读取失败。为保护已有资料，暂时无法修改；请重新启动后重试。" });
    }
  }
  private accept(playlist: LocalPlaylist) {
    const summary = {
      playlistId: playlist.playlistId,
      name: playlist.name,
      trackCount: playlist.tracks.length,
      revision: playlist.revision,
      createdAt: playlist.createdAt,
      updatedAt: playlist.updatedAt,
    };
    this.patch({
      items: [summary, ...this.state.items.filter((p) => p.playlistId !== playlist.playlistId)],
      details: { ...this.state.details, [playlist.playlistId]: playlist },
    });
  }
  private operation<T>(work: () => Promise<T>): Promise<T | undefined> {
    if (this.state.status !== "ready") return Promise.resolve(undefined);
    const epoch = this.epoch;
    this.patch({ pending: this.state.pending + 1 });
    const task = this.tail
      .catch(() => {})
      .then(async () => {
        try {
          // A conflict invalidates operations enqueued against the previous view.
          if (this.state.status !== "ready" || epoch !== this.epoch) return undefined;
          const result = await work();
          this.patch({ error: "" });
          return result;
        } catch (error) {
          const code = String(error);
          if (/STALE_PLAYLIST_REVISION|PLAYLIST_NOT_FOUND/.test(code)) {
            this.epoch++;
            await this.reload();
            if (this.state.status === "ready")
              this.patch({ error: "歌单已被修改或删除，已重新读取。请查看最新内容后重试。" });
          } else {
            this.patch({ error: playlistError(code) });
          }
          return undefined;
        } finally {
          this.patch({ pending: this.state.pending - 1 });
        }
      });
    this.tail = task;
    return task;
  }
  load(id: string) {
    return this.operation(async () => {
      const playlist = await this.bridge.GetLocalPlaylist(id);
      this.accept(playlist);
      return playlist;
    });
  }
  create(name: string, initialTrack?: TrackSnapshot) {
    const snapshot = initialTrack && trackSnapshot(initialTrack);
    return this.operation(async () => {
      let playlist = await this.bridge.CreateLocalPlaylist(name.trim());
      // Keep creation visible even if the subsequent add fails; never hide a persisted empty playlist.
      this.accept(playlist);
      if (snapshot) {
        playlist = await this.bridge.UpdateLocalPlaylist({
          playlistId: playlist.playlistId,
          expectedRevision: playlist.revision,
          name: playlist.name,
          tracks: [snapshot],
        });
        this.accept(playlist);
      }
      return playlist;
    });
  }
  private edit(id: string, transform: (playlist: LocalPlaylist) => { name: string; tracks: TrackSnapshot[] }) {
    return this.operation(async () => {
      // Use this store's latest acknowledged revision, never silently replace an external edit.
      const playlist = this.state.details[id] ?? (await this.bridge.GetLocalPlaylist(id));
      const next = transform(playlist);
      const saved = await this.bridge.UpdateLocalPlaylist({
        playlistId: id,
        expectedRevision: playlist.revision,
        name: next.name,
        tracks: next.tracks.map(trackSnapshot),
      });
      this.accept(saved);
      return saved;
    });
  }
  rename(id: string, name: string) {
    return this.edit(id, (p) => ({ name: name.trim(), tracks: p.tracks }));
  }
  add(id: string, track: TrackSnapshot) {
    const snapshot = trackSnapshot(track);
    return this.edit(id, (p) => {
      if (p.tracks.some((t) => t.neteaseSongId === snapshot.neteaseSongId)) throw new Error("DUPLICATE_PLAYLIST_TRACK");
      return { name: p.name, tracks: [...p.tracks, snapshot] };
    });
  }
  remove(id: string, songId: string) {
    return this.edit(id, (p) => ({ name: p.name, tracks: p.tracks.filter((t) => t.neteaseSongId !== songId) }));
  }
  move(id: string, songId: string, direction: -1 | 1) {
    return this.edit(id, (p) => {
      const tracks = [...p.tracks];
      const index = tracks.findIndex((t) => t.neteaseSongId === songId);
      const target = index + direction;
      if (index >= 0 && target >= 0 && target < tracks.length)
        [tracks[index], tracks[target]] = [tracks[target], tracks[index]];
      return { name: p.name, tracks };
    });
  }
  delete(id: string) {
    return this.operation(async () => {
      const playlist = this.state.details[id] ?? (await this.bridge.GetLocalPlaylist(id));
      await this.bridge.DeleteLocalPlaylist(id, playlist.revision);
      const details = { ...this.state.details };
      delete details[id];
      this.patch({ items: this.state.items.filter((p) => p.playlistId !== id), details });
      return true;
    });
  }
  async flush() {
    let current: Promise<unknown>;
    do {
      current = this.tail;
      await current;
    } while (current !== this.tail);
  }
}
function playlistError(code: string) {
  if (code.includes("DUPLICATE_PLAYLIST_TRACK")) return "这首歌曲已在歌单中。";
  if (code.includes("INVALID_PLAYLIST_NAME")) return "请输入 1–128 个字符的歌单名称，不含控制字符。";
  if (code.includes("PLAYLIST_LIMIT_REACHED")) return "最多可创建 100 个本地歌单。";
  if (/PLAYLIST_TOO_LARGE|INVALID_PLAYLIST_TRACKS/.test(code))
    return "歌单容量已满或歌曲资料无效，每个歌单最多 1000 首。";
  return "本地歌单操作失败，请重试。";
}

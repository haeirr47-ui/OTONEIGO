import { expect, it, vi } from "vitest";
import { PlaylistStore } from "./PlaylistStore";
import type { DesktopBridge, LocalPlaylist, TrackSnapshot, PlaylistUpdate } from "./contracts";
const track: TrackSnapshot = { neteaseSongId: "123456789012345678", name: "第一首", artists: [{ name: "歌手" }], durationMs: 1000 };
const other = { ...track, neteaseSongId: "2", name: "第二首" };
function setup() {
  let playlist: LocalPlaylist | undefined = { playlistId: "local-1", name: "我的歌单", tracks: [], revision: 1, createdAt: 1, updatedAt: 1 };
  const bridge = {
    ListLocalPlaylists: vi.fn(async () => playlist ? [{ ...playlist, trackCount: playlist.tracks.length }] : []),
    GetLocalPlaylist: vi.fn(async () => { if (!playlist) throw new Error("PLAYLIST_NOT_FOUND"); return structuredClone(playlist); }),
    CreateLocalPlaylist: vi.fn(async (name: string) => { playlist = { playlistId: "created", name, tracks: [], revision: 1, createdAt: 1, updatedAt: 1 }; return structuredClone(playlist); }),
    UpdateLocalPlaylist: vi.fn(async (update: PlaylistUpdate) => {
      if (!playlist) throw new Error("PLAYLIST_NOT_FOUND");
      if (update.expectedRevision !== playlist.revision) throw new Error("STALE_PLAYLIST_REVISION");
      playlist = { ...playlist, name: update.name, tracks: structuredClone(update.tracks), revision: playlist.revision + 1, updatedAt: playlist.updatedAt + 1 };
      return structuredClone(playlist);
    }),
    DeleteLocalPlaylist: vi.fn(async (_id: string, revision: number) => {
      if (!playlist || revision !== playlist.revision) throw new Error("STALE_PLAYLIST_REVISION");
      playlist = undefined;
    }),
  };
  return { bridge, store: new PlaylistStore(bridge as unknown as DesktopBridge), externalChange: () => { playlist = { ...playlist!, revision: playlist!.revision + 1, name: "其他窗口修改" }; } };
}
it("serializes shared edits against acknowledged revisions and persists ordered metadata only", async () => {
  const { bridge, store } = setup();
  await store.hydrate();
  const unsafe = { ...track, url: "https://media.example/?token=secret", token: "secret" };
  const added = store.add("local-1", unsafe);
  const renamed = store.rename("local-1", "新名称");
  const appended = store.add("local-1", other);
  const moved = store.move("local-1", other.neteaseSongId, -1);
  await Promise.all([added, renamed, appended, moved]);
  expect(bridge.UpdateLocalPlaylist.mock.calls.map(([p]) => p.expectedRevision)).toEqual([1, 2, 3, 4]);
  expect(store.getSnapshot().details["local-1"].tracks.map(t => t.neteaseSongId)).toEqual(["2", track.neteaseSongId]);
  expect(store.getSnapshot().details["local-1"].name).toBe("新名称");
  expect(JSON.stringify(bridge.UpdateLocalPlaylist.mock.calls)).not.toMatch(/secret|token|https:/);
  await store.add("local-1", track);
  expect(bridge.UpdateLocalPlaylist).toHaveBeenCalledTimes(4);
  expect(store.getSnapshot().error).toContain("已在歌单");
  store.remove("local-1", track.neteaseSongId);
  store.delete("local-1");
  await store.flush();
  expect(bridge.DeleteLocalPlaylist).toHaveBeenCalledWith("local-1", 6);
  expect(store.getSnapshot().items).toEqual([]);
  expect(store.getSnapshot().pending).toBe(0);
});
it("reloads conflicts and cancels already queued stale edits without overwriting or resurrecting", async () => {
  const { store, bridge, externalChange } = setup();
  await store.hydrate();
  await store.load("local-1");
  externalChange();
  store.rename("local-1", "过时名称");
  store.add("local-1", track);
  await store.flush();
  expect(bridge.UpdateLocalPlaylist).toHaveBeenCalledTimes(1);
  expect(store.getSnapshot().items[0].name).toBe("其他窗口修改");
  expect(store.getSnapshot().details).toEqual({});
  expect(store.getSnapshot().error).toContain("重新读取");
  expect(store.getSnapshot().pending).toBe(0);
  await store.load("local-1");
  await store.delete("local-1");
  await store.rename("local-1", "不能复活");
  expect(store.getSnapshot().items).toEqual([]);
  expect(bridge.CreateLocalPlaylist).not.toHaveBeenCalled();
});
it("flush waits for both create and initial-track save, including delayed RPC continuation", async () => {
  const { store, bridge } = setup();
  await store.hydrate();
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  const create = bridge.CreateLocalPlaylist.getMockImplementation()!;
  bridge.CreateLocalPlaylist.mockImplementation(async name => { await gate; return create(name); });
  store.create("新歌单", track);
  let flushed = false;
  const flush = store.flush().then(() => { flushed = true; });
  await Promise.resolve();
  expect(flushed).toBe(false);
  release();
  await flush;
  expect(bridge.UpdateLocalPlaylist).toHaveBeenCalledWith(expect.objectContaining({ playlistId: "created", expectedRevision: 1, tracks: [track] }));
  expect(store.getSnapshot().details.created.tracks).toEqual([track]);
});
it("blocks writes after hydration failure and keeps a created empty playlist visible when initial add fails", async () => {
  const { store, bridge } = setup();
  bridge.ListLocalPlaylists.mockRejectedValueOnce(new Error("disk failure"));
  await store.hydrate();
  await store.create("不能写入");
  expect(bridge.CreateLocalPlaylist).not.toHaveBeenCalled();
  expect(store.getSnapshot().status).toBe("failed");
  const working = setup();
  await working.store.hydrate();
  working.bridge.UpdateLocalPlaylist.mockRejectedValueOnce(new Error("disk failure"));
  await working.store.create("已经创建", track);
  expect(working.store.getSnapshot().items[0]).toMatchObject({ name: "已经创建", trackCount: 0 });
  expect(working.store.getSnapshot().error).toContain("操作失败");
});

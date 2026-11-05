# 0.5 桌面窗口协议

> 2026-11-02 / 0.5.0 源码契约；原生 WebView2、production 和 portable ZIP 验收以 [P0 记录](../validation/p0-validation.md)为准。0.4.0 历史证据不自动覆盖本协议。

主窗口仍是唯一 PlayerStore/AudioEngine。`mini`、`lyrics` 两个按需创建的 Wails 窗口只显示白名单投影和转发命令，不创建播放器、播放队列或独立播放时钟。窗口 URL 只用本地入口 `/?view=mini`、`/?view=lyrics`；未知 view 不启动播放器。

## 设置与数据

`LocalSettings.desktop` 使用可比较的 `DesktopSettings`：

- `miniAlwaysOnTop`、`lyricsAlwaysOnTop` 默认 true；`lyricsLocked` 默认 false。
- `lyricsFontSize` 默认 28（允许 16..64）；`lyricsOpacity` 默认 0.9（允许 0.3..1）。
- `notifyTrackChanges` 默认 false；`taskbarProgress` 默认 true。
- `miniPlacement`、`lyricsPlacement` 为 `WindowPlacement {valid,x,y,width,height}`，坐标与尺寸是 DIP。默认 valid=false；mini 尺寸 420×176、lyrics 尺寸 820×156。

几何位置只由 Go 窗口事件保存；普通 SetSettings 保留数据库最新位置，避免旧前端设置覆盖拖动结果。它与 SaveDesktopPlacement 共用设置提交锁，锁内读最新值再合并；原生位置保存保留其他选项和另一浮窗位置，旧调用缺少 desktop 时保留既有桌面选项。schema 5→6 在同一事务解码、校验并升级设置契约，不新增设置行，保留曲库/队列/缓存；旧 desktop 零值补默认，损坏设置回滚，不提供用户迁移/备份功能。

## 状态与代际

Go `DesktopSnapshot` 的 JSON 字段如下：

```text
epoch, sequence: JS-safe integer
ready, closing: boolean
queueEntryId, songId, title, artist, album, coverRef: string
playback: paused | loading | playing
positionMs, durationMs: nonnegative bounded integer
volume: 0..1; favorite, canPrevious, canNext, cacheHit: boolean
provider: "" | netease-direct | gd-netease
quality: bounded display string
currentLyric, nextLyric, translation: bounded display strings
lyricProgress: 0..1; error: bounded display string
options: DesktopSettings (Go overwrites with committed settings)
```

投影不含 stream、音频 URL、cache token、凭据、完整歌词或整条队列。title/artist/album 各最多 1024 UTF-8 字节，coverRef 沿用既有验证；三个歌词字段各最多 4096 字节，quality 最多 128 字节，error 最多 1024 字节；事件 JSON 总量最多 64 KiB。

主窗口从 `GetDesktopEpoch` 取当前 epoch，通过 `otoneigo:desktop-publish` 发布；Go 只接受框架派生 Sender=`main`，拒绝旧 epoch 和乱序 sequence。主窗口 NavigationCompleted 会递增 epoch、置为未就绪并发送 `otoneigo:desktop-epoch`；主发布器重读 epoch 并重新发送当前投影，兼容页面完成事件晚于脚本启动。浮窗先订阅再发送 `otoneigo:desktop-ready`，并以 GetDesktopSnapshot 补读，Go 仅向对应窗口发送最新 `otoneigo:desktop-snapshot`。Go→WebView 使用定向 `DispatchWailsEvent`；固定 Wails 版本的 `EmitEvent` 会全局广播，不能用于此路由，以免回流至应用事件监听器。

主发布器合并高频更新，目标最多约 4 次/秒。Go 可在浮窗可见时发 `otoneigo:desktop-refresh`，主窗口读取原 AudioEngine 的实际时钟响应，不能把该刷新当成第二播放时钟。关闭开始后 Hub 推进 sequence 并置为 ready=false、closing=true，形成终止状态；发布保留一个 JS-safe sequence 供终止投影，拒绝普通发布占用该值。关闭后迟到发布与导航不能重开状态，浮窗也拒绝旧 epoch/sequence 和 closing 后复活。所有传输命令冻结；真正关闭主窗口时先允许所有浮窗关闭，防止隐藏浮窗保活进程。

## 操作接口

AppService 的可选 `DesktopViews` 实现：`Epoch() int64`、`Snapshot() DesktopSnapshot`、`State() DesktopNativeState`、`ShowView(kind) error`、`HideView(kind) error`、`Dispatch(DesktopCommand) error`、`ResetPositions() error`。

对应 RPC：`GetDesktopEpoch`、`GetDesktopSnapshot`、`GetDesktopNativeState`、`ShowDesktopView`、`HideDesktopView`、`DispatchDesktopCommand`、`ResetDesktopViewPositions`。

`DesktopCommand {epoch,command,songId,value}`：play/pause/toggle-play/previous/next/seek/volume/toggle-favorite，以及 toggle-lyrics-lock/toggle-mini-topmost/toggle-lyrics-topmost。seek 和 toggle-favorite 必须检查当前曲目身份；epoch 不符、未就绪和退出中拒绝执行。命令只向主窗口 `otoneigo:desktop-command` 转发，主窗口再次校验并调用既有 store。打开主窗口沿用 ShowMainWindow。

`DesktopNativeState` 返回：mainVisible/mainMinimized/miniVisible/lyricsVisible/lyricsLocked，taskbarAvailable/taskbarState/taskbarPositionMs/taskbarDurationMs，notificationRequests/notificationAccepted，lastError。它记录原生 API 接受状态，不声称系统一定展示通知。

关闭浮窗只隐藏。点击歌词锁定会启用整个歌词窗口鼠标穿透，主设置和托盘必须提供解锁入口。默认仅显示主窗口；新窗口创建失败不得隐藏主窗口。恢复或重置位置按当前屏幕 WorkArea/DIP 夹取，保留负坐标多屏布局；创建窗口显式指定 `InitialPosition: WindowXY` 以使用保存坐标。仅已完成导航的窗口保存 Bounds，移动/缩放 350 ms 去抖，重置与新任务以 generation 作废旧任务。最终关闭先停止待保存任务、读取已就绪窗口最终 Bounds 并在数据库关闭前 flush，再关闭浮窗；强杀只保证此前提交值。

通知只对开始实际播放的新条目判定一次，不因 tick/seek/暂停或稍后隐藏重复。默认关闭，只在主窗口隐藏/最小化时请求；使用现有自有托盘的原生提示，不注册 toast 或注册表项。任务栏标题/进度与同一投影一致，COM 调用需在同一 UI 线程创建、使用、释放；失败安全降级。所有状态都不改变网易云/GD 来源、音质、缓存及限速策略。

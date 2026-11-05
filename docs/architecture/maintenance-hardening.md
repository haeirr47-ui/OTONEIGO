# 2026-11-03 维护加固：目录锁、panic 记录与补丁依赖

本批在 0.5.3 已提交基线上实施。只更新源码与本地构建，不把旧 ZIP 当作本批产物，不制作安装器，也不重新开展账号、VPN/Tailscale 或中国大陆出口测试。

## 评估与实施

| 建议 | 决定与边界 |
| --- | --- |
| 合并两套缓存目录锁 | 采用。`internal/dirlock` 唯一维护操作系统互斥；两个缓存的薄封装保留各自 `ErrInUse` / 缓存失败错误映射。 |
| 增加有界、脱敏崩溃记录 | 采用。安装到主入口、Wails 已捕获 panic 的回调，以及本工程拥有的后台入口。 |
| 所有 worker recover 后继续 | 收窄。只隔离不持共享状态锁、可丢弃结果且已有失败收尾的网络/解码操作；内部不变量异常不能保证安全继续。 |
| 只升级补丁，固定 Wails | 采用。SQLite 1.61.0→1.61.1、websocket 1.8.14→1.8.15、godbus 5.2.2→5.2.4；x/sys 没有正式补丁，保留 0.49.0。Go 与前端 Wails 保持 3.0.0。 |

共享目录锁继续使用持久的 `cache.lock`，Windows 为非阻塞独占 `LockFileEx`，原 Unix 分支保留 `Flock`。释放时关闭句柄但不删除锁文件；进程结束由 OS 释放。不能用替换/删除锁文件来“解锁”，否则可能出现两个目录所有者。

## 本地崩溃记录

`internal/crashreport` 独立于 SQLite、缓存及 playback recorder 的锁。默认目录为 `%LOCALAPPDATA%\OTONEIGO\log`，进程级 `OTONEIGO_DATA_DIR` 会相应改变根目录。文件名为 `crash-<UTC 时间>-<随机值>.txt`，单份最多 16 KiB、最多 48 个当前 goroutine 的调用位置，最多保留五份。写入前预留名额；旧文件无法移除时拒绝新建，避免失败期间无限增长。文件使用独占创建、Sync 后关闭，并被 Git 忽略。

内容只包含固定组件标签、Go 值类别和函数名/文件 basename/行号。Go 没有这里可直接取得的“goroutine 名称”，标签由各入口固定指定。不调用 panic 对象的 `String`、`Error` 或序列化方法，不保存原始 panic 文本、调用参数、全体 goroutine dump、绝对路径或 Wails 的 `Error/StackTrace/FullStackTrace` 文本。这牺牲了异常原文，但避免 URL、签名、Cookie、Token 或本地用户名混入报告。只有发生异常时才写盘，不增加正常播放路径的日志写入。

Wails 3.0.0 原本已有事件、绑定等部分 recovery，不能描述成“整个程序完全没有 recover”。现在它的 `PanicHandler` 接入上述独立记录。无法确认状态完整的异常以退出码 2 终止；主入口避免在 panic 后再次进入可能已经被污染的 shutdown 锁。正常退出仍走原持久化握手和清理。

可恢复范围：地区检测的网络读取失败会结束共享检测并回到未知地区；monitor 异常后退休该监听，原定时检测继续；资源获取/解码失败会释放槽位、结束合并任务和等待者；音频 transport/body 读取失败走原下载失败清理，允许后续重试。内部缓存/reaper/解析器/桌面/退出 gate 等未审计为可安全恢复的状态异常只记录后终止，不自动重启循环。`http.ErrAbortHandler` 仍由 net/http 按普通流中断处理，不生成崩溃或退出进程。

这些机制不覆盖强杀、断电、Go 不可恢复 runtime fatal、任意第三方内部 goroutine 或原生/WebView2 进程崩溃。磁盘写满、权限/文件占用等情况下记录仍可能失败；终止策略不会因此失效。不上传报告、不安装遥测。路径拒绝已有符号链接/Windows reparse point，但不宣称能抵御恶意进程同时替换父目录的竞态。

## 依赖维护策略

本阶段只评审稳定补丁版；不通过 `@latest` 或伪版本顺带升级小版本，不改前端工具链。上述三项均核实了发布标签、模块声明和校验和。参考 [SQLite 上游](https://gitlab.com/cznic/sqlite/-/tags/v1.61.1)、[websocket 发布记录](https://github.com/coder/websocket/releases/tag/v1.8.15)、[godbus 发布记录](https://github.com/godbus/dbus/releases/tag/v5.2.4)。

`scripts/audit-dependencies.ps1` 明确检查 Go Wails 与前端 runtime 都固定为 3.0.0。Wails 升级需单独一批，同时评审托盘窗口识别、图标 ID、原生任务栏调用和所有相关 API，再重跑 `native-desktop05`、`native-audit053` 及 production 退出测试。本批不把固定 Wails 误称为长期不更新，也不依据补丁版本号假定兼容性：必须以回归、Windows 构建及许可审计决定是否采用。（2026-11-05 该锁定点已按上述条件移到 3.1.0，评审与重跑结果见 [Wails 3.1.0 升级记录](../audits/wails-3.1.0-upgrade.md)；本文其余内容仍代其原批次。）

## 验证边界

针对性测试覆盖跨进程目录锁争用/释放/重取、缓存错误映射、报告隐私/容量/并发/锁定文件/符号链接、fatal 子进程退出码，以及地区/资源/音频任务 panic 后的等待者、槽位、重试与 Close。宿主测试直接调用 Wails panic handler 和真实 close-gate timer；它们是本机 Go 子进程测试，不是原生 WebView2 界面验收。完整门禁与当前宿主启动/退出结果另记在 [P0 验证记录](../validation/p0-validation.md)。

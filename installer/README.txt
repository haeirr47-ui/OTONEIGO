OTONEIGO 0.5.4 - Windows x64

启动 / Start: OTONEIGO.exe
需要 Microsoft Edge WebView2 Runtime（不包含在本包中）。
Requires Microsoft Edge WebView2 Runtime (not bundled):
https://developer.microsoft.com/microsoft-edge/webview2/

便携包：解压全部四个文件后启动，无需安装。
Portable: extract all four files together, then start OTONEIGO.exe.

播放队列和曲库保存在 %LOCALAPPDATA%\OTONEIGO\library.db。
免安装运行也使用该目录，数据不放在解压目录中。
Queue/library data: %LOCALAPPDATA%\OTONEIGO\library.db, also for portable runs.
The portable app uses this user-data directory; data is not stored beside the executable.

GD 网易云同 ID 备用为可选设置，默认关闭；有限 MP3 样本已验证，不保证所有曲目、权限或音质。
Optional same-ID GD Netease fallback is off by default; a verified MP3 sample does not guarantee all tracks, rights or quality levels.
本版本无账号登录、导入/导出、自动更新或代码签名。
No account login, import/export, automatic updater or code signing in this version.
网络和曲目可用性依赖网易云音乐服务，部分功能仍处于 P0 验证阶段。
Availability depends on Netease services; P0 verification is still in progress.

正常关闭会尝试保存，最多等待 3 秒；只保证已经提交的本地状态。
Normal close attempts to save for up to 3 seconds; only committed local state is durable.

许可 / Licenses: LICENSE (MIT) and THIRD_PARTY_NOTICES.txt.

首次运行将本工程数据库升级为 schema 7，保留收藏、历史、歌单、设置和队列。旧版不支持升级后的数据库。
This version upgrades this app database to schema 7 while preserving the library and queue. Older versions cannot open the upgraded database.

音质：未登录或未确认会员时仅可首选320 kbps；192/128只作自动回退。当前没有会员账号验证功能，更高音质未开放。
Quality: unsigned/unverified accounts select320 kbps;192/128 are automatic fallback only. Verified membership and premium playback are not yet delivered.

自动模式按出口地区预选网易云播放路径：中国大陆普通请求，海外从首次请求起直接兼容（X-Real-IP），未知地区保留兜底。网易云选定路径失败后才尝试启用的GD，GD不使用该兼容头。缓存优先；网易云已有完整可播结果时不会为追求码率额外请求GD。

本版本新增音频缓存（默认20 GiB，可调整）、长期历史及分页、托盘与关闭隐藏选项。
音频缓存目录：%LOCALAPPDATA%\OTONEIGO\cache\audio。合资格的完整缓存优先复用，未命中时边下载边播放；下载完成并通过校验后才保留。
达到缓存上限时按最近使用淘汰，磁盘余量不足时不继续新增缓存。清理音频缓存不删除收藏、歌单和历史；被播放或暂停媒体源占用的文件延后释放。
历史不再自动删除最旧记录，可在程序中手动清空。关闭窗口时隐藏到托盘默认关闭，可在设置中启用。

0.4.0 新增封面与歌词缓存，默认启用、共享1 GiB，可在设置中调整或清理；目录为 %LOCALAPPDATA%\OTONEIGO\cache\resources。JPEG/PNG/GIF 经完整解码校验，WebP/AVIF 暂时保留联网读取。过期歌词先显示缓存，再后台刷新。没有有效缓存时仍需联网；不是所有图片或音频格式都已验证。
收藏和历史支持数据库整库检索，歌曲、歌手与专辑均可查找；每页最多200条，界面翻页不会丢弃数据库记录。最近搜索保存在本地，可逐条删除或清空。现有100个本地歌单、单歌单及播放队列1000首保护仍保留，收藏不按该数量裁剪。

0.5.0 新增桌面歌词、迷你播放器、任务栏进度与可选切歌通知。请在设置中打开浮窗；它们共用主播放器，关闭浮窗仅隐藏，真正退出主窗口会一并关闭。桌面歌词支持置顶、字号、透明度与锁定穿透；锁定后可在主窗口设置或托盘解锁。位置保存到本地设置，可重置到当前工作区。
切歌通知默认关闭，启用后仅在主窗口隐藏/最小化时请求；Windows免打扰等设置仍可能抑制显示。通知使用已有托盘，不注册额外安装项。桌面功能不改变缓存优先、按出口选择网易云、最后才GD的来源规则。

0.5.1 修复缓存热读开销、锁内音频校验、渐进读取句柄、曲库重试/分页、进度保存合并及设置读取副作用；GD 保持有限预算和限速，缓存的访问时间最多每5秒合并落盘，正常退出时尝试flush。缓存内容的提交与失效仍同步持久化。

0.5.2 将封面内容校验移至首次使用；音频缓存使用1 MiB分块摘要，逐块校验后发送，旧缓存首次完整验证后补建索引。逻辑损坏的收藏、历史或设置在事务中隔离保留，设置回落默认并显示提示，其他正常资料可继续使用；物理数据库错误和未来schema仍拒绝打开。清空历史会同时清除隔离历史。没有新增用户导入导出或备份恢复功能。

0.5.3 将封面首验与网络下载分池；恢复提示可在本次会话“知道了”隐藏，状态检查30秒、缓存统计仅设置页刷新；缓存接纳失败允许同一预算内一次无缓存重解析。访问时间写入小型recency.json，不重复写分块清单；结构清单失败保留脏状态并重试。schema仍为7，原始隔离记录不因关闭提示而删除。

0.5.4 程序文件内嵌图标、清单与版本信息：资源管理器、任务栏和标题栏不再使用 Windows 默认图标，文件属性显示 0.5.4。图标沿用 OTONEI 标帜（小尺寸下加粗描边），免安装四文件与数据目录不变。
0.5.4 embeds the application icon, manifest and version information in OTONEIGO.exe, so Explorer, the taskbar and the title bar no longer use the generic Windows icon. The mark follows OTONEI's logo with a heavier stroke for small sizes; the portable payload and data directory are unchanged.

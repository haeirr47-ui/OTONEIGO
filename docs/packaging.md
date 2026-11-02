# Windows 免安装版打包

> 当前交付版本 0.5.3；最终 Windows production 构建、许可审计、ZIP 四文件及输入哈希核对和有限便携覆盖更新验收已通过。准确条件见 [P0 记录](p0-validation.md)。

当前只交付 Windows x64 免安装 ZIP，安装包制作及安装测试暂停。解压全部四个文件，直接启动 OTONEIGO.exe；需要 Microsoft Edge WebView2 Runtime，当前验证主机已具备该运行时。

## 构建

```powershell
./scripts/package-windows.ps1
# 生产构建与许可审计已完成时：
./scripts/package-windows.ps1 -SkipBuild
```

PortableOnly 默认开启。默认流程不下载或运行 NSIS、不生成 setup、不进行安装操作。生产 EXE 的 metadata 必须为 Windows amd64 且准确使用 production tag；不接受 p0debug。程序与来源许可由显式四文件白名单打包，数据库、缓存、参考项目、调试 EXE 和日志均不入包。

| 文件 | 说明 |
| --- | --- |
| bin/OTONEIGO-0.5.3-windows-x64.zip | 根目录恰好包含 OTONEIGO.exe、LICENSE、THIRD_PARTY_NOTICES.txt、README.txt |
| bin/OTONEIGO-0.5.3-windows-x64-portable.sha256 | 当前 ZIP 的 SHA-256 |

最终核对已确认包内程序与最后生产构建的 bin/OTONEIGO.exe 逐字节一致，许可与说明一并核对。字体及图标许可在内嵌资产和随包通知中保留。免安装 ZIP 不附带未使用的 NSIS 运行时通知。历史 0.3.0 验收记录在 output/packaging/portable-0.3-final-report.json，该目录被 Git 忽略；旧 portable-final-report.json 仅代表先前产物。以下为历史 0.4.0 核对，不能替代 0.5.0：报告为 output/packaging/portable-0.4-final-report.json。ZIP 大小 19,385,471 字节，SHA-256 为 `dda777ad145e85d53672a626e15c0fbf3d4b73336f5862f12f3d0b114e0fdf02`，恰好四个白名单文件且逐项哈希匹配最终 bin 输入。

## 数据与退出

本批 schema 5→6 事务升级桌面设置，保留其他本地数据；失败回滚，高于6拒绝打开。免安装表示无需执行安装器；本地数据仍存于 %LOCALAPPDATA%\OTONEIGO，包含 schema 7 library.db、cache/audio 音频缓存、cache/resources 封面/歌词缓存、WebView2 profile 与脱敏日志。解压目录不是数据目录。更新时关闭程序，替换解压出的四个文件；不删除用户数据目录。本工程 schema 4→5 在单事务中校验旧数据并回填检索字段，失败整体回滚；当前版本隔离 profile 的实际强杀恢复、正常退出与便携覆盖更新已通过有限用例；schema 4→5 另以合成 fixture 验证。不是旧产品迁移或用户备份/恢复。closeToTray 默认 false；启用且托盘可用时关窗隐藏现有播放窗口，托盘退出才进入最多三秒的保存确认，强杀只保证已经提交的状态。

## 0.4.0 便携覆盖更新验收

`output/packaging/portable-update-0.4-report.json` 记录旧 payload 先复制、随后由当前四文件覆盖，替换期间数据库哈希不变；`oldVersionLaunched=false`，没有把真实旧 production 版本启动到该数据库上。schema 4→5 使用独立合成旧 schema fixture 验证，不能将文件覆盖检查称为旧应用跨版本启动验收。

解压出的最终 0.4.0 production EXE 在进程级不可达代理下实际启动、显示主窗口并完成初始化，WM_CLOSE 后正常退出码 0。Go/SQLite 复核 schema 5、integrity、6010 收藏、6012 历史、1 歌单、1 关键词、队列两项/3000 ms/revision 60，未发现运行时媒体字段持久化。较早隐藏启动 smoke 未等待稳定显示初始化，退出码 1 且窗口不可观测；测试改为精确窗口类/标题、显示及初始化等待后通过，未因此修改原生代码。这是有限生产包检查，不是安装器、真实旧版本运行、物理断网或长期稳定性验收。

## 安装测试历史

旧安装器源码保留待后续明确需求，当前不作为交付或验收入口。此前隔离安装测试结果在 [P0 记录](p0-validation.md) 中按时间保留，不能代替当前版本的安装验收。

本次 UI 版本安装复验因 /D 参数引号问题误写默认程序目录，未完成升级/卸载复验；重试被自动审批拒绝，随后停止安装工作。默认目录已核实为本次新建，精确核对六个测试文件哈希后清理，并只删除空目录；未执行卸载器、未访问或删除用户曲库。事件与清理证据在 output/packaging/otonei-ui-smoke.json 和 test-install-cleanup.json。


## 0.5.0 最终免安装交付

`output/packaging/portable-0.5-final-report.json` 核对最终四文件与构建输入 SHA-256 一致。ZIP 大小 19437467 字节，SHA-256 `037e2b226abc8d3ef7bd5a19d1b0eec48d22beeb3c907b98c9eb219903167d56`。准确 production tag，不带 CDP、数据库、缓存、参考代码或测试日志。桌面窗口、通知和任务栏仍只使用现有 Wails/Windows 能力，没有注册安装项。

`portable-update-0.5-report.json` 先解压0.4 payload再覆盖0.5四文件，替换期间隔离数据库哈希不变；旧应用未启动。最终解压 production EXE 在进程级不可达代理下显示/初始化，WM_CLOSE正常退出码0，schema6/integrity、1收藏/4历史/两队列与桌面位置保留。旧schema5→6另由独立fixture和迁移回归验证。不是安装器、真实网络出口切换、多小时稳定性或用户备份恢复验收。


## 0.5.1 审计补丁版

本批仍仅ZIP。真实production解压程序启动/初始化/退出码0、schema6/千条隔离曲库及原始高级偏好保留通过；覆盖更新只放置旧payload再替换文件，未运行旧版本。ZIP白名单及精确结果见[核实记录](audit-fixes-0.5.1.md)。

暂停的NSIS源码已移除固定标题FindWindow保护，改为精确目标EXE的GENERIC_WRITE/OPEN_EXISTING检查，打开失败即拒绝；没有截断或写入字节。Windows原生同机制验证运行映像写打开失败、退出后成功，文件哈希不变；NSIS仅静态复查，未编译/运行，安装/升级验收仍暂停。保护不依赖动态标题，不等同于完整安装器生命周期验收。


## 0.5.2 启动与恢复补丁

只交付ZIP。首次将本工程schema0..6事务升级到7，坏行保留在私有隔离表、设置可回落默认并提示；旧程序不能打开7。物理/SQL错误和未来schema不自动默认覆盖。音频内部块索引兼容旧缓存，首次无索引完整校验成本仍存在。原始隔离数据、cache、日志和调试EXE不入包。

当前完整测试/生产构建、真实WebView2的恢复/索引/损坏410/未知字段及精确ZIP生产启动退出条件见[审计记录](audit-fixes-0.5.2.md)。没有安装器执行、签名或远程发布；下述旧批次证据仍只属于其原版本。

0.5.2最终ZIP大小 19468539 字节，SHA-256 `2ad8ab0c028eb10cf8595124d963e6c0ab930857c494776643cbba958235725b`；报告output/packaging/portable-0.5.2-final-report.json逐项核对四文件。portable-update-0.5.2-report.json验证精确解压EXE正常退出码0、schema7数据和未知字段保留；旧应用未启动。


## 0.5.3 缓存与提示补丁

只交付ZIP，schema仍7，缓存内部新增可选Generation和recency.json，旧清单可读；会话关闭提示不删除归档或写入浏览器存储。原始记录、侧车、缓存、日志和debug程序不入四文件包。最终白名单、hash和精确production程序启动退出条件见[0.5.3审计](audit-fixes-0.5.3.md)。安装器/签名/远程发布继续暂停。

0.5.3最终ZIP为 19475988 字节，SHA-256 `80b501f8d8890df9357a9c2e48c0a19f4ec642b2c0f91c39f5dacaa6c18e05b4`。四白名单文件逐项匹配最终输入；精确解压production程序正常启动、初始化、退出码0，schema7/integrity、1004收藏/1007历史、两条隔离记录和未知设置保留。覆盖只复制旧payload再替换，oldVersionLaunched=false。

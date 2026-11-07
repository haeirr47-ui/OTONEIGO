# Windows 免安装版打包

> 当前交付版本 0.5.4。本批新增 EXE 内的 Windows 资源（图标、清单、版本信息），并收尾 Wails 3.0.0→3.1.0 升级（Go/runtime/CLI 锁定点与许可清单同步）：`scripts/verify.ps1` 全量、许可审计、资源门禁、production 启动/退出、audit053 矩阵（本批有一次非断言类等待超时抖动，原样重跑通过，见升级记录）、desktop05 浮窗/托盘/任务栏/通知阶段与重启恢复均通过；未通过的是 desktop05 矩阵里依赖 live 网易云取流的请求阶段计数断言，以及因此未执行到的自然结束/自动连播尾段，原因见 [Wails 3.1.0 升级记录](../audits/wails-3.1.0-upgrade.md)，不写成“全部通过”。历史 0.5.3 ZIP 及其哈希仍只代表 0.5.3。准确条件见 [P0 记录](../validation/p0-validation.md)。

当前只交付 Windows x64 免安装 ZIP，安装包制作及安装测试暂停。解压全部四个文件，直接启动 OTONEIGO.exe；需要 Microsoft Edge WebView2 Runtime，当前验证主机已具备该运行时。

## 构建

```powershell
./scripts/package-windows.ps1
# 生产构建与许可审计已完成时：
./scripts/package-windows.ps1 -SkipBuild
```

PortableOnly 默认开启。默认流程不下载或运行 NSIS、不生成 setup、不进行安装操作。生产 EXE 的 metadata 必须为 Windows amd64 且准确使用 production tag；不接受 p0debug。程序与来源许可由显式四文件白名单打包，数据库、缓存、参考项目、调试 EXE 和日志均不入包。

## EXE 资源（0.5.4）

`scripts/build.ps1` 在 `go build` 前执行标准 Wails 资源流水线，产物只进入 EXE，不新增 ZIP 文件：

| 文件 | 角色 | Git |
| --- | --- | --- |
| `cmd/appicon/main.go` | 由 OTONEI 标帜几何渲染 1024×1024 母图，小尺寸使用加粗描边 | 跟踪 |
| `packaging/windows/appicon.png` | 图标母图 | 跟踪 |
| `packaging/windows/info.json` | 版本信息表（en-US 0409） | 跟踪 |
| `packaging/windows/wails.exe.manifest` | Per-Monitor V2 DPI、Common Controls 6、asInvoker | 跟踪 |
| `packaging/windows/icon.ico` | `wails3 generate icons` 中间产物 | 忽略 |
| `wails_windows_amd64.syso` | `wails3 generate syso` 中间产物，构建后立即删除 | 忽略 |

图标来源不依赖 `reference/`：标帜矢量件已在产品内保存为 `frontend/public/logo.svg`，与 `docs/delivery/source-provenance.json` 记录的 OTONEI 原件哈希逐字节一致，删除参考副本仍可重建图标与资源。

发布版本号由 `frontend/package.json` 拥有。`scripts/build.ps1` 在构建前拒绝与它不一致的 `packaging/windows/info.json`（`fixed.file_version`、`fixed.product_version`、`info."0409".FileVersion`、`info."0409".ProductVersion`）与 `wails.exe.manifest` 的 `assemblyIdentity version`；`installer/README.txt` 首行的版号检查放在 `scripts/toolchain.ps1` 的 `Assert-PayloadVersionDocuments`，由 build 与 package 两条路径共同调用，因此 `-SkipBuild` 也不能把旧版说明和新版程序一起打进包。`scripts/package-windows.ps1` 要求 EXE 的 ProductVersion 与 FileVersion 完全等于该版号（前缀匹配会放过 0.5.40，所以不用），并按 PE 资源目录枚举断言图标、清单与版本资源存在，使 `-SkipBuild` 也不能带走旧镜像或缺资源的镜像。因此升版需同步：`frontend/package.json`、`packaging/windows/info.json` 四处、`packaging/windows/wails.exe.manifest` 与 `installer/README.txt` 首行；不一致时构建直接失败，不依赖人工记忆。

图标母图本身也是构建输入，因此 `cmd/appicon/main_test.go` 渲染 1024×1024 后与 `packaging/windows/appicon.png` 逐字节比对：手工替换母图会在这里失败，而不是让一个只数 `RT_ICON` 条目的门禁悄悄放行。该断言同时使 `docs/delivery/source-provenance.json` 记录的母图哈希保持有效，编码器行为变化时也同样报错。

打包完成后 `scripts/package-windows.ps1` 自己写出 `output/packaging/portable-<版本>-final-report.json`：ZIP 大小与 SHA-256、四个包内条目逐项哈希、对应输入文件哈希、EXE 资源枚举计数、未重跑项清单，以及 `gateScriptSHA256`（`toolchain`/`build`/`audit-dependencies`/`package-windows` 在出报告那一刻的哈希），不再需要手工解压比对；后续任何脚本改动都会改变其中的哈希，使"报告与代码是否同一版本"可判定而不是靠推断。

## 交付物

| 文件 | 说明 |
| --- | --- |
| bin/OTONEIGO-0.5.4-windows-x64.zip | 2026-11-07 切片 5（含审查修复）后重新打包：19,561,729 字节，SHA-256 `19d40347a64e542682e05b5f8bc2686000a5feea6be968dfdb282f7dab9315a4`；版号仍为 0.5.4，但内容含 11-05～11-07 的全部前端批次（含多类型搜索与站内专辑/歌手详情）；根目录恰好包含 OTONEIGO.exe、LICENSE、THIRD_PARTY_NOTICES.txt、README.txt |
| bin/OTONEIGO-0.5.4-windows-x64-portable.sha256 | 当前 ZIP 的 SHA-256 |
| bin/OTONEIGO-0.5.3-windows-x64.zip | 上一交付批次，不含 EXE 资源段 |
| bin/OTONEIGO-0.5.3-windows-x64-portable.sha256 | 历史 ZIP 的 SHA-256 |

最终核对已确认包内程序与最后生产构建的 bin/OTONEIGO.exe 逐字节一致，许可与说明一并核对。字体及图标许可在内嵌资产和随包通知中保留。免安装 ZIP 不附带未使用的 NSIS 运行时通知。历史 0.3.0 验收记录在 output/packaging/portable-0.3-final-report.json，该目录被 Git 忽略；旧 portable-final-report.json 仅代表先前产物。以下为历史 0.4.0 核对，不能替代 0.5.0：报告为 output/packaging/portable-0.4-final-report.json。ZIP 大小 19,385,471 字节，SHA-256 为 `dda777ad145e85d53672a626e15c0fbf3d4b73336f5862f12f3d0b114e0fdf02`，恰好四个白名单文件且逐项哈希匹配最终 bin 输入。

资源段由 `scripts/native-icon054.ps1` 验收（断言，不是记录）：PE 资源目录枚举得 `icon=8, groupIcon=1, version=1, manifest=1`，尺寸数量必须等于 `Get-IconSizePlan` 的 8；`FileVersionInfo` 的 ProductVersion/FileVersion 与 0.5.4 完全相等；shell 能取出图标（本机 32px，像素值不作断言）；新建 production WebView 窗口的 DPI 上下文必须是 Per-Monitor V2；WM_CLOSE 退出码 0。DPI 216、窗口 1200×800 在当前主机上作为观察值记录，不是验收条件。报告 `output/packaging/native-icon-054-report.json` 绑定的是 0.5.4 交付批次的 `OTONEIGO.exe`（SHA-256 `9b5257e4ec06ed6c45fa092939e0d4950a963270beda69b0682f4fcaa84eee57`，当时逐字节等于最后生产构建；当前包内镜像见下方 11-07 段）。四文件包内/输入逐项哈希见 `output/packaging/portable-0.5.4-final-report.json`。CDP 主界面与浮窗矩阵因同批升级 Wails 而重跑，结果与未达成项见 [Wails 3.1.0 升级记录](../audits/wails-3.1.0-upgrade.md)。

0.5.4 未重跑便携覆盖更新与旧版本启动，未执行安装器、签名或远程发布；这些限制不因本批完成而自动取消。

2026-11-05 播放器订阅批次重建了 `bin/OTONEIGO.exe`（17:15:58，SHA-256 `d79b2cc295f7a48addfe6190550d5a777bfc2e57afe48d62dde5f92c67464f7f`），但**未重新打包**：`bin/OTONEIGO-0.5.4-windows-x64.zip` 仍是 0.5.4 交付批次（`db347318…78afd6`，内含 `9b5257e4…a84eee57`），`output/packaging/portable-0.5.4-final-report.json` 的 `inputSHA256` 也随之过期。所以"包内程序与最后生产构建逐字节一致"只对 0.5.4 打包那一刻成立，当前工作区的 EXE 比 ZIP 新。若要发布含该批次改动的包，必须重新执行 `scripts/package-windows.ps1`（不带 `-SkipBuild`）并重记本表哈希与报告。该状态已于 2026-11-07 收口，见下段。

2026-11-06 匿名目录批次再次经 `verify.ps1` 重建 `bin/OTONEIGO.exe`（09:51:44，30,263,808 字节，SHA-256 `592d37bfbc0fee445a6139629de647c83276a135b9501dc294ba4939e051b6c0`，资源 `icon=8 / groupIcon=1 / version=1 / manifest=1`），当时仍未重新打包：ZIP 与会话内报告的哈希仍指向 0.5.4 交付批次，而 `bin/OTONEIGO.exe` 已包含排行榜、队列操作与快捷键三个前端批次。该漂移已于 2026-11-07 收口，见下段。

2026-11-07 播放器时间气泡修正后（本日早前）重新构建并重新打包，该产物已被切片 5 构建覆盖：`./scripts/verify.ps1` exit=0（Go 全量测试/vet、前端 223 项测试、lint/format、49 方法/34 模型绑定、78 modules 的 Vite 构建、Windows production 构建、11 份上游许可门禁），随后 `./scripts/package-windows.ps1 -SkipBuild` 重出 ZIP。`bin/OTONEIGO.exe` 为 30,264,832 字节、SHA-256 `0154029bc14f8df2ecd925e30dea24bfae13aadabbf8aa6e59a3d926f2f7d5e1`（14:39:47），资源 `icon=8 / groupIcon=1 / version=1 / manifest=1`；ZIP 为 19,550,889 字节、SHA-256 `79f1340df496d0d6518b1447c2c9b742bad33da5089da3abbf977810ceebc1d4`（14:40:08）。`output/packaging/portable-0.5.4-final-report.json` 的 `entrySHA256.OTONEIGO.exe` 与 `inputSHA256.OTONEIGO.exe` 同为 `0154029b…7d5e1`，四文件白名单与逐项哈希匹配，"包内程序与最后生产构建逐字节一致"再次成立。版号仍是 0.5.4，但此包已包含播放器选择性订阅、快捷键/帮助、匿名目录、下一首播放、曲目菜单与分享以及本次时间气泡修正；`installer/README.txt` 仍写 0.5.4 的说明。可直接解压该 ZIP 或直接运行 `bin/OTONEIGO.exe` 验证。未重跑便携覆盖更新、旧版本启动、安装器/签名/远程发布。

2026-11-07 切片 5（多类型搜索、站内专辑/歌手详情）含代码审查修复后经 `verify.ps1` exit=0 重新打包，**这是当前有效状态**：`bin/OTONEIGO.exe` 为 30,306,816 字节、SHA-256 `025a6f6ef0026aea086492be9a9c77543390cf5770fc6f41f9cade2cfa574981`，资源 `icon=8 / groupIcon=1 / version=1 / manifest=1`；ZIP 为 19,561,729 字节、SHA-256 `19d40347a64e542682e05b5f8bc2686000a5feea6be968dfdb282f7dab9315a4`。`output/packaging/portable-0.5.4-final-report.json` 的 `entrySHA256.OTONEIGO.exe` 与 `inputSHA256.OTONEIGO.exe` 同为 `025a6f6e…4981`，四文件白名单与逐项哈希匹配。绑定为 51 方法/37 模型，Vite 构建 80 modules。可直接解压 ZIP 或运行 `bin/OTONEIGO.exe` 验证：搜索页四类切换、实体结果卡片、站内详情页与右键菜单的"查看专辑/查看歌手"。未重跑便携覆盖更新、旧版本启动、安装器/签名/远程发布，新增页面也未做原生 WebView2 验收。

复核补记（2026-11-05 16:02–16:12）：`bin/OTONEIGO.exe` 仍是 14:58:45 那次 production 构建的产物（SHA-256 `9b5257e4…a84eee57`，未重建）；打包与资源两份报告由最终脚本重跑生成，ZIP 逐字节不变（19,530,458 字节、`db347318…78afd6`），因此当时上表的哈希仍然有效（上表已于 2026-11-07 更新为最新打包结果）。注意 ZIP 哈希与载荷的最后写入时间绑定（`Compress-Archive` 记录 DOS 时间戳）：复核中重跑 `audit-dependencies.ps1` 即使 `bin/THIRD_PARTY_NOTICES.txt` 内容逐字节相同，只要改写时间变了，重新打包就会得到不同哈希而内容不变；按原时间回填该文件后复现了上表哈希，这也说明"哈希变了"不等于"内容变了"，比对前应先看报告里的条目哈希。同一次复核把 `installer/README.txt` 的版号检查移入 `scripts/toolchain.ps1` 并让打包路径也调用它，补上母图复现测试与报告里的 `gateScriptSHA256`。未重跑的是 `scripts/native-wails054.ps1` 的整链聚合（它会经 `verify.ps1` 重建 EXE 并改变上述哈希），其 15:00 报告仍是那次运行的原样记录。

## 数据与退出

本批 schema 5→6 事务升级桌面设置，保留其他本地数据；失败回滚，高于6拒绝打开。免安装表示无需执行安装器；本地数据仍存于 %LOCALAPPDATA%\OTONEIGO，包含 schema 7 library.db、cache/audio 音频缓存、cache/resources 封面/歌词缓存、WebView2 profile 与脱敏日志。解压目录不是数据目录。更新时关闭程序，替换解压出的四个文件；不删除用户数据目录。本工程 schema 4→5 在单事务中校验旧数据并回填检索字段，失败整体回滚；当前版本隔离 profile 的实际强杀恢复、正常退出与便携覆盖更新已通过有限用例；schema 4→5 另以合成 fixture 验证。不是旧产品迁移或用户备份/恢复。closeToTray 默认 false；启用且托盘可用时关窗隐藏现有播放窗口，托盘退出才进入最多三秒的保存确认，强杀只保证已经提交的状态。

## 0.4.0 便携覆盖更新验收

`output/packaging/portable-update-0.4-report.json` 记录旧 payload 先复制、随后由当前四文件覆盖，替换期间数据库哈希不变；`oldVersionLaunched=false`，没有把真实旧 production 版本启动到该数据库上。schema 4→5 使用独立合成旧 schema fixture 验证，不能将文件覆盖检查称为旧应用跨版本启动验收。

解压出的最终 0.4.0 production EXE 在进程级不可达代理下实际启动、显示主窗口并完成初始化，WM_CLOSE 后正常退出码 0。Go/SQLite 复核 schema 5、integrity、6010 收藏、6012 历史、1 歌单、1 关键词、队列两项/3000 ms/revision 60，未发现运行时媒体字段持久化。较早隐藏启动 smoke 未等待稳定显示初始化，退出码 1 且窗口不可观测；测试改为精确窗口类/标题、显示及初始化等待后通过，未因此修改原生代码。这是有限生产包检查，不是安装器、真实旧版本运行、物理断网或长期稳定性验收。

## 安装测试历史

旧安装器源码保留待后续明确需求，当前不作为交付或验收入口。此前隔离安装测试结果在 [P0 记录](../validation/p0-validation.md) 中按时间保留，不能代替当前版本的安装验收。

本次 UI 版本安装复验因 /D 参数引号问题误写默认程序目录，未完成升级/卸载复验；重试被自动审批拒绝，随后停止安装工作。默认目录已核实为本次新建，精确核对六个测试文件哈希后清理，并只删除空目录；未执行卸载器、未访问或删除用户曲库。事件与清理证据在 output/packaging/otonei-ui-smoke.json 和 test-install-cleanup.json。


## 0.5.0 最终免安装交付

`output/packaging/portable-0.5-final-report.json` 核对最终四文件与构建输入 SHA-256 一致。ZIP 大小 19437467 字节，SHA-256 `037e2b226abc8d3ef7bd5a19d1b0eec48d22beeb3c907b98c9eb219903167d56`。准确 production tag，不带 CDP、数据库、缓存、参考代码或测试日志。桌面窗口、通知和任务栏仍只使用现有 Wails/Windows 能力，没有注册安装项。

`portable-update-0.5-report.json` 先解压0.4 payload再覆盖0.5四文件，替换期间隔离数据库哈希不变；旧应用未启动。最终解压 production EXE 在进程级不可达代理下显示/初始化，WM_CLOSE正常退出码0，schema6/integrity、1收藏/4历史/两队列与桌面位置保留。旧schema5→6另由独立fixture和迁移回归验证。不是安装器、真实网络出口切换、多小时稳定性或用户备份恢复验收。


## 0.5.1 审计补丁版

本批仍仅ZIP。真实production解压程序启动/初始化/退出码0、schema6/千条隔离曲库及原始高级偏好保留通过；覆盖更新只放置旧payload再替换文件，未运行旧版本。ZIP白名单及精确结果见[核实记录](../audits/audit-fixes-0.5.1.md)。

暂停的NSIS源码已移除固定标题FindWindow保护，改为精确目标EXE的GENERIC_WRITE/OPEN_EXISTING检查，打开失败即拒绝；没有截断或写入字节。Windows原生同机制验证运行映像写打开失败、退出后成功，文件哈希不变；NSIS仅静态复查，未编译/运行，安装/升级验收仍暂停。保护不依赖动态标题，不等同于完整安装器生命周期验收。


## 0.5.2 启动与恢复补丁

只交付ZIP。首次将本工程schema0..6事务升级到7，坏行保留在私有隔离表、设置可回落默认并提示；旧程序不能打开7。物理/SQL错误和未来schema不自动默认覆盖。音频内部块索引兼容旧缓存，首次无索引完整校验成本仍存在。原始隔离数据、cache、日志和调试EXE不入包。

当前完整测试/生产构建、真实WebView2的恢复/索引/损坏410/未知字段及精确ZIP生产启动退出条件见[审计记录](../audits/audit-fixes-0.5.2.md)。没有安装器执行、签名或远程发布；下述旧批次证据仍只属于其原版本。

0.5.2最终ZIP大小 19468539 字节，SHA-256 `2ad8ab0c028eb10cf8595124d963e6c0ab930857c494776643cbba958235725b`；报告output/packaging/portable-0.5.2-final-report.json逐项核对四文件。portable-update-0.5.2-report.json验证精确解压EXE正常退出码0、schema7数据和未知字段保留；旧应用未启动。


## 0.5.3 缓存与提示补丁

只交付ZIP，schema仍7，缓存内部新增可选Generation和recency.json，旧清单可读；会话关闭提示不删除归档或写入浏览器存储。原始记录、侧车、缓存、日志和debug程序不入四文件包。最终白名单、hash和精确production程序启动退出条件见[0.5.3审计](../audits/audit-fixes-0.5.3.md)。安装器/签名/远程发布继续暂停。

0.5.3最终ZIP为 19475988 字节，SHA-256 `80b501f8d8890df9357a9c2e48c0a19f4ec642b2c0f91c39f5dacaa6c18e05b4`。四白名单文件逐项匹配最终输入；精确解压production程序正常启动、初始化、退出码0，schema7/integrity、1004收藏/1007历史、两条隔离记录和未知设置保留。覆盖只复制旧payload再替换，oldVersionLaunched=false。

# 0.5.4 EXE 图标、清单、版本信息与 Wails 3.1.0

2026-11-05。本批交付两件事：Windows 可执行文件资源段（图标、application manifest、版本信息）与 Wails 3.0.0→3.1.0 升级收尾。schema 仍为 7，源选择/缓存/GD/限速规则、账号禁用、导入导出与安装器范围全部不变；没有修改 Go 运行时代码或前端业务代码。改动面如实列出：新增 `cmd/appicon` 与 `packaging/windows/` 资产、新增 `scripts/native-icon054.ps1`/`scripts/native-wails054.ps1`/`scripts/native-wails054-fixture.py`、修改 `scripts/toolchain.ps1`（共享版本与资源断言）、`scripts/build.ps1`、`scripts/package-windows.ps1`、`scripts/audit-dependencies.ps1`、`scripts/bootstrap.ps1`、两处依赖 Wails 内部细节的 Go 注释（仅注释，不改行为）以及文档。历史验收脚本本体（含 `scripts/native-audit053.ps1`）保持不动，fixture 复位集中在本批新增的 helper 里。

## 起因与判定

0.5.3 及以前的 `scripts/build.ps1` 直接 `go build`，不生成 `.syso`，仓库内也没有 `.ico/.rc`。核对 `bin/OTONEIGO.exe` 的 PE 头确认资源目录 RVA 为 0、段表只有 `.text/.rdata/.data/.pdata/.xdata/.idata/.reloc/.symtab`，即整份镜像没有 `.rsrc`：文件、任务栏与标题栏只能用系统默认图标，文件属性也没有版本字段。Wails 在 Windows 上正是从 EXE 资源取图标（`application_windows.go` 用 `LoadIconWithResourceID`，托盘默认取 `RT_ICON`），因此缺资源段与缺图标是同一件事。托盘当时仍显示图标，是因为 `desktop_windows.go` 的 `desktopIcon()` 在运行时自行绘制 PNG，与资源段无关；本批保留该行为不变。

## 实施

| 位置 | 变更 |
| --- | --- |
| `cmd/appicon/main.go` | 新增 stdlib 渲染器：按 `frontend/public/logo.svg` 的 viewBox 几何（环心 32,32、半径 20、符干 x=37/y 14.5–49.5、圆头端点）用距离场绘制 1024×1024 母图，描边 4.6 单位（web 为 3）以保住 16/24px 可读性，浅底 `#f7f7f5` 圆角方形 + 黑色标帜，并输出缩小预览供肉眼检查。 |
| `packaging/windows/appicon.png` | 上一步产物，作为图标母图入库。 |
| `packaging/windows/wails.exe.manifest` | 标准 Wails 清单：Per-Monitor V2（回退 permonitor）、Common Controls 6.0.0.0、`asInvoker`。 |
| `packaging/windows/info.json` | 版本信息表，语言键 `0409`、代码页 1200，含 ProductName/FileVersion/ProductVersion/FileDescription/CompanyName/LegalCopyright/OriginalFilename。 |
| `scripts/build.ps1` | `go build` 前依次 `wails3 generate icons`（尺寸清单集中在 `toolchain.ps1` 的 `Get-IconSizePlan`）与 `wails3 generate syso`；icons/syso/build 整段包在 `try/finally` 里，任一步失败都不遗留 `.syso`。构建前拒绝 `frontend/package.json`、`info.json` 四处与清单共六处版本不一致，并以完整点分数字精确比较 `installer/README.txt` 首行版本号；构建后按 PE 资源目录枚举断言 RT_ICON/RT_GROUP_ICON/RT_VERSION/RT_MANIFEST，并要求版本串完全相等。 |
| `scripts/toolchain.ps1` | 新增 `Get-ProjectVersion`/`Get-IconSizePlan`/`Get-WindowsResourceInventory`/`Assert-WindowsResources`/`Assert-ExecutableVersion`，避开三处重复的前缀匹配。`ExtractAssociatedIcon` 不再作为存在性证据：无资源镜像它会返回 shell 默认图标。 |
| `scripts/package-windows.ps1` | 版本号改读 `frontend/package.json`；用完全相等断言 EXE 版本资源、按 PE 资源目录枚举断言图标与清单存在，使 `-SkipBuild` 也不能带走缺资源的旧镜像；新增四文件包内逐项哈希与输入比对，并写出 `output/packaging/portable-<版本>-final-report.json`。 |
| `scripts/native-icon054.ps1` | 资源枚举、版本串、shell 取图标、Per-Monitor V2 上下文、正常退出码均为断言；DPI 值与窗口尺寸作为主机观察记录，不当作验收条件。报告 `pass` 由各项计算得出，不再是常量。 |
| `.gitignore` | 忽略生成的 `packaging/windows/icon.ico` 与 `wails_windows_amd64.syso`。 |
| `installer/README.txt`、`frontend/THIRD_PARTY_NOTICES.md`、`docs/*` | 版本说明、图标资产来源与许可声明。 |

DPI 行为不变的依据：Wails `newPlatformApp` 本来就调用 `setupDPIAwareness()` 请求 Per-Monitor V2，并在检测到上下文已由清单设定后跳过 API（框架源码注释引用 issue 4803/4835）。清单只是把设定时点从 `application.New` 提前到进程加载，最终上下文相同；新验收实测窗口 DPI 上下文仍等于 `DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2`。Common Controls 6 依赖会让框架的 `TaskDialogAvailable()` 变为真，但本工程未调用任何 Wails 对话框 API（全库检索 `MessageDialog/ErrorDialog/ConfirmDialog/Dialogs` 无命中），崩溃与错误提示仍走 `windows.MessageBox`，因此该路径未被启用。

图标来源与 `reference/` 解耦：矢量标帜早已作为产品资产存在于 `frontend/public/logo.svg`，本批重新计算其 SHA-256，与 `docs/source-provenance.json` 记录的 OTONEI 原件（现标注 `missing: true`）逐字节一致；构建与打包读取的都是产品内文件，删除 `reference/` 后仍可完整重建图标与资源。OTONEI 与本工程同为 MIT、同一版权人，衍生母图已在随包通知中声明。

## 验收

- `./scripts/verify.ps1`：gofmt、`go test ./...`、`go vet ./...`、前端 format/lint、190 项前端测试（14 文件）、TypeScript/Vite 与带资源的 production 构建全部通过，末行报告资源枚举计数与 `version 0.5.4`。
- 资源核对：`bin/OTONEIGO.exe` 现有 8 个 `RT_ICON`、`RT_GROUP_ICON`、`RT_VERSION`（lang 0409）与 `RT_MANIFEST`，由 `Assert-WindowsResources` 通过 `EnumResourceNames` 枚举断言（数量必须等于 `Get-IconSizePlan` 的 8）；`FileVersionInfo` 的 ProductName/ProductVersion/FileVersion 要求与发布版号完全相等；`ExtractAssociatedIcon` 在本机取到 32px，像素值只记录，门禁仅断言能取出图标，作为 shell 级额外检查。开发期另外用 Win32 `GetFileVersionInfo`/`VerQueryValue` 手工核对过各字符串表字段，但脚本门禁不依赖那段临时检查。资源段使 EXE 从 30,161,408 增至 30,187,520 字节（+26,112）。
- `scripts/native-icon054.ps1`：隔离 `OTONEIGO_DATA_DIR` + 进程级不可达代理下启动真实 production EXE，唯一 `WailsWebviewWindow` 显示并完成初始化，Per-Monitor V2 上下文为真（断言），WM_CLOSE 退出码 0；DPI 216、窗口 1200×800 作为当前主机观察值记录，不是验收条件（100% 缩放主机合法地报 96，那样也不应算失败）。报告 `output/packaging/native-icon-054-report.json` 包含逐项 `checks` 与绑定的镜像哈希。
- 图标实现本身不依赖 Wails 版本，但同批升级了 Wails，因此 CDP 主界面与浮窗矩阵已在 3.1.0 下重跑（含窗口样式与重启）；结果、未达成断言与原因单独记录在 [Wails 3.1.0 升级记录](wails-3.1.0-upgrade.md)，本批不拿它当作图标效果的证明。未执行：Go race（CGO 关闭）；安装器、签名、远程发布仍暂停；未做多屏/休眠或真实出口切换验证；便携覆盖更新未重跑。

## 交付

开工时 HEAD 提交 `3db7f87 chore(deps)` 把 `go.mod` 的 Wails 带到 v3.1.0、`@wailsio/runtime` 带到 3.1.0，但 `scripts/audit-dependencies.ps1` 仍硬锁 v3.0.0、`scripts/bootstrap.ps1` 仍安装 CLI v3.0.0，许可审计门禁必然失败，因此本批先按项目规则把升级单独完成（评审、锁定点同步、原生矩阵重跑），再出包；全部过程、未通过项与原因见 [Wails 3.1.0 升级记录](wails-3.1.0-upgrade.md)。

最终免安装 ZIP 为 19,530,458 字节，SHA-256 `db34731819d359b35d5955371e55a275b23bcc8bead47ad6e7345dd67278afd6`（`bin/OTONEIGO-0.5.4-windows-x64-portable.sha256`）。包内恰好四个白名单文件，`OTONEIGO.exe` 与最后生产构建逐字节一致（SHA-256 `9b5257e4ec06ed6c45fa092939e0d4950a963270beda69b0682f4fcaa84eee57`），逐项包内/输入哈希由 `scripts/package-windows.ps1` 自己写入 `output/packaging/portable-0.5.4-final-report.json`；随包通知已重生成并记录 Wails v3.1.0。该 EXE 的真实启动/初始化/退出验收见 `output/packaging/native-icon-054-report.json`；覆盖更新与旧版本启动未重跑，安装器、签名、远程发布继续暂停。本批不执行自动暂存、提交或推送，Git 状态以仓库实际记录为准。

## 复核补记（2026-11-05 16:03）

代码审查发现上文的"报告与代码状态同一时刻"不成立：本批脚本在首次整链运行（14:58–15:00）之后仍被修改过。按"必须修或必须改正"处理如下，判断项保持原样。

| 项 | 处理 |
| --- | --- |
| `-SkipBuild` 可绕过 README 版号门禁 | 检查下沉为 `toolchain.ps1` 的 `Assert-PayloadVersionDocuments`，`build.ps1` 与 `package-windows.ps1` 均调用；匹配与拒绝两个分支已分别验证 |
| `Add-Type` 使用 C# 9 目标类型推断 `new()` | 改为显式 `new List<string>()`，去掉对编译器的隐式版本要求，与仓库其他脚本一致 |
| "母图可由 `cmd/appicon` 复现"只有文字声明 | 新增 `cmd/appicon/main_test.go`：渲染 1024×1024 并与已提交 PNG 逐字节比对 |
| 报告无法判断是否对应当前脚本 | `portable-<版本>-final-report.json` 新增 `gateScriptSHA256`（四个门禁脚本的出报告时哈希） |
| 文档与产物不一致 | 更正"同一时刻"表述并记录真实时间线；补写验收 EXE 不含资源段，desktop05 矩阵不覆盖资源段效果 |

复核实际执行：`gofmt -l`、`go vet ./...`、`go test ./...`（含新增测试）、`scripts/native-icon054.ps1`、`scripts/package-windows.ps1 -SkipBuild`。`bin/OTONEIGO.exe`（14:58:45）未重建，ZIP 逐字节不变，两份报告已由最终脚本重跑覆盖。未执行完整 `verify.ps1`（会重建 EXE 并改变上文哈希），也未重跑 `native-wails054.ps1` 整链聚合；其报告仍是 15:00 的原样记录。未修改的判断项：`native-icon054.ps1` 的 `checks` 与既有断言重复求值；`-PortableOnly:$false` 时报告仍命名为 `portable-*`；聚合驱动在子报告缺失时不落盘且 `$closeExit` 可能掩盖原始异常；`native-wails054-fixture.py` 硬编码 schema 对象名，归档目录不设保留策略。

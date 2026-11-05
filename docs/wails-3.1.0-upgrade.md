# Wails 3.0.0 → 3.1.0 升级批次

2026-11-05。HEAD 的依赖提交 `3db7f87` 已把 go.mod 与前端 runtime 带到 3.1.0，但锁定点、CLI 安装与文档仍写着 3.0.0，许可审计门禁因此必然失败。本批按 [维护加固](maintenance-hardening.md) 设定的条件完成这次升级：先逐文件评审与本仓库原生耦合点相关的差异，再同步锁定点，最后重跑原生验收矩阵。它与 0.5.4 图标批次同批交付，见 [0.5.4 审计](audit-fixes-0.5.4.md)。

## 锁定点同步

| 位置 | 变更 |
| --- | --- |
| `go.mod` / `go.sum` | `github.com/wailsapp/wails/v3 v3.1.0`（沿用依赖提交，未再改动） |
| `frontend/package.json` / `pnpm-lock.yaml` | `@wailsio/runtime` 声明 `^3.1.0`，安装解析 3.1.0 |
| `scripts/bootstrap.ps1` | 项目内 CLI 安装改为 `cmd/wails3@v3.1.0`，本机 `.tools/bin/wails3.exe` 已重装并核对 `version` 输出 v3.1.0 |
| `scripts/audit-dependencies.ps1` | 锁定点改为 Go/运行时/CLI 三项 v3.1.0/3.1.0/v3.1.0。前端解析版本改从已跟踪的 `frontend/pnpm-lock.yaml` 读取（不再要求先跑过 `pnpm install`），并同时校验 `frontend/package.json` 的声明范围；新增读取 `.tools/bin/wails3.exe version` 并与 Go 版本比对，把 README 里“go.mod 与 CLI 必须同步”的人工约定变成门禁。缺 CLI 时报错指向 `bootstrap.ps1`。 |

`wails3 generate bindings -ts -i` 用 3.1.0 CLI 重跑后，`frontend/bindings/` 下 7 个跟踪文件逐字节不变；前端 190 项测试与 TypeScript/Vite 构建不变通过。

## 与本项目耦合点的差异评审

以两份模块缓存副本逐文件哈希比对（非 changelog 推断）：`pkg/w32` 全部非测试文件、`pkg/application/systemtray_windows.go`、`internal/webview2` 整个包、Windows 单实例实现均与 3.0.0 **逐字节相同**。因此以下依赖框架内部细节的适配层前提仍然成立，注释已改标 3.1.0：

- `desktopTrayAvailable()`：托盘仍只有 ID 1 的 message-only 窗口，`w32.GetSystrayBounds(hwnd, 1)` 语义不变（`desktop_windows.go`）。
- `desktopTaskbarLayout`：直接按 ITaskbarList3 vtable 第 9/10 槽调用进度接口，`w32.NewTaskbarList3()`/`ITaskbarList3` 定义未变（`desktop_native_windows.go`）。
- 单实例与 WebView2 用户目录、`WailsWebviewWindow` 窗口类：实现文件未变。

实际有差异且可能触及本项目的部分：

- `application_windows.go`：`TaskbarCreated` 去抖由错误的 `time.Sleep(1000)`（1000ns）改为 1 秒 `AfterFunc`，Explorer 重启时每个托盘图标只重新注册一次。属于修复，不改变我们的探测方式。
- `application.go`：新增 `earlyDispatch` 队列，`Run()` 之前投递到主线程的工作被缓存并在启动后重放，避免构造期回调进入无主循环状态。
- `pkg/application/events.go`：同一监听器改为 mailbox 串行、按发出顺序投递，并复制 hook 切片避免注册/注销原地改写竞态；3.0.0 的 `Emit` 本就是异步 goroutine，本项目的三个 Go 监听器（`close-ready`、`desktop-publish`、`desktop-ready`）都不依赖同步完成，顺序保证只增强幂等判断。
- `webview_window_windows.go` / `webview_window.go` / `menu*.go`：`SetMenu(nil)` 现在真正移除窗口菜单并撤销加速键、zoom 下限改由 `zoomFactor()` 判定、全屏/最大化期间不再强行套用尺寸约束。本项目不调用窗口菜单、SetZoom、全屏/最大化接口，故未进入这些路径。
- 依赖面：3.1.0 把 websocket 传输移出 `pkg/application` 默认编译路径，`go list -deps .` 不再包含 `coder/websocket`，镜像内上游许可文件由 13 份变为 11 份。

## 验收

`scripts/native-wails054.ps1` 是本批的一键验收驱动，它按顺序真实调用：`verify.ps1`（全量门禁）→ `package-windows.ps1 -SkipBuild`（出 ZIP 并写四文件核对报告）→ `native-icon054.ps1`（production 启动/资源/退出）→ 构建 p0debug 验收 EXE → `native-wails054-fixture.py` 复位两个 fixture → `native-audit053.ps1` → `native-desktop05-acceptance.mjs` + `native-desktop05-windowstyle.ps1` → `native-desktop05-restart.ps1`，最后汇总各脚本自己写的报告。它不重写任何断言；历史验收脚本本体（包括 `native-audit053.ps1`）保持原样，fixture 复位集中在新增的 Python helper 里，并且只归档不覆盖上一轮日志与缓存。因为 desktop05 有一条当前不可满足的断言，驱动在汇总时允许该脚本非零退出继续跑完剩余阶段，但聚合 `pass` 仍包含 `desktop05.pass`，所以报告为 `pass=false` 并在 `knownFailures` 里写出原因，不把已知失败从合取里剔除后冒充通过。

该驱动第 4 步构建的 `bin/OTONEIGO-acceptance.exe` 是 `go build` 直连（`build.ps1` 已在 `finally` 删除 `.syso`），因此**不含**本批新增的图标、版本信息与 manifest（`asInvoker`、Common Controls 6、Per-Monitor V2）。desktop05 矩阵跑在这个镜像上，其 DPI 上下文由 Wails 运行时 API 设置，与本批"清单生效"的路径不是同一条；交付镜像的资源与 Per-Monitor V2 断言由 `native-icon054.ps1` 单独覆盖，见上文。这一点使"矩阵在 3.1.0 下通过"只适用于 UI 行为，不适用于资源段效果。

- `scripts/verify.ps1` 全量：gofmt、Go 测试/vet、前端 format/lint、190 前端测试、TS/Vite、production 构建、许可审计（11 份上游许可文件，并比对 Go/runtime/CLI 三项版本）全部通过。
- `native-audit053`：通过。真实 WebView2 + CDP、SQLite schema 7/integrity ok、两条隔离记录与未知设置保留、缓存 manifest 内容/mtime 不变而 recency 落盘、空闲 8 秒零轮询、设置页进出与真实 30 秒周期计数、正常退出码 0，进程级不可达代理。本批有一次非断言类抖动：第 43 行等待设置页缓存统计的 `until` 默认 10 秒超时，报 NATIVE_CHECK_FAILED；单独原样重跑即通过，未因此修改该历史脚本，也不把它说成已验证的稳定性。
- `native-desktop05-acceptance`：单执行器、播放/暂停/seek/volume/收藏、过期 epoch 与过期曲目拒绝、浮窗位置保存、隐藏后重开复用同一窗口、歌词锁定/解锁、主窗口 reload 后恢复 paused、隐藏时时钟继续、歌词从主窗口投影、GD 结果 227.73 s 播放、通知请求 1 次且 API 接受 1 次、任务栏进度可用，均通过；最后停在第 99 行的请求阶段计数断言（见下）。
- `native-desktop05-windowstyle`：通过。main/mini/lyrics 三个窗口 topmost 与 click-through 状态正确，DPI 216。
- `native-desktop05-restart`：通过。位置恢复、隐藏浮窗后关闭主窗口、退出码 0。
- `native-icon054`：通过。打包用的 production 镜像（SHA-256 `9b5257e4ec06ed6c45fa092939e0d4950a963270beda69b0682f4fcaa84eee57`）含 8 个图标条目、清单与版本资源，Per-Monitor V2 上下文为真，WM_CLOSE 退出码 0；窗口 1200×800 只是当前主机观察值，不是验收条件。

聚合报告：`output/playwright/native-wails054-report.json`。它的 `pass` 是所有阶段的合取，包含 `desktop05.pass`，所以本次结果为 `pass=false`，并在 `knownFailures` 里写出唯一未达成项（desktop05 第 99 行请求阶段计数）；其他阶段（icon054、audit053、windowstyle、restart）均为 pass=true，production EXE 哈希 `9b5257e4ec06ed6c45fa092939e0d4950a963270beda69b0682f4fcaa84eee57`、验收 EXE 哈希 `8b14c1962bf7396ab3210b1649c44a364b18fdc4a8606d759814f8e069e74a28` 一并绑定在报告里。驱动退出码为 1，不是“跑完即通过”。

顺序说明：上述结果由 `scripts/native-wails054.ps1` 一次跑完得到（14:58–15:00），包含重新构建、重新出包与重新跑全部原生矩阵；早期手工分段执行留下的旧报告已被覆盖。

该次运行之后，同批脚本又经过一次复核修改（15:25–15:27 与 16:02–16:03，见 [0.5.4 审计](audit-fixes-0.5.4.md) 的复核补记）。因此 `output/playwright/native-wails054-report.json` 记录的是**修改前**的脚本状态，不能与当前脚本视为同一时刻。复核只重跑了不需要重建 EXE 的两段：`native-icon054.ps1` 与 `package-windows.ps1 -SkipBuild`，`bin/OTONEIGO.exe`（`9b5257e4…a84eee57`，14:58:45）与 ZIP（`db347318…78afd6`）逐字节未变，两份报告已由最终脚本重新生成；打包报告新增的 `gateScriptSHA256` 就是为此可判定而加的。整链聚合未重跑，因为它会经 `verify.ps1` 重建 EXE 并改变上述两个哈希；上面各阶段的结论仍以 15:00 那次运行为准。作为脚本改动的替代证据，复核运行了 `gofmt`、`go vet ./...`、`go test ./...`（含新增的母图复现测试）、`native-icon054.ps1` 与 `package-windows.ps1 -SkipBuild` 的真实执行，未重跑完整 `verify.ps1`（含 production 构建与许可审计）。

## 未通过项与原因（如实记录）

desktop05 矩阵第 99 行要求本轮日志里 `compatible-api > 0` 且 `gd-api == 1`。本批两种缓存状态下都不可满足：

- 冷缓存：网易云匿名取流对三个质量档全部返回 `UNAUTHORIZED`，随后 GD 成功，于是第 44 行的 `provider == netease-direct` 断言先失败。这是当前服务侧条件，不是 3.1.0 造成（本项目取流代码本批未改，且 `internal/webview2` 与 3.0.0 逐字节相同），但它意味着**本批没有在本机重新走通 live 网易云取流**。
- 热缓存：字节全部来自磁盘缓存，4 次 `cache-hit`、0 次 API 请求，第 99 行计数断言必然不成立。

因此本批采用热缓存重跑，取流来源断言以历史 0.5.x 记录为准，不声称在 3.1.0 下复验过 live 网易云取流；GD 样本与缓存/事件/窗口/托盘/任务栏/通知路径已在 3.1.0 下复验。`native-desktop05-acceptance` 第 100 行之后的自然结束观察与自动连播段因该断言提前失败而未执行，历史通过记录仍只代表其原批次。

其他未执行：Go race（CGO 关闭）、代码签名、安装器、真实出口切换与多屏/休眠；账号能力仍关闭。

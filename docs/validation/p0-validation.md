# 当前验证记录 — 0.5.4 交付 / 历史证据分期

## 播放器选择性订阅（2026-11-05）

本批仅调整 React 订阅与运行时快照引用：`usePlayerStore` 用于命令，`usePlayerSelector` 用于稳定的选择结果，兼容完整订阅保留；收藏完整 ID 集合进入不可变状态。App、歌曲卡片、收藏按钮、封面和设置/歌单读取所需字段，进度/时间/歌词在局部更新。进度保存复用运行时条目引用，但 `SaveQueue` 仍获得独立归一化副本。唯一队列、唯一音频执行器、网络源选择及 SQLite schema 7 不变。

新增 store 回归覆盖分页外收藏成员、成功/失败写入和旧快照不变，以及 seek/timeupdate 保存后的运行时引用稳定、独立持久化副本和整数毫秒。现有前端 Vitest 共 193 项、14 文件通过；Go 全量测试与 vet 通过。构建和动态渲染证据以本节后续记录为准，不将静态 SSR 输出当作动态订阅验收。

复核调整：歌曲卡片的高亮改读 `PlayerState.currentSongId`（`patch` 收到新 queue 时从当前 entry 派生一次），不再每张卡片在每次通知时遍历 `entries`；同一批次把选择器的 `isEqual` 要求写进代码注释，并让验收脚本按 `PLAYER_SELECTOR_BROWSER` → 本机 Chrome → Playwright 自带浏览器解析路径。该调整由既有 store 测试与重跑的浏览器验收覆盖，未新增测试项。

最终 `./scripts/verify.ps1` exit=0：Go 格式/全量测试/vet、前端格式/lint/193 项测试、47 方法/31 模型绑定生成、TypeScript、Vite 与 Windows production 构建、11 份上游许可及依赖排除门禁全部通过。前端 production 为 73 modules，JS 376.44 kB（gzip 115.22 kB；复核调整后同法构建为 376.52 kB / gzip 115.27 kB）；此体积不是 CPU 或内存测量。当前 EXE 已重建，原有 ZIP 仍代表此前批次，未重新打包。

`node scripts/player-selector-acceptance.mjs` 另在实际挂载的 Chrome DOM 中通过 32 项断言，报告 `output/playwright/player-selector-report.json`。测试仅在 Vite 验收转换中注入产品函数渲染计数，替换标记必须唯一且所有计数器先确认确实运行；生产代码不带计数器。确认搜索实际生成歌曲卡片、普通位置变化不渲染 App/TrackCard、seek 去抖保存和 timeupdate 持久化不渲染 OtoneiPlayer、组合选择值不变时跳过渲染、收藏对应项更新/失败保留、已挂载卡片换歌曲后回调收到新参数，以及选择器参数变化、Provider 换 store、SSR 与旧 hook 兼容。该工具需要既有 `.tools/browser` Playwright 和 Chrome（可用 `PLAYER_SELECTOR_BROWSER` 指定路径），不纳入常规无浏览器依赖的 verify；fixture 的桥接/音频是假实现，此报告只证明 React 订阅和 DOM 行为，原生媒体证明单独如下。复核调整后重跑仍为 32 项断言，渲染计数 App 3 / OtoneiPlayer 2 / TrackCard 5 / Cover 6 / FavoriteButton 14 不变（报告已覆盖为最新一次运行）。

`output/playwright/native-player-selectors-20261105-171444-report.json` 与同名前缀的 `-host-report.json`：真实 Wails WebView2、SQLite 和媒体通道，无 RPC 或媒体响应替换，进程级不可达代理；数据从既有测试 profile 复制到独立目录，源文件哈希未变。两份真实缓存 MP3 的长度/hash 在启动前检查；播放样本实际时长 267.245714 秒，验证进度、81 秒 seek、暂停/继续、下一首及换曲后仍为单一 HTMLAudioElement、收藏列表当前曲高亮与同曲爱心同步、队列/歌词焦点限制及 Escape 恢复、设置 auto→off→auto。正常关闭 exit=0，SQLite integrity=ok、存储字段隐私扫描通过，最终当前曲 1850977722、已保存位置 2688 ms。入口：`./scripts/native-player-selectors.ps1`（要求隔离缓存 fixture 与当前 `production,p0debug` 验收 EXE）。主机脚本检查 Python 检查进程的退出码，并通过保留的进程句柄清理自有验收进程。

此原生结果是已缓存媒体的功能回归，不是新一次线上网易云/GD 取流或整曲自然结束证明；未测整机 CPU、内存/GC、扬声器听感或长期压力。原有 P0 未测试条件保持。初次验收脚本的页面参数/相对定位问题已修正，早期失败报告保留在 output，不作为通过证据；本节只引用最终重跑报告。没有构建或运行安装器，没有发布、提交或推送。原生矩阵未在复核调整后重跑，它仍代表调整前实现的同一用户可见行为；`currentSongId` 改动由上述 store 测试与浏览器断言覆盖，不宣称原生复验。

## 2026-11-05 EXE 图标、Windows 资源与 Wails 3.1.0（已出 ZIP）

范围、成因与遗留项见 [0.5.4 审计](../audits/audit-fixes-0.5.4.md) 与 [Wails 3.1.0 升级记录](../audits/wails-3.1.0-upgrade.md)；不改写下列历史产物的版本或哈希。

- 图标批次后 `scripts/verify.ps1` 一度只在最后的许可审计失败：`audit-dependencies.ps1` 要求 Wails v3.0.0，而 HEAD 依赖提交已使 go.mod 为 v3.1.0。本批按维护加固章节预设的升级条件完成它（逐文件差异评审 → 锁定点与 CLI 同步 → 原生矩阵重跑）后，全量门禁通过，许可审计记录 11 份上游许可文件。
- 资源核对：`bin/OTONEIGO.exe` 从“无 `.rsrc` 段”变为 8 个 RT_ICON + RT_GROUP_ICON + RT_VERSION(0409) + RT_MANIFEST，由 `EnumResourceNames` 枚举报出数量并与 `Get-IconSizePlan` 比对；`FileVersionInfo` 的 ProductVersion/FileVersion 与 0.5.4 完全相等，`ExtractAssociatedIcon` 在本机取到 32px（像素值只记录，门禁仅断言能取出图标，不再单独作为存在性证据）。资源段使 EXE 字节 +26,112。
- 真实原生：`scripts/native-icon054.ps1` 在隔离数据目录与进程级不可达代理下启动 production EXE，唯一 `WailsWebviewWindow` 显示并初始化，Per-Monitor V2 上下文、资源枚举（icon=8/groupIcon=1/version=1/manifest=1）、版本串完全相等与退出码 0 均为断言；DPI 216、窗口 1200×800 只是当前主机观察值记录（`output/packaging/native-icon-054-report.json`，含逐项 checks）。
- 升级重跑：`scripts/native-wails054.ps1` 一次跑完 verify → 出包 → icon054 → 验收 EXE 构建 → fixture 复位 → audit053 → desktop05 acceptance/windowstyle → restart，聚合报告 `output/playwright/native-wails054-report.json` 为 `pass=false` 并在 `knownFailures` 写明唯一未达成项；icon054、audit053、windowstyle、restart 均 pass=true。`native-desktop05-acceptance` 的单执行器、控件、epoch、浮窗、通知、任务栏与 GD 播放段通过，但第 99 行 live 请求阶段计数断言不可满足：热缓存下 4 次 cache-hit/0 次 API；冷缓存下网易云匿名取流三档均返回 `UNAUTHORIZED` 后回退 GD，使第 44 行来源断言先失败。因此本批未在本机复验 live 网易云取流，自然结束/自动连播尾段也未执行。audit053 在本批出现过一次第 43 行 `until` 10 秒超时的非断言抖动，单独原样重跑通过。
- 交付：`bin/OTONEIGO-0.5.4-windows-x64.zip` 19,530,458 字节，SHA-256 `db34731819d359b35d5955371e55a275b23bcc8bead47ad6e7345dd67278afd6`；包内 `OTONEIGO.exe` 逐字节等于最后生产构建（SHA-256 `9b5257e4ec06ed6c45fa092939e0d4950a963270beda69b0682f4fcaa84eee57`），逐项包内/输入哈希由打包脚本写入 `output/packaging/portable-0.5.4-final-report.json`；随包通知已重生成并记录 v3.1.0。
- 图标资产与 `reference/` 解耦已核实：`frontend/public/logo.svg` 哈希等于 provenance 记录的 OTONEI 原件，构建/打包只读产品内文件；母图与矢量副本哈希已登记到 `docs/delivery/source-provenance.json`。
- 仍未运行：Go race（CGO 关闭）、便携覆盖更新与旧版本启动、安装器/签名/远程发布、多屏或休眠、真实出口切换；账号能力仍关闭。
- 复核（16:02–16:12）：审查后修正 `-SkipBuild` 的 README 版号门禁缺口、`Add-Type` 的 C# 版本依赖，补上母图复现测试与报告 `gateScriptSHA256`，并把上文"同一时刻"改为真实时间线。只重跑了不重建 EXE 的两项（`native-icon054.ps1`、`package-windows.ps1 -SkipBuild`），EXE `9b5257e4…` 与 ZIP `db347318…` 逐字节未变；整链聚合与完整 `verify.ps1` 未重跑，详见 [0.5.4 审计](../audits/audit-fixes-0.5.4.md) 复核补记。

## 2026-11-03 维护加固验证（源码批次，未重打 ZIP）

共享目录锁、panic 记录/隔离和补丁依赖的评估与实现范围见[维护加固](../architecture/maintenance-hardening.md)。不改写下列历史产物的版本或哈希。

- 完整 `scripts/verify.ps1` 通过：Go 测试/vet、Biome format 和严格 lint、190 个前端测试、TypeScript/Vite、Windows production 构建、13 份 Go 上游许可记录与前端依赖审计。Wails Go/runtime 保持 3.0.0，新增审计断言锁定这两个版本。
- 独立复查发现普通 HTTP abort 被误判 fatal、音频 handler panic 在 pin 清理前可能死锁两项；修复后复查无剩余实质发现。相关回归分别保留 `http.ErrAbortHandler` 语义、使用持锁 Entitlement panic 子进程确认退出码 2 和报告；音频异常/重试等关键回归重复五次通过。
- 本地子进程测试证明 fatal/close-gate/Wails handler 写出脱敏报告并退出 2；资源/地区/音频隔离测试证明等待者和 worker 槽位可释放、后续调用可重试。不是在真实 WebView2 内主动触发 panic，也不证明所有第三方/native 崩溃都可捕获。
- 新隔离数据目录中的真实 Wails/WebView2 smoke 通过：设置写读、本地收藏写读、打开 mini/lyrics 两浮窗、隐藏歌词、保留迷你窗口时正常退出全部窗口，进程退出 0，没有崩溃报告。关闭后直接检查 SQLite schema 7、integrity_check=ok、提交设置与收藏记录保留。报告为忽略目录下 `output/hardening-native-report.json`。测试只用进程级无效代理制造离线，不截图、不修改系统网络，不验证实际播放/任务栏通知或海外网络。
- `scripts/native-harness.mjs` 的播放器定位改为稳定的 `.audio-player` 类与 aria 标签，不再依赖元素标签名；其余验收脚本的 `footer` 选择器因播放器保留 footer 语义而继续有效。
- 文档本地链接 118 处通过，`go mod verify` 通过，参考路径同步后现存 36 份原文件哈希匹配。四份归档 public 原件缺失，但产品副本匹配历史哈希；详见[许可来源限制](../delivery/licenses.md)。不能声称本次完整重验了 40 份原件。

本批不重新运行完整 `native-desktop05` / `native-audit053` 矩阵，不更新旧 ZIP，不制作/运行安装器；Wails 升级和其他延期能力保持独立验收范围。

当前已形成可运行的匿名 Windows 客户端：网易云搜索/歌词/封面、同一音频执行器、海外检测与有限兼容、本地收藏/历史/设置、SQLite 队列、可选 GD 网易云备用及免安装 ZIP。0.4.0 已补整库检索/分页、关键词历史、封面/歌词磁盘缓存及新版耐久分项验证；0.5.0 再补桌面歌词、迷你播放器、任务栏和可选通知，最新结果见本文最后的 0.5.0 段落。按出口地区预选取流策略已另做真实 TH 原生验证。账号、云端歌单/同步、签名和更新器没有交付。0.3.0 的托盘/WinRT/格式等结果保留各自原批次含义。P0 条件矩阵尚有未测试项，不能据有限样本宣布全部 P0 或 P1/P2 验收完成。

[2026-10-31 原始验证记录](p0-validation-2026-10-31.md)保留当时失败及测试桥接过程。下列正式服务路径证据取代其中的“尚未集成”现状描述，不改写历史实验。

## 2026-11-01 检查（当日版本的历史证据）

| 检查 | 证据和边界 |
| --- | --- |
| 构建与自动检查 | scripts/verify.ps1：Go tests/vet、66 前端测试、TS、Vite、15 个服务方法绑定、Windows production 构建与依赖门禁通过；13 份上游许可记录 |
| 独立复查 | 首次发现媒体元数据等待期间策略取消缺口，已修复并回归；最终关闭握手、完整时长验证及诊断复查未发现新的实质性缺陷 |
| 隔离与来源 | 三个参考目录、工具、产物和数据库均 Git 忽略；40 个来源文件 SHA-256 与记录匹配；没有修改参考副本、暂存、提交或推送 |
| 地区检测 | 当前 Go 检测路径实际观测 TH/overseas；不证明网易云和媒体使用相同出口，未改变系统网络配置 |
| 正式后端取流 | 同 ID 33894312 匿名 standard：兼容关闭且 GD 关闭时 EMPTY_URL；auto 时普通失败后精确播放端点 header 返回 full；不经接口拦截或测试桥接 |
| GD 独立能力 | region off + GD 用户开关打开：正式 normal→GD 路径返回同 ID 的 MP3/128000 bit/s，原生媒体时长 267.245714 秒，完整自然结束并自动下一项，仍为同一个 HTMLAudioElement；测试静音，不证明扬声器听感 |
| 录音一致性样本 | GD 与官方兼容音频均为 4276601 字节，完整流 SHA-256 均为 4a16a7c0d10eed3468361709295aaffb088b46836f3fe542d676dc32c02b88b0；仅在内存中哈希，未保存音频或地址。这证明该样本逐字节一致，不外推其他歌曲 |
| 音质请求 | 同一样本请求 standard/higher/exhigh 分别返回 provider 标记 128/192/320 kbps MP3；lossless/hires 均实际降到 exhigh/320，不显示成无损。192/320 是服务端码率字段，未逐首完整测量；128 样本另有 GD 帧探测和原生完整播放 |
| 启动/取消 | 同一个总 deadline 约束解析与媒体启动；所有 full 及 GD 检查同一个元素实际元数据时长；策略/网络变化可取消待启动音频，已经播放的音频保持；受控回归通过 |
| SQLite | schema 2 迁移回滚/保留队列、CAS、并发、库限制/历史保留、设置 capability 门与错误脱敏均有实际 SQLite 测试；原生最终重启记录另附本节下方 |
| 历史安装测试（已暂停） | 隔离 /NOINTEGRATION 新装和覆盖更新 exit 0、四文件 hash 匹配；运行中阻止更新 exit 2；卸载 exit 0、未知哨兵保留。没有执行真实开始菜单/注册表集成或签名信誉验收 |

原生工具 `scripts/native-client-acceptance.mjs` 只观察真实绑定结果，包装 play 用于静音和对象计数，不替换 API、音频或媒体时钟。只保存 metadata 报告和截图至忽略的 output/playwright。初次工具在下一项达到历史阈值前过早暂停，历史断言超时；这是测试顺序错误，已调整为先等待实际收听阈值再暂停，保留 first-pass 报告，不把失败记录当作通过。

## 能力决策与未测试条件

- P0-A 当前主机基础窗口/绑定/存储与构建通过；其他 Windows/DPI 组合未测试。
- P0-B 匿名目录和受限兼容取流可用；账号/QR/会话、普通或会员账号均未实现或未测试，明确延期并禁用账号 capability。
- P0-C 当前泰国样本允许使用精确端点 header 策略；CDN 改写/no-cache 未实现且不启用。中国大陆 Exit Node、实际 VPN/Tailscale 切换与分流对照未测试，不做地域根因定论或通用解锁保证。
- P0-D 开放已独立验证的 CBR MP3 GD 能力，用户设置默认关闭；只用 source=netease，同一网易云身份。GD 返回其他格式或不明完整性时仍拒绝使用；网易云权限结果在用户开启 GD 后独立尝试备用，不保证 VIP 权益。
- P0-E 当前样本原生完整播放与单执行器通过；其他歌曲、无损/Hi-Res、扬声器听审未测试。

这些延期能力不计为通过；匿名基础版本可供本机试用，不宣称整个长期方案已完成。GD 限速、取消、缓存、预算和失败分支中的 fixture 测试只证明边界逻辑，不能算真实服务限流/故障的现场复现。

## 复现入口

```powershell
./scripts/verify.ps1
./scripts/package-windows.ps1 -SkipBuild
```

真实原生测试需显式 production,p0debug,devtools 验收构建、隔离 OTONEIGO_DATA_DIR 和 loopback CDP。正式生产包只使用 production，不编译 CDP 参数；不把调试 EXE 装包。正常关闭采用最多 3 秒的 hydrate/pause/flush 确认；失败或超时只保证已提交的 SQLite 事务，强杀不保证 JS 待提交状态。

## 最终原生结果

最终 production+p0debug 验收二进制重复通过：真实正式服务调用，整曲自然结束 267.245714 秒、约 268.326 秒墙钟、自动下一条独立队列项、单 Audio；收藏/历史界面及实际 SQLite 三条历史通过。900×640 原生最小窗口的 CSS 内容区域为 888×605，播放器完整可见、无横向溢出。报告为 output/playwright/native-production-report.json；该次媒体验收没有任何绑定或音频拦截。

同一 schema 2 数据目录在仅新进程使用不可达 HTTP(S) 代理后重启：两条队列、124000 ms 位置、音量 0.37、一条收藏和三条历史均恢复，启动暂停。报告 native-restart-report.json；这是进程级网络故障模拟，非实际切换 VPN。

正常关窗另测受控延迟：真实 SaveQueue 请求延迟 1000 ms 后放行，Windows WM_CLOSE 触发一次 prepare 和一次 acknowledgement，1194 ms 正常退出。报告 native-close-report.json。最初脚本额外导入 /wails/runtime.js 替换了测试页事件分发，导致 3 秒兜底；已修正为重载页面并通过 PID 限定的 Windows 关闭消息触发，不需改应用源码。该受控延迟只验保存关闭逻辑，不冒充真实网络或磁盘慢写入现场。

最终打包复验使用含空格目录 output/install final smoke：新装/更新/卸载均 exit 0，未标记且含未知 EXE 的目录拒绝 exit 4；四个有效载荷文件与 ZIP 一致，未知哨兵及操作期间曲库 SHA-256 保持。报告 output/packaging/final-smoke.json。系统集成仍未测。

最后以明确 PID 和二进制路径核验后强杀隔离宿主，重开 schema 2 库仍恢复两项队列、124000 ms、音量 0.37、一条收藏和三条历史且暂停；仅证明已提交数据，报告 native-crash-recovery-report.json。随后重做 Windows 关窗受控延迟测试，一次 prepare/ack，1173 ms 正常退出，测试宿主均已关闭。首次 1194 ms 与本次 1173 ms 是两次独立通过结果；当前 native-close-report.json 保存最后一次。

## OTONEI 界面继承修正验收

按用户要求，原来的独立简化技术界面已替换为 OTONEI 原版组件标记、布局、主题和样式的适配。复用侧栏/窄屏导航、顶部搜索、歌曲卡片、底部播放器、封面和展开歌词，在原壳内接本地库/设置及队列抽屉。当前来源清单覆盖 40 个参考文件，42 个已验证后端/状态/音频行为文件哈希保持一致；不把原 Firebase、下载或其他曲库逻辑移入新程序。

本轮 Go 测试/vet、66 项前端测试、类型检查、生成绑定、Windows production 构建与 Go/npm 运行时许可门禁通过。首次原生 UI 验收使用真实搜索和媒体（无接口/音频拦截），通过播放、81 秒实际媒体 seek、下一首、单 Audio、歌词展开/关闭、队列、本地收藏/历史/设置与字体加载；900×640 窗口的 888×605 内容区域搜索/播放器可见且无横向溢出。报告 otonei-ui-report.json。

独立复查修复两项问题：弹层键盘焦点越界/关闭丢失焦点，以及 500ms 相邻歌词误取前一句译文。当前原生 Tab/Shift+Tab 通过歌词+底部播放器范围、队列独占范围、背景 inert、先关顶层队列和恢复触发按钮；报告 otonei-keyboard-report.json。旧核心 native-client 与 controlled-media 验收脚本也已同步新控件选择器；受控媒体不作真实网易云证明。

海外默认机制没有因 UI 改动而改变：auto 仍正常取流优先；失败且非 CN/未知时允许精确 player endpoint header 尝试；GD 默认关闭，需用户启用才回退；CDN 改写与 no-cache 未实现。界面修正不新增账号或授权能力。

最终 UI 构建完整重跑 scripts/native-client-acceptance.mjs 通过：正式 API 与音频均未拦截，GD MP3 自然结束于 267.245714 秒（约 268.634 秒墙钟），自动下一条、同一个 Audio、收藏/历史/设置与新布局通过。原生 quick UI 与 keyboard 报告也重验通过。更新后的 controlled-media 脚本另以 30 秒受控 WAV 验证 10 秒定位恢复、真实 SQLite 提交、单执行器及迟到取消，明确不是在线音源证明。最终关窗在 1000ms 请求延迟后 1301ms 收到一次 prepare/ack 正常退出。

本轮未重新运行上一阶段的中国出口、账号或物理 VPN 对照；这些仍未测试。正式安装包只含 production 构建及许可，新的前端字形和字体资源随内嵌资产保留；原三个参考目录继续忽略且只读，没有暂存/提交。

## 免安装交付调整

用户明确暂停安装包工作，当前只交付免安装 ZIP，打包默认 PortableOnly=true。此前 UI 安装复验误写默认目录，安装/升级/卸载验收没有完成；重试被自动审批拒绝后已停止。误创建的目录经路径、创建时间及六个文件完整哈希核实后精确清理，没有执行卸载器，也未触及用户曲库。清理记录 test-install-cleanup.json，部分测试/拒绝记录 otonei-ui-smoke.json。

最终 ZIP 以四文件白名单核对：程序、许可、说明均与生产输入一致；不包含安装器、NSIS、数据库、日志或调试版本。当前产物以 portable-final-report.json 和 portable.sha256 为准，旧安装包输出已撤下。免安装不改变已有默认用户数据路径。

## GD 开启后的权限分支回退修复

用户报告《又三郎》ID 1850977722 在 GD 已开启时显示权限错误。修复前真实后端链 normal-api EMPTY_URL → compatible-api UNAUTHORIZED 后结束，GD resolver/HTTP 请求次数均为 0。已拆分地域兼容和 GD 的资格判断；不改 UI、认证、GD 媒体校验或存储。

同一网络、同一 ID、auto、GD=true 的修复后真实请求：正常空地址 → 兼容未授权 → GET GD source=netease、id=1850977722、br=128 恰好一次 → HTTP 200。GD 返回通过现有 MP3/元数据校验的 full 候选，实际码率 128000 bit/s、时长 227760 ms、provider=gd-netease。只保存脱敏阶段与计数到 output/playwright/gd-permission-fallback-live.jsonl，不保存媒体地址；没有截图或原生整曲播放，本次按用户要求验后端调用。

回归用例先在旧实现看到六组启用 GD 的 normal/compatible 权限/试听/登录要求场景调用次数为 0，再验证修复后的开关矩阵、失败结果保留、取消/限流/不可信响应不回退及无效备用不被接受。UI 未修改，交付仍只更新免安装 ZIP。

该修复最终 Go 全套测试/vet、66 前端测试、TS/Vite、Windows production 构建、许可审计及免安装四文件核对通过；独立复查发现并修复了兼容 unknown+URL 被意外丢弃的回归，保留原有行为且不累计失败冷却。未修改 UI 源码，也未执行截图、安装或安装测试。

## 默认音质调整（代码与 API 验证）

新建/无设置配置的默认质量从 standard 改为 exhigh（320 kbps）；已有 standard/higher/exhigh/lossless/hires 保存值在重开数据库后完整保留。当前真实后端对《又三郎》1850977722 使用新默认：普通空地址 → 兼容未授权 → GD source=netease、br=320 恰好一次，HTTP 200，返回通过原有校验的 full MP3 候选，实际 320000 bit/s、227760 ms。仅验证 API/音频头，不作整曲原生播放结论；脱敏记录 output/playwright/default-320-live.jsonl。

现有低档完整流立即使用的逻辑继续保留；没有新增空地址后的逐档质量重试。账号收藏模式只是评估，本次未开放登录或远程写入，也未修改现有本地收藏数据。

## 网易云最高音质匿名实测

截图歌曲 1833299189《春泥棒》/ヨルシカ，歌曲详情时长 290266 ms；另测 33894312《情非得已 (童声版)》/群星。两首均以普通/地域 header 两种路径请求 standard/higher/exhigh/lossless/hires/jyeffect/sky/jymaster，不调用 GD、UNM 或认证接口。普通路径均未获得地址。兼容路径 standard/higher/exhigh 得到 128/192/320 kbps MP3；音频 Range 请求均返回 206，64 KiB 内解析连续 MPEG-1 Layer III 帧确认对应实际码率（320 档至少 62 帧）。高于 exhigh 的成功请求实际仍是 exhigh/MP3/320000 bit/s；sky 未授权或不可用。

另按 go-musicfox SDK 默认 encodeType=flac，对五个高档位逐一比较两种路径，成功结果仍为 exhigh MP3 320000 bit/s，未观察到 FLAC。当前匿名样本最高实测是 320 kbps MP3，不能推断网易云或有会员账号的全局上限。没有截图或整曲播放，音频仅有界读取并丢弃，报告没有媒体 URL：output/playwright/netease-max-quality.json。探针已归入忽略路径 .tools/musicfox-diagnosis/max-quality-live，复现命令用项目 toolchain 后 go run ./.tools/musicfox-diagnosis/max-quality-live。

界面音质选项现在显式标出 128/192/320 kbps；播放器显示返回的实际码率，不用首选值冒充实际质量。旧日志截图 requestedQuality=standard 对应 128 kbps 请求，不表示网易云源只能播放 128。新配置默认为 exhigh/320，已有保存选择保留。

## 320上限、会员门控与自动低档回退验收

按用户新规则，标准/较高档从手动首选移除，旧首选按后端资格归一为320；Go只在确认LoggedIn&&Member时允许高级档。实际当前账号入口保持关闭，因此生产按匿名策略。新服务GetPlaybackQualityPolicy返回匿名only exhigh/maxBitrate320000；参数伪造不能使SetSettings接收低档或会员档。

Go回归覆盖320→192→128、会员GD br999/740/320/192/128去重回退、GD六秒间隔、正常质量缺失不误触熔断、网络熔断保留、非会员超限结果拒绝、阶段超时保留权限原因、调用者取消/总超时优先和短/长调用者共享请求。真实调用同首1850977722的六个正常/兼容请求后，GD br320一次HTTP200返回320000bit/s/227760ms，报告quality-policy-live.jsonl。

原生Wails验收无截图/无网络拦截：旧hires设置归一为exhigh，GD开关和音量保留；设置仅一个320选项；直接调用128/192及伪造会员参数均被后端拒绝，播放RPC传hires也被限制为exhigh。真实GD320播放启动，原生时长227.731917秒，播放器显示320kbps且仅一个Audio，报告quality-policy-native-report.json。只是启动验证，不是本轮整曲播放。既有本地歌单/收藏/历史/四项队列在进程离线代理下重启读取通过，正常关闭确认1069ms通过。高级会员认证/原生格式仍未测，GD FLAC未作已验证播放支持。

完整Go测试/vet、101前端测试、TS/Vite、Windows production和许可审计通过；独立复查发现的四个回退边界已修复。依赖目录异常共享链接导致一次npm审计失败，使用锁文件npm ci --ignore-scripts修复后全套重跑通过，未改锁定依赖版本。

## 0.3.0 实现与分项验收（2026-11-02）

本节描述当前源码，前述 schema 2、测试数量、原生 MP3 与打包结论均属于各自日期/二进制，不自动成为本批通过证据。当前 schema 4 迁移保留本工程数据；收藏 added_order 最近加入优先，历史复合游标分页且取消自动 5000 条裁剪，本地歌单已实现，其他业务库配额仍保留。账号与同步 capability 仍禁用。

音频缓存默认 enabled=true、20 GiB，在每个合资格质量档先检查已验证字节，再请求来源；同进程媒体 loopback 通道支持渐进播放，完整长度、格式/时长及 SHA-256 通过后才提交缓存。活动 pin、清理延期、LRU、空间检查、损坏/中断清理和会员账号 scope 都是实现边界。对应 Go/frontend 自动化合同包括迁移回滚、收藏顺序、历史分页、缓存 Range/完整性/取消/离线/清理及单播放器状态；它们不是原生或真实会员网络证明。

| 本批项目 | 证据状态与需补记录 |
| --- | --- |
| schema 4 / 历史 / 收藏 / 本地歌单 | 原生报告确认 schema 3 数据保留、历史超过 5000 条、游标分页再取 200 条及新收藏置顶；进程级离线重启恢复本地库并保持暂停通过，最终 Go/frontend/Windows 构建通过 |
| MP3 缓存与渐进媒体通道 | 真实 CDN、无 API/媒体拦截的原生渐进播放与完整提交通过；缓存热播 152 ms、无新增普通/兼容/GD 请求、81 秒 Range seek、单 Audio；进程级离线重启缓存启动 176 ms、音频 API 增量零、91 秒 seek 通过 |
| FLAC/WAV | 自生成各 3 秒音频在真实 WebView2 同一执行器自然结束、各 13 次 timeupdate、无媒体错误；resolver/媒体被受控拦截，不证明真实会员音源或缓存通道，也未证明 seek |
| 托盘 / 单播放器 / SMTC | closeToTray 默认 false；原生隐藏/恢复后连续播放、单 Audio 通过。最终真实 WinRT 会话匹配，Pause→Paused、Play→Playing、SkipNext→不同队列项 Playing，始终单 Audio；此前清空 src 导致会话丢失已修复 |
| ZIP | 0.3.0 portable ZIP 已生成；生产标签、四文件白名单及逐文件 SHA-256 核对通过，不进行安装器构建或 smoke |
| VPN / Tailscale / 中国出口 | 用户已暂缓真实切换和 A/B；未测试，不变更代理、DNS 或 Hosts，不作地域因果结论 |

最终原生与构建结果必须记录所用二进制和报告路径；未完成的格子保持 pending/untested，不由本次文档同步宣告通过。

### 0.3.0 本批实际证据

`output/playwright/cache-online-native-report.json` 的 pass=true：使用真实 CDN，不拦截 API 或媒体。仅测试构建对媒体读取施加每 32 KiB 延迟 80 ms，以观测未到 EOF 的渐进播放，不能将该条件下首播 4222 ms 当生产性能基准。首次 playing 时 usedBytes=360448、cachedTracks=0、activeDownloads=1，证明完整缓存尚未提交即开始播放；最终完整 9111405 字节提交为一条缓存。热播 152 ms，普通/兼容/GD 请求增量均为零；81 秒 Range seek、唯一 Audio、历史超过 5000 条/游标后续 200 条、收藏最新优先及托盘隐藏恢复连续播放均通过。该报告不是离线重启证据。

`output/playwright/native-format-report.json` 的 passed=true：自生成 WAV（288044 字节）与 FLAC（288438 字节）各 3 秒，经真实 WebView2 解码，同一 Audio 分别自然结束于 3 秒、各 13 次 timeupdate、无媒体错误，队列已恢复。测试静音且受控拦截 resolver 和媒体；它只证明该宿主的格式解码/自然结束，不证明网易云高级权益、真实会员播放、缓存传输或扬声器听感。

`verify.ps1` 已把 Go 检查扩大为 `go test ./...`，包括根目录关闭/托盘回归。暂停/缓冲边界修复并独立复查后，最终完整 Go tests/vet、132 个前端测试、TS/Vite、Windows production 与 13 份上游许可审计全部通过。一次包管理器链接布局重建使 npm 依赖审计报 extraneous，使用未更改的锁文件 `npm ci --ignore-scripts` 恢复后整套重跑通过，不把环境失败计为成功。

`output/playwright/native-smtc-final-report.json` 的 pass=true：实际 Windows.Media.Control WinRT 会话按标题/状态匹配，TryPauseAsync 被接受并观察到 Paused，TryPlayAsync 后观察到 Playing，TrySkipNextAsync 切到不同 queueEntryId 后仍 Playing，保持同一个 Audio。此轮仍为真实 CDN，无网络拦截；控制前 cachedTracks=0、activeDownloads=1、usedBytes=360448，原生暂停时 cachedTracks=0、activeDownloads=0、usedBytes=1081344，证明在未提交完整缓存时也保持可控会话。此前清空 src 导致丢失 SMTC 的失败已修复，不抹去失败历史。

普通已加载媒体的暂停保留源与元数据以维持 SMTC；取消待启动、选曲/移除、退出/销毁仍做完整源复位，继续播放重新解析合资格源。暂停不保证立即解除浏览器的文件响应 pin，也不保证清理后立即 unlink；完整释放后的延期删除才是边界。

`output/playwright/cache-offline-native-report.json` 的 pass=true：仅新进程使用不可达代理模拟离线，无网络拦截，已持久化本地库（历史超过 5000 条）恢复且暂停；缓存启动 176 ms、音频 API 请求增量零、91 秒 Range seek、单 Audio 通过。关闭缓存和手动清理均不打断已播放媒体，clear 返回 removedTracks=0、retainedActiveTracks=1；报告的最终零字节/零缓存结果包含完整源复位步骤，不能解释成普通暂停立即删除。它是进程级故障模拟，不是物理断网或 VPN/Tailscale 切换。

退出/托盘竞态最终原生复验在 WM_CLOSE 后继续阻塞设置写入 1000 ms，一次 prepare/ack、2362 ms 正常退出，未因迟到的 closeToTray 设置变更而隐藏已冻结的窗口。报告 `output/playwright/native-close-tray-race-report.json`，隔离测试进程已退出。

最终产物 `bin/OTONEIGO-0.3.0-windows-x64.zip`（19304504 字节）仅包含生产 EXE、LICENSE、THIRD_PARTY_NOTICES.txt、README.txt，逐文件与生产输入 SHA-256 一致；EXE 严格使用 production tag，不带 CDP 或媒体限速功能。ZIP SHA-256：`8ccc3a7910ff64fda9de5f59b9588f74c3c6870e82f23f7bed3d64615ce56d4f`，报告 `output/packaging/portable-0.3-final-report.json`。原生媒体验收使用同一源码/前端资产的独立 production,p0debug,devtools 二进制，不能将其标签描述成正式 EXE；正式 ZIP 未包含验收二进制。未制作或运行安装器。

## 0.4.0 本批交付与最新地区策略（2026-11-02）

本节覆盖本批增量，不改写以上历史条件。登录/会话/DPAPI、云端歌单/收藏补齐、会员音源实测继续暂停；安装器和真实 VPN/Tailscale/分流/中国出口对照继续暂停。下一批桌面歌词、迷你播放器、通知等见 [实施路线](../planning/roadmap.md)，不能因本批通过而计作完成。

### 本地数据与资源

隔离 profile `output/native-library-04` 从明确构造的本工程 schema 4 开始，6010 条收藏、6010 条历史、两条队列、一个歌单及旧设置在实际宿主中升级为 schema 5。收藏/历史查询在 SQLite 对 Unicode 小写派生文本执行字面子串筛选，UI 每页最多 200 条；首屏之外的中文、日文、专辑及字面 `_ %` 筛选（实际夹具包含相邻 `_%`）通过。爱心状态使用完整 ID 成员集，不依赖当前 metadata 页。关键词最近记录、删除/清空与重启保留通过，不混为播放历史。这里验证 6010 条规模，不等于无限数据量或完整性能压测。

`output/playwright/library04-online-report.json`：真实 Wails RPC 和 CDN，没有 API/媒体响应拦截或截图。MP3 首次播放 3144 ms（测试构建每 32 KiB 人为延迟 80 ms），首播时 cachedTracks=0、activeDownloads=1；随后完整 9111405 字节缓存提交，实际 81 秒 seek、单 Audio 通过。该延迟条件不能当生产性能基准，也不代替整曲自然结束证明。

同一真实歌曲原封面是 6,050,435 字节 PNG，超过单图 4 MiB 上限，初次正确退回原图联网显示，但不足以完成离线缓存目标。实现改为仅在可信原地址的出站请求内部派生固定 `param=1024y1024`，取得 1,863,113 字节、1024×1024 PNG；响应虽标 `image/jpg`，仍按实际字节与完整解码验证。原始 CoverRef/缓存身份未改，不开放调用者 query。有效歌词摘要为 3513 字节；报告只保存长度和 SHA-256，不保存歌词正文或媒体地址。直接清理缓存后诱发真实图片 404，再次解析同一 hash 路径、React 图像节点重建且实际解码通过；这是受控淘汰故障，不是现场网络故障。关闭/清理资源缓存不打断当前音频或删除曲库，重启前重新缓存封面/歌词。

`output/playwright/library04-offline-report.json`：仅新进程使用不可达 HTTP(S) 代理，无响应拦截。6010 收藏、超过 5000 条后的历史、歌单、关键词、音量与音质设置恢复且暂停；封面/歌词校验、实际图片解码合计 308 ms，缓存音频启动 184 ms，普通/兼容/GD 播放诊断请求增量为零，实际 seek 与唯一 Audio 通过，124000 ms 位置已提交。它是进程级网络故障模拟，不是物理断网、真实出口切换，也不证明所有资源都有离线缓存。

### 最新版强杀恢复与便携覆盖

`library04-precrash-state.json` 与 `library04-recovery-report.json`：核对测试 PID/二进制路径后，在真实音频仅下载 163840 字节、cachedTracks=0、activeDownloads=1 时强制结束宿主。立即离线重开，已提交 revision 44、3000 ms、队列/收藏/关键词等恢复；未完成音频清理为零字节/零已完成缓存，未冒充完整歌曲；已完成的封面/歌词仍可读取。`library04-db-after-crash.json` 确认 schema 5、SQLite integrity_check=ok，并扫描已存快照无 URL/token/Cookie/runtime cover 字段。这里的禁止 URL 字段不排除协议允许的原始公开 CoverRef。只保证已提交状态，不保证强杀时 JS 中待提交操作。

`output/packaging/portable-update-0.4-report.json`：在独立目录先解压旧 payload，再替换为 0.4 四文件；旧生产版本未在该阶段启动，不能称为旧版本运行全链路验收。替换期间数据库 SHA-256 不变；解压后的 production EXE 在同一隔离 profile、进程级离线代理下创建真实主窗口，显示初始化后由 PID/窗口类型和标题限定的 WM_CLOSE 正常退出，exit=0。读取确认 6010 收藏、6012 历史、一歌单、一关键词、两队列、3000 ms、schema 5/integrity 和快照隐私仍有效。schema 4→5 的真实迁移由前面的独立夹具运行证明。早期 smoke 在隐藏窗口初始化前关闭得到 exit=1，或因未显示而无法确认可见窗口；改进测试为稳定的真实主窗口显示初始化，没有据此修改应用退出代码或放宽 exit=0。

### 按 IP 预选网易云取流，而非海外普通预请求

用户纠正后：缓存第一；auto+CN 仅普通取流，auto+overseas 和 always 从第一次、每个音质尝试起直接兼容；off 仅普通，unknown/检测失败保留普通后合资格兼容兜底。GD 不加 Next 头，只在选中的网易云路径失败后按独立开关和限速回退。完整网易云结果立即用，不为追求码率调用 GD。兼容冷却不得退回普通；缓存命中不清除网络冷却，旧代际响应不更新新代际健康状态。

`output/playwright/native-egress04-report.json` 在实际 TH/overseas 分类、auto/GD=true 下无响应拦截地验证新顺序：

| 歌曲 ID | 普通取流次数 | 兼容取流次数 | GD 次数 | 原生结果 |
| --- | --- | --- | --- | --- |
| 33894312 | 0 | 1 | 0 | 网易云 region-header、full/MP3/320000 bit/s；实际时长 267.245714 秒，启动 1435 ms |
| 1850977722 | 0 | 3 | 1 | 兼容有限音质链失败后 GD full；实际时长 227.731917 秒，启动 2209 ms |

这是启动/时长及实际请求计数验证，不是本批两首整曲自然结束。计数针对取流阶段，不宣称搜索/资料/歌词等所有普通网易云请求为零。始终同一个 Audio；测试临时队列及设置已恢复。CN/off/always/unknown、取消/代际/冷却与缓存分支另由自动化覆盖，不将其冒充真实中国出口对照。资源/检索/离线/强杀报告先于本次路由更正，未替代此独立网络验证；它们仍分别证明对应非路由行为。

### 复查、构建与产物

独立复查修复了损坏旧记录迁移漏检、UTF-8 查询契约不一致、不完整图片入缓存、封面抢占歌词并发、已淘汰封面重试、历史清空翻页竞态，以及缓存命中误清兼容冷却。最终 `scripts/verify.ps1` 的完整 Go tests/vet、149 前端测试、TS/Vite、production Windows 构建及依赖许可检查通过，13 份 Go 根许可及前端/字体/图标通知保留。CGO=0 环境未运行 Go race detector；不把重复测试当 race 证明。

正式 `bin/OTONEIGO-0.4.0-windows-x64.zip` 为 19385471 字节，仅包含 OTONEIGO.exe、LICENSE、THIRD_PARTY_NOTICES.txt、README.txt；逐文件 SHA-256 与生产输入一致，准确 production tag，无 CDP/测试限速。ZIP SHA-256：`dda777ad145e85d53672a626e15c0fbf3d4b73336f5862f12f3d0b114e0fdf02`，报告 `output/packaging/portable-0.4-final-report.json`。原生 CDP 验收使用同一源码/前端资产的 production,p0debug,devtools 构建；单独的正式 ZIP 启动/退出由覆盖 smoke 证明。没有安装器构建/运行、代码签名、上传、暂存、提交或推送。


## 0.5.0 桌面增强与最终便携版（2026-11-02）

仅推进非账号桌面功能；没有登录、云同步、安装器、真实网络出口切换或系统代理/DNS/Hosts 修改。主窗口独占 PlayerStore/AudioEngine，迷你播放器与桌面歌词只收白名单投影并转发命令。详细契约见 [窗口协议](../architecture/desktop-window-protocol.md)。

### 自动化与独立复查

最终 `scripts/verify.ps1` 的 Go 全量测试/vet、12 文件共 164 个前端测试、生成 46 个 Wails 方法/30 个模型、TypeScript/Vite、Windows production 构建与依赖门禁通过；13 份 Go 根许可及前端/字体/图标通知保留。此前依赖目录被符号链接布局替换，首次审计 npm ls 失败；按锁文件重新 `npm ci --ignore-scripts` 后完整门禁通过，没有为通过审计放宽规则。CGO=0 未运行 race detector。

schema 5→6 有旧设置/队列/库保留、非法及未知设置字段事务回滚回归；原生位置与普通设置共享锁，过期 UI 写入不覆盖新位置，并发合并定向测试重复 50 次。两个独立复查发现并修复：Wails 默认居中覆盖显式坐标，改用 WindowXY；终止投影 sequence 不递增导致观察窗丢弃，改为幂等单次递增并保留 JS-safe 终态序号，前端优先展示退出。修复后重新独立复审无剩余实质问题。

### 真实 WebView2 播放与观察窗

`output/playwright/native-desktop05-report.json`：独立 profile 由本工程 schema 5 fixture 启动，未拦截网络、RPC 或音频响应；媒体静音，不宣称听感判断。通过 Audio 构造器/原型观察到主/mini/lyrics 的构造数为 **1/0/0**；两个浮窗不另建播放器。实际控制暂停、81 秒 seek、音量、收藏同步，拒绝旧 epoch 与错误歌曲目标；浮窗隐藏/重开复用同一窗口，主页面重载递增 epoch 并暂停恢复，主窗口隐藏后实际媒体时钟继续推进，投影歌词与桌面显示一致。

| 样本 | 真实来源 | 实际质量及媒体时长 | 本批证明 |
| --- | --- | --- | --- |
| 33894312 | 网易云兼容直连 | MP3 / 320 kbps / 267.245714 秒 | 启动、元数据、控制与后续缓存复用；未在本批等待它完整自然结束 |
| 1850977722 | 选定网易云路径失败后 GD | MP3 / 320 kbps / 227.731917 秒 | 真实自然结束并自动下一首，同一音频实例 |

本批日志普通/兼容/GD/缓存命中计数为 0/4/1/2：第一首兼容一次成功，第二首兼容三档失败才调用 GD 一次，随后暂停恢复复用缓存。GD 不受 Next 头改写，不为可播网易云流追码率。该长播验收先完成播放路径，之后仅修复前述位置/关闭投影；最终同源 production 前端/程序另做以下重启和退出验证，不能把早期长播说成所有最终版本场景重新跑遍。

任务栏真实 COM API 接受 normal、paused 与关闭进度状态。切歌通知默认关闭；测试明确启用后在主窗口隐藏时，对新播放条目请求一次，Shell 接受一次，tick/暂停/恢复不重复。记录 API 接受而未断言系统实际展示横幅，未测试免打扰策略。

### 原生窗口、位置和退出

`native-desktop05-windowstyle-report.json` 在实际主机 DPI=216 / 225% 下检查自有 HWND：mini/lyrics 的 WS_EX_TOPMOST 为真；锁定歌词启用 WS_EX_TRANSPARENT，解锁后移除。未采用截图或鼠标穿透模拟，不宣称视觉透明效果的人工验收。

`native-desktop05-restore-report.json`：启动只创建主窗口，按需创建浮窗后精确恢复非居中位置。真实设置 UI 改变歌词字号及不透明度，投影和 CSS 生效；位置重置落在实际 WorkArea 内，随后恢复旧位置。900×640 原生主窗口的内容区为 888×605，无横向溢出且底部播放器可见；默认 mini 420×176、lyrics 820×156 也无控件越界。

`native-desktop05-close-visible-report.json` / `close-hidden-report.json` 两浮窗都收到 closing=true/ready=false 终态；`native-desktop05-restart-visible-report.json` / `restart-report.json` 确认真正退出均为 exit=0。移动后立即关闭、重启精确恢复 mini 95/105、lyrics 115/315，验证去抖窗口内最终 bounds flush。schema 6、SQLite integrity=ok，本地收藏1、历史4、两条真实网易云 ID 队列及其位置设置保留；存储扫描无媒体 URL、stream、Cookie/token 字段。这里允许协议内公开 coverRef。实际物理多显示器移动、改变系统缩放、休眠/唤醒及多小时内存压力没有执行。

### 最终生产 ZIP

`output/packaging/portable-0.5-final-report.json`：最终 `bin/OTONEIGO-0.5.0-windows-x64.zip` 为 **19437467 字节**，SHA-256 `037e2b226abc8d3ef7bd5a19d1b0eec48d22beeb3c907b98c9eb219903167d56`。恰好四项 OTONEIGO.exe、LICENSE、THIRD_PARTY_NOTICES.txt、README.txt，逐项与最终构建输入哈希一致；metadata 准确 production，无 p0debug/CDP 或测试节流。

`portable-update-0.5-report.json` 只先放置旧 0.4 payload 再用 0.5 四文件覆盖，替换期间数据库哈希不变，oldVersionLaunched=false；真实旧应用跨版本运行未冒称通过。解压出的最终 production EXE 在同一隔离 profile、进程级不可达代理下创建/显示主窗口、初始化并 WM_CLOSE 正常退出码0，schema6/曲库/队列/位置和隐私扫描有效。没有制作或运行安装器，没有签名、上传、暂存、提交或推送。

以上是指定主机、有限样本证据，不是全曲目权益、全部 P0 或长期稳定性通过。账号/云端/会员能力继续暂停，真实 VPN/Tailscale/中国出口对照按用户要求未做；其余后续稳定性和候选功能见 [实施路线](../planning/roadmap.md)。


## 0.5.1 审计修复与复验（2026-11-02）

逐项核实、更正的推断、微基准、合成/真实/未测试边界见[0.5.1核实记录](../audits/audit-fixes-0.5.1.md)。确认修复封面重复解码/锁、音频访问manifest写放大/锁内校验/流句柄、曲库重试/分页、seek逐次保存、GetSettings读副作用/偏好丢失、锁内回调、数字域名前缀及托盘故障可观测性；GD预算与共享调用者生命周期补回归。没有更改缓存优先、按IP预选网易云、最后GD的规则。

独立复查发现的Clear+损坏校验复活任务和短预算首调用者误伤长等待者两个P2均已先红后绿并复审。最终Go全量测试/vet、178前端测试、TypeScript/Vite、Windowsproduction和13份Go许可门禁通过。CGO关闭未通过race检测。

`output/playwright/native-audit051-report.json` 在真实WebView2和SQLite注入一次初始资料fetch失败，重试恢复，无媒体拦截；收藏/历史第5页写入不清空跳页，50次seek只提交1次快照，真实缓存MP3时长267.245714秒，单Audio，20次实际封面请求全200。`native-audit051-host-report.json` 记录进程级不可达代理、exit0、数据库integrity=ok、1004收藏/1006历史、有效exhigh而原始hires保留。两个原有真实缓存MP3完整hash/结构检查通过，但较大标签/ID3v1仅合成覆盖。另`gd-audit051-live-report.json` 是独立单次真实320kbps同IDGD探测，不冒充线上三档链。

正式ZIP仅四文件，精确production，无调试监听。最终解压EXE的覆盖文件、启动和退出见`output/packaging/portable-update-0.5.1-report.json`，oldVersionLaunched=false。安装器仅源码标题保护修复和Windows写打开机制检查，未编译/运行。其余账号、真实VPN/中国出口、多屏/休眠和长期压力范围未扩大。


## 0.5.2 启动与数据恢复复验（2026-11-02）

逐项判断及限制见[0.5.2审计](../audits/audit-fixes-0.5.2.md)。启动不再读/解码全部资源payload；音频采用“先校验实际交付块”，没有未经验证先播放再后台补查。旧记录第一次完整扫描升级的成本明确保留。存储升级7隔离逻辑坏行并保留原SQL值，当前schema只在读取库记录时恢复，不引入每次全历史扫描；未知设置安全合并、冲突拒写，手动历史清理包括隔离历史。GD保留明确主失败原因，日志有界重试及固定状态/Close错误，失败面板不再常显加载。

微基准100.06MiB合成媒体Lookup→Prepare→HTTP首字节约126.19→5.057ms，为OS缓存条件。资源64图New+Close不随32px→2048px像素成本增加，但元数据条目数量仍影响启动。实际Windows锁/并发取消/clear代次/替换失败、坏行SQL类型/回滚/未知别名及历史删除边界回归通过。独立复查发现的问题修复后复审无剩余实质问题。

`output/playwright/native-audit052-{recovery,indexed,unknown,lazy}-report.json`与对应host报告：真实WebView2/SQLite/媒体服务，无RPC或媒体拦截，无截图，进程级不可达代理。各配置额外1000张1024大封面并正常启动退出码0；旧音频实际播放/81秒定位、11块升级，重启索引播放、损坏中间块的有效租约Range连续410；原始坏设置/收藏原样保留、健康资料可用，手动清空含隔离历史；未知字段及别名保存后重开保留；当前7的懒隔离同步收藏成员资格。不是新一轮整曲自然结束、机械盘或真实会员无损验证。

最终Go全量测试/vet、182前端测试/13文件、TypeScript/Vite、production Windows构建及13份Go许可门禁通过；47方法/31模型只增安全恢复计数。Go race未运行（CGO关闭）。最终便携包白名单/hash及真实解压production启动退出另见output/packaging/portable-0.5.2-final-report.json与portable-update-0.5.2-report.json；旧payload只复制替换，旧应用未运行，不冒称跨版本旧EXE验收。账号、真实网络出口切换、多屏/休眠、安装器继续按既有范围暂停。


## 0.5.3 工作池、提示和访问时间（2026-11-02）

[逐项记录](../audits/audit-fixes-0.5.3.md)区分合成饥饿/结构失败回归、延迟Promise竞态和真实窗口证据。源选择/资格/限速不变；资源验证两槽独立于网络四槽；恢复提示会话确认不删档、30秒检查；缓存统计只设置页，旧挂载请求不能覆盖新值；缓存接纳失败一次原预算skip-cache重解析；LastAccess侧车不重写Chunks，结构故障补存。结构变更/启动仍O完整清单。

最终Go全量/vet、190前端/14文件、TypeScript/Vite、production和13份Go许可门禁通过；真实点击发现的pointer-events继承已修正并重新全部前端/生产构建及原生验收。独立复审无剩余实质问题，Go race仍未运行。

output/playwright/native-audit053-report.json与host报告：真实WebView2、SQLite、媒体服务，没有RPC/媒体响应替换或截图，进程级离线代理。确认会话隐藏留档、同状态检查不再弹出、实际新隔离状态重新提示；8秒空闲恢复与两类缓存统计0、地区1，设置离开后统计停，真实30秒周期恢复1/两类统计各3（含两次入页）。实际1622字节manifest在reaper/Close内容和mtime不变，写172字节侧车并验证持久匹配，exit0/integrity/未知设置保留。20GiB索引基准和实际少量媒体分开，不把受控缓存竞态说成真机随机复现。

最终精确便携ZIP的四文件/hash及生产启动退出见output/packaging/portable-0.5.3-final-report.json、portable-update-0.5.3-report.json；旧payload复制覆盖不等于运行旧应用。没有扩大账号、出口切换、多屏/休眠或安装器范围。

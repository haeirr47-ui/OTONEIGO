# OTONEIGO

2026-11-03 维护补充：缓存共享目录锁、脱敏本地崩溃记录及补丁依赖策略见[维护加固](docs/maintenance-hardening.md)。报告默认位于 `%LOCALAPPDATA%\OTONEIGO\log\crash-*.txt`，最多五份；不自动上传。

当前补丁版：[0.5.3免安装ZIP](bin/OTONEIGO-0.5.3-windows-x64.zip)。缓存队列、会话提示、取流重试和访问写入审计见[逐项记录](docs/audit-fixes-0.5.3.md)。

独立的 Windows 网易云音乐客户端，在 OTONEI 现有 React 界面组件、布局和主题上适配与扩展，使用 React + Wails 3 + Go + SQLite。

**当前仍有 P0 未测试条件，不能宣称 P1/P2 全部验收。** 当前代码已接入地区观测、受限取流兼容、单一播放器、本地收藏/历史/设置、schema 7 SQLite、本地歌单、整库检索、收藏/历史分页、关键词历史、磁盘音频缓存、封面/歌词缓存和脱敏诊断；0.5.0 增加桌面歌词、迷你播放器、任务栏进度和可选通知。取流先查合资格缓存，再按地区/设置选择普通或受限兼容路径；已确认海外的 `auto` 从首个请求直接使用兼容头，不做普通预请求。它不是通用海外解锁。账号未实现，GD 同 ID 备用已按有限 MP3 原生样本开放发布 capability（`gdReleaseValidated = true`），用户开关仍默认关闭，可在设置中自愿启用。

2026-10-31 的隔离 header 实验取得一首歌曲 MP3，并在原生 WebView2 静音播放约 267 秒至自然结束、自动下一首和 81 秒定位恢复；那是历史实验，不能替代当前正式路径验收。2026-11-01 历史批次未拦截原生 GD 样本已自然结束于 267.245714 秒并自动下一首；网易云兼容路径与 GD 整文件 hash 一致仅证明该样本，不证明所有曲目或音质。最新 live/mock/failed/untested 证据及 P0 A—E 决策见 [P0 验证记录](docs/p0-validation.md)。

## 开发与构建

需要 Windows x64、Node.js 26、pnpm 12 与 Go 1.26 以上。当前验证使用 Node 26.12.1、pnpm 12.16.1、Go 1.27.3、Wails 3.0.0。前端依赖由 pnpm 管理（`frontend/pnpm-lock.yaml` 为唯一锁文件）。首次运行：

```powershell
./scripts/bootstrap.ps1
./scripts/verify.ps1
./bin/OTONEIGO.exe
```

`bootstrap.ps1` 在缺少 Go 时下载校验过的官方便携工具链，Wails CLI 与缓存放在 `.tools/`，不修改系统 PATH。`verify.ps1` 依次执行 Go 格式检查（`gofmt -l`）、Go 测试/vet、前端格式与 lint 检查（Biome）、前端测试、类型检查、绑定生成、Windows 生产构建和依赖门禁。单独构建使用 `./scripts/build.ps1`，单独生成依赖及许可记录使用 `./scripts/audit-dependencies.ps1`。

格式化与静态检查：Go 用 `gofmt -w <目录或文件>` 写入、`verify.ps1` 用 `gofmt -l` 校验；前端用 Biome（`frontend/biome.json`），`pnpm --dir frontend run format` 写入，`format:check` 与 `lint` 只检查，`frontend/bindings`、`dist` 与 CSS 不在范围内。依赖升级保持“先查后升、升完必验”：后端 `go list -m -u all` → `go get`/`go mod tidy`，前端 `pnpm --dir frontend outdated` → 安装精确版本并提交 `pnpm-lock.yaml`，两者都需重跑 `verify.ps1`；升级 Wails 时 `go.mod` 与 `bootstrap.ps1` 的 CLI 版本必须同步，并重跑原生验收脚本。

前端依赖装在 pnpm 的全局内容仓库里：`frontend/.npmrc` 设置 `enable-global-virtual-store=true`，虚拟仓库位于 pnpm store（默认 `%LOCALAPPDATA%\pnpm\store`），`frontend/node_modules` 只保留指向仓库的链接，因此多个检出共享同一份包文件。不要在仓库里再用 `npm install`，它会生成 `package-lock.json` 并破坏链接布局；该文件已加入 `.gitignore`。

构建结果为 `bin/OTONEIGO.exe`，配套许可文件位于同一目录；当前按用户要求只交付免安装 ZIP，安装包工作暂停；打包脚本默认仅生成 ZIP，最终包需与最后一次构建核对；没有代码签名、自动更新或远程发布。打包命令与限制见[打包说明](docs/packaging.md)。运行需要 Microsoft Edge WebView2 Runtime。普通前端 Vite 页面只是开发工具，业务依赖 Wails 桌面绑定。

关闭到托盘默认关闭；启用且托盘可用时，关窗隐藏同一播放 WebView。实际退出会冻结交互并尝试 hydrate、暂停和 flush，最多等待 3 秒后退出；确认握手不等于每次保存均成功，耐久边界仍是已提交事务。

## 项目边界

- `reference/` 下的 `OTONEI/`、`go-musicfox-master/`、`NetEaseMusicWorldNext-master/` 是只读参考副本，整个目录已加入根 `.gitignore`；新工程不包含其 Git 历史，也不自动提交或推送。
- 只有网易云曲库，歌曲 ID 始终为字符串。本地收藏/历史/设置已实现；账号保持禁用，GD 发布 capability 已开放，用户设置默认关闭；不承诺 VIP、其他录音版本或高音质覆盖。
- 没有 Firebase、UNM、额外本地 HTTP API 进程或自建远程服务器。同进程随机 loopback 端口仅提供受限媒体字节通道，业务仍通过 Wails 绑定。
- 本地数据默认位于 `%LOCALAPPDATA%\OTONEIGO\library.db`；启动恢复为暂停。验证可通过进程环境变量 `OTONEIGO_DATA_DIR` 指定隔离目录。
- 不读取旧 OTONEI 用户数据，不提供导入、导出、备份或恢复功能。

## 维护入口

- [执行方案](OTONEI_Windows_Netease_Implementation_Plan_2026-10-31.md)
- [海外检测与播放兼容 P0 补充方案](docs/overseas-compatibility-p0.md)：已实现的自动/始终启用/关闭策略、实验范围和待验收项
- [P0 实测与未通过门槛](docs/p0-validation.md)
- [架构](docs/architecture.md)、[存储协议](docs/storage.md)
- [许可与来源](docs/licenses.md)、[网易云适配器说明](docs/netease-adapter.md)


## OTONEI 界面复用

早期用于技术验证的独立简化界面已替换。当前复用原版侧栏/窄屏导航、顶部搜索、歌曲卡片、底部播放器、封面和展开歌词结构；在该界面中接入本地收藏/历史/设置及队列抽屉。原版主题、样式与 MiSans 字体随应用保留，状态与网络适配到已验证的 Go/PlayerStore，未引入旧 Firebase、下载或多音源状态。

实际来源清单见 frontend/THIRD_PARTY_NOTICES.md，当前验证见[P0 记录](docs/p0-validation.md)。先复用合资格的已验证本地缓存；网络取流按解析开始时的设置与地区选择路径：`auto+CN` 仅普通网易云，`auto+overseas` 与 `always` 从首个请求及所有降级档位直接使用兼容头、不先试普通请求，`off` 仅普通请求；`auto+unknown`（含检测失败）保留普通失败后合资格兼容的有界回退。所选网易云路径的有限音质链均失败后，才允许已启用、限速的同 ID GD；完整可播网易云结果立即接受，不为追求更高码率调用 GD。兼容只改写精确播放端点，搜索/元数据/歌词/封面与 GD 不带该头；CDN 域名改写没有实现。

2026-11-01 历史修复 GD 已开启却被网易云权限/试听分支提前截断的问题：当时普通优先路径的这些结果仅阻止随后地域兼容重写，不再阻止启用的同 ID GD 备用。当前已确认海外路径直接兼容的策略取代此前普遍普通优先的顺序。开关关闭、请求取消或响应无效时仍不会调用 GD。

新建配置的首选音质已调整为 exhigh（320 kbps）；旧的低档首选及未验证会员的高档首选归一为 320。接口返回更低档完整音频时按实际档位播放，不把偏好值当成真实音质。

当前音质规则：未登录/非会员手动仅选择 320 kbps，192/128 仅由程序自动降级；已验证会员的高级档位由 Go 策略决定。当前账号验证仍未接入，实际按匿名规则运行；GD 遵循相同请求/响应上限。会员策略测试不代表实际高级格式已验收。

### 0.3.0 历史验收

以下为 0.3.0 已归档结果，不替代 0.4.0 当前版本验收。音频缓存默认启用、上限 20 GiB，每个实际质量档位先查已验证缓存，再按原来源顺序取流。新媒体可通过同进程媒体通道边下边播，完整下载通过格式/时长/长度与 SHA-256 校验后才提交为缓存；不持久化签名地址。支持活动播放保护、清理、LRU 和磁盘余量检查。真实 CDN 原生渐进缓存播放、热播与 Range seek 已通过；受控自生成 WAV/FLAC 各 3 秒在原生 WebView2 自然结束，但不证明真实会员高级音源或其缓存传输；账号、云端同步仍禁用。托盘命令与系统媒体控制继续使用唯一 PlayerStore/AudioEngine；本批原生验收状态见 [P0 记录](docs/p0-validation.md)。

本批实际 Windows WinRT 媒体控制 Pause/Play/SkipNext 与同一 Audio 路由已验证；普通暂停保留媒体源/元数据，清理活动缓存需等待完整释放。进程级离线重启已验证缓存启动、零新增音频 API 请求及 seek；它不代表真实 VPN/Tailscale 对照。最终 Go 测试/vet、132 个前端测试、Windows 生产构建、许可检查和 0.3.0 portable ZIP 四文件哈希核对通过，详见 [P0 记录](docs/p0-validation.md)。

### 0.4.0 本地资源与曲库基线

封面与歌词缓存默认启用，共享 1 GiB 上限，可在设置中调整、关闭或清理；音频缓存仍独立默认 20 GiB。已缓存且校验有效的封面/歌词可离线读取，未缓存资源仍需网络；可信 WebP/AVIF 封面回退原地址，不纳入本地缓存。两类缓存都可丢弃，不删除本地曲库。

收藏和播放历史按歌曲、歌手、专辑在 SQLite 整库筛选，使用稳定游标、每页最多 200 条，页面只呈现当前页；爱心依据完整收藏 ID 集合。搜索关键词最近优先、去重，支持逐条删除和清空，不自动裁剪存储。收藏无硬性条数上限，但仍保留最多 100 个本地歌单、单歌单/队列 1000 首及队列 2 MiB 等保护。schema 升级保留本工程数据，不提供旧产品迁移或用户备份/恢复。

0.4.0 原生在线/进程级离线资源、6010 条收藏与超过 5000 条历史的整库检索、关键词 UI、实际强杀恢复及最终生产便携覆盖更新已通过有限用例；schema 4→5 使用独立合成 fixture 验证。0.4.0 最终 Go 全量测试/vet、149 个前端测试、TypeScript/Vite、Windows production 构建及 13 份 Go 许可通知审计通过；portable ZIP 四文件白名单及各 payload 哈希与最后构建输入一致。详见 [P0 记录](docs/p0-validation.md)。账号及云端功能、真实 VPN/中国出口对照和安装器继续暂停。

海外直接兼容遇到冷却时不退回普通请求，只按独立开关尝试 GD 或返回 `COMPATIBILITY_COOLDOWN`。实际 TH/overseas、auto、无网络拦截的原生 WebView2 已验证新路由：样本 33894312 普通/兼容/GD 请求数为 0/1/0，实际 320 kbps、启动 1435 ms、媒体时长 267.245714 秒；另一同 ID GD 回退样本为 0/3/1、启动 2209 ms、时长 227.731917 秒，均只有一个 Audio。此次验证是启动与媒体时长检查，不是两首完整自然结束。资源/查询、离线和强杀报告使用旧路由，不单独作为此次顺序证明；新路由由 `output/playwright/native-egress04-report.json` 独立记录。完整条件见 [P0 记录](docs/p0-validation.md)。

封面缓存内部固定请求 1024×1024 显示图，原始可信封面引用及缓存身份不变。真实同源封面在清理后产生一次 404，重新挂载相同散列路径后恢复解码；进程级不可达代理下已缓存封面/歌词读取 308 ms，音频启动 184 ms、音频 API 请求增量为零。这些是指定主机/样本结果，不是时延保证或真实断网/VPN 对照。

### 0.5.0 桌面增强

在设置的“桌面窗口”中打开迷你播放器和桌面歌词。浮窗共用主窗口唯一播放器，支持播放控制、定位、音量和收藏；桌面歌词支持拖动、置顶、字号、不透明度和锁定穿透，锁定后可从主设置或托盘解锁。浮窗关窗仅隐藏，位置保存在本地；真正退出主窗口会关闭所有浮窗。任务栏显示同一播放状态，切歌通知默认关闭，启用后仅在主窗口隐藏/最小化时请求。

本批真实 WebView2 已验证主/迷你/歌词 Audio 构造数为 1/0/0，隐藏后时钟继续、控制与歌词同步、同一 GD 320 kbps 样本自然结束及自动下一首。主机 225% 缩放下的穿透/置顶原生样式、位置恢复/重置、字号/透明度、最小窗口与两种浮窗退出场景通过；不代表物理多屏切换或休眠通过。完整 Go/vet、164 个前端测试、production 构建/许可审计与最终四文件 ZIP/哈希核对通过，正式解压程序启动退出码为 0。仅交付 [0.5.0 免安装 ZIP](bin/OTONEIGO-0.5.0-windows-x64.zip)；条件和剩余项见 [P0 记录](docs/p0-validation.md)及[实施路线](docs/roadmap.md)。


### 0.5.2 启动与恢复

资源缓存启动仅加载索引和文件属性，首次使用才完整验证内容。音频新缓存保存1MiB块摘要，交付前验证对应块；旧缓存首次完整校验后补建索引，成功保存后重启不必先全读大文件。逻辑损坏的收藏、历史和设置由schema7事务隔离保留，设置回落默认并显示提示，其他健康资料继续使用。当前schema不每次扫描全部历史；未知设置字段合并保留，无法安全改写的未来记录明确拒写。手动清空历史同时清除隔离历史。物理/SQL错误和未来schema仍拒绝打开。

182前端测试、Go/vet和production构建通过；真实WebView2已验证1000张大封面配置启动、坏行恢复、旧音频11块索引升级、损坏Range连续410及未知字段保存重启，均正常退出。微基准和旧缓存首次升级成本等限制见[0.5.2审计](docs/audit-fixes-0.5.2.md)，不将结果外推为机械盘/会员或全部P0通过。


### 0.5.3 缓存与空闲工作

封面首次验证独立两槽，网络下载仍四槽；恢复提示可在本次会话“知道了”关闭，不删除原始隔离记录。恢复检查改为完成后30秒；缓存统计只在设置页立即及30秒刷新，离开停止，旧请求不覆盖新结果。缓存候选接纳失败有一次同预算的无缓存重解析；已有完整可信地址不会因可选缓存故障被丢弃。访问时间写入小型recency.json，避免高频重写Chunks，结构清单失败会补写。schema仍为7。

190前端测试、Go/vet、production与许可通过，真实点击/计时/侧车落盘和正常退出通过。结构变更与启动仍可重写全manifest，访问增量也非固定大小；详情见[0.5.3记录](docs/audit-fixes-0.5.3.md)。

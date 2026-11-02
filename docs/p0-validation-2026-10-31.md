# P0 验证记录 — 2026-10-31

**结论：P0 已取得真实歌曲播放的隔离实验闭环，但 A—E 尚未全部验收，未进入 P1。** 普通匿名取流仍失败；受限 `X-Real-IP` 实验对一个 fee=0 样本成功，并在真实 WebView2 完整静音播放、自然结束、自动下一首和定位恢复。正式客户端尚未接入该策略、自动地区检测、账号或 GD，不能把测试桥接算作现有可执行程序的正式功能。

环境：当前 Windows x64 主机，Node 26.12.1、Go 1.27.3、Wails/runtime 3.0.0、React 19.3.1、modernc SQLite 1.61.0。原生宿主加载内嵌 React 资源，WebView2 CDP 地址为 `http://wails.localhost/`。没有把普通浏览器 mock 当成原生窗口结果。

| 检查 | 实际证据 | 状态 |
| --- | --- | --- |
| Git 隔离 | `git check-ignore` 命中三份原项目副本；编译物、数据库、缓存、截图均忽略 | 通过 |
| Go / 前端 | `scripts/verify.ps1`：Go tests、vet、22 项前端测试、TS、Vite、绑定生成、Windows 生产构建 | 通过 |
| 绑定一致性 | 重生成 bindings 后逐文件 SHA-256 相同；1 service / 5 methods | 通过 |
| 排除依赖 | 编译 package、module graph、binary build info 无 UNM/Firebase/原 musicfox module；13 份许可文件记录 | 通过（完整分发审计延期） |
| Wails 原生窗口 | 页面正常绘制，真实绑定读库/搜索；当前窗口字体/布局可见；`environment.Debug=false` | 当前主机通过；其他 DPI、Win10/11 组合未测 |
| 匿名搜索 | 原生窗口搜索“夜空中最亮的星”返回 310 条总数与首屏；另有 Go 在线 probe | 通过（本次网络条件） |
| 歌词 / 封面 | 原生选歌后 LRC 加载、封面 `naturalWidth>0` | 通过（本次网络条件） |
| 普通匿名歌曲直连 | 多个候选（含详情 fee=0 样本）普通 v1/legacy/公开 API 返回 matching ID、item code 404、空 URL | 普通路径未通过；新增 header 成功实验另记 |
| 原生单音频执行器 | 真实 Wails WebView2 + 支持 Range 的 30 秒静音 WAV；暂停定位 10 秒，恢复实际媒体位置为 10 秒；下一项仍用同一个音频对象 | 受控媒体通过；不是在线网易云音频证明 |
| 迟到取消 | 受控 resolver 延迟后取消，迟到响应没有恢复播放；不同歌曲竞争另有单测 | 通过（受控结果） |
| SQLite 提交 | 原生重复加歌得到独立 entry ID；加强测试实际读回 10000 ms、revision 34→47→54，未保存 fixture URL | 通过 |
| 异常退出 / hydrate | 强杀测试专用 PID，重开保留两条队列与 revision 13；启动不改 revision、保持暂停 | 通过 |
| 模拟断网 | 重启进程的 HTTP(S) proxy 指向不可达 loopback；真实搜索失败，本地两条元数据仍显示 | 通过（进程网络故障模拟，未改系统网络） |
| 正常退出 | 通过 Wails Window.Close 关闭主窗口，宿主进程退出 | 通过 |
| 账号 / GD | 无相关绑定、依赖或 UI；均未启用 | 未测试 / 未交付 |
| 安装、升级、签名、托盘 | P0 不包含 | 未测试 / 未交付 |

## 在线直连调查边界

搜索/歌词成功不表示音频接口可用。对歌曲 `25706282`、`548823721`、`33894312` 等进行了有限调用；v1、旧 WEAPI、公开非加密接口均出现不可用结果。对一个样本试过上游默认 `encodeType=flac` 和普通桌面请求头，仍无 URL。官方公开 outer-media 链接重定向未落到允许的音频主机，没有追随或新增旁路。

上述为初始普通请求结果。下述新增 header 单变量实验已取得一个样本的真实音频，并验证完整原生播放；正式集成和其他 P0 分组仍待完成。尚未完成同状态中国出口对照，不能把 header 成功当成地域根因完全证实，也不能据此宣称 VIP 权益、所有歌曲或所有音质均可用。

## 原版对照与 DNS 诊断补充

下列检查同样在 2026-10-31 执行，没有修改三份参考项目源码，没有输入账号或开启 UNM。它们补足初始调查中缺少的原版对照，不是新客户端海外兼容已经有效的证明。

| 对照 | 实际条件与结果 |
| --- | --- |
| 本地原样 SDK v1.6.0 | 通过 `go-musicfox-master/vendor` 运行最小匿名探针，保留 SDK 自己的 CookieJar/设备初始化；v1 默认 flac 与原样 legacy **Linux API** 对歌曲 `25706282` 都返回顶层 200、item 404、ID 匹配、空地址。此结果区别于初轮自行构造的旧 WEAPI 请求 |
| 官方 go-musicfox Windows v5.1.0 | 下载官方 release 并核验 GitHub asset SHA-256；二进制 build info 显示 SDK v1.6.0、Windows amd64、CGO=1。隔离 `MUSICFOX_ROOT`，清除该进程继承的 `MUSICFOX_COOKIE`，关闭 UNM、上报、更新检查与通知；实际启动 TUI，未登录搜索同名歌曲成功，选择播放后原版记录 `url=""`，首曲 `25706282` 及自动跳过的 `28707594`、`28427772` 均未获得地址；随后正常退出 |
| 源码构建限制 | 本机未安装 C 编译工具链；原项目即使使用 purego tag，CGO=0 仍因 minimp3 导入无法编译。因此整程序对照运行的是官方 release；原样 SDK 对照运行的是用户提供的本地 vendor，二者没有混称 |
| 公开歌曲详情 | 原 SDK 批量查 `25706282`、`548823721`、`33894312`，返回三者 `st=-100`、`pl=0`；其中 `33894312` 的歌曲 fee 为 0。这里只记录字段，不能单独据此判定地区、会员或版权原因 |
| 本机 DNS 与公共 DoH | `music.163.com` 都解析至 `overseasv4.music.ntes53.netease.com` 的相同地址；截图的 `fcount-api.webapp.163.com`、`ac.dun.163yun.com`、`temp.163.com` 在本机 DNS 返回 `0.0.0.0`，公共 DoH 返回正常地址 |
| 进程内 DNS 对照 | 同一 catalog resolver、同一歌曲、相同直连传输配置分别用系统 DNS 和公共 DoH 返回的固定 IP；第二组实际执行了一次固定 IP 拨号，HTTPS 主机名/证书校验保持开启，两组均返回 unavailable/无地址；未修改系统 DNS、Hosts、代理或 AdGH 设置 |

官方 release ZIP SHA-256 为 `a931153dfbe7eb90493a3980094a68485ce131edc2e2893d5431b139c3b34be3`，来源见 [v5.1.0 release](https://github.com/go-musicfox/go-musicfox/releases/tag/v5.1.0)。本地 SDK `util/cryto.go` 的 Git blob SHA-1 `25f79c51e7bba7802aa48fd7d6a20616c93e781e` 与上游 [v1.6.0 对应文件](https://github.com/go-musicfox/netease-music/blob/v1.6.0/util/cryto.go) 匹配；不由此推断整份归档匹配某个提交。

这些结果支持把“当前请求条件/出口地区”列为下一步重点，减弱了“只因新客户端实现遗漏”或“截图三条 DNS 拦截直接导致取流失败”的解释。但没有完成同歌曲、同账号/匿名状态、同音质下的中国大陆出口对照，**地域限制仍是待验证假设**。没有把 DNS 中的 overseas 名称、item 404 或单个权限字段当成已确认地区限制。

诊断用 Go 探针位于被忽略的 `.tools/musicfox-diagnosis/`，原版二进制和隔离状态位于 `output/musicfox-diagnosis/`。这使 GPL 参考 SDK 的测试依赖不进入新工程 `go list ./...` 的遍历或正式二进制。原版程序自带日志可能包含地址，保留的调查摘要只包含必要状态，不将原始日志提交到 Git 或复制进文档。

## 新增 header A/B 与真实 MP3 验证

本轮按新补充的 P0-C/E 方向执行受限原型，未安装 Chrome 扩展、未复制 GPL 源码、未修改正式服务绑定、系统代理、DNS、Hosts 或 AdGH。参考中的固定 `X-Real-IP` 值仅作为一次性实验变量，不能称为真实公网出口、生产默认值或永久可用保证。

| 检查 | 实际结果与边界 |
| --- | --- |
| 地区路径观测 | 独立 Go HTTPS 请求 Cloudflare trace，保留 `country=TH`，丢弃原始 IP；只证明该检测路径的观测，不能排除网易云/API/媒体分流；未实现启动自动检测、TTL 或网络变更刷新 |
| 普通/请求头 A/B | 同一匿名歌曲 `33894312`、standard、同一 catalog resolver 与播放端点；普通请求 unavailable/无地址，仅给 `POST https://music.163.com/weapi/song/enhance/player/url/v1` 加 header 后返回 full、时长 267232 ms、服务端标记 standard/MP3。没有给搜索、歌词、GeoIP、认证或媒体 CDN 加该 header |
| Go 音频探测 | 对兼容地址只读取并丢弃 2048 字节，返回 HTTP 206、`audio/mpeg`；HTTPS 主机及证书验证保留。没有做 CDN 域名替换或 no-cache 改写 |
| 元数据一致性 | 普通搜索取得同 ID《情非得已 (童声版)》、歌手“群星”、元数据时长 267232 ms，与取流接口 ID/时长匹配。未人工听审录音版本或测量真实码率；standard/MP3 是服务端标签 |
| 原生完整播放 | 原始 Wails WebView2、现有 PlayerStore/AudioEngine；真实 MP3 时长 267.245714 秒，自然结束时播放时钟同值，约 269.583 秒墙钟后已进入下一条队列。音频被测试工具静音，证明解码/播放时钟/自然结束，不证明扬声器听感 |
| 下一首与单实例 | 自然结束后自动进入同歌曲的第二个独立 queueEntryId，继续真实播放；整个测试只使用一个 HTMLAudioElement；三次取流均由独立 Go 实际请求完成 |
| 定位/SQLite | 暂停后将第二项定位到 81000 ms，实际 Go/SQLite 读回；重新解析播放后原生媒体从 81 秒继续，最终 revision=63；保存快照中无签名媒体 URL |

**桥接范围必须区分：** 未改动正式 AppService。测试工具仅拦截测试窗口的 ResolveStream 返回，把独立 Go HTTP 原型实际收到的网易云结果通过内存管道交给现有前端；未伪造媒体、时长或播放时钟，其他绑定仍走实际 Wails/SQLite。Go 成功结果中的地址只在子进程管道和 JS 内存传递，不输出到终端、文件、HAR 或报告。此结果验证“header 原型 → 真实 CDN → 原生播放器”的可行性，正式服务集成仍未验收。

metadata-only 报告与截图在 `output/playwright/native-live-region-report.json`、`native-live-region-playing.png`；原型/测试脚本在 `.tools/musicfox-diagnosis/`（均 Git 忽略）。独立复核确认自然结束、自动下一首、81 秒定位与真实 SQLite 证据有效，同时保留一个歌曲/端点/音质的顺序 A/B、静音、测试桥接和缺少中国出口对照的限制。没有启用生产策略或宣称 P0 全部通过。

## 海外兼容补充后的分组状态

目标和完整验收矩阵以 [海外 P0 补充方案](overseas-compatibility-p0.md) 及根实施方案为准；以下仅是当前状态。

| 分组 | 已验证 | 仍待验证或实现 |
| --- | --- | --- |
| P0-A 基础宿主 | 当前主机 Wails/React/Go、生成绑定、构建 | Win10/11/DPI 组合覆盖 |
| P0-B 网易云目录、登录与取流 | 匿名搜索/歌词/封面、原版与 SDK 对照；隔离 header 路径取得一个可播样本 | 普通路径失败原因、正式取流集成、登录/会话实验 |
| P0-C 出口与地域兼容 | DNS 对照、参考机制/许可、Go 路径国家观测 TH、一个端点 header A/B 有效 | 自动地区检测及缓存、自动/始终/关闭、参数维护、CDN/no-cache、中国大陆出口 A/B、网络切换 |
| P0-D GD 网易云备用 | 仅已有接口规划 | 同 ID 请求、限流、完整性、失败回退和保留/禁用决策 |
| P0-E 原生音频完整性/音质 | 受控 WAV；新增真实网易云 MP3 完整自然结束、自动下一首、81 秒 seek、单实例与 SQLite 提交 | 正式路径集成、更多歌曲/音质、真实码率/听审、CDN 候选及 GD 媒体 |

三份参考都保持只读；NetEaseMusicWorldNext 没有安装或运行。其 manifest 版本为 1.0.0，GPL-3.0；本轮新增方案及隔离实验原型，不新增正式运行时模块、绑定或启用开关。

## 复现与产物

```powershell
./scripts/verify.ps1
. ./scripts/toolchain.ps1
Invoke-ProjectToolchain { go run ./cmd/p0probe -query '夜空中最亮的星' }
```

probe 只打印 ID、计数和状态，不打印媒体 URL 或会话。`scripts/native-media-smoke.mjs` 是显式受控测试，替换 resolver/lyrics 返回并保留真实队列与 SQLite；它要求测试专用的 `production,p0debug,devtools` 二进制、9225 loopback CDP 和已加入两条队列的隔离数据库。安装测试工具可使用 `npm install --prefix .tools/browser @playwright/cli@0.1.22`；验收构建示例：

```powershell
Invoke-ProjectToolchain { go build -tags 'production,p0debug,devtools' -o bin/OTONEIGO-p0-debug.exe . }
$env:OTONEIGO_DATA_DIR = Join-Path $PWD 'output/native-p0'
# 启动测试专用二进制后，通过原生搜索加入两条队列
node scripts/native-media-smoke.mjs
```

CDP 只存在于显式 P0 调试构建，生产构建使用 `production` 且不编译调试参数。测试完成已关闭宿主。截图与 JSON 报告在 `output/playwright/`；依赖/许可/build info 在 `output/p0/`；所有测试数据库在 `output/native-p0/`，均不进入 Git。Wails 的可选 `/wails/custom.js` 在 desktop 模式返回 404 是上游行为；没有把它当成业务接口失败。


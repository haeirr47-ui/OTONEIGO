# go-musicfox 匿名功能参考与 OTONEIGO 差距计划

> 评审日期：2026-11-05；实装跟踪：2026-11-07（切片 1-5 已落地）。本文保留静态调研与分批计划，实际验证以 P0 记录为准，不据计划推断在线可用性或 P0/P1/P2 验收完成。
> 关联：[当前实施路线](roadmap.md)、[架构](../architecture/architecture.md)、[存储协议](../architecture/storage.md)、[网易云适配器](../architecture/netease-adapter.md)、[许可与来源](../delivery/licenses.md)、[P0 验证记录](../validation/p0-validation.md)、[根实施计划](../../OTONEI_Windows_Netease_Implementation_Plan_2026-10-31.md)。

## 1. 评审结论与证据口径

建议保留功能盘点，按实际依赖拆成小批交付。原 A+B 整组并非最低风险：暴露歌曲资料查询会新增网络活动，歌手/专辑详情需要新端点，逐字歌词和播放模式涉及跨层契约。发现列表也必须和详情、资料解析及加入播放队列一起验证，不能先交付无法使用的入口。

优先顺序为聚焦窗口快捷键与轻量帮助、独立的“下一首播放”队列操作；右键菜单复用稳定命令。网络候选先做有界匿名预检，再决定页面范围。2026-11-05 评审仅修改方案；2026-11-06 根据后续指示开始切片 1，其他切片仍为建议/待验证，不自动计为交付。

“原版”仅指只读副本 `reference/go-musicfox-master/`，即 GPL-3.0 的 go-musicfox TUI，不把 GoFox 当作另一个官方产品。其 `internal/types/constants.go` 默认 `AppVersion` 为 `v5.0.0`，构建可覆盖；尚未证明副本对应的远端提交，不能声称已审计最新版本。

| 证据级别 | 能说明什么 | 不能说明什么 |
| --- | --- | --- |
| 当前工程源码 | 已有模块、接口和调用关系 | 所有原生环境或实时服务都通过 |
| 参考客户端无硬鉴权拦截 | 代码允许发起匿名尝试 | 服务端接受纯匿名、返回完整或可播放内容 |
| SDK 存在端点 | 可研究协议参数 | 原 TUI 有此产品功能、匿名可用或可直接复制 |
| live 有界样本 | 注明日期、地区、会话和对象下的结果 | 全部歌曲、地区、权益或未来结果 |
| mock / optional / failed / untested | 受控行为、候选、失败或尚未执行 | live 成功或原生 WebView2 验收 |

参考 TUI 的 `internal/ui/menu_main.go` 无条件列出主菜单不代表全部匿名可用。`utils/struct/structs.go` 的 `CheckUserInfo`、`internal/ui/executor.go` 的 `NeedsAuth` 是硬门槛；`CheckCode` 将 301/302/20001 转为 `NeedLogin` 是请求后的软兜底。

`vendor/github.com/go-musicfox/netease-music/util/request.go` 使用全局 cookie jar；即使调用点没有 guard，jar 也可能已有会话。没有 `MUSIC_U` 字面量不能证明请求干净匿名。本文所有新增端点仅有静态依据，均未做本轮 live 验证。

最新 [Wails 升级记录](../audits/wails-3.1.0-upgrade.md)及 P0 记录已有冷缓存匿名取流 `UNAUTHORIZED` 的失败样本；它不证明目录端点同样失败，也不支持“匿名端点普遍可用”。保留失败原样，不用历史成功覆盖当前未复验项。

## 2. 校正后的当前基线

| 能力 | 当前事实与源码锚点 | 差距判断 |
| --- | --- | --- |
| 收藏/历史整库筛选 | `frontend/src/LibraryPanels.tsx`、`PlayerStore.ts` 的 `queryFavorites`/`queryHistory`，`internal/storage/libraryquery.go` 已执行 SQLite 筛选与分页 | 已有；不能再列“本地过滤缺失”。本地歌单已加载列表搜索可另作可选 UX |
| 队列与排序 | 单一 PlayerStore；`PlaylistPanels.tsx` 已有上移/下移；支持三种播放模式 | 下一首播放可独立做，不能只靠数组插入；拖拽和扩展模式另拆 |
| 歌曲资料 | `internal/catalog/client.go` 的 `GetSong` 已存在，未作通用前端资料查询暴露 | 新 Wails 入口仍会新增请求和失败路径；分享不以它为前置 |
| 歌手/专辑标识 | `TrackSnapshot` 已有歌手/专辑 ID | 仅具备导航标识，详情仍需端点和独立模型 |
| 歌词 | 已有逐行 LRC、翻译对齐和缓存，见 `frontend/src/lyrics.ts` | YRC、时间偏移是两个独立候选 |
| 桌面位置 | `main.go` / appservice 原生回调已使用 `SaveDesktopPlacement`，位置恢复和重置已有 | 不因前端未调用而判缺失；数值编辑仅是可选 UX |
| 桌面诊断 | `GetDesktopNativeState` 有绑定，组件未调用 | 诊断呈现选择，不是必须补齐的产品缺口 |
| 运行时字段 | provider/quality 等已有运行时用途；album/cacheHit 等按具体视图评估 | “未展示”不等于“未消费”，不把 networkGeneration 等技术字段直接接到产品界面 |
| 系统媒体控制 | `frontend/src/main.tsx` 接入 `mediaSession.ts`；历史 native-smtc 脚本以 WinRT 探测/控制 Windows 会话 | 已有系统控制；未定位独立 Go SMTC 适配器，不能据此判缺失或宣称已有该适配器 |
| 启动队列 | 已恢复队列且保持暂停 | 不把自动开播夹带为恢复功能补齐 |
| Windows 与本地能力 | 已有托盘、任务栏进度、迷你播放器、桌面歌词、通知、SQLite 库与缓存 | 按路线及有限原生样本描述，不重复建模块 |

当前 catalog 的搜索、歌曲资料、歌词、取流已有四类端点；账号和播放权益门控仍关闭。既有匿名搜索不等于所有实体类型都已支持，也不等于高级音质资格开放。

## 3. 候选库存：本地交互与跨层功能

| 候选 | 参考依据（相对参考副本） | 建议边界及复杂度 |
| --- | --- | --- |
| 聚焦窗口快捷键 | `internal/keybindings/keybindings.go` | 首批建议；复用现有播放、队列、收藏命令。应用聚焦快捷键与 OS 全局热键分开 |
| 小型快捷键帮助 | `internal/ui/help_popup.go` | 随首批提供按当前上下文可用的简短说明，不做强制教程或大手册 |
| 下一首播放 | `OpAppendSongsToNext` / `appendSongsToCurPlaylist(..., true)`（`internal/ui/event_handler.go`） | 已实现（2026-11-06）：独立队列行为，不增加播放模式 |
| 右键菜单 | `internal/ui/action_select.go` | 复用已验收命令，同时提供键盘菜单/按钮入口 |
| 拖拽排序 | 当前 GUI 增强 | 后续建议；先区分播放队列与本地歌单，保留键盘上移/下移，各自沿用现有身份和 revision 契约 |
| 分享与打开网易云页面 | `internal/composer/share.go`、`internal/ui/operate.go` | 由已有字符串 ID 构造规范网易云 HTTPS 链接；无需强制 `GetSong` |
| 本地歌单已加载列表搜索 | `internal/ui/menu_local_search.go` | 可选，明确只筛已加载内容；不替代现有 SQLite 收藏/历史整库检索 |
| 主题、中英 i18n | `internal/configs/theme.go`、`internal/ui/i18n.go` | 后续独立建议；覆盖弹层、浮窗、可访问性和持久化，不整组并入首批 |
| 通知封面与点击动作 | `utils/notify/notification.go` | 后续建议；原生生命周期、图片信任和点击命令需验证，保持默认关闭 |
| 资料展示/歌曲详情 | 当前 `GetSong` 和已有 snapshot | 按实际用户需求加字段；新增查询须有界、可取消，不盲接诊断数据 |
| YRC 逐字歌词 | 当前 catalog/LRC 对照 | 跨层候选：响应契约、解析、缓存兼容、渲染、无 YRC 降级及桌面投影 |
| LRC 时间偏移 | 当前歌词时钟 | 独立候选：定义全局/逐曲、符号/范围、重置及持久化语义，不改变音频时钟 |
| 顺序播放/无限随机 | `internal/types/player.go` | 后续跨层候选，不能视作只改按钮枚举 |
| OS 全局热键 | 原版可选 `enable_global_hotkey` | 单独的 Windows 原生注册、冲突、注销及生命周期任务 |

播放模式若立项，需协调 `internal/domain/types.go` 的 `ValidateQueue`、前端 `contracts.ts`/`wire.ts`、PlayerStore 自然结束与手动上下曲语义，并验证旧持久化值兼容。不得预设不涉及存储契约或 schema。

系统媒体控制只有遇到可复现的 MediaSession/WebView2 限制，才评估替换或补充原生适配器；不为“看起来缺 Go 接口”重复建设。

## 4. 候选库存：新增网易云请求与页面

下表均是“参考客户端无硬拦截或 SDK 静态协议线索；纯匿名 live 待验证”。软兜底、字段及地区差异须逐端点确认，不使用“明确无门槛”的保证性表述。

| 候选 | 参考端点/菜单 | 建议依赖与次序 |
| --- | --- | --- |
| 排行榜 → 歌单详情 | `api/toplist`、`menu_ranks.go` → `weapi/v3/playlist/detail`、`menu_playlist_detail.go` | 首个网络纵向切片，先验证详情、资料与既有队列可用性 |
| 新碟/最新专辑 → 专辑详情 | `weapi/album/new`、`album/newest`、`menu_album_new*.go`、`menu_album_detail.go` | 排行榜切片之后；分页与详情单独验证 |
| 热门歌手 → 歌手歌曲/专辑 | `weapi/artist/top`、`menu_hot_artists.go` 及相关菜单 | 验证各详情端点，不把已有 artist ID 当页面实现 |
| 精选歌单 → 详情 | `weapi/playlist/highquality/list`、`menu_high_quality_playlists.go` | 同一详情链稳定后复用；不是个性化每日推荐 |
| 多实体搜索 | `menu_search_type.go`、`search_page.go` | 先 SONG/ALBUM/ARTIST/PLAYLIST；实体模型与导航分别处理 |
| 搜索建议/热搜 | SDK 与参考搜索流程 | 独立请求候选；去抖、限速、取消，不随每次渲染查询 |
| 相似歌曲 | `weapi/v1/discovery/simiSong`、`menu_similar_songs.go` | 后续独立建议，不假定各曲目都有结果 |
| 电台/播客分类、榜单、节目详情 | `menu_radio_dj_type.go`、`menu_dj_*.go` | 低优先级；独立实体模型和匿名验证，不以入口数量证明能力 |
| 用户/歌词/电台搜索 | 参考搜索类型菜单 | 低优先级，先证明价值与响应契约，不一次扩齐所有类型 |
| 评论只读 | SDK `api/v1/resource/comments/R_SO_4_{id}` | SDK 候选，原 TUI 没有此 UI，不是原版产品差距 |
| 私人 FM | `menu_personal_fm.go` | 低可信度、暂后置；无硬 guard 不能推出匿名可用 |

电台 `entity.id`、节目 `episode.id`、节目 `mainSong.id` 不是同一标识。若支持播放，只能将有效字符串 `NeteaseSongId`（来自 `mainSong`）加入歌曲队列；不能把节目 ID 强转歌曲 ID。先定义独立元数据模型，再谈复用歌曲卡片。

### 新端点的有界匿名预检

预检是未来建议，不在本次执行。每个端点以空 cookie jar、无导入会话（noJar/等效隔离请求）独立验证，不直接使用可能携带全局会话的参考 SDK。

- 记录端点、最小样本、参数、日期、地区、HTTP/业务状态与有限响应结构，不保存 Cookie/Token/签名媒体地址或原始敏感响应。
- 分别报告需登录、429、网络失败、schema 不兼容、无数据、部分数据与成功；失败不转登录页，不增加账号空入口。
- 排行榜覆盖列表、一个详情、歌曲资料解析、加入既有队列及尝试播放；目录成功与取流失败分别记账。详情依赖未通过前不交付只能点开的列表。
- `playlist.trackIds` 与 `tracks` 可能不等长。不得继承参考 `n=100000`；先验证实际分页或有界批量资料补齐，不能补齐时明确显示部分结果。
- 页面请求使用各自有限预算、限速/合并和取消代际；切页/改查询/关闭后丢弃迟到结果。不在卡片渲染时 N+1 查询、不无限扇出、不启动时预载完整库快照。
- 新目录/资料预算独立于既有取流总预算，不改变 resolver 的共享有限 deadline、缓存顺序和降级策略。
- 精确播放器端点之外不添加兼容头；目录/搜索/歌词/封面不交给 GD，不扩大登录/QR/认证改写范围。
- 保留当前固定字段脱敏日志，不复制参考 SDK 的原始 cookie/响应错误日志。

## 5. 建议实施顺序与验收条件

### 状态台账（2026-11-06）

| 切片 | 状态 | 证据与未验证边界 |
| --- | --- | --- |
| 1 聚焦快捷键与轻量帮助 | 已实现 | `frontend/src/playerShortcuts.ts`；证据见 [P0 记录](../validation/p0-validation.md) 的 2026-11-06 节。该节记的 199 项测试属当时快照，当前为 222 项 / 16 文件 |
| 2 下一首播放 | 已实现 | `PlayerStore.playNext` + 会话级 `pendingNextIds`；前端测试覆盖多批插入、重复曲目、容量与清空时机；**无原生验收**，未验证"点两次"在真实播放中的顺序 |
| 3 右键菜单与分享 | 已实现（拖拽排序未做） | `frontend/src/TrackActions.tsx`、`trackSharing.ts`；只打开固定 `music.163.com` 链接，ID 先经正则校验。菜单可在卡片与队列行打开，**无原生验收** |
| 4 排行榜 → 歌单详情 | 已实现，完成 TH 单样本匿名探测 | `internal/catalog/discovery.go`、`frontend/src/DiscoveryPanel.tsx`；fixture/单元测试覆盖匿名传输、上限、不可信封面与分页切片；2026-11-06 在 `loc=TH` 出口实测 `n:30` 返回全量 `trackIds`、offset 0/30/60 正常、越界不报错，专辑/歌手/新碟/精选歌单端点均 200。单样本不等于全地区可用 |
| 5 多类型搜索与专辑/歌手详情 | 已实现（2026-11-07） | 搜索四类切换、站内详情页、右键菜单入口；`loc=TH` 单样本 live 探测三类搜索与两种详情均解析正常；原生 WebView2 验收未做 |

### 切片 1：聚焦窗口快捷键与轻量帮助（2026-11-06 首批实装）

沿用现有命令入口，限定应用当前聚焦视图；先定义播放/暂停、上下曲、音量、收藏和队列等少量键位与可用条件。

验收：input、textarea、contenteditable 不误触；IME composing、重复按键、弹层/对话框焦点和 Escape/Tab 正确。命令不绕过关闭或禁用状态，不重复注册；帮助与真实键位同源，键盘可打开/关闭，失焦后不劫持系统按键。

首批限定主窗口，迷你播放器/桌面歌词不挂载键盘分发器。键位及验证结果记录在 [P0 记录](../validation/p0-validation.md)，新增功能不含系统热键、下一首优先规则或新匿名端点。

状态：首批已实现；当时为 199 项前端测试（其后批次增至 222 项 / 16 文件）、Go/vet、Windows 构建、32 项浏览器订阅回归和有限缓存媒体的原生 WebView2 按键回归通过。真实 IME/物理键盘布局及线上取流不由本次结果推定。切片 2-4 随后实现，见下方状态与台账。

### 切片 2：下一首播放（2026-11-06 已实现，独立队列行为变更）

复用单一 PlayerStore/音频执行器。保留重复歌曲，每个条目有唯一 `queueEntryId`；歌曲字符串 ID 只代表曲目，不能代替条目身份。

- 结构上整批插入当前条目后，多次命令以最新一次最靠近当前条目，批内次序保持不变。
- 仅数组插入不保证 shuffle 或自动 repeat-one 下真正“下一首”。实现前须确定模式交互：建议在同一 PlayerStore 内按条目 ID 维护一次性用户优先规则，手动 Next/自然 ended 按明确规则消费，不建第二个队列所有者或执行器。
- 优先规则必须定义重复命令、目标移除、播放失败、手动选曲、关闭/重启的行为及是否持久化；若持久化，显式更新快照契约/校验与存储文档。未确定这些语义时，只能交付诚实命名的“插入当前项之后”，不能承诺下一首播放。
- 空队列采用既有添加语义，选中条目但保持暂停，不自动开播；当前条目缺失等边界须有确定结果。
- 不替换当前条目、不重置位置、不打断进行中的播放解析；异步结果仍按现有身份/代际校验。
- hydration/closing、1000 条及 2 MiB 上限沿用既有保护；运行时修改前完成容量等预检，失败整批拒绝，不留下部分插入。
- 完整快照按现有 revision 串行保存；SQLite 原子拒绝旧 revision 不等于回滚前端。异步保存失败或冲突沿用“保留当前运行时队列并报告未保存”的行为，不静默覆盖较新磁盘快照，也不宣称自动回滚后续已生效操作。

验收覆盖三种现有模式的手动/自然切歌、一次性优先消费、重复曲目、多批插入、空队列、容量/旧 revision/持久化失败，以及在播/暂停/解析中状态。不得把模式语义变化藏在按钮实现里，也不为此增加新播放模式。

状态（2026-11-06）：`PlayerStore.playNext` 按本节实现——整批插入当前项之后、批内顺序不变，`pendingNextIds` 为会话级一次性优先队列；`select`/`previous`/`replace`/`hydrate`/`close`/`dispose` 清空，`remove` 剪除，插入前做 1000 条容量与歌曲 ID 校验；`next()` 先消费优先项，再走 repeat-one/shuffle。不持久化，重启后不保留（与本节"未确定语义前不写快照契约"一致）。仍缺：界面没有"已排队下一首"的标记，原生验收未做。

### 切片 3：命令入口复用与分享（2026-11-06 已实现菜单与分享，拖拽排序未做）

右键菜单复用前两片的稳定命令，保留可见按钮或键盘替代入口；覆盖焦点回收、点击外部、删除目标/切页后的失效命令。分享使用已有 ID 生成规范网易云 HTTPS 链接，剪贴板失败可反馈；浏览器只打开固定可信域名/协议，不接收任意 URL 或 shell 命令。

拖拽排序另排后续：保留键盘移动控制和焦点。播放队列用 `queueEntryId` 区分重复曲目；本地歌单沿用既有歌曲身份及歌单 revision，不把队列条目模型强加给歌单。两类排序分别验证，不跨实体复用写入协议。

状态（2026-11-06）：`TrackActions.tsx` 实现卡片/队列行的右键与"更多操作"菜单（Shift+F10、Escape、焦点回收、目标消失即关闭），`trackSharing.ts` 只把已校验的字符串 ID 拼成固定 `music.163.com/{song|album|artist}` 链接，并用 Wails 的 `Browser.OpenURL`/`Clipboard.SetText` 执行；不接收任意 URL。拖拽排序未做。

### 切片 4：排行榜完整匿名链路（2026-11-06 已实现代码，匿名预检仍缺）

先做上一节的 clean-anonymous 最小验证，按结果收窄端点和入口，再实现榜单 → 详情 → 资料 → 既有队列/播放。允许明确展示部分结果和服务暂不可用，不把公共榜单命名为每日个性化推荐。

验收：详情依赖可用、分页/资料有界、取消和迟到结果不污染页面、故障可重试；资料可查不保证有播放权益，取流完全复用原链。匿名失败记录阻塞事实，不引入登录或其他曲库绕过。

状态（2026-11-06）：`GetToplists`（`GET /api/toplist`）与 `GetPublicPlaylist`（`/weapi/v3/playlist/detail`，本页缺资料时再批量 `/weapi/v3/song/detail`）已实现：250 ms 间距、单一 discovery gate、12 秒共享 deadline、4 MiB 响应上限、100 条榜单 / 1000 条曲目 / 每页 30 条上限、不可信封面丢弃、`Partial`/`HasMore` 语义；前端有取消代际、迟到丢弃与重试。clean-anonymous 预检已执行（`loc=TH`，空 cookie jar、无兼容头）：`n:30` 返回全量 `trackIds`，100 首榜单在 offset 0/30/60 各返回 30 行且 `partial=false`，offset 990 越界不报错；证据与原始计数见 [P0 记录](../validation/p0-validation.md) 的"匿名目录 live 探测"。未覆盖其他地区、更多对象与 `-462` 实际触发。

### 切片 5：多类型搜索与站内专辑/歌手详情（2026-11-07 已实现）

按用户确认的范围落地，入口为**搜索结果卡片 + 右键菜单**，网页外链保留为次要项：搜索页加歌曲/专辑/歌手/歌单四类切换；专辑/歌手/歌单三类走同一 `/weapi/cloudsearch/pc`（type 10/100/1000），结果卡片可进入站内详情页；专辑/歌手详情走 `/weapi/v1/album/{id}`、`/weapi/v1/artist/{id}`，歌单复用既有 `GetPublicPlaylist` 分页。右键菜单新增"查看专辑/查看歌手"站内项，原外链改名"在网易云打开…"。

实现沿用单一 `PlayerStore`/音频执行器与 `usePlayerSelector`/`usePlayerStore`：`EntitySummary`/`EntitySearchResult`/`EntityDetail` 是浏览期视图模型、不入库；请求共用 discovery gate、250 ms 间距、12 秒共享 deadline、4 MiB 响应上限、30 条/页与 200 首详情上限，不带兼容头、不查 GD、不用 cookie jar；前端有取消代际、迟到丢弃、重试与字段级校验。播放 tick 不触发目录请求，也不整表重渲染。

仍按原计划未做：新碟/热门歌手的独立导航入口、相似歌曲、建议/热搜、YRC、时间偏移、主题、i18n、通知增强、播放模式。

其余相似歌曲、建议/热搜、YRC、时间偏移、主题、i18n、通知增强、播放模式按收益独立排期。此顺序是拆分建议，不要求为每个常规实现步骤重复申请批准。

### 待验证与待决策（2026-11-07 更新）

已完成（2026-11-06，`loc=TH`）：clean-anonymous 预检覆盖 `offset=0/30/60/990`，确认 `n:30` 返回全量 `trackIds`（100 首榜单 `total=available=100`），专辑/歌手/热门歌手/新碟/精选歌单端点均 200；计数与边界见 [P0 记录](../validation/p0-validation.md)"匿名目录 live 探测"。该结果只代表单地区单会话，不能外推。

已完成（2026-11-07，`loc=TH`）：切片 5 的 clean-anonymous 探测确认三类搜索各返回 30 项（total 592/84/462）、专辑/歌手详情分别解析出 1/50 首且封面与简介有效；计数见 [P0 记录](../validation/p0-validation.md)"切片 5"节。同样是单地区单会话样本。

仍开放：

- `-462` 的实际触发与前端提示（当前只有单元测试）；中国大陆与其他地区、更多对象的复验。
- 限流预算：目录侧只有同类请求的 250 ms 间距，缺跨端点总速率上限与"目录先降级、不影响播放取流"的明确顺序。
- 新增页面的原生 WebView2 验收：类型切换、实体卡片、详情页与右键菜单站内项均待实机验证；离线 harness 只覆盖既有播放/快捷键/焦点与排行榜失败态。
- 搜索结果歌单进入详情复用的 `GetPublicPlaylist` 未对"搜索歌单"单独做 live 探测。

待决策（实现下一批前定，不要留在代码里隐含）：

1. 目录浏览模型一律**不入库**（切片 5 已按此实现：`EntityDetail`/`EntitySummary` 不写库，`TrackSnapshot` 未扩字段）。
2. 目录请求需要一条全局速率上限，并明确"目录先降级、不影响播放取流"的顺序；现有 `discoverySpacing=250ms` 只是同类请求的起点（尚未实现跨端点总预算）。
3. 列表/详情缓存只做有界内存 LRU（含 TTL），不写入 `cache/resources`（那是封面/歌词）。
4. 新端点是否进入 `playback.jsonl` 白名单（`stage=discovery`）及字段范围；不记录 URL/Cookie/响应体。
5. 海外匿名目录整体不可用时的产品表现（隐藏入口 / 保留入口并明确不可用）。
6. 预检脚本落点（`cmd/probe` 或带 tag 的 Go 测试），且不得进入便携 ZIP。

已知未改的边界：`Partial` 会因 `trackCount > trackIds`（下架/地区限制）长期为真并显示"仅显示部分"；`playNext` 无"已排队"界面标记；发现页"替换队列并播放本页"仍是替换语义（按钮已改名说明）；`TrackActions` 仅在菜单打开时扫描队列判断目标是否仍存在。

## 6. 暂缓、排除与不变边界

- 账号相关继续暂缓：每日推荐、云盘、我的歌单、云端历史、我的电台订阅、心动模式、云端喜欢/收藏写入、会员认证与高级音源。匿名公共目录与个性化推荐分开。
- Last.fm 不需要网易云登录但需独立授权和上传；可选播放记录上报继续暂停，不默认发送本地历史。
- 下载/标签、本地文件库、原生音频/无缝播放是可选独立产品范围，不因缓存已有而开放；不能夹带自动开播。
- 用户导入/导出/备份/恢复及旧产品数据迁移明确不做；本工程正常 schema 升级和更新保留数据按既有要求验证。
- 安装器继续暂停，交付只做 portable ZIP；便携更新/代码签名是有条件后续事项，不误写为永久禁止。
- CDN 改写/no-cache 尚未实现，只能独立候选实验；真实 VPN/Tailscale/中国大陆出口对照继续暂缓，不修改系统代理、DNS 或 Hosts。
- TUI 的 AltScreen、Kitty、主题 TOML、终端动画及 Linux MPRIS/macOS 专用能力不直接移植；GUI 按 OTONEI 实际 React 布局和交互派生。

一个 Go 模块、Wails 3、React、SQLite、一个前端队列所有者和一个音频执行器保持不变。Netease ID 保持字符串，不新增本地 HTTP API 进程、Firebase、UNM 或其他曲库。

来源顺序保持：合资格已验证本地缓存优先；自动模式已知 CN 仅普通、已知海外每档直接精确端点兼容、未知才普通后兼容；always/off 各自语义不变，冷却不让已知海外暗退普通。选定网易云路径失败后才允许已启用且限速的 GD `source=netease`；完整可播流立即接受，不追码率、不迟到换源、不向 GD 加兼容头。

go-musicfox GPL 应用代码仅作行为参考，不复制业务/TUI/播放器代码。既有合资格 `netease-music 1.6` MIT 协议范围及署名以许可清单为准，不因参考库存新增依赖，也不复制 GPL 扩展。媒体 URL、Cookie、Token 不进入快照、日志或 Git。

## 7. 后续实现的验证与文档收口

各切片实现时运行相应 Go/前端测试及 Windows 构建；涉及原生快捷键、窗口、通知或媒体会话时另记录真实 WebView2/Windows 验收。浏览器 mock 只证明受控前端行为，不能冒充原生通过；新端点逐项区分 live/mocked/optional/failed/untested。

按变更属主同步架构、存储契约、适配器、许可和 P0 证据；不为无关文档机械改动，不以计划状态宣布 P0 完成。2026-11-05 的文档评审仅核对本地链接与源码锚点；2026-11-06 首批实现的测试、Windows 构建及原生回归另按实际结果记录，不替代新端点的在线探测。

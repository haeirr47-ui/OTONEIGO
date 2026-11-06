# 本地存储协议

2026-11-06 主窗口快捷键仅调用已有音量、收藏和队列/播放操作，沿用各自的串行保存、失败提示和关闭握手；键位及帮助开关不持久化，不新增表、schema 或用户数据导入/导出接口。快捷键设置音量与界面滑条共用现有设置合并逻辑，收藏仍按完整字符串歌曲 ID 成员集合处理。

> 2026-11-02 / 0.5.3 源码同步；验收结果见 [P0 验证记录](../validation/p0-validation.md)。

默认数据库 `%LOCALAPPDATA%\OTONEIGO\library.db`，WebView2 用户目录为同目录的 `webview/`，固定字段诊断在 `log/`。进程级 `OTONEIGO_DATA_DIR` 可隔离测试。新工程不访问旧 OTONEI 用户数据，不提供导入、导出、备份或恢复接口。

SQLite 使用 modernc.org/sqlite v1.61.1、单连接/写锁、WAL、synchronous=FULL和busy_timeout=5000。同一目录单实例；当前PRAGMA user_version为 **7**。0..6的本工程库在同一事务建立缺失表/索引与派生检索字段、补桌面默认字段，再推进7；敏感回填前先隔离逻辑坏记录，合法记录继续升级。归档、删除活动坏行、默认设置及DDL/版本推进一起提交或回滚。高于7、物理或SQL错误仍拒绝打开。旧版本遇到任意逻辑坏行即整体退出的合同自0.5.2起被隔离恢复取代；不是旧产品用户数据迁移。

| 表 | 当前协议 |
| --- | --- |
| `queue_snapshot` | 固定主键 1、revision、完整 JSON metadata；UPSERT 仅接受严格更大的 revision，否则 `STALE_QUEUE_REVISION` |
| `local_settings` | 固定主键 1、JSON metadata；无记录返回默认设置，不隐式写入 |
| `local_favorites` | 字符串 song_id 主键和完整 TrackSnapshot；search_text 为本地派生检索字段；added_order 单调增加、最近加入优先、song_id 稳定破同序；更新已收藏元数据不改变顺序，取消后重新收藏排到最前，无需远端补齐 |
| `local_history` | entry_id 主键、played_at、HistoryEntry metadata、search_text；按时间及 ID 降序；插入幂等，不再自动删除旧历史 |
| `search_history` | canonical_query 主键、显示 query、searched_at；Unicode 小写去重，最近使用优先，无自动裁剪 |
| `local_record_quarantine` | source_table、原rowid、固定原因及无亲和性的原SQL列值；只经计数桥接，不把原文写日志；显式清空历史同时清除历史隔离记录 |

TrackSnapshot 只含字符串网易云 ID、名称、歌手/专辑、时长和无查询参数的 HTTPS 网易云封面引用。媒体 URL、解析结果、Cookie/token 不属于库或队列协议。库 metadata 上限 128 KiB；本地收藏/历史/设置读取允许未知成员，但拒绝尾随JSON、非法UTF8、超限或无效已知字段/身份；异常原值保存在隔离表，不静默丢弃。设置更新合并保留未知值，无法安全改写的收藏或冲突设置明确拒写。

2026-11-05 选择性订阅调整：完整收藏成员集合以 `PlayerState.favoriteIDs: ReadonlySet<string>` 暴露给前端选择器，初始化读取完整 ID，收藏写入成功后复制替换集合并通知。已有快照保持不变，失败写入不提前改变成员资格；当前页 `favorites` metadata 与完整成员集合分别负责分页展示和爱心判断。此集合不写入队列 JSON，不新增数据库表、schema 或浏览器持久化；SQLite 仍是收藏关系的持久化来源。

2026-11-06 匿名目录与队列操作：`Toplist`/`PublicPlaylistPage` 只是浏览期视图模型，不写库、不进队列快照；`playNext` 的优先顺序只存在于内存（`pendingNextIds`），重启即失效。schema 仍为 7，未新增表或字段。

队列项使用独立 `queueEntryId`，允许同一歌曲重复；含当前 entry/index、循环模式、整数毫秒位置和 JS 安全整数 revision。校验最多 1000 项、2 MiB JSON、字段长度、ID、位置和当前项一致性。前端串行保存完整快照，队列编辑立即提交，进度最多每 5 秒保存一次，暂停保存当前位置。

启动先 LoadQueue；新库 null 表示空队列但不写入，读取失败禁止默认空队列覆盖。恢复保持暂停；用户播放时重新解析地址，加载媒体元数据后恢复位置。本地库和设置单独 hydrate，读取失败会报告错误，不把默认设置自动写回。GD capability 已按有限 MP3 原生样本开放，用户设置默认仍为 false；代码仍在 capability 未开放的构建中归零库内 GD 开关，AppService 拒绝启用未获准能力。

设置保存 `preferredQuality`（手动 exhigh 及已验证会员的 lossless/hires/jyeffect/sky/jymaster）、`regionMode`（auto/always/off）、`gdEnabled` 和 0..1 的音量；默认 exhigh（320 kbps 优先）/auto/false/1；旧 standard/higher 及未确认会员的高级首选仅在运行时视图归一为 exhigh，GetSettings不写盘；高级原始偏好在匿名音量/窗口等保存及启动时保留，可信会员明确更改时才替换。实际取流仍按已验证资格限制，不因保留偏好开放高级播放。另保存 audioCache（默认 enabled=true、maxBytes=20 GiB）、resourceCache（封面/歌词共享，默认 enabled=true、maxBytes=1 GiB）与 closeToTray（默认 false）；两类缓存容量各允许 64 MiB..1 TiB。旧设置缺少 resourceCache 时读取归一为默认值，保存兼容旧调用时保留已有配置。国家观测、网络 generation 和媒体内容均不入 SQLite。

HistoryEntry 含独立 historyEntryId、TrackSnapshot、playedAt、listenedMs。同 ID 重试幂等并保留原记录；页大小 1..200；界面使用 (playedAt, historyEntryId) 复合游标向前加载，返回 hasMore，新插入的历史不使后续页重复或跳过。兼容 offset 接口只接受非负 JS 安全整数，不再以 5000 为上限。PlayerStore 仅在真实 `playing` 状态累计至少 500 毫秒有效媒体时间增量后提交，每次播放尝试最多一条；seek 跳变不直接当作收听时间。该记录是本地播放证据，不等于完整播放、听歌计数或网易云上报。支持清空本地历史。

已提交事务是耐久边界。实际退出冻结交互，前端先等待 hydrate、暂停，再 flush 已排队保存并向 Go 确认；原生 CloseGate 最多等待 3 秒后兜底关闭数据库。确认只表示保存尝试结束，不代表全部成功；超时及强杀只保证此前已成功提交的状态，JS 中尚未交给 Go 的操作不保证保留。2026-10-31 旧 PoC 的 revision 13 强杀恢复记录仍是当日历史证据，不替代当前 schema 7 的验收；0.4.0 本工程 schema 4→5 合成 fixture、当前原生强杀恢复和便携覆盖更新已分别验证。离线可显示文本元数据；有效资源缓存可提供已缓存的封面/歌词，未缓存资源及绕过本地缓存的格式仍需网络。


`local_playlists` 保存本地歌单 ID、名称、revision、创建/更新时间、曲目数及有序 metadata，revision CAS 防止过时覆盖；不保存媒体地址。歌单编辑/删除不更改已复制到播放队列的歌曲，离线重启仍可读。收藏没有硬性条数上限；仍保留最多 100 个本地歌单、单歌单/队列 1000 首、队列 2 MiB 与库 metadata 128 KiB 等业务保护；历史不再自动裁剪不等于所有本地库配额已移除，不把这些上限解释为 SQLite 限制。

## 桌面选项与窗口位置（0.5.0）

`local_settings.metadata.desktop` 保存置顶、歌词锁定、字号/透明度、曲目通知、任务栏进度和 mini/lyrics 位置。两种置顶默认 true，歌词锁定 false，字号 28（16..64），透明度 0.9（0.3..1），通知默认 false，任务栏进度 true。`WindowPlacement {valid,x,y,width,height}` 为 DIP；默认 valid=false，mini 420×176、lyrics 820×156。完整字段协议见 [桌面窗口协议](desktop-window-protocol.md)。不保存浮窗可见性、投影、音频 URL、完整歌词或通知内容；默认仅主窗口启动。

`SetSettings` 与 `SaveDesktopPlacement` 共用 AppService 设置锁，在锁内读取数据库最新设置并合并后提交。普通设置写入保留最新原生位置；缺少 desktop 的旧调用保留既有桌面选项；原生位置写入仅修改对应 placement，保留其他选项与另一窗口位置，防止并发更新互相覆盖。提交成功后才更新运行时选项，投影 options 使用 Go 的已提交值。

几何由已就绪原生窗口的移动/缩放事件读取 Bounds，350 ms 去抖；重置/后续更新以 generation 作废旧保存。恢复按屏幕 WorkArea/DIP 夹取并显式设置 `WindowXY`，负坐标不归零。最终关闭在数据库关闭前取消待保存任务、同步读取已就绪浮窗 Bounds 并尝试 flush；这与主前端最多 3 秒的队列/库/设置关闭握手互补。强杀仍只保证已提交事务，不保证最近去抖操作。schema 5→6 是本工程内部事务演进，不提供用户迁移/导入导出或备份恢复。

上述为 0.5.0 源码契约；本页下述 0.4.0 耐久/覆盖更新记录保留原条件，不作为 schema 6 或桌面窗口原生通过声明。

## 磁盘音频缓存

`cache/audio/` 与数据库分离，包含专用 `manifest.json`、完整 `.blob` 和临时 `.part`。键区分字符串 song ID、provider、实际质量、格式、码率及账号 scope；manifest 只记录这些身份/质量、长度、时长、SHA-256、最近访问和散列账号 scope，不存媒体 URL、Cookie/token 或 loopback lease。库与队列仍只保存文本快照，恢复播放先查合资格已验证缓存；未命中才按当前地区/设置选择网易云普通或兼容路径，再在失败时考虑独立启用的 GD。已确认海外的 auto 与 always 从首请求及所有降级档直接兼容，不做普通预请求；unknown 保留普通后合资格兼容。缓存命中资格不随本次网络顺序修正而放宽。

完整下载经过长度、音频结构/时长校验和 SHA-256 后提交；命中检查大小及有界结构；有分块索引时每个交付块先核验摘要，旧记录先完整hash再升级。不会每次重启对已索引大文件全量hash。启动清理残留 partial 和无 manifest 的孤立 blob，未完成音频不作为离线命中。0.3.0 历史批次真实 CDN 原生渐进 MP3 与完整缓存提交/热播/Range seek 已验证，进程级不可达代理下离线重启、零音频 API 请求启动与 91 秒 seek 也已验证；受控自生成 FLAC/WAV 原生解码通过不证明其缓存传输或真实会员音源。普通低档 MP3 可按公共范围使用；高级媒体需当前可信资格匹配已散列账号且仍为会员，当前账号入口仍禁用。

默认 20 GiB、LRU 淘汰；活动下载预留容量，新下载检查可用空间并保留至少 64 MiB 余量。活动媒体受 pin 保护，普通已加载媒体暂停保留源/元数据，浏览器响应可能继续占用文件；手动清理会报告仍在使用的项并延期删除，完整源复位/释放后重试，删除失败不提前扣减物理字节统计。容量缩小也不打断活动播放。缓存可关闭/清理，不提供用户文件导出、备份或恢复。

0.3.0 历史离线原生验收恢复已持久化本地库（历史超过 5000 条）、启动暂停，缓存启动 176 ms 且音频 API 增量为零；关闭缓存/清理不打断播放。清理报告保留一条活动媒体，最终缓存归零包含完整源复位，不能归因于普通暂停立即 unlink。该证据采用进程级不可达代理，不代表真实 VPN 或物理断网。

## 整库检索、分页与关键词历史

`search_text` 由 Go `strings.ToLower` 对歌曲名、全部歌手名和专辑名以换行连接后生成，插入/更新 metadata 时同步写入；不是依赖 SQLite ASCII lower。查询需有效 UTF-8、去除首尾空白后最多 512 字节，空查询列出全库。SQLite `instr(search_text, ?)>0` 做字面子串检索，`%` 和 `_` 不作通配符；不是全文分词或拼音搜索。

`SearchLocalFavorites` 使用 (added_order 降序, song_id 升序) 游标，`SearchLocalHistory` 使用 (played_at, entry_id) 降序游标；页大小 1..200、额外读取一条判断 hasMore。UI 仅保存/渲染当前页 metadata，并保留翻页游标；改变关键词重置游标，取消与 generation 拦截迟到响应。游标保证稳定排序下的边界，不能视为跨多页冻结数据库快照。`GetLocalFavoriteIDs` 返回全部字符串 ID 供成员判断，不返回全量 metadata。

关键词 `RecordSearchQuery` 只记录成功的非空搜索；`canonical_query` 为 Go Unicode 小写值，重复使用更新显示词和单调递增时间，按最近优先。`ListSearchHistory` 允许 1..100，当前界面显示最近 20 条；支持 `DeleteSearchQuery` 和 `ClearSearchHistory`，数据库不自动裁剪。写入串行化，删除/清空推进 generation，旧搜索响应不重新写回；退出 flush 包含已排队关键词保存。它与实际收听的播放历史是两种独立数据。

## 封面与歌词磁盘缓存

`cache/resources/` 独立于 `cache/audio/` 与数据库。manifest 存散列键、种类、SHA-256、大小和创建/访问时间，不存源 URL、歌曲 ID 或凭据；缓存文件是散列名 `.bin`，临时写入后原子提交。目录寿命锁避免多个管理器互相覆盖；启动剔除损坏/孤立文件，命中重新检查摘要和内容，清理不删除曲库或音频缓存。默认封面/歌词共享 1 GiB，LRU、最多 10000 条、4 MiB manifest 和 32 MiB 磁盘余量共同约束写入，不能把容量设置理解为无限条数。

封面限可信 HTTPS 网易云引用、4 MiB、单边不超过 8192、总像素不超过 32 Mi，并须完整解码 JPEG/PNG/GIF；不支持本地解码的 WebP/AVIF 回退可信原地址。资源通过现有 Wails 同源 `/artwork/<hash>` 读取已校验 buffer、使用 no-store，不新增 listener。库快照始终保存原远端封面引用。缓存获取在已通过无 query 白名单校验的原始封面引用上，仅为内部网易云显示图请求添加固定 `param=1024y1024`；输入引用仍拒绝 query，快照、回退和缓存键仍使用原始 canonical 引用。响应依据字节完整解码而非 HTTP MIME 判定。

歌词 JSON 最多 1 MiB，非空有效缓存立即返回；七天后异步刷新，失败保留旧值并节流五分钟，空结果不作永久缓存。封面/歌词 worker 分别为 4/2，全局 64 个在途任务、封面最多占至 60 的提交门槛；清理/关闭防止旧任务重新写入。缓存关闭后普通联网仍可用。0.4.0 当前版本的在线/离线资源、实际强杀恢复和便携覆盖更新用例已通过，具体条件见 [P0 记录](../validation/p0-validation.md)，不借用 0.3.0 历史结论。

## 0.4.0 有限耐久与资源证据

`output/playwright/library04-online-report.json` 验证 6010 条收藏、超过 5000 条历史的整库 Unicode/字面 `%`、`_` 检索与关键词 UI。真实冷音频在 debug 读取节流 80 ms/32 KiB 下 3144 ms 启动，此时完整缓存为 0、活动下载为 1；最终完整缓存 9,111,405 字节、81 秒 Range seek、单 Audio。它证明真实字节渐进传输，不是无节流性能指标。真实同 canonical 封面经内部固定显示参数取得 PNG 1,863,113 字节/1024×1024；原图 6,050,435 字节。清理后同散列资源实际 404 触发一次错误，创建新 img DOM 后恢复解码；音频和曲库保持可用。

`library04-offline-report.json` 在进程级代理 127.0.0.1:1、无网络拦截下验证缓存资源读取 308 ms、音频启动 184 ms，普通/兼容/GD 音频请求增量为零；6010 收藏、超 5000 历史、关键词、歌单/设置保留，队列暂停恢复 124000 ms。这不等于物理断网或 VPN 对照。

`library04-precrash-state.json` 与 `library04-recovery-report.json` 记录实际自有进程在部分音频 163840 字节、完整缓存 0/活动下载 1 时被强制终止；重启精确恢复已提交 revision 44、位置 3000 ms、两队列项，未完成音频清至零字节/零完整缓存，已缓存封面与歌词仍可用。SQLite integrity、schema 5 与原始存储隐私校验通过；不能外推未提交 JS 操作或断电文件系统行为。

`output/packaging/portable-update-0.4-report.json` 验证旧 payload 文件先复制再由新 ZIP 覆盖，替换期间数据库哈希不变；未运行真实旧版本（oldVersionLaunched=false）。解压出的 0.4 production EXE 离线显示并完成初始化，经 WM_CLOSE 正常退出码 0；Go/SQLite 核对 6010 收藏、6012 历史、1 歌单、1 关键词、队列 2 项/3000 ms/revision 60。schema 4→5 另以合成旧 schema fixture 验证，不把文件覆盖当成真实旧应用升级启动。上述资源/耐久样本采用旧网络路由；新海外直接兼容由独立 native-egress04 报告证明。


## 0.5.1 元数据写入频率与纯读

本批无schema变更，仍为6。GetSettings不保存、不触发SettingsChanged；原生位置与普通设置提交继续共享锁，回调以锁外单一通知队列保持最终顺序并允许重入。音频缓存最近访问只影响LRU：热命中不逐次fsync，后台最多每5秒合并并在Close尝试flush；崩溃可能丢最近访问次序，不能据此声称丢失已提交音频。新blob、失效、清理仍同步提交manifest，失败删除保留真实字节记账。

seek的250ms合并仅减少位置快照，不延迟音频定位；暂停、关闭、换曲等处理待提交位置，revision仍串行。收藏和历史写操作不清空当前列表或重置到首屏；初始化失败的重试包含收藏ID及两类首屏，全部读取成功才允许写入。新增真实SQLite/原生与并发回归条件见[核实记录](../audits/audit-fixes-0.5.1.md)，不替代旧批次强杀或物理断电验收。


## 0.5.2 坏行、扩展字段与分块记录

首次从旧schema升级可一次性分批校验原记录；已经7的Open只检查单行设置，库读查询按100行恢复坏记录并继续填健康页面，避免随长期历史每次全扫。归档使用SQL INSERT SELECT原样复制NULL/BLOB/超长值，再在同事务移出活动表；不将超长内容先装入Go或截断。设置的正常GetSettings保持纯读，异常设置恢复发生在Open事务。RecoverySummary累计计数，前端只看计数/恢复标志和固定提示。

未知字段容忍仅改变本地metadata已知投影读取；类型、身份和容量防护保留。设置递归合并已知字段的大小写别名及未知后代，冲突未知标量以LOCAL_METADATA_REQUIRES_NEWER_VERSION拒写且原文不变。已有收藏含未来字段时也拒绝盲目upsert；历史幂等写不覆盖原行。有效收藏upsert前检查完整旧SQL行及排序值。手动清空历史事务删除活动与隔离历史，收藏/设置隔离内容保留；失败全部回滚。不新增用户恢复/导出功能。

音频manifest新增最多512个1MiB块摘要，完整提交和旧记录完整校验时同一遍读取构建；文件长度、结构和当前资格仍验证。索引读取只复制hash成功的相同缓冲区，未读段不预扫描。首块损坏410，后续块损坏中断并排除持久记录。旧无索引记录一次全量升级并成功保存后，下次进程可分块；失败保存不承诺免除下次全读。manifest有界读取/保存上限128MiB，拒绝超限覆盖有效旧清单。最近访问仍最多每5秒合并落盘，内容提交/失效同步；库快照仍不保存媒体地址。来源与精确证明见[审计记录](../audits/audit-fixes-0.5.2.md)。


## 0.5.3 访问增量侧车

schema仍为7。音频entry增加可选内部Generation；recency.json只保存相对结构基线改变的key→Generation/BaseAccess/LastAccess，最多65536条、16MiB。只对当前存在且身份/基线匹配的条目覆盖更近访问时间；损坏、过大或旧侧车忽略，不恢复已删除音频。后台及Close热访问保存不包含Chunks，不改主清单；增量会随自最近结构保存以来访问的记录数增长，非每次恒定单记录。

新下载提交、删除、旧块索引升级、启动结构保存仍同步写完整manifest。结构写失败保留structuralDirty，reaper按至少5秒重试，Close优先补存；成功后更新基线，使旧侧车自然失效。测试包含阻塞结构写入后真实下载完成、解除故障后Close/reaper与重启保留。原始隔离记录不因前端“知道了”而改变，会话签名不写数据库/浏览器存储。见[0.5.3证据](../audits/audit-fixes-0.5.3.md)。


## 2026-11-03 维护补充

音频和资源缓存的进程生命周期目录锁已统一到 `internal/dirlock`，文件名与非阻塞互斥语义保持不变，不改变 schema 7 或缓存数据格式。异常任务只在可安全丢弃结果的网络/解码边界隔离；内部状态异常不会继续写缓存。独立崩溃记录最多五份、单份 16 KiB，位于数据目录的 `log/crash-*.txt`，不包含原始 panic 文本、地址或凭据。详细策略及不能保证记录的情况见[维护加固](maintenance-hardening.md)。

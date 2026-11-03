# P0 许可与来源记录

2026-11-03：补丁更新 SQLite 1.61.1、coder/websocket 1.8.15、godbus 5.2.4；Wails/runtime 3.0.0 与 x/sys 0.49.0 保持。共享目录锁和崩溃记录为本工程独立代码，未引入新依赖或复制参考项目代码；本批构建需重新生成通知清单，旧 ZIP 仍代表其历史版本。策略和来源见[维护加固](maintenance-hardening.md)。

> 2026-11-02 / 0.5.3 源码同步。下述既有审计结果保留原批次含义；0.5.0 的最终构建、审计和 ZIP 证据另行记录，不能由历史通过外推；0.4.0 最终 production 构建、依赖审计及随包通知/ZIP 哈希核对已通过，以 [P0 记录](p0-validation.md) 为准。

新工程以 MIT 许可维护，保留 [根 LICENSE](../LICENSE) 的版权声明。原项目目录保持只读并由 Git 忽略，不作为新仓库源码整体纳入。

| 来源 | 当前使用范围 | 许可与处理 |
| --- | --- | --- |
| 本地 OTONEI v1.8.0 参考副本 | App/导航/搜索卡片/桌面播放器展示适配、九份完整主题布局样式、AudioEngine 模式与原版 SVG 资产 | MIT；适配文件及 `frontend/THIRD_PARTY_NOTICES.md` 标注来源并保留完整通知 |
| 本地 go-musicfox vendor 中的 netease-music v1.6.0 | 最小 WEAPI 协议常量、接口路径与参数参考 | MIT；标准库重写，完整上游通知见 `netease-adapter.md`；该 module 不进入依赖 |
| go-musicfox 主程序 | 不复制 TUI、播放器、队列或业务实现 | GPL-3.0；不进入新工程 |
| NetEaseMusicWorldNext 本地参考副本 v1.0.0 | 仅核对请求头、CDN 候选及启用/关闭行为，独立 Go 实现受限取流 header，CDN 候选仍未实现 | GPL-3.0；不复制源码，不安装/打包 Chrome 扩展，不作为 Go/前端依赖 |
| Wails 3.0.0、React 19.3.1、SQLite driver 及传递依赖 | 桌面运行时、UI、数据库 | 版本锁定；依赖与许可文件清单由审计脚本记录 |

精确本地参考文件 SHA-256 见 [source-provenance.json](source-provenance.json)。原方案中的远程提交号是规划阶段的证据，不视为当前本地归档逐字来源的证明；没有把顶层 GPL 项目的许可当成 vendor 内部独立 MIT 项目的许可。

2026-11-03 校验同步了清单的 `reference/` 前缀，未改原哈希：当前归档中现存 36 份文件均匹配。归档 `OTONEI/public/` 下的 logo.svg、MiSansVF.woff2、default_cover.svg、lyric.svg 四份原件当前缺失，清单对这 4 条标注 `"missing": true`；产品 `frontend/public/` 保留的同名副本均与历史哈希一致。后者不能替代“参考原件仍存在”的证明，本批未补写或修改参考目录。

第三份参考的实际入口是 `background.js` 的动态规则和 `content-script.js` 注入的 `page-script.js`；`rules.json` 没有被 manifest 声明为静态规则资源。它没有实现公网出口地区自动检测。仅参考公开协议机制，未复制 GPL 实现；独立 header 和 GD 的有限媒体样本证据不代表普遍播放权限或服务可用性；具体范围见 [海外 P0 方案](overseas-compatibility-p0.md)。本轮未增加相关运行时依赖。

`scripts/audit-dependencies.ps1` 检查 `go list -deps ./...`、`go mod graph` 和可执行文件 build info，拒绝 UNM、Firebase、go-musicfox 主程序及原网易云库依赖。0.3.0 批次历史验证保留；0.4.0 最终二进制已重新通过该门禁及 13 份 Go 根许可通知检查。审计实际二进制使用的 Go module 根许可文件，输出 `output/p0/license-inventory.json`（当前 13 份许可文件）、依赖图和 build info，同时把完整通知整理到 `bin/THIRD_PARTY_NOTICES.txt`。这些生成物保持 Git 忽略。

这是 P0 的源码来源、依赖排除和许可保留记录。模块根许可文件清单不等于对每个嵌入文件、WebView2 分发许可或音乐服务条款的完整分发审计。当前仅交付便携 ZIP；安装包工作已按用户要求暂停，历史安装器不作为当前产物。0.4.0 最终包已在最后构建后重新生成并完成四文件白名单/输入哈希核对，没有代码签名、上传或远程发布。ZIP 包含应用及实际运行依赖通知，不包含 NSIS 运行时；打包边界和剩余检查见[打包说明](packaging.md)。


本次 UI 修正保留 MiSans 字体原始文件和小米独立许可（public/licenses/MiSans-License.pdf），字体不归入 MIT，也不做子集或改字形。react-icons 5.7.0 的 Font Awesome 图标遵循 CC BY 4.0，Material Design 图标遵循 Apache 2.0；来源和许可通知见 frontend/THIRD_PARTY_NOTICES.md 与随包 notices。审计新增实际 npm production 依赖清单（当前五项，Wails runtime 对应同版本 Go MIT 通知），以及 Material 图标完整 Apache 2.0 文本；13 份 Go 根许可记录仍单独保留。

0.4.0 的资源缓存、SQLite 整库检索/分页与关键词历史沿用现有 Go 标准库、SQLite 和前端依赖，未新增依赖或复制参考项目代码。JPEG/PNG/GIF 使用 Go 标准库解码，WebP/AVIF 保留可信原图退路，不为格式支持引入第三方解码器。缓存的封面/歌词和音频内容不转为本项目 MIT 资产，也不随 portable ZIP 分发；来源和播放权限边界保持原契约。既有 MiSans、图标与 MIT 来源通知继续保留；0.4.0 最终审计及 ZIP 白名单/哈希核对已完成，具体产物见[打包说明](packaging.md)。

0.5.0 的迷你播放器、桌面歌词和主窗口设置继续适配既有 MIT 展示来源、MiSans 与图标资产，原通知保留。桌面投影、SQLite 设置升级、原生托盘提示及任务栏 COM 沿用既有 Wails、Go 标准库和已锁定 Windows 依赖，未增加依赖或复制 GPL 参考实现。通知使用应用自有托盘，不新增 toast 注册项或随包服务；portable ZIP 仍是唯一交付范围。此处记录源码来源与实现边界，不声明新增原生验收、0.5.0 最终依赖审计或打包已通过。


0.5.0 最终 `scripts/verify.ps1` 已重新完成实际 production 依赖/许可门禁，仍为13份Go根许可文件；ZIP仅四个显式白名单输入且逐项哈希匹配。桌面协议、任务栏COM与自有托盘通知未增加依赖，未采用注册toast组件；原生代码独立实现，不复制GPL参考代码。产物与精确条件见[打包说明](packaging.md)及[P0记录](p0-validation.md)。


0.5.1缓存/状态审计修复未新增依赖，仍使用Go标准库与已有x/sys Windows文件共享能力。未复制GPL参考代码。NSIS目标文件写访问保护仅修改暂停的安装器源码，不编译/运行，不进入portable ZIP；没有把安装器未验收状态说成通过。本批实际production依赖门禁、13份Go根许可和ZIP四文件/哈希重新核对，精确证据见[核实记录](audit-fixes-0.5.1.md)。


0.5.2的块摘要、资源懒校验、坏行隔离和轮转重试仅使用已有标准库/SQLite依赖，未引入新依赖或GPL参考代码。原始异常记录留在本地私有数据库，只输出固定计数和原因；它们不是分发资产。production、13份Go根许可及最终四文件ZIP通知重新核对，见[0.5.2审计](audit-fixes-0.5.2.md)。


0.5.3独立工作池、会话签名、计时控制器、一次缓存接纳重试及访问时间侧车未增加依赖或复制GPL代码；沿用标准库和现有React。更新production/13份Go许可与四文件ZIP核对，见[0.5.3审计](audit-fixes-0.5.3.md)。

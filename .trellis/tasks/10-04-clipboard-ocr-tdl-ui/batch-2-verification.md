# 第二批实现与验收记录

日期：2026-10-05。分支 `creepy-kiwi`。第二批四个模块已实现、接入应用并完成本地验证，进入 **review**；真实外部依赖验收仍未完成。未提交、push、创建 PR、合并或部署。第一批与更早的运行修复均保留。

## 已交付行为

| 模块 | 当前实现 |
| --- | --- |
| 多图 OCR、规则、AI | 同一作品最多 10 图，浏览器内串行识别简中/繁中/日文/英文，排序、校对、取消和重试；版本化有限规则；可选已保存 AI 配置生成候选 |
| 书目来源 | MangaBaka、MangaUpdates、Bangumi 独立配置和搜索/详情；支持的授权分别加密保存；逐字段核对与显式自定义字段映射 |
| Telegram | 管理员保护的账号页、QR/2FA/helper 状态机、跨进程单账号锁；单条消息 ZIP/RAR/7Z 附件进入 Task Core，冻结账号身份和版本 |
| ComicInfo | 固定 2.1 draft profile；原值保留与明确清空、自然页序、图像字节/CRC/SHA256 核对；有效文档与提交文档分开；原件及不可映射信息私有留存 |

主页连接了上述模块和统一元数据编辑器；设置页可管理规则、来源、AI/模型与自定义字段。字段不限于七项。采用候选前重新核对字段定义及对应来源/规则/AI配置版本，手工锁、明确清空和迟到响应仍由共享 draft 契约处理。

AI 接口可以配置、发现并选择模型；**当前实际提取只支持已经实现精确预算协议的固定 llama.cpp commit** `11fe02151f79c41d0d4af7da708755d73b9c0da6`。其他兼容接口发现/中性测试成功不等于提取可用。没有静默截断、换模型或自动发送截图。

迁移 018–022 依次接入账号、Telegram 输入、保留清单、失败/取消时保留记录与打包报告；未修改已经应用的 015–017。Docker 同时构建官方 tdl 0.20.4 与独立 helper，同源发布已锁定的 15 个 OCR 资源。

## 实际验证

验证使用任务专属 PostgreSQL 16，以及临时管理员、数据库、下载目录和会话/留存目录的隔离 Compose 栈。未读取生产库、真实频道正文或用户凭据。

| 检查 | 结果 |
| --- | --- |
| `go test -p 1 ./... -count=1`，带隔离 `TEST_DATABASE_URL` | 通过 |
| `go test -race -p 1 ./... -count=1`，同一测试库串行 | 通过 |
| `go vet ./...` | 通过 |
| 独立 helper 模块 readonly race/vet、Linux amd64/arm64 构建 | 通过 |
| 最终前端 Node 测试 | **166/166 通过** |
| ESLint、Vite 生产构建、临时 Git index 的 dist 一致性 | 通过；真实暂存区未改变 |
| OCR 资源哈希、大小、许可证归属核对 | 15/15 通过 |
| 四语言真实 Chromium Worker/WASM、缺资源失败、缓存后离线识别 | 通过；干净合成图，非用户截图 |
| 固定 XSD + 实际 `xmllint --nonet` | 2.1 输出通过；扩展输出不符合 2.0，核心兼容样例通过 2.0 |
| Docker API/worker/helper/tdl 完整构建、隔离栈启动 | 通过 |
| 浏览器生产页面 | **20 项均有通过记录**，包括最后 2 项 OCR/鉴权专项补跑 |
| 最后新增 worker 原件清理边界 race 回归 | 通过 |
| `git diff --check`、四个任务 context 校验、新规范本地链接 | 通过 |

发布脚本在全套 Go、race、vet、helper、资源、前端检查后，因为并行审查最后一处前端修改引起 dist 变化而退出。同步构建产物后，从产物检查和 E2E 继续执行，未虚报原脚本一次退出成功。最后全套浏览器运行通过 19 项；OCR 用例当时误把页面启动时的既有 htmx CDN 请求计入识别请求，收窄至识别开始后再专项补跑 2/2 通过。页面仍从 CDN 加载 htmx，不能据此声称整站离线或零外域请求。

浏览器实际证明：两图生产 OCR 合并、保存规则提取、经版本检查采用候选、人工标题不被预览覆盖、识别阶段零外域请求且未上传图片；匿名不能访问新接口和 OCR 资源；自定义字段保存/重载/任务历史完整；真实上传经过 PostgreSQL 与 worker，保留原出版社和 Notes、删除明确清空的 Summary，生成自然页序且图片字节不变的 CBZ，再复制到测试 Komga 目录。最后一项是文件复制，**不是 Komga 服务导入**。

来源设置浏览器用例对持久化和重载使用真实 API，仅对外部连接测试响应使用受控夹具。三个来源的匿名中性搜索和详情曾通过真实 Go adapter 验证，见独立报告；两类证据不混用。

## 审查修复

- 对照固定 llama.cpp 源码修正 native completion 不存在 `generation_settings.n_ctx` 的假设；分词加入模板特殊 token，推理使用同一 token 数组，并限制整个提取为一个 120s deadline。
- 修复模型私有配置 Handle 的格式化泄露路径，Go/JS 规则标签归一与 null/重复标签校验一致。
- 修复字段重载丢失未采用 OCR 内容；实际采用动作重检权威版本，取消/卸载后迟到检查不能修改草稿。
- 私有留存先复制并登记任务代际，再清理 Telegram 临时源。取消、元数据解析失败和打包失败均有回归；登记冲突或数据库错误不得提前删源。
- 打包拒绝覆盖已有产物时，失败清理不能删除已有文件。最终发布与源留存采用代际约束。
- 浏览器验收修正过期占位预期和选择器，来源连接范围改为有限中文文案，未知上游文本不回显。

## 视觉核对

真实生产页面测试覆盖 1440、768、390、360px，无横向溢出；键盘导航/字段焦点与 reduced-motion 检查通过。已查看桌面工作区及窄屏设置截图，截图仅含合成测试内容：

- [桌面工作区](research/batch-2-ui/workspace-desktop.png)、[窄屏工作区](research/batch-2-ui/workspace-mobile.png)
- [桌面设置](research/batch-2-ui/settings-desktop.png)、[窄屏设置](research/batch-2-ui/settings-mobile.png)

## 尚未完成的验收

1. 新网页/helper/官方 CLI 的真实扫码确认、会话恢复和只读频道附件下载；没有自动复用之前的私人会话。真实长连接经过 nginx 超过 30 秒的表现也未单独实测。
2. 应用 Go 容器至 RackNerd MiniCPM5-2B Q4 的受保护网络路径、真实扩展字段输出和拒答表现。没有新建持久隧道或修改服务器部署。
3. 带授权来源、成人/BL/冷门同人作品的实际收录和可见性；匿名中性样例不代表这些覆盖。
4. 固定版本 Komga/Kavita 服务实际导入与字段显示，以及完整部署备份恢复演练，留给第三批集成验收。
5. 真实移动设备内存与复杂/低清截图识别准确率。四语言合成图存在 CJK 空格/标点差异，必须保留人工校对。

上述条件未完成，因此四个子任务保留 review，父计划和第三批均不标完成。原件私有保留当前无自动到期清理策略，部署须考虑存储容量。

## 参考

- [使用及部署说明](../../../docs/development/metadata-workspace.md)
- [OCR 模块验证](../10-04-multi-image-ocr-ai/implementation-report.md)、[独立集成审查](../10-04-multi-image-ocr-ai/integration-review.md)
- [公开来源协议验证](../10-04-metadata-provider-search/research/protocol-evidence.md)
- [ComicInfo 专项验证](../10-04-comicinfo-roundtrip/research/implementation-verification.md)
- [真实 Telegram smoke 步骤](../../../tools/tdl-auth-helper/README.md)

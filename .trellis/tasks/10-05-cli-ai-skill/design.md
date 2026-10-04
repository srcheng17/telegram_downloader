# 全功能 CLI 与项目 Skill 技术设计

## 边界与交付形态

暂定命令名 `mediactl`。CLI 作为**客户端**运行在管理员的 Mac 或其他受支持的主机，不连接 PostgreSQL、Komga 文件挂载或 tdl 私密会话卷。任务、设置、账号与 Komga 业务动作都经过现有管理员 HTTP 边界；缺少的读/写 API 在应用层用例之上补齐。CLI 与网页共享状态和资格，不另建 Task Core 状态机。

首版优先使用仓库现有 Node ESM 与 `tesseract.js@7.0.0` 构建 CLI：`frontend/src/shared/metadata/{draft,schema}.js` 和 `frontend/src/ocr/rules.js` 的纯逻辑可直接复用，减少 Go/JS 双份字段与规则语义。CLI 的图片读取、Tesseract Node adapter、终端交互和 HTTP session transport 放在独立 `cli/`；不把 DOM/Worker 模块直接载入 Node。打包时固定 Node 版本要求、npm lock、四语言 OCR 资源与 hash；安装检查在本机完成，`--version` 对应仓库构建版本。生产 Go API 镜像继续是服务端；是否在镜像中附带 CLI 不改变“CLI 从客户端经 HTTP 调用”的架构，发布说明须明确 CLI 包和服务端的版本对应范围。

## 命令覆盖矩阵

具体 flag 在实现时可小幅调整，但以下动作及输出契约不可缩水。

| 页面业务 | CLI 命令族（暂定） | 服务端/本地依赖 |
| --- | --- | --- |
| 管理员登录与安全 | `auth login/status/logout/password-change` | session API；密码从隐藏终端输入 |
| 新建任务：元数据 | `workspace` 交互会话、`metadata schema/validate/history` | 纯 JS draft，schema/patch/validate/history API |
| 截图与规则 | `workspace image add/reorder/edit/retry/remove`、`metadata ocr`、`metadata rules preview` | 本机图片/剪贴板 adapter、Tesseract、共享规则执行 |
| AI 与公开书目 | `metadata ai extract`、`metadata providers/search/resolve`、`workspace candidate preview/adopt` | 保存的 AI/来源配置及受保护 API；显式发送/采用 |
| Telegraph/上传 | `tasks create-url/upload` | `POST /download`；上传 init→PUT；重复确认/force |
| 任务与产物 | `overview`、`tasks list/show/wait/cancel/retry/artifact-check/artifact-get/copy-to-komga` | summary/logs/tasks/download/copy API；补精确 task GET |
| 下载设置 | `settings download get/set` | 补 JSON API；不抓取 HTML 表单当持久接口 |
| 来源设置 | `settings sources list/set/test` | 现有来源 API |
| AI 设置 | `settings ai get/set/models/test` | 现有 AI API |
| 字段与规则 | `settings fields get/set`、`settings rules get/set/preview` | 现有配置 API + 本地规则 |
| 连接 | `connections telegram status/verify/login/cancel`、`connections komga status/set/test` | tdl 账号/尝试/SSE API；Komga 子任务连接 API 与服务端凭据；二维码与 2FA 交互 |
| Komga 存量编辑 | `komga libraries/books list/show/metadata preview/update`、`komga edits status/sync/restore` | Komga 子任务的 book/edit API、CBZ 回写与部分完成恢复契约 |

页面导航、Tab 切换、折叠、模态框等纯呈现控件不映射命令。任务页的“错误详情”映射为有界的 `tasks show` 诊断视图：默认机器结果只给安全错误码/摘要，用户在交互终端可明确请求受保护详情；AI Skill 默认不转述私密正文。现有 `/api/telegram/downloads` 虽仍在服务端用于历史兼容，不暴露 `tasks create-telegram`。

## 本地草稿、OCR 与候选

`mediactl workspace start --json` 启动每作品一个本机内存 broker，返回随机会话 ID/修订号；后续短命命令或 `workspace --jsonl --attach <id>` 通过仅本机的进程间通道操作同一份 metadata_document、候选、图片原文、逐图编辑文字、合并预览与 AI 发送快照。每次结构化请求带会话 ID、操作名与预期修订号，响应带序号、状态和安全摘要；Mac/Linux 使用权限为 0700 的用户私有运行目录及 0600 Unix socket，其他支持平台须有等价的当前用户独占通道，否则明确不支持会话功能。`mediactl workspace review <id>` 只在用户独立交互 TTY 附着同一个 broker，显示 OCR/候选具体值与 AI 发送快照，并在该终端完成校对、采用或外发确认；AI 的 JSON/JSONL 客户端默认无这些值。客户端断线只断开该连接，broker 在显式关闭、登出、空闲超时或进程故障时销毁草稿，不能假装恢复已丢失内存内容；socket/进程异常清理由下次 start/status 检查。Skill 示例须实测 AI 安全命令与独立终端同时附着、修订冲突、退出/断线、超时和故障。若用户显式导出跨进程人工草稿，写到指定 0600 文件，OCR 原文不随默认导出。

机器输出默认采用 AI-safe allowlist：仅给操作状态、image_id/candidate_id、字段 key、修订、长度/冲突码等，不含 OCR/R18 正文、候选字段值或 AI 发送预览。原文和具体值由用户通过 `workspace review <id>` 在独立交互终端核对后，以不透明候选 ID 与字段 key 授权采用；只有用户明确要求 AI 解读这些内容时，CLI 才允许单次显式 reveal，并在 Skill 中标注这会进入 AI 上下文。该双端通道防止默认命令意外回显，不承诺抵御同一 OS 用户身份下的恶意本地进程；AI 工具创建的 PTY 不是独立用户终端。合成敏感候选必须验证 stdout/stderr/日志、JSONL 和 Skill 默认调用无值泄漏。

图片按现有契约接收静态 PNG/JPEG/WebP：最多 10 张、10 MiB/图、50 MiB/组、1200 万像素/图、8192px/边。先检查容器/MIME/大小/声明尺寸，再解码复核；同一 Tesseract 版本与语言数据做串行识别，不调用远端 OCR。Mac `--clipboard` 由 AppKit 本机适配器从 NSPasteboard 读图并以管道交给 CLI，不写临时截图；适配器也要在转换前检查剪贴板原始表示的大小，拒绝无界解码。其他平台明确返回“不支持剪贴板”，多 `--image` 文件仍可使用。粘贴多图时以用户指定顺序为准。失败/重试不覆盖已校对文字；未完成图片必须明确选择完成子集才生成候选。

规则直接复用 JS 执行模块；设置 API 只提供配置快照，不能替代本地执行。候选采用使用共享 draft 的字段版本/手工锁逻辑，并在采用时按 origin 重新获取 schema + rules/AI/source 的权威版本。AI 仅在用户核对发送目标、模型、字段与实际文字后主动触发；请求体仍由服务器的已保存配置决定目的地，CLI 不传任意 base_url/密钥/模型。候选预览与采用分开，失败保留草稿。

## HTTP 与凭据

CLI URL 只能是 HTTPS，或显式 `--allow-insecure-loopback` 且目标和实际连接均为 loopback 的 HTTP；拒绝 URL 用户名/密码、片段及跨源或降级重定向，任何重定向都不转发 cookie/CSRF。首次 GET `/api/auth/session` 获取预登录 cookie/CSRF，再 POST 登录；每次写操作发送服务端精确 Origin 和当前 CSRF。会话续期、401、403、409、422 等保持区别，不自动用管理员密码重新登录。会话文件置于用户配置目录的私有子目录，目录 0700、文件 0600、原子替换且拒绝符号链接/权限过宽；`logout` 清理本地会话并调用服务端撤销。密码、2FA、来源/AI/Komga 密钥只从用户独立交互终端的隐藏 TTY 读取；秘密变更命令在 `--json`/JSONL 或没有 TTY 时只返回 `interaction_required` 且不发起变更，拒绝 argv、环境变量和 stdin 管道作为秘密入口。普通文档/图片数据仍可走结构化 stdin，不与秘密输入混用。

`--json` 统一输出 `{ok,code,data?,message?}`，错误写 stderr 并使用稳定退出码类别（输入无效、未认证/禁止、冲突、不可用、传输失败）；具体字段按 API 版本冻结。默认结果过滤敏感字段，尤其不原样传播 `copy-to-komga.target_path`、session cookie/CSRF、二维码 base64、2FA、来源密钥、AI prompt/OCR 正文。网页和 CLI 都可能需要受保护详情时，以明确交互操作读取；Skill 仍只报告安全摘要。输入 URL、文件名与 OCR 文本均作为数据参数传递，不拼接 shell。

上传先本地校验文件/大小，再 init、按 API 约定 PUT，确认 task/source 状态；中途失败不报告已成功。文件下载先 HEAD 资格、读取有界响应到同目录临时文件、校验长度/响应与文件类型，再 fsync + 原子 rename；默认拒绝覆盖既有目标。复制到 Komga 后回读任务结果/可观察状态，隐藏宿主路径。取消是异步请求，`wait` 以 Task Core 最终状态为准。Komga `metadata update` 只调用子任务的保存 API，使用 book ID + 预期版本/文件指纹 + 字段差异；操作可能停在 file committed / sync pending，CLI 须返回 operation ID，并用 `edits status/sync/restore` 调用同一受保护 API；不直接改本地挂载或把单书 analyze 的 202 当作完成。

## tdl 登录与 Skill

`connections telegram login` 需要用户**独立终端**的交互 TTY：非交互 `--json`/JSONL 在调用 StartLogin **之前**返回 `interaction_required`，不创建 attempt、不占用服务端单次登录锁。交互模式由用户终端自己启动 attempt；服务端给出有期限的 PNG data URL，CLI 有界解码后只在该 TTY 渲染可扫描的黑白字符二维码，过期即清除，不把 data URL 打印或落盘。用户手机确认；若要求两步验证，只在隐藏输入中读取并发送现有 password API。终端离开、超时或 Ctrl-C 时取消 attempt、清除二维码与密码缓存；最终调用 account verify。非交互状态命令只返回安全状态码，绝不包含 QR/base64/2FA。`auth login/password-change` 和来源/AI/Komga 密钥写入同样由用户独立终端完成；AI 执行工具的 PTY 输出可能进入模型上下文，不能当作安全输入界面。Skill 仅指引用户完成后读取 status/verify 安全摘要，不从 AI 对话收集秘密。

Skill 入口保持简短 frontmatter 和调用边界；命令手册可放 `references/`。只在用户要求操作该工作台时触发，先读目标/版本/可用动作，再执行用户授权的写入并回读。书目搜索与 AI 提取是显式外部请求，不因 OCR 结束自动发送。实际 AI 工具通常接收 shell 命令文本，因此 Skill 只调用固定子命令；URL、路径、用户文字、OCR 和字段值通过 CLI 的结构化 stdin/受控输入文件协议承载，不插入 shell 命令。先实现、验证真实 CLI，最后写 Skill 与可执行示例；Skill 不是访问控制。

## 兼容、发布与失败恢复

先补共享 API，再接 CLI，最后写 Skill；任何新增 API 继续走现有管理员中间件和应用服务。已存历史任务、定义版本和 Telegram 账号不迁移到 CLI 专属数据库。可以先发布并行 CLI 包但不得在缺 Komga 保存命令时声称全功能验收。撤回 CLI 不删除服务端数据或 CBZ；Komga 文件回滚由其子任务负责。不同 CLI/服务端版本不兼容时明确 `version_mismatch` 并拒绝有副作用命令；不靠猜字段继续写入。

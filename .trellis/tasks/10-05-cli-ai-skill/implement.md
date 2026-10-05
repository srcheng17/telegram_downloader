# 分批实施计划：全功能 CLI 与 AI Skill

状态：in_progress；用户已批准计划并启动任务。以下清单保留原实施顺序，实际证据与未完成项记录在文末。Komga 命令使用 `10-05-komga-metadata-management` 的实际受保护接口与 CBZ 写回契约，不以占位行为冒充完成。

## 执行前门槛

- [ ] 读 `docs/development/module-boundaries.md`、后端/前端 spec index、workspace auth、extraction/candidate/packaging/telegram 规范及本任务 research，复核父任务与 Komga 子任务最新契约。
- [ ] 清点当前未提交工作，给 API、CLI、OCR/本地草稿、打包/Skill 分配互不冲突的文件所有权。并行实现者不是独自在代码库，不得撤销其他人的改动；共享 `package.json`、路由和集成文件由一人串行接线。
- [ ] 固定命令清单与网页业务覆盖表、JSON/退出码规范、Node 运行版本、服务端能力版本及安装产物；决定纯模块抽取范围，写契约测试后再分工。

## 第一批：服务端契约与 CLI 基础（可并行于本地 OCR）

- [ ] 给 `GET /api/tasks/{id}` 增加经过同一中间件的精确详情，复用 Task Core `GetTask` 与 action eligibility；补下载设置 JSON 读写 API，调用现有配置服务，保留值域和写入语义；不解析 HTML 模板作为数据 API。
- [ ] 为 CLI 需要的其他缺口补最小受保护 API，尤其 Komga 接口只在其子任务完成后接入；所有写入复用应用用例、版本/CSRF/Origin 与限流。不可把 `/api/metadata/patch` 标成保存。
- [ ] 建立 `cli/` Node ESM 命令入口、help/version、配置加载、HTTP/会话传输、私有 cookie 存储、`--json`/退出码、安全错误映射，以及不把不可信用户值插入 shell 的结构化 stdin 协议。登录完成后验证会话；401 不自动重放密码，跨源重定向不带凭据。
- [ ] 增加 API/CLI 合约测试：匿名/CSRF/Origin/版本冲突、失效会话、HTTPS/loopback、权限不当文件、错误脱敏、不同版本拒绝写入。用隔离测试账号和合成秘密；不在 fixture 提交真实凭据。

## 第二批：本机元数据工作流（可与第一批独立开发，最后接线）

- [ ] 建立每作品一个本机内存 broker 与纯 JS draft/候选复用，定义 `workspace start/attach/review/stop`、会话 ID、消息序号、预期修订、双端附着、连接断开/空闲超时/进程故障和显式私密草稿导出；支持 schema、手工 set/clear/unlock、历史/书目/规则/AI 候选预览和逐字段采用。AI JSON/JSONL 默认只输出安全 allowlist，不含候选值/OCR 正文；独立用户 TTY 通过同一 broker 核对原文、候选和外发预览，用户显式要求 AI 解读时才允许单次 reveal。采用时重新取权威版本，保留冲突草稿；AI 发送预览和主动确认需在 CLI 中真实可用。
- [ ] 建立 Node 本地 OCR adapter 和固定资源包装，验证 PNG/JPEG/WebP 的大小/声明尺寸/解码尺寸、10 张/单张/总量限制，串行识别及四语言切换；实现多 `--image` 和 Mac 剪贴板管道 adapter。逐图 ID、校对、重试、重排、删除、完成子集确认与取消要有状态测试。
- [ ] 本地规则直接调用共享 `frontend/src/ocr/rules.js`；以相同 schema/文本 fixture 对照网页产生相同候选、警告、版本和 provenance。OCR 原文不落盘、不进入 JSON/日志；资源缺失不回退远端 CDN。
- [ ] 加 `npm run test:cli`、`npm run lint:cli`、`npm run build:cli` 与 `npm pack` 后清洁安装的 help/version 检查；现有 `npm run lint` 和 Vite `npm run build` 不覆盖新增 `cli/`。若抽取或修改 `frontend/src/`，同步重建 `web/static/dist/*.bundle.js`。

## 第三批：所有网页业务命令与真实路径

- [ ] 实现 URL 重复确认/force、上传 init→attach；元数据 document 与任务快照一致。任务概览/筛选分页/详情/等待/取消/重试/下载/复制按服务端资格执行；下载使用私有临时文件、校验、fsync、原子 rename 且默认不覆盖。
- [ ] 实现七类设置命令：下载、来源、AI、字段、规则、连接、安全；连接包括 Telegram 与 Komga 状态/凭据/测试，Komga 本地挂载映射不可由 CLI 任意扩大。秘密只由用户独立终端的隐藏 TTY 提供；`--json`/JSONL、stdin 管道和无 TTY 调用须在变更前返回 `interaction_required`。AI Skill 只读取安全 status/verify；写入使用 expected version/CAS。来源、AI 与 Komga 测试只输出安全摘要，不回显 URL 中的凭据或上游正文。
- [ ] 实现 tdl QR/2FA 交互、attempt 状态/取消、账号 verify；用户独立终端扫码和输入密码，非交互 JSON 在 StartLogin 前返回 `interaction_required` 且不留下活动 attempt。控制 Ctrl-C/超时/登出时清理尝试，不能开放新的 Telegram 消息创建入口。
- [ ] Komga 子任务交付后，接列表/详情/预览/ComicInfo 保存及 edit operation 状态、同步重试、显式恢复命令；以其文件指纹、备份、并发拒绝、原子回写和双阶段回读为准。对非 CBZ 只读，文件成功而 Komga 未同步时明确报告 partial，不报完全成功。

## 第四批：Skill、交付与跨层验收

- [ ] 先对真实 CLI 完成 help/JSON 命令快照与 E2E；再写 `.agents/skills/media-workspace-cli/SKILL.md` 和必要的按需 references。按 `skill-creator` 用简短 frontmatter、精准触发描述和真实命令，不复制通用操作手册或隐藏权限规则。
- [ ] 用技能校验器检查 Skill 格式，并从全新 Codex 会话实际验证项目本地 Skill 被发现并触发一次只读命令；用代表性 AI 请求测试经用户授权的任务变更、OCR/书目/AI 候选的 AI-safe 输出、敏感登录转用户独立终端、Komga 保存后的回读。以合成敏感文本检查 stdout/stderr/日志和默认 Skill 调用不泄漏正文；Skill 不输出密码、会话、二维码、2FA、密钥或宿主路径，不通过 shell 拼接用户文本。
- [ ] 文档写清安装/升级、Node/OCR 资源校验、CLI/服务端版本兼容、Mac 剪贴板支持和其他系统文件输入、会话文件位置/权限、恢复/卸载。CLI 回滚只移除客户端，不清空数据库或 Komga 文件；CBZ 回滚按 Komga 子任务程序。

## 验证与发布门槛

- [x] 计划期：已校验计划并在用户确认后运行 `task.py start`；后续复核仍需运行 `git diff --check`。
- [x] 实现期本地检查：完整 release gate 在隔离 PostgreSQL 上通过 Go 全套/race/vet、前端 214/214、OCR 资源、构建和浏览器 E2E 30/30；CLI 66/66、lint、build 与 `npm pack` 后离线清洁安装的 help/version/OCR 通过。外部依赖验收仍按下一项单独记录。
- [ ] 真实本机 Mac 剪贴板截图及多图 OCR、同一 workspace 的 AI 安全命令与独立用户终端同时附着/核对/确认、断线与超时、网页/CLI 同文规则候选、受保护网络登录与 CSRF、无 TTY/JSON 密钥写入和 Telegram 登录不产生变更、假服务端故障、真实 tdl 授权和 Komga CBZ 备份/回读分别验证。外部服务不可用时标记相应验收未完成，不用 mock 代替。
- [ ] 发布前做一张“页面业务→CLI 命令→验证证据”表，覆盖 PRD AC1–AC8；每条 Skill 示例对已构建的同版本 CLI 实测。缺 Komga 真正回写、OCR 本机路径、tdl 验证或安装产物时不得宣称首版“全页面功能”完成。

## 2026-10-05 实施证据与待验收边界

- CLI 已提供认证、内存草稿、多图本地 OCR、规则与候选、公开书目及可选 AI、任务提交/查询/下载、七类设置与连接、Komga 存量编辑；`workspace tasks create-url|upload` 可将当前内存草稿直接提交，不需将私密元数据导出到普通文件。重复 URL 明确报告 `snapshot_attached:false`。业务写请求先验证 `/api/client-contract` 协议版本；不兼容服务端拒绝写入。
- 独立 CLI 复核新增设置与任务错误的用户终端 `review`、固定参数数组辅助入口，并修正历史候选采用时的版本检查；默认机器输出仅保留安全摘要。单次受控 reveal 已加入 60 秒、完整修订、一次性与 `--json --to-ai-context` 门槛；JSONL 保持禁止。`npm run test:cli` 66/66，`lint:cli`、`build:cli` 和清洁 `npm pack` 安装的 help/version/离线 OCR 均通过。最终 `bash scripts/verify_release_gates.sh` 也已完整通过；验收矩阵见[集成记录](../10-04-media-workspace-integration/research/verification-2026-10-05.md)。
- 用独立 PostgreSQL + Komga 1.28.1 + 合成 CBZ 测试了真实 CLI `books edit → metadata preview → update → edits status → restore`。保存后状态为 `current_value_consistent`，恢复后 CBZ 完整 SHA-256 与编辑前一致；测试没有访问用户生产书库。
- 用两张仓库内合成截图在 Mac 客户端运行实际 Tesseract，本机内存工作区报告两张均识别完成、合并文字非空；随后手工设置合成标题，经单次 reveal 核对所选字段，用结构化 JSON 从该草稿新建隔离 Telegraph 任务，服务端回读到同一标题快照，最后取消该测试任务。该测试没有读取用户剪贴板，也没有启动下载 worker 或访问外部 Telegram。
- 本轮隔离浏览器 E2E 为 30/30。服务端完整 Go 测试与 race 测试在独立 PostgreSQL 数据库通过，前端 214/214、`go vet`、OCR 资源和镜像构建也通过最终 release gate。
- 真实 Mac 剪贴板截图、多图人工校对、用户独立终端 AI 发送、tdl 实际授权和新 Codex 会话自动发现 Skill 尚未做现场验收。Komga/Kavita 跨阅读器兼容性也尚未验证；这些不能由 mock 或上述合成路径替代。任务在这些边界核实前保持 `in_progress`。

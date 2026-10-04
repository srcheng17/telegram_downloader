> 2026-10-05 范围调整：用户移除新建任务的 Telegram 消息下载来源，保留 tdl 登录并迁入「设置 → 连接」；旧 `/telegram` 跳至 `/settings#connections`。后端接口、历史任务及账号数据保留。后续验收按 [UI v2 设计](../10-04-modern-ui-shell/redesign-v2.md) 执行，不再要求首页 Telegram 来源入口。

> 执行更新（2026-10-05）：第二批已实现并完成本地集成检查，状态 review；实际外部验收仍未全部完成。结果以[第二批验收记录](../10-04-clipboard-ocr-tdl-ui/batch-2-verification.md)为准。下文的 planning/未执行描述保留为原计划背景。

# tdl 账号与下载技术设计

## 版本、边界与所有权

固定官方 `iyear/tdl v0.20.4` 下载 CLI；登录 helper 固定同版本 root/core 模块及已研究 gotd v0.140.0，构建须验证上游 Go 1.25.8 要求和 Linux amd64/arm64。先完成隔离编译/交接，不自动升级依赖解决兼容问题，不解析 ANSI 终端输出作为正式协议。

子任务新增 `internal/app/telegram/`、`internal/infra/telegram/`、`internal/httpapi/telegram_*.go`、`frontend/src/telegram/`、专属测试及 helper 独立目录（建议 `tools/tdl-auth-helper/` 独立 Go module）。必要 SQL 仅使用父任务预留 `018_telegram_account.sql`。既有 Task Core kind/input/source qualification、worker dispatch、store query 和 router/cmd/runtime config 的接线由父集成者串行完成；本任务提交精确接口及测试，不能并行改别的子任务共有文件。

配置与凭据来自 source-settings-auth；元数据来自 metadata-contracts。helper 不实现第二套管理员/配置存储，不自建任务队列或生命周期。新 kind 建议 `telegram`，状态仍是现有 CREATED/READY/RUNNING/CANCELING/SUCCEEDED/FAILED/CANCELED；实际沿现有 domain/app 资格扩展，禁止前端另造 retry/cancel 规则。

## HTTP 契约（路由由父设计统一冻结）

| 请求 | 行为 |
| --- | --- |
| GET /api/telegram/account | 脱敏账号/上次真实验证/忙碌状态，不为轮询重新打开 KV |
| POST /api/telegram/login-attempts | 创建有期限的 attempt，返回 attempt_id、revision 和状态；busy 返回明确冲突 |
| GET /api/telegram/login-attempts/{id} | 当前状态快照；恢复页面不回放旧密码/失效 QR |
| GET /api/telegram/login-attempts/{id}/events | 同源受保护 SSE；事件含 attempt_id、seq、revision、type、expires_at 和必要 payload |
| POST /api/telegram/login-attempts/{id}/password | 仅 password_required 状态接收一次受限长度密码，经私有 pipe 交 helper |
| POST /api/telegram/login-attempts/{id}/cancel | 幂等取消并等待退出；非立即伪造成功 |
| POST /api/telegram/downloads | {message_url, metadata_document, runtime_settings, force?} 经 app 创建 Task Core 任务 |

受共享单管理员 session/CSRF 保护；SSE/QR no-store，网页不获本地路径、namespace、session 或普通子进程输出。账号/attempt 状态不是 Task Core 状态，允许 waiting_qr/password_required/verifying/connected/cancelled/expired/failed，错误有 busy/auth_required/password_invalid/rate_limited/network_error/account_changed 等稳定类别，遵循现有 /api 错误 envelope。

## 登录、锁与交接

1. 服务端生成不可猜测 attempt ID 与 candidate namespace；active namespace 保持不动。应用总期限与 QR expires 独立，保留 tdl 默认 reconnect timeout 5m，不用短 CLI timeout 代替应用期限。
2. 协调器取得专用 PG connection 的 session advisory lock（单账号键）和共享私密存储目录的 OS 排他锁后才启动任何 helper/CLI。跨 API/worker 必须走同一个协调器接口；所有进程在同一受控主机/共享持久目录执行，横跨主机或不可靠 NFS 锁不在首版支持范围。
3. 锁必须覆盖进程真实寿命，OS lock 描述符须继承到实际 helper/CLI 并在其生命期保持，或采用已验证的内核监督方案；不能仅由 launcher 持有。launcher 被 SIGKILL 而 CLI 存活时仍须排斥新 owner。DB 连接/协调 owner 丢失立即取消子进程并等待退出，下一 owner 仍须取得 OS lock，不能仅因 DB advisory lock 自动释放就同时启动。不能使用仅进程内 mutex 或到期后直接抢锁的裸 lease。
4. helper 输出严格 JSONL 事件，限帧长/事件频率，stdout 不混普通日志；密码仅经有界私有输入通道。qr 刷新替换旧 token，不持久化 QR/密码事件。所有成功/失败/取消路径关闭 `pkg/kv.Storage` engine。
5. 真正 Self/Auth.Status 成功后关闭 helper 及 engine，保持协调锁，再以非登录模式新进程重新读取 candidate 验证恢复授权。验证后 CAS 更新 active namespace/account revision；只允许当前 attempt owner 晋升。官方 CLI 后续下载是第二层真实交接验证。
6. 取消/过期/失败先终止并回收进程，释放/清理候选资源；active 不变。成功切换保留旧 namespace 至无执行引用且清理策略允许，再安全删除；不把 candidate 键简单覆盖到 active DB 文件。

单账号同一时间只有一个活跃 tdl/helper。下载任务可 READY 排队；worker 等待账号锁期间保持现有 heartbeat/取消响应，账号页展示 busy。普通账号查询只读服务状态，不开 Bolt；显式验证也必须持锁。账号重连等待当前下载结束，不能中途切换其凭据。锁实现和 launcher 的失联回收是必须通过的技术门槛，不能用“PG 锁已释放”等价“CLI 已退出”。

## 下载与 Task Core

解析允许的 t.me 消息 URL，由服务端转换成结构化消息 identity；拒绝任意 scheme/host/path、用户 namespace/path/CLI flags。不会用网页 HTML 是否可见判断 MTProto 权限，也不修改账号内容过滤设置。一个任务一个已解析附件，适配器先获得结构化类型/声明大小；不支持或多目标时明确拒绝/要求选择，不静默批量下载。

新增输入保存 canonical message identity、创建时 active account identity/revision、metadata_document 与下载设置快照；不保存 session/token。Task Core canonical identity 锁/查询语义扩展到 kind/account/message 维度，由 domain/app/store 统一维护；READY/RUNNING/CANCELING 原有去重、force、retry、generation fencing 与 artifact 资格沿用。Retry 需有效源 identity，执行时账号已更换返回 account_changed，不静默更新创建时账号。

下载使用固定 CLI 可执行文件与参数数组，无 shell；source 存入 DOWNLOAD_PATH/taskID/generation 下私有暂存目录。500 MiB 压缩上限（共享受保护配置只允许降低）在声明大小和实际输出上双重执行；CLI 若无法证明严格写入上限，必须以有硬额度的受控输出/文件系统 quota 实现，不能仅轮询大小后宣称上限已保障。超过上限立即取消并等待退出、删除不完整源，不出版 artifact。大小未知也须受实际硬额度约束。

完整源复用现有流式 ZIP/RAR/7Z 处理：300 images、25 MiB/image、500 MiB regular members，自然排序包含目录；浏览器64 MiB限制不改。产物按既有 generation/realpath containment 发布；取消等待进程/Execute 退出及清理后再 ack。已完成且可验证源可保留供失败重试，半文件不能视为 attached source；成功源清理仅在 Complete 成功且不存在保留引用后。未知/冲突/损坏元数据或未识别非图片 entry 要求保留原件时，先提升为私密持久 source artifact，按父集成策略保留至管理员明确删除，不随 Complete 清掉。源缓存记录尺寸/哈希并限定真实根目录，重试不能跨代际复用未经验证半文件。

## 配置、迁移与回滚

source-settings-auth 提供可信配置、管理员/CSRF、凭据目录和 redaction；本子任务仅保存不敏感的 active namespace 引用、account revision、attempt owner/状态与必要 Task Core input（如需迁移用018，不能改已应用 migration）。不持久化 QR/2FA。会话目录权限最小化并明确备份敏感性，普通日志只记代码/attempt/任务引用。

关闭 Telegram 入口即停止新账号操作/新任务；先排空或取消运行中进程再停组件。保留原有效 active 会话和任务资料供恢复；不能通过删除 session 当作普通回滚。旧 Telegraph/upload 与旧 kind 读取保持兼容；不在运行任务期间降级删除 schema。

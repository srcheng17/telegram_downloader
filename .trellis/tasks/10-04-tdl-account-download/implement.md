> 2026-10-05 范围调整：用户移除新建任务的 Telegram 消息下载来源，保留 tdl 登录并迁入「设置 → 连接」；旧 `/telegram` 跳至 `/settings#connections`。后端接口、历史任务及账号数据保留。后续验收按 [UI v2 设计](../10-04-modern-ui-shell/redesign-v2.md) 执行，不再要求首页 Telegram 来源入口。

> 执行更新（2026-10-05）：第二批已实现并完成本地集成检查，状态 review；实际外部验收仍未全部完成。结果以[第二批验收记录](../10-04-clipboard-ocr-tdl-ui/batch-2-verification.md)为准。下文的 planning/未执行描述保留为原计划背景。

# 实施计划：tdl 登录与下载（Batch 2，尚未开始实现）

## 进入条件

父任务最终规划获准实施，Batch 1 的管理员/凭据、metadata_document、shell/draft 接口冻结。父集成者保留 router/cmd/shared runtime config、旧 Task Core 文件和 dist 接线所有权；本任务只交新模块、精确接线清单和 migration018（必要时）。不得修改其他 agent 正在写的文件。

## 有序步骤与停止点

1. [ ] 用固定版本建立最小 helper 独立模块，验证 Linux amd64/arm64 构建与无副作用启动；生成合成 QR/2FA JSONL 协议测试。若 tdl 公共包初始化/构建不可控，先报告具体阻碍并回设计，禁止直接切成网页终端。
2. [ ] 实现受控 launcher 与 PG+OS 双重进程寿命锁，写跨进程争抢、DB 断连、子进程失联/崩溃和 cancel/wait 测试；未证明无双写前不接真实账号。
3. [ ] 实现 active/candidate、attempt/seq/revision、真实授权后关闭再重新打开验证与 CAS 晋升；验证所有退出路径关闭 KV、旧 active 不被失败覆盖。
4. [ ] 实现账号 HTTP/SSE/密码输入及中文页面，复用共享管理员/CSRF。事件重连只发当前有效公开状态，不回放敏感历史。
5. [ ] 实现 message identity/附件检查/有界 CLI 下载；先证明500 MiB硬限制，再与现有解包/CBZ pipeline 连接。父集成者添加 kind/input/HasSource、去重锁和 worker dispatch，迁移018如需由其串行注册。
6. [ ] 独立 checker 覆盖 Task Core 代际、cancel/retry/source cleanup 与 secret redaction；父集成者运行原流程回归和 bundle 构建。
7. [ ] 使用用户在验收时明确提供的隔离测试账号/合成附件完成真实 QR→可选2FA→新进程恢复→官方CLI下载→CBZ/ComicInfo 验证。没有真实交接证据不得报告账号功能已验证；自动测试不接触现有频道媒体。

## 计划测试矩阵（未执行）

- helper：二维码刷新/过期、wrong password、FloodWait、取消 exit0、DC迁移、未知/超长/断帧事件、stdout/stderr 脱敏、存储 Close 和非登录恢复。
- coordinator：API+两个 worker 跨进程争抢、同账号只有一个活跃进程、owner丢失子进程尚存、OS锁仍阻止新写者、旧attempt迟到晋升、失败重连保持active。
- HTTP/frontend：未登录/无CSRF、no-store、旧seq/revision忽略、重复挂载清理、页面重连、密码清除、busy/auth/网络/限流分别反馈。
- 下载：声明/实际字节上限，500 MiB边界、用户降低限额、未知大小、无附件/不支持/多目标、路径/参数注入、同名generation隔离、过期owner禁止Complete、cancel先退出后ack、account_changed。
- archive/Task Core 回归：64 MiB上传上限仍在；tdl大于64 MiB合成源可执行且仍受解包300页/25 MiB/500 MiB；乱序目录页面、精确图像字节和 metadata_document/ComicInfo保真；成功后才清源、失败半文件不可重用。
- 命令：`go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`；helper独立模块内运行同等测试和双架构编译；设置隔离 TEST_DATABASE_URL 后串行运行PG测试，不能和其他agent在同一库并跑固定ID测试。
- 前端：`npm run test:frontend`、`npm run lint`；父集成者执行`npm run build`、`npm run e2e:test`，最终运行`bash scripts/verify_release_gates.sh`。真实Telegram smoke与隔离Compose synthetic E2E分开记录。
- 规划检查：`python3 .trellis/scripts/task.py validate .trellis/tasks/10-04-tdl-account-download`、`git diff --check`。

## 风险及回滚

重点风险是 helper/官方CLI 的真实交接、Bolt跨进程单写、CLI实际输出硬上限和退出回收。每项都有先测后接线门槛，不把尚未验证的保证写成实现结果。需要额外平台锁/磁盘quota依赖时由父设计明确部署条件；不扩大多主机、多账号或更大文件范围。回滚前先取消/排空子进程，保留有效会话和既有任务，不破坏Telegraph/upload路径。

集成门槛：launcher 被 SIGKILL 而 CLI 仍存活时实际锁仍排斥第二 CLI；原件保留引用存在时 Complete 不清源；通过 nginx 访问扫码 SSE 持续超过30秒，心跳/刷新即时可见且登出后关闭。

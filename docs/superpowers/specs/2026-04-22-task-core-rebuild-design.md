# Task Core 破坏性重构设计

日期：2026-04-22
项目：telegram-downloader
状态：设计已确认，待实施计划

## 1. 背景

当前任务生命周期规则散落在 `internal/app/tasks/`、`internal/httpapi/`、`internal/httpv2/`、`internal/worker/`、`internal/store/postgres/` 和 legacy adapter 中。虽然现有功能能运行，但状态推进、动作资格、进度语义和 worker 恢复规则没有单一事实来源，容易出现以下问题：

- 状态判断在 HTTP、worker、store、adapter 中重复实现；
- cancel / retry / success action 的资格容易不一致；
- worker 崩溃或迟到上报时缺少统一 lease / attempt 保护；
- 前端需要通过底层字段组合推断“准备中、上传中、处理中”等文案；
- legacy adapter 仍承担事实语义，而不只是协议兼容。

本次用户明确选择：

- 采用激进重构；
- 允许破坏旧任务数据和内部 API 兼容；
- 保留用户可见体验；
- 优先保证状态清晰、运行可恢复、测试覆盖完整，并用实现简单作为约束。

## 2. 目标与非目标

### 目标

1. 建立新的统一 Task Core，作为任务状态、动作资格、进度语义和结果语义的唯一事实来源。
2. 重建任务表结构，不强兼容旧任务日志数据。
3. 引入 worker lease / heartbeat / attempt 模型，保证 worker 崩溃后任务可恢复，迟到上报不可覆盖新 attempt。
4. 保留用户可见体验：URL 下载、上传生成 CBZ、日志页、取消、重试、下载、复制到 Komga。
5. 通过领域、应用、存储和 E2E 测试覆盖核心生命周期。

### 非目标

1. 不迁移旧任务日志数据。
2. 不重做整个前端视觉设计。
3. 不重新设计 Telegraph 下载、CBZ 打包、Komga 路径规则本身；这些执行细节先接入新生命周期。
4. 不在 HTTP、worker、store 中继续新增长期任务规则。

## 3. 推荐方案

采用“新 Task Core + 旧体验适配”。

建议模块边界：

```text
internal/domain/taskcore/          # 纯领域模型：状态、事件、合法流转、动作资格、进度语义
internal/app/taskcore/             # 应用用例：创建、上传源提交、取消、重试、claim、heartbeat、complete、fail、recovery
internal/store/postgres/taskcore/  # 新 Postgres 存储实现
internal/worker/taskcore/          # worker 执行协调：claim、heartbeat、执行、report
```

HTTP 层只负责请求解析和响应转换；worker 只负责执行和上报；store 只负责事务和条件更新。任务规则只允许出现在 domain/app 层。

旧 v2 / legacy 任务实现先保留作为参考和回退点。新链路验证后，再删除或隔离旧实现。

## 4. 状态模型

新的主状态集合：

```text
CREATED
READY
RUNNING
CANCELING
SUCCEEDED
FAILED
CANCELED
```

状态含义：

- `CREATED`：任务记录已创建，但输入未准备好。上传任务初始化后处于此状态。
- `READY`：输入已就绪，等待 worker 领取。URL 任务创建后可直接进入此状态。
- `RUNNING`：worker 已领取并执行中。
- `CANCELING`：用户已请求取消，等待执行链路确认。
- `SUCCEEDED`：任务成功，存在结果产物。
- `FAILED`：任务失败，记录失败原因。
- `CANCELED`：任务取消完成。

合法流转：

```text
CREATED -> READY
CREATED -> CANCELING -> CANCELED

READY -> RUNNING
READY -> CANCELING -> CANCELED
READY -> FAILED

RUNNING -> SUCCEEDED
RUNNING -> FAILED
RUNNING -> CANCELING -> CANCELED

FAILED -> READY       # retry
CANCELED -> READY     # retry

RUNNING -> READY      # recovery: lease expired / worker lost
```

规则：

- 终态只有 `SUCCEEDED / FAILED / CANCELED`。
- `SUCCEEDED` 不可 retry，不可 cancel。
- cancel 是请求型动作：先进入 `CANCELING`，再由 worker 或 recovery job 最终落为 `CANCELED`。
- `RUNNING -> READY` 只允许 recovery 触发。
- worker 不直接任意写状态，只能调用应用层事件：`Claim`、`Heartbeat`、`ReportProgress`、`Complete`、`Fail`、`AcknowledgeCancel`、`ReleaseExpiredLease`。

## 5. 数据模型

使用一张主表 + 附属表。新表可独立命名为 `task_core_*`，避免破坏旧表回滚能力。

### 5.1 `task_core_tasks`

```text
id UUID primary key
kind TEXT not null                  -- url / upload
status TEXT not null                -- CREATED / READY / RUNNING / CANCELING / SUCCEEDED / FAILED / CANCELED
attempt INT not null
created_at TIMESTAMPTZ not null
updated_at TIMESTAMPTZ not null

ready_at TIMESTAMPTZ
started_at TIMESTAMPTZ
finished_at TIMESTAMPTZ

cancel_requested_at TIMESTAMPTZ
last_error TEXT

lease_owner TEXT
lease_expires_at TIMESTAMPTZ
heartbeat_at TIMESTAMPTZ
```

### 5.2 `task_core_inputs`

```text
task_id UUID primary key references task_core_tasks(id)
url TEXT
canonical_url TEXT
source_archive_name TEXT
source_archive_path TEXT
source_archive_size BIGINT
metadata JSONB not null default '{}'
```

规则：

- `kind=url` 必须有 `url`。
- `kind=upload` 在 `CREATED` 时可以没有 `source_archive_path`；上传完成进入 `READY` 前必须有源包路径。
- `metadata` 保存作者、系列、漫画名、简介、标签、类型等输入元数据。

### 5.3 `task_core_progress`

```text
task_id UUID primary key references task_core_tasks(id)
phase TEXT not null                 -- uploading / preparing / downloading / packaging / copying / done
current BIGINT not null default 0
total BIGINT not null default 0
unit TEXT not null                  -- bytes / images / files / steps / none
message TEXT not null default ''
updated_at TIMESTAMPTZ not null
```

前端不再根据 `total_images == 0` 或 upload bytes 自行推断阶段，而是直接消费 `phase + current + total + unit + message`。

### 5.4 `task_core_results`

```text
task_id UUID primary key references task_core_tasks(id)
artifact_path TEXT not null
artifact_name TEXT not null
artifact_size BIGINT not null
artifact_kind TEXT not null          -- cbz
created_at TIMESTAMPTZ not null
komga_copied_at TIMESTAMPTZ
komga_target_path TEXT
```

规则：

- 只有 `SUCCEEDED` 才允许下载结果。
- `can_download` / `can_copy_to_komga` 基于 `status + result` 统一计算。

### 5.5 `task_core_events`

```text
id BIGSERIAL primary key
task_id UUID not null references task_core_tasks(id)
event_type TEXT not null
from_status TEXT
to_status TEXT
actor TEXT not null                  -- api / worker / recovery
message TEXT not null default ''
payload JSONB not null default '{}'
created_at TIMESTAMPTZ not null
```

用途：

- 调试状态异常；
- 验证 worker recovery；
- 后续支持日志页任务时间线。

## 6. Worker Claim / Heartbeat / Recovery

### 6.1 Claim

worker 周期性调用 `ClaimNext(workerID)`，从 `READY` 中领取最早 `ready_at` 的任务：

```text
READY -> RUNNING
```

领取时在同一事务中写入：

```text
lease_owner = worker_id
lease_expires_at = now + lease_ttl
heartbeat_at = now
attempt = attempt + 1
started_at = now
```

必须使用事务 + 条件更新，保证同一任务只能被一个 worker 领取。

### 6.2 Heartbeat

执行过程中 worker 定期调用 `Heartbeat(taskID, workerID)`。

仅允许当前 lease owner 刷新：

```text
lease_expires_at = now + lease_ttl
heartbeat_at = now
```

如果任务已进入 `CANCELING`，heartbeat 返回 `cancel_requested=true`，worker 应尽快停止执行并调用 `AcknowledgeCancel`。

### 6.3 Recovery

recovery job 定期扫描：

```text
status = RUNNING AND lease_expires_at < now
```

处理规则：

- `attempt < max_attempts`：`RUNNING -> READY`，记录 `LEASE_EXPIRED_REQUEUED`；
- `attempt >= max_attempts`：`RUNNING -> FAILED`，记录 `LEASE_EXPIRED_FAILED`。

对 `CANCELING` 做兜底：

- 如果 `CANCELING` 超过配置阈值仍未确认：`CANCELING -> CANCELED`。

### 6.4 幂等完成与迟到上报保护

worker 调用 `Complete`、`Fail`、`AcknowledgeCancel` 时必须带 `worker_id + attempt`。

只有匹配当前 `lease_owner` 和 `attempt` 的 worker 可以完成任务。旧 worker 的迟到上报必须被拒绝，且不得覆盖新 attempt 的状态或结果。

## 7. HTTP/API 与前端兼容策略

### 7.1 HTTP handler

HTTP handler 改为调用 task core app 用例：

- `POST /download`
  - URL 模式：创建 `url` 任务并置为 `READY`。
- `POST /api/tasks/upload/init`
  - 创建 `upload` 任务，状态为 `CREATED`。
- `PUT /api/tasks/{id}/upload-source`
  - 保存源包，写 input；
  - 更新上传进度；
  - `CREATED -> READY`。
- `POST /api/tasks/{id}/cancel`
  - 调用 `RequestCancel`。
- `POST /api/tasks/{id}/retry`
  - 调用 `Retry`。
- `GET /api/tasks`
  - 返回新 task core 的日志列表视图。
- `HEAD/GET /api/tasks/{id}/download`
  - 使用统一 `CanDownload`。
- `POST /api/tasks/{id}/copy-to-komga`
  - 使用统一 `CanCopyToKomga`。

### 7.2 前端 presenter

后端为前端输出稳定视图字段：

```text
status_label
phase_label
progress.current
progress.total
progress.unit
available_actions[]
```

按钮显隐全部以后端 `available_actions` 为准：

- `cancel`
- `retry`
- `download`
- `copy_to_komga`

前端只展示，不再复制业务资格判断。

### 7.3 Legacy 层

由于本轮允许破坏性重构：

- 不维护旧任务数据兼容；
- 新日志页接口不依赖 legacy adapter；
- 如果前端仍需要旧字段，由新 presenter 输出兼容字段，而不是让 legacy adapter 继续承担事实语义。

## 8. 测试策略

### 8.1 领域状态机单元测试

覆盖：

- 所有合法流转；
- 非法流转必须拒绝；
- 终态不可 cancel/retry；
- `FAILED/CANCELED -> READY` retry；
- `RUNNING -> READY` 仅允许 recovery；
- 迟到 worker 上报不能覆盖新 attempt。

### 8.2 App 用例测试

覆盖：

- 创建 URL 任务后直接 `READY`；
- 上传任务 `CREATED -> READY`；
- cancel 在 `CREATED/READY/RUNNING` 下的行为；
- retry 重置错误、lease、进度、结果；
- complete 必须生成 result；
- fail 必须记录 `last_error`；
- recovery 扫描 lease 过期任务并 requeue/fail。

### 8.3 Store 集成测试

基于 Postgres 覆盖：

- claim 并发只能一个 worker 成功；
- heartbeat 只允许 lease owner；
- complete/fail 必须匹配 `worker_id + attempt`；
- task + event 写入事务一致；
- list tasks 稳定输出前端需要的排序和 actions。

### 8.4 E2E / Smoke

覆盖：

- URL 下载成功；
- 上传压缩包成功；
- cancel 一个运行中任务；
- failed/canceled retry；
- 成功任务下载预检；
- Komga copy 路径；
- worker 重启后任务被 recovery 处理。

发布门禁使用：

```bash
bash scripts/verify_release_gates.sh
```

## 9. 发布与回滚

### 发布方式

1. 新增 task core 表和代码；
2. 让新 HTTP/worker 全部切到 task core；
3. 本地旧任务数据不迁移；
4. README/runbook 明确说明：这是任务系统破坏性重构，旧任务日志不保留；
5. 通过发布门禁后再部署。

### 回滚方式

- 保留旧表，不主动 drop；
- 新表使用独立名称 `task_core_*`；
- 若回滚到旧代码，旧代码仍可读旧表；
- 新表数据可丢弃。

## 10. 验收标准

1. 任务状态只能通过 task core 合法流转。
2. HTTP、worker、store 不再各自实现 cancel/retry/download/copy 资格规则。
3. worker 崩溃后，lease 过期任务会被 recovery requeue 或 fail。
4. 迟到 worker 上报不能覆盖新 attempt。
5. 前端日志页按钮由 `available_actions` 驱动。
6. URL 下载、上传、取消、重试、下载预检、Komga copy 均有 E2E 或 smoke 覆盖。
7. `scripts/verify_release_gates.sh` 通过。

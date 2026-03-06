# 全项目 Go 全面重写（单阶段切换）Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在 5-8 周内完成全项目 Go 主线重构，重定 `/v2` 协议，前端整体改造，并完成历史数据全量迁移后一次性切换。

**Architecture:** 新系统采用 `go-api + go-worker + postgres + redis streams`。`go-api` 负责协议与编排入口，`go-worker` 负责下载执行与状态推进；Postgres 为唯一业务真相，Redis 仅承担异步调度。前端切到新协议客户端，不保留旧接口兼容层。

**Tech Stack:** Go 1.23、chi、pgx、redis/go-redis、PostgreSQL、Redis Streams、htmx+vanilla JS（重构后模块化）、Playwright、Docker Compose

---

### Task 1: 搭建 v2 领域模型与状态机骨架

**Files:**
- Create: `go-backend/internal/domain/v2/task.go`
- Create: `go-backend/internal/domain/v2/state_machine.go`
- Create: `go-backend/internal/domain/v2/errors.go`
- Create: `go-backend/internal/domain/v2/state_machine_test.go`

**Step 1: Write the failing test**

```go
func TestTransitionAllowsQueuedToRunning(t *testing.T) {}
func TestTransitionRejectsRunningToQueued(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/v2 -run TestTransitionAllowsQueuedToRunning -count=1`
Expected: FAIL（状态机函数未实现）

**Step 3: Write minimal implementation**

```go
var allowed = map[Status]map[Status]struct{}{
    StatusQueued:  {StatusRunning: {}},
    StatusRunning: {StatusSuccess: {}, StatusFailed: {}, StatusCanceled: {}},
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/v2 -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/domain/v2/task.go go-backend/internal/domain/v2/state_machine.go go-backend/internal/domain/v2/errors.go go-backend/internal/domain/v2/state_machine_test.go
git commit -m "feat: add v2 domain state machine"
```

### Task 2: 建立 v2 数据库 schema 与仓储读写接口

**Files:**
- Create: `go-backend/internal/store/postgres/migrations/002_v2_schema.sql`
- Create: `go-backend/internal/store/postgres/v2_repo.go`
- Create: `go-backend/internal/store/postgres/v2_repo_test.go`

**Step 1: Write the failing test**

```go
func TestCreateTaskPersistsQueuedTask(t *testing.T) {}
func TestAppendEventPersistsTransitionAudit(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/postgres -run TestCreateTaskPersistsQueuedTask -count=1`
Expected: FAIL（v2 repo 与表结构不存在）

**Step 3: Write minimal implementation**

```go
type V2TaskRepo interface {
    CreateTask(ctx context.Context, in CreateTaskInput) (TaskRecord, error)
    UpdateTaskStatus(ctx context.Context, id string, from, to string, patch StatusPatch) error
    AppendTaskEvent(ctx context.Context, event TaskEvent) error
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/store/postgres -run TestCreateTaskPersistsQueuedTask -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/store/postgres/migrations/002_v2_schema.sql go-backend/internal/store/postgres/v2_repo.go go-backend/internal/store/postgres/v2_repo_test.go
git commit -m "feat: add v2 postgres schema and repository"
```

### Task 3: 建立 v2 队列协议与 Redis Streams 生产/消费封装

**Files:**
- Create: `go-backend/internal/queue/v2/message.go`
- Create: `go-backend/internal/queue/v2/producer.go`
- Create: `go-backend/internal/queue/v2/consumer.go`
- Create: `go-backend/internal/queue/v2/queue_test.go`

**Step 1: Write the failing test**

```go
func TestProducerWritesTaskMessage(t *testing.T) {}
func TestConsumerParsesTaskMessage(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/queue/v2 -run TestProducerWritesTaskMessage -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

```go
type TaskMessage struct {
    TaskID string
    Token  string
}
```

- producer 仅写 `task_id/token`。
- consumer 仅解析并返回 typed message。

**Step 4: Run test to verify it passes**

Run: `go test ./internal/queue/v2 -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/queue/v2/message.go go-backend/internal/queue/v2/producer.go go-backend/internal/queue/v2/consumer.go go-backend/internal/queue/v2/queue_test.go
git commit -m "feat: add v2 redis streams queue contract"
```

### Task 4: 实现 `/v2/tasks` 创建与查询 API

**Files:**
- Create: `go-backend/internal/httpv2/router.go`
- Create: `go-backend/internal/httpv2/tasks_handler.go`
- Create: `go-backend/internal/httpv2/tasks_handler_test.go`
- Modify: `go-backend/cmd/server/main.go`

**Step 1: Write the failing test**

```go
func TestCreateTaskReturns202AndTaskID(t *testing.T) {}
func TestListTasksReturnsPagination(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/httpv2 -run TestCreateTaskReturns202AndTaskID -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

```go
r.Post("/v2/tasks", h.CreateTask)
r.Get("/v2/tasks", h.ListTasks)
r.Get("/v2/tasks/{task_id}", h.GetTask)
```

- CreateTask: 入库 `QUEUED` + enqueue。
- ListTasks: 支持 `page/per_page/status/q`。

**Step 4: Run test to verify it passes**

Run: `go test ./internal/httpv2 -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/httpv2/router.go go-backend/internal/httpv2/tasks_handler.go go-backend/internal/httpv2/tasks_handler_test.go go-backend/cmd/server/main.go
git commit -m "feat: add v2 task create and query endpoints"
```

### Task 5: 实现 worker 执行主链路（下载、重试、状态推进）

**Files:**
- Modify: `go-backend/cmd/worker/main.go`
- Create: `go-backend/internal/worker/v2_executor.go`
- Create: `go-backend/internal/worker/v2_executor_test.go`
- Modify: `go-backend/internal/downloader/downloader.go`
- Modify: `go-backend/internal/downloader/downloader_test.go`

**Step 1: Write the failing test**

```go
func TestExecutorMarksSuccessAndStoresArtifact(t *testing.T) {}
func TestExecutorMarksFailedAfterRetryExhausted(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/worker -run TestExecutorMarksSuccessAndStoresArtifact -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

```go
// execute flow: claim -> RUNNING -> download -> package -> SUCCESS
// error flow: retry transient -> FAILED
```

- 保持 `limit/cancel` 快速失败语义。
- 每次流转写入 `task_events`。

**Step 4: Run test to verify it passes**

Run: `go test ./internal/worker -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/cmd/worker/main.go go-backend/internal/worker/v2_executor.go go-backend/internal/worker/v2_executor_test.go go-backend/internal/downloader/downloader.go go-backend/internal/downloader/downloader_test.go
git commit -m "feat: add v2 worker execution pipeline"
```

### Task 6: 实现 `/v2/tasks/{id}/cancel` 与 `/v2/tasks/{id}/artifact`

**Files:**
- Modify: `go-backend/internal/httpv2/tasks_handler.go`
- Modify: `go-backend/internal/httpv2/tasks_handler_test.go`
- Create: `go-backend/internal/service/v2_artifact_service.go`
- Create: `go-backend/internal/service/v2_artifact_service_test.go`

**Step 1: Write the failing test**

```go
func TestCancelTaskReturnsAccepted(t *testing.T) {}
func TestDownloadArtifactReturnsFile(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/httpv2 -run TestCancelTaskReturnsAccepted -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

```go
r.Post("/v2/tasks/{task_id}/cancel", h.CancelTask)
r.Get("/v2/tasks/{task_id}/artifact", h.DownloadArtifact)
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/httpv2 -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/httpv2/tasks_handler.go go-backend/internal/httpv2/tasks_handler_test.go go-backend/internal/service/v2_artifact_service.go go-backend/internal/service/v2_artifact_service_test.go
git commit -m "feat: add v2 cancel and artifact endpoints"
```

### Task 7: 实现 `/v2/settings` 与配置生效链路

**Files:**
- Create: `go-backend/internal/httpv2/settings_handler.go`
- Create: `go-backend/internal/httpv2/settings_handler_test.go`
- Modify: `go-backend/internal/config/config.go`
- Modify: `go-backend/internal/config/config_test.go`

**Step 1: Write the failing test**

```go
func TestUpdateSettingsPersistsAndReturnsSnapshot(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/httpv2 -run TestUpdateSettingsPersistsAndReturnsSnapshot -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

```go
r.Get("/v2/settings", h.GetSettings)
r.Put("/v2/settings", h.UpdateSettings)
```

- settings 写入 `app_settings`。
- worker 拉取或缓存最新配置快照。

**Step 4: Run test to verify it passes**

Run: `go test ./internal/httpv2 ./internal/config -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/httpv2/settings_handler.go go-backend/internal/httpv2/settings_handler_test.go go-backend/internal/config/config.go go-backend/internal/config/config_test.go
git commit -m "feat: add v2 runtime settings endpoints"
```

### Task 8: 新增全量数据迁移工具（旧库 -> v2 schema）

**Files:**
- Create: `go-backend/tools/migration/cmd/main.go`
- Create: `go-backend/tools/migration/legacy_reader.go`
- Create: `go-backend/tools/migration/v2_writer.go`
- Create: `go-backend/tools/migration/checksum.go`
- Create: `go-backend/tools/migration/migration_test.go`

**Step 1: Write the failing test**

```go
func TestMigrationCopiesAllTasksAndEvents(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tools/migration -run TestMigrationCopiesAllTasksAndEvents -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

```go
// read legacy rows in batches -> map -> write v2 rows -> validate counts/checksum
```

**Step 4: Run test to verify it passes**

Run: `go test ./tools/migration -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/tools/migration/cmd/main.go go-backend/tools/migration/legacy_reader.go go-backend/tools/migration/v2_writer.go go-backend/tools/migration/checksum.go go-backend/tools/migration/migration_test.go
git commit -m "feat: add full data migration tool for v2"
```

### Task 9: 前端整体切到 v2 协议客户端

**Files:**
- Create: `static/v2/api-client.js`
- Create: `static/v2/dashboard.js`
- Create: `static/v2/tasks.js`
- Create: `templates/v2/index.html`
- Create: `templates/v2/tasks.html`
- Modify: `templates/base.html`
- Create: `tests/e2e/v2/tasks-flow.spec.ts`

**Step 1: Write the failing test**

```ts
test('create task -> cancel -> filter -> download flow on v2', async () => {})
```

**Step 2: Run test to verify it fails**

Run: `npm run e2e:test -- tests/e2e/v2/tasks-flow.spec.ts`
Expected: FAIL（页面与接口未接 v2）

**Step 3: Write minimal implementation**

```js
export async function createTask(payload) {
  return fetch('/v2/tasks', { method: 'POST', body: JSON.stringify(payload) })
}
```

- 首页/日志页都改为调用 `static/v2/api-client.js`。

**Step 4: Run test to verify it passes**

Run: `npm run e2e:test -- tests/e2e/v2/tasks-flow.spec.ts`
Expected: PASS

**Step 5: Commit**

```bash
git add static/v2/api-client.js static/v2/dashboard.js static/v2/tasks.js templates/v2/index.html templates/v2/tasks.html templates/base.html tests/e2e/v2/tasks-flow.spec.ts
git commit -m "feat: migrate frontend to v2 api client"
```

### Task 10: Compose 单主线部署与路由收敛

**Files:**
- Modify: `docker-compose.yml`
- Modify: `deploy/nginx/canary-go-full.conf`
- Modify: `README.md`

**Step 1: Write the failing test**

```go
func TestComposeTopologyMatchesSingleGoStack(t *testing.T) {}
```

- 该测试可放在 `go-backend/internal/config/compose_contract_test.go`，读取 compose 模板关键字段断言。

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config -run TestComposeTopologyMatchesSingleGoStack -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

- Compose 仅保留：`go-api/go-worker/postgres/redis/gateway`。
- 网关页面与 API 统一转发到 Go。
- README 改为新主线运行说明。

**Step 4: Run test to verify it passes**

Run:
- `go test ./internal/config -count=1`
- `docker compose config`
Expected: PASS

**Step 5: Commit**

```bash
git add docker-compose.yml deploy/nginx/canary-go-full.conf README.md go-backend/internal/config/compose_contract_test.go
git commit -m "chore: switch compose topology to single go stack"
```

### Task 11: 新协议契约测试与关键回归补齐

**Files:**
- Create: `go-backend/internal/httpv2/contract_test.go`
- Modify: `go-backend/internal/httpv2/tasks_handler_test.go`
- Modify: `go-backend/internal/httpv2/settings_handler_test.go`

**Step 1: Write the failing test**

```go
func TestContract_V2CreateTaskSchema(t *testing.T) {}
func TestContract_V2SummarySchema(t *testing.T) {}
func TestContract_V2TaskDetailSchema(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/httpv2 -run TestContract_ -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

- 冻结 v2 响应字段、类型、错误结构。
- 对 4xx/5xx 的 message 统一约定并断言。

**Step 4: Run test to verify it passes**

Run: `go test ./internal/httpv2 -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/httpv2/contract_test.go go-backend/internal/httpv2/tasks_handler_test.go go-backend/internal/httpv2/settings_handler_test.go
git commit -m "test: freeze v2 api contract schemas"
```

### Task 12: 切换前总验收与发布文档

**Files:**
- Modify: `docs/PROJECT_UPDATES.md`
- Create: `docs/runbooks/v2-cutover-checklist.md`
- Create: `docs/runbooks/v2-rollback-checklist.md`

**Step 1: Run complete validation**

Run:
- `cd go-backend && go test ./...`
- `cd go-backend && go vet ./...`
- `cd go-backend && go test -race ./...`
- `npm run e2e:test`
Expected: PASS

**Step 2: Run compose smoke**

Run:
- `docker compose up -d --build`
- `curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/v2/dashboard/summary`
- `curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/v2/tasks`
Expected: 200 + valid JSON

**Step 3: Update release and runbook docs**

- 记录：架构图、迁移步骤、切换窗口 checklist、失败回退流程。

**Step 4: Commit docs**

```bash
git add docs/PROJECT_UPDATES.md docs/runbooks/v2-cutover-checklist.md docs/runbooks/v2-rollback-checklist.md
git commit -m "docs: finalize v2 cutover and rollback runbooks"
```

---

## 执行约束

- 每个任务严格按 `@superpowers/test-driven-development` 执行 RED -> GREEN -> REFACTOR。
- 每个任务完成后运行最小必要验证后再 commit。
- 最终声明完成前必须执行 `@superpowers/verification-before-completion`。
- 仅实现本计划范围，不做计划外扩展（YAGNI）。

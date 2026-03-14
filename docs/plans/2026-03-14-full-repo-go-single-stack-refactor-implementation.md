# Full Repo Go Single-Stack Refactor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将仓库重构为 Go 单栈主线，优先保障下载创建、日志查询、任务取消、产物下载四个核心闭环，并显著提升代码可维护性。

**Architecture:** 以分层重构为主线：把后端收敛为 `api/http -> application -> domain -> infrastructure` 四层，统一任务状态机与幂等策略；前端收敛为页面模块 + 共享 API 客户端；Python 运行时路径退役为工具/迁移用途。重构按阶段执行，允许停机切换与接口重整。

**Tech Stack:** Go (chi, pgx, redis), PostgreSQL, Redis Streams, Vite/ESM, Node test runner, Playwright, Docker Compose.

**Skills:** 执行时使用 @using-git-worktrees、@test-driven-development、@systematic-debugging、@verification-before-completion。

**Preflight:** 先在独立 worktree 执行。命令：`codex skill using-git-worktrees`（或手工 `git worktree add`）并确保 `git status --short` 初始干净。

---

### Task 1: 建立后端四层骨架与统一依赖装配入口

**Files:**
- Create: `go-backend/internal/bootstrap/server.go`
- Create: `go-backend/internal/bootstrap/server_test.go`
- Create: `go-backend/internal/application/ports.go`
- Create: `go-backend/internal/application/errors.go`
- Modify: `go-backend/cmd/server/main.go`
- Modify: `go-backend/cmd/worker/main.go`

**Step 1: Write the failing test**

在 `server_test.go` 新增：

```go
func TestBuildServerWiresDependencies(t *testing.T) {
    _, err := BuildServer(context.Background(), Config{})
    if err == nil {
        t.Fatalf("expected dependency validation error")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd go-backend && go test ./internal/bootstrap -run TestBuildServerWiresDependencies -count=1`
Expected: FAIL（`BuildServer` 尚不存在）

**Step 3: Write minimal implementation**

在 `server.go` 写最小装配器：

```go
func BuildServer(ctx context.Context, cfg Config) (*http.Server, error) {
    if strings.TrimSpace(cfg.DatabaseURL) == "" {
        return nil, errors.New("database url is required")
    }
    // 构建 router + infra + usecases
    return &http.Server{Addr: cfg.Addr, Handler: cfg.Router}, nil
}
```

并让 `cmd/server/main.go`、`cmd/worker/main.go` 改为调用 bootstrap。

**Step 4: Run test to verify it passes**

Run: `cd go-backend && go test ./internal/bootstrap -run TestBuildServerWiresDependencies -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/bootstrap/server.go go-backend/internal/bootstrap/server_test.go go-backend/internal/application/ports.go go-backend/internal/application/errors.go go-backend/cmd/server/main.go go-backend/cmd/worker/main.go
git commit -m "refactor(bootstrap): add layered wiring entrypoints"
```

---

### Task 2: 收敛 Domain 任务状态机与幂等键策略

**Files:**
- Create: `go-backend/internal/domain/task/state.go`
- Create: `go-backend/internal/domain/task/state_test.go`
- Create: `go-backend/internal/domain/task/idempotency.go`
- Create: `go-backend/internal/domain/task/idempotency_test.go`
- Modify: `go-backend/internal/domain/v2/state_machine.go`
- Modify: `go-backend/internal/domain/v2/state_machine_test.go`

**Step 1: Write the failing test**

在 `state_test.go` 新增迁移合法性断言：

```go
func TestCanTransition(t *testing.T) {
    if !CanTransition(StatusPending, StatusRunning) {
        t.Fatalf("pending -> running should be allowed")
    }
    if CanTransition(StatusSucceeded, StatusRunning) {
        t.Fatalf("succeeded -> running should be rejected")
    }
}
```

在 `idempotency_test.go` 新增：

```go
func TestBuildIdempotencyKeyStable(t *testing.T) {
    keyA := BuildIdempotencyKey("https://telegra.ph/a", map[string]string{"author": "x"})
    keyB := BuildIdempotencyKey("https://telegra.ph/a", map[string]string{"author": "x"})
    if keyA != keyB {
        t.Fatalf("expected stable key")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd go-backend && go test ./internal/domain/task -count=1`
Expected: FAIL（新包尚不存在）

**Step 3: Write minimal implementation**

在 `state.go` 定义状态与迁移表：

```go
var allowedTransitions = map[Status]map[Status]bool{
    StatusPending: {StatusRunning: true, StatusCanceled: true},
    StatusRunning: {StatusSucceeded: true, StatusFailed: true, StatusCanceled: true},
}
```

在 `idempotency.go` 定义规范化与哈希：

```go
func BuildIdempotencyKey(url string, metadata map[string]string) string {
    normalized := normalize(url, metadata)
    sum := sha256.Sum256([]byte(normalized))
    return hex.EncodeToString(sum[:])
}
```

并让 `internal/domain/v2/state_machine.go` 复用新规则。

**Step 4: Run test to verify it passes**

Run: `cd go-backend && go test ./internal/domain/task ./internal/domain/v2 -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/domain/task/state.go go-backend/internal/domain/task/state_test.go go-backend/internal/domain/task/idempotency.go go-backend/internal/domain/task/idempotency_test.go go-backend/internal/domain/v2/state_machine.go go-backend/internal/domain/v2/state_machine_test.go
git commit -m "refactor(domain): unify task state machine and idempotency key"
```

---

### Task 3: 定义 Application 端口并实现 Postgres 仓储适配

**Files:**
- Create: `go-backend/internal/application/ports/task_repository.go`
- Create: `go-backend/internal/infrastructure/postgres/task_repository.go`
- Create: `go-backend/internal/infrastructure/postgres/task_repository_test.go`
- Modify: `go-backend/internal/store/postgres/v2_repo.go`
- Modify: `go-backend/internal/store/postgres/v2_repo_test.go`

**Step 1: Write the failing test**

在 `task_repository_test.go` 添加：

```go
func TestTaskRepositoryCreateAndGet(t *testing.T) {
    repo := newTestRepo(t)
    created, err := repo.CreateTask(context.Background(), application.CreateTaskInput{URL: "https://telegra.ph/a"})
    if err != nil {
        t.Fatalf("create: %v", err)
    }
    got, err := repo.GetTask(context.Background(), created.ID)
    if err != nil || got.ID != created.ID {
        t.Fatalf("get mismatch")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd go-backend && go test ./internal/infrastructure/postgres -run TestTaskRepositoryCreateAndGet -count=1`
Expected: FAIL（adapter 未实现）

**Step 3: Write minimal implementation**

在 `task_repository.go` 实现 `TaskRepository` 接口，先桥接现有 `v2_repo`：

```go
type TaskRepository struct { inner *postgres.V2TaskRepo }

func (r *TaskRepository) CreateTask(ctx context.Context, in application.CreateTaskInput) (application.TaskRecord, error) {
    row, err := r.inner.CreateTask(ctx, postgres.CreateTaskParams{URL: in.URL})
    if err != nil { return application.TaskRecord{}, err }
    return toRecord(row), nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd go-backend && go test ./internal/infrastructure/postgres ./internal/store/postgres -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/application/ports/task_repository.go go-backend/internal/infrastructure/postgres/task_repository.go go-backend/internal/infrastructure/postgres/task_repository_test.go go-backend/internal/store/postgres/v2_repo.go go-backend/internal/store/postgres/v2_repo_test.go
git commit -m "refactor(infra): add postgres task repository adapter for application layer"
```

---

### Task 4: 实现 Redis Streams 队列端口并统一消息协议

**Files:**
- Create: `go-backend/internal/application/ports/task_queue.go`
- Create: `go-backend/internal/infrastructure/redisstream/task_queue.go`
- Create: `go-backend/internal/infrastructure/redisstream/task_queue_test.go`
- Modify: `go-backend/internal/queue/v2/message.go`
- Modify: `go-backend/internal/queue/v2/producer.go`
- Modify: `go-backend/internal/queue/v2/queue_test.go`

**Step 1: Write the failing test**

在 `task_queue_test.go` 新增：

```go
func TestPublishTaskMessageIncludesIdempotencyKey(t *testing.T) {
    q := newFakeQueue()
    err := q.Publish(context.Background(), application.TaskMessage{TaskID: "t1", IdempotencyKey: "k1"})
    if err != nil {
        t.Fatalf("publish: %v", err)
    }
    if q.last.IdempotencyKey != "k1" {
        t.Fatalf("missing idempotency key")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd go-backend && go test ./internal/infrastructure/redisstream -run TestPublishTaskMessageIncludesIdempotencyKey -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

在 `task_queue.go` 适配现有 v2 producer：

```go
func (q *TaskQueue) Publish(ctx context.Context, msg application.TaskMessage) error {
    return q.producer.Enqueue(ctx, queuev2.Message{
        TaskID: msg.TaskID,
        URL: msg.URL,
        IdempotencyKey: msg.IdempotencyKey,
    })
}
```

并在 `message.go` 补齐 `IdempotencyKey` 字段和序列化单测。

**Step 4: Run test to verify it passes**

Run: `cd go-backend && go test ./internal/infrastructure/redisstream ./internal/queue/v2 -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/application/ports/task_queue.go go-backend/internal/infrastructure/redisstream/task_queue.go go-backend/internal/infrastructure/redisstream/task_queue_test.go go-backend/internal/queue/v2/message.go go-backend/internal/queue/v2/producer.go go-backend/internal/queue/v2/queue_test.go
git commit -m "refactor(queue): add application queue port and unified stream message"
```

---

### Task 5: 落地 CreateDownload 用例（创建 + 幂等 + 入队）

**Files:**
- Create: `go-backend/internal/application/usecases/create_download.go`
- Create: `go-backend/internal/application/usecases/create_download_test.go`
- Modify: `go-backend/internal/httpapi/download_request.go`
- Modify: `go-backend/internal/httpapi/download_response.go`
- Modify: `go-backend/internal/httpapi/api.go`

**Step 1: Write the failing test**

在 `create_download_test.go` 新增：

```go
func TestCreateDownloadReusesExistingTaskByIdempotencyKey(t *testing.T) {
    uc := newUseCaseWithFakes()
    first, _ := uc.Execute(context.Background(), Input{URL: "https://telegra.ph/a"})
    second, _ := uc.Execute(context.Background(), Input{URL: "https://telegra.ph/a"})
    if first.TaskID != second.TaskID {
        t.Fatalf("expected idempotent reuse")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd go-backend && go test ./internal/application/usecases -run TestCreateDownloadReusesExistingTaskByIdempotencyKey -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

在 `create_download.go` 实现：

```go
func (u *CreateDownload) Execute(ctx context.Context, in Input) (Output, error) {
    key := task.BuildIdempotencyKey(in.URL, in.Metadata)
    if existing, ok := u.repo.FindByIdempotencyKey(ctx, key); ok {
        return Output{TaskID: existing.ID, Decision: "reused"}, nil
    }
    created, err := u.repo.CreateTask(ctx, application.CreateTaskInput{URL: in.URL, IdempotencyKey: key})
    if err != nil { return Output{}, err }
    if err := u.queue.Publish(ctx, application.TaskMessage{TaskID: created.ID, URL: in.URL, IdempotencyKey: key}); err != nil {
        return Output{}, err
    }
    return Output{TaskID: created.ID, Decision: "created"}, nil
}
```

再将 HTTP handler 改为只做 DTO 映射并调用用例。

**Step 4: Run test to verify it passes**

Run: `cd go-backend && go test ./internal/application/usecases ./internal/httpapi -run 'TestCreateDownloadReusesExistingTaskByIdempotencyKey|TestDownloadCreatesTaskAndSubmitsJob' -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/application/usecases/create_download.go go-backend/internal/application/usecases/create_download_test.go go-backend/internal/httpapi/download_request.go go-backend/internal/httpapi/download_response.go go-backend/internal/httpapi/api.go
git commit -m "feat(application): route download creation through usecase"
```

---

### Task 6: 落地 ListTasks / CancelTask / GetArtifact 用例

**Files:**
- Create: `go-backend/internal/application/usecases/list_tasks.go`
- Create: `go-backend/internal/application/usecases/list_tasks_test.go`
- Create: `go-backend/internal/application/usecases/cancel_task.go`
- Create: `go-backend/internal/application/usecases/cancel_task_test.go`
- Create: `go-backend/internal/application/usecases/get_artifact.go`
- Create: `go-backend/internal/application/usecases/get_artifact_test.go`
- Modify: `go-backend/internal/httpv2/tasks_handler.go`
- Modify: `go-backend/internal/httpapi/api.go`

**Step 1: Write the failing test**

分别新增测试：

```go
func TestListTasksBuildsSharedFilters(t *testing.T) { /* ... */ }
func TestCancelTaskMarksCancelRequested(t *testing.T) { /* ... */ }
func TestGetArtifactRejectsNonTerminalTask(t *testing.T) { /* ... */ }
```

**Step 2: Run test to verify it fails**

Run: `cd go-backend && go test ./internal/application/usecases -run 'TestListTasksBuildsSharedFilters|TestCancelTaskMarksCancelRequested|TestGetArtifactRejectsNonTerminalTask' -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

在三个 usecase 中分别实现：

```go
// cancel_task.go
if !task.CanTransition(record.Status, task.StatusCanceled) && record.Status != task.StatusRunning {
    return ErrTaskAlreadyFinished
}
return u.repo.RequestCancel(ctx, in.TaskID)
```

并让 `httpv2/tasks_handler.go` 与 legacy download endpoint 统一调用 usecase。

**Step 4: Run test to verify it passes**

Run: `cd go-backend && go test ./internal/application/usecases ./internal/httpv2 ./internal/httpapi -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/application/usecases/list_tasks.go go-backend/internal/application/usecases/list_tasks_test.go go-backend/internal/application/usecases/cancel_task.go go-backend/internal/application/usecases/cancel_task_test.go go-backend/internal/application/usecases/get_artifact.go go-backend/internal/application/usecases/get_artifact_test.go go-backend/internal/httpv2/tasks_handler.go go-backend/internal/httpapi/api.go
git commit -m "feat(application): add list cancel artifact usecases"
```

---

### Task 7: 合并 HTTP 入口为统一 API 层并下掉重复路由拼装

**Files:**
- Create: `go-backend/internal/api/http/router.go`
- Create: `go-backend/internal/api/http/router_test.go`
- Create: `go-backend/internal/api/http/tasks_handler.go`
- Create: `go-backend/internal/api/http/tasks_handler_test.go`
- Modify: `go-backend/cmd/server/main.go`
- Modify: `go-backend/internal/httpapi/api.go`
- Modify: `go-backend/internal/httpv2/router.go`

**Step 1: Write the failing test**

在 `router_test.go` 新增：

```go
func TestRouterRegistersCoreEndpoints(t *testing.T) {
    r := NewRouter(Dependencies{})
    req := httptest.NewRequest(http.MethodGet, "/v2/tasks", nil)
    rr := httptest.NewRecorder()
    r.ServeHTTP(rr, req)
    if rr.Code == http.StatusNotFound {
        t.Fatalf("expected route to exist")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd go-backend && go test ./internal/api/http -run TestRouterRegistersCoreEndpoints -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

在 `router.go` 集中注册核心路由：

```go
func NewRouter(deps Dependencies) http.Handler {
    r := chi.NewRouter()
    r.Post("/download", deps.Tasks.CreateDownload)
    r.Get("/v2/tasks", deps.Tasks.ListTasks)
    r.Post("/v2/tasks/{id}/cancel", deps.Tasks.CancelTask)
    r.Get("/api/tasks/{id}/download", deps.Tasks.DownloadArtifact)
    return r
}
```

`cmd/server/main.go` 改为挂载统一 router，`httpapi/httpv2` 仅保留被复用 helper，逐步停止暴露 router 入口。

**Step 4: Run test to verify it passes**

Run: `cd go-backend && go test ./internal/api/http ./cmd/server -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/api/http/router.go go-backend/internal/api/http/router_test.go go-backend/internal/api/http/tasks_handler.go go-backend/internal/api/http/tasks_handler_test.go go-backend/cmd/server/main.go go-backend/internal/httpv2/router.go
git commit -m "refactor(api): consolidate core routes into unified http layer"
```

---

### Task 8: 收敛 Worker 执行链路到统一状态机与 application 端口

**Files:**
- Create: `go-backend/internal/application/usecases/execute_task.go`
- Create: `go-backend/internal/application/usecases/execute_task_test.go`
- Modify: `go-backend/internal/worker/v2_executor.go`
- Modify: `go-backend/internal/worker/v2_executor_test.go`
- Modify: `go-backend/internal/worker/consumer.go`
- Modify: `go-backend/cmd/worker/main.go`

**Step 1: Write the failing test**

在 `execute_task_test.go` 添加：

```go
func TestExecuteTaskTransitionsPendingToSucceeded(t *testing.T) {
    uc := newExecuteTaskUsecaseWithFakes()
    err := uc.Run(context.Background(), Input{TaskID: "t1"})
    if err != nil { t.Fatalf("run: %v", err) }
    assertTransitions(t, uc.repo.History("t1"), "PENDING", "RUNNING", "SUCCEEDED")
}
```

**Step 2: Run test to verify it fails**

Run: `cd go-backend && go test ./internal/application/usecases -run TestExecuteTaskTransitionsPendingToSucceeded -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

在 `execute_task.go` 实现统一迁移：

```go
func (u *ExecuteTask) Run(ctx context.Context, in Input) error {
    if err := u.repo.Transition(ctx, in.TaskID, task.StatusPending, task.StatusRunning); err != nil { return err }
    artifact, err := u.downloader.Download(ctx, in.TaskID)
    if err != nil {
        return u.repo.Transition(ctx, in.TaskID, task.StatusRunning, task.StatusFailed)
    }
    if err := u.repo.SaveArtifact(ctx, in.TaskID, artifact); err != nil { return err }
    return u.repo.Transition(ctx, in.TaskID, task.StatusRunning, task.StatusSucceeded)
}
```

`v2_executor.go` 改为调用 usecase。

**Step 4: Run test to verify it passes**

Run: `cd go-backend && go test ./internal/application/usecases ./internal/worker -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/application/usecases/execute_task.go go-backend/internal/application/usecases/execute_task_test.go go-backend/internal/worker/v2_executor.go go-backend/internal/worker/v2_executor_test.go go-backend/internal/worker/consumer.go go-backend/cmd/worker/main.go
git commit -m "refactor(worker): execute tasks through application usecase"
```

---

### Task 9: 前端页面入口收敛与统一 API 客户端

**Files:**
- Create: `frontend/src/pages/home/module.js`
- Create: `frontend/src/pages/logs/module.js`
- Create: `frontend/src/pages/settings/module.js`
- Create: `frontend/src/shared/api_client.js`
- Create: `frontend/src/tests/api_client.test.mjs`
- Modify: `frontend/src/index.js`
- Modify: `frontend/src/logs.js`
- Modify: `frontend/src/settings.js`
- Modify: `frontend/src/shared/page_modules.js`

**Step 1: Write the failing test**

在 `api_client.test.mjs` 新增：

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { createApiClient } from '../shared/api_client.js';

test('createApiClient normalizes non-2xx errors', async () => {
  const client = createApiClient(async () => new Response('{"message":"Task not found."}', { status: 404 }));
  await assert.rejects(() => client.getTask('missing'), /Task not found/);
});
```

**Step 2: Run test to verify it fails**

Run: `node --test frontend/src/tests/api_client.test.mjs`
Expected: FAIL

**Step 3: Write minimal implementation**

在 `api_client.js` 实现统一请求器：

```js
export function createApiClient(fetchImpl = fetch) {
  async function request(path, init) {
    const response = await fetchImpl(path, init);
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(payload.message || `HTTP ${response.status}`);
    }
    return payload;
  }
  return {
    createDownload: (body) => request('/download', { method: 'POST', body }),
    listTasks: (query) => request(`/v2/tasks?${query}`),
    cancelTask: (id) => request(`/v2/tasks/${id}/cancel`, { method: 'POST' }),
  };
}
```

然后让 home/logs/settings 页面模块通过该客户端访问 API。

**Step 4: Run test to verify it passes**

Run: `npm run test:frontend`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/src/pages/home/module.js frontend/src/pages/logs/module.js frontend/src/pages/settings/module.js frontend/src/shared/api_client.js frontend/src/tests/api_client.test.mjs frontend/src/index.js frontend/src/logs.js frontend/src/settings.js frontend/src/shared/page_modules.js
git commit -m "refactor(frontend): centralize page modules and api client"
```

---

### Task 10: 模板入口与静态产物契约收敛

**Files:**
- Modify: `go-backend/internal/httpui/templates/base.html`
- Modify: `templates/base.html`
- Modify: `vite.config.js`
- Modify: `tests/e2e/specs/backend-smoke.spec.js`
- Modify: `tests/e2e/specs/logs-flow.spec.js`

**Step 1: Write the failing test**

在 `backend-smoke.spec.js` 增加断言：

```js
await expect(page.locator('script[src="/static/dist/app.bundle.js"]')).toHaveCount(1);
await expect(page.locator('script[src="/static/index.js"]')).toHaveCount(0);
```

**Step 2: Run test to verify it fails**

Run: `npm run e2e:test -- --grep "backend smoke"`
Expected: FAIL（若模板仍引用旧入口）

**Step 3: Write minimal implementation**

更新模板为仅加载 dist 产物，移除旧脚本；必要时在 `vite.config.js` 固定 entry：

```js
build: {
  rollupOptions: {
    input: {
      app: 'frontend/src/app.js',
      index: 'frontend/src/index.js',
      logs: 'frontend/src/logs.js',
      settings: 'frontend/src/settings.js',
    },
  },
}
```

**Step 4: Run test to verify it passes**

Run: `npm run build && npm run e2e:test -- --grep "backend smoke|logs flow"`
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/httpui/templates/base.html templates/base.html vite.config.js tests/e2e/specs/backend-smoke.spec.js tests/e2e/specs/logs-flow.spec.js static/dist
git commit -m "build(ui): enforce dist bundle-only template contract"
```

---

### Task 11: Python runtime 退役与文档/CI 收敛

**Files:**
- Modify: `README.md`
- Modify: `docs/PROJECT_UPDATES.md`
- Modify: `.github/workflows/ci.yml`
- Modify: `tests/web/test_go_proxy.py`
- Modify: `tests/web/test_routes_api_logs.py`
- Create: `docs/runbooks/go-single-stack-cutover.md`

**Step 1: Write the failing test**

在 CI 配置检查里新增预期（例如 shell contract test）：

```bash
grep -q "go test ./..." .github/workflows/ci.yml
grep -q "npm run e2e:test" .github/workflows/ci.yml
```

并在 Python 兼容测试中仅保留“显式 compatibility”断言，不再假设 Python 承载主链路。

**Step 2: Run test to verify it fails**

Run: `PYTHONPATH=. .venv/bin/pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q`
Expected: FAIL（旧断言依赖历史行为）

**Step 3: Write minimal implementation**

- README 与 PROJECT_UPDATES 改为“Go 单栈是唯一运行时”。
- CI 保留 Go + Frontend + E2E 主线门禁，Python 测试降级为迁移期可选 job。
- 新增 cutover runbook，写清停机迁移与回滚步骤。

**Step 4: Run test to verify it passes**

Run: `PYTHONPATH=. .venv/bin/pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q`
Expected: PASS（兼容测试仍可运行，但语义已调整）

**Step 5: Commit**

```bash
git add README.md docs/PROJECT_UPDATES.md .github/workflows/ci.yml tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py docs/runbooks/go-single-stack-cutover.md
git commit -m "docs(ci): retire python runtime assumptions and publish cutover runbook"
```

---

### Task 12: 全量验证与最终收口

**Files:**
- Modify: `docs/plans/2026-03-14-full-repo-go-single-stack-refactor-design.md`
- Modify: `docs/plans/2026-03-14-full-repo-go-single-stack-refactor-implementation.md`
- Create: `docs/runbooks/go-single-stack-verification-evidence.md`

**Step 1: Write the failing test**

先准备验证清单文档（若缺任一结果则视为失败）：

- Go unit + race
- Frontend tests + lint + build
- E2E core flows
- Compose smoke (`/healthz`, `/readyz`, `/v2/tasks`)

**Step 2: Run test to verify it fails**

Run:
```bash
cd go-backend && go test ./... && go test -race ./...
cd .. && npm run test:frontend && npm run lint && npm run build && npm run e2e:test
```
Expected: 初次执行可能 FAIL（发现遗漏后回修）

**Step 3: Write minimal implementation**

按失败点逐项修复，直到上述命令全绿；将关键输出摘要写入 `go-single-stack-verification-evidence.md`。

**Step 4: Run test to verify it passes**

Run:
```bash
cd go-backend && go test ./... && go test -race ./...
cd .. && npm run test:frontend && npm run lint && npm run build && npm run e2e:test
curl -fsS http://localhost:${APP_PORT:-5002}/healthz
curl -fsS http://localhost:${APP_PORT:-5002}/readyz
curl -fsS http://localhost:${APP_PORT:-5002}/v2/tasks
```
Expected: 全部 PASS / HTTP 200

**Step 5: Commit**

```bash
git add docs/runbooks/go-single-stack-verification-evidence.md docs/plans/2026-03-14-full-repo-go-single-stack-refactor-design.md docs/plans/2026-03-14-full-repo-go-single-stack-refactor-implementation.md
git commit -m "chore(release): add single-stack verification evidence and finalize plan docs"
```

---

## Completion Criteria

- Go 单栈成为唯一主运行时（核心 API/UI/worker 全链路可运行）。
- 四层架构在目录与依赖方向上可见且可测。
- 核心业务闭环（创建、日志、取消、下载）E2E 通过。
- 停机迁移与回滚 runbook 可直接执行。
- 文档、CI、模板产物契约全部与新主线一致。

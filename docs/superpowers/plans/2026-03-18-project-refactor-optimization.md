# 整仓重构优化 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将当前过渡态仓库分阶段收敛为纯 Go 单栈、单前端构建链路、单测试/交付主线，同时保留现有核心下载与任务能力。

**Architecture:** 先在现有 `go-backend/` 内完成业务语义与应用服务收敛，让 `httpapi/httpv2/worker` 退化为薄边界层；再把前端拆成 shared api/models/modules/bootstrap 并锁定模板/bundle 合同；最后收敛 CI/Compose/runbook，并在末阶段完成 Go 主线提升到仓库根目录、`web/` 资源目录落地、Python compatibility runtime 下线。

**Tech Stack:** Go 1.23, Chi, pgx/PostgreSQL, Redis Streams, HTML templates, Vite/ESM, Node test runner, Playwright, Docker Compose, pytest（仅兼容层下线前阶段保留）。

**Skills:** 执行时使用 @superpowers:using-git-worktrees、@superpowers:test-driven-development、@superpowers:systematic-debugging、@superpowers:verification-before-completion。

**Spec:** `docs/superpowers/specs/2026-03-18-project-refactor-optimization-design.md`

**Scope note:** 该 spec 覆盖 4 个相互依赖的阶段（后端、前端、测试/交付、清理迁移）。本计划保留为一份 master plan，但执行时必须按 Phase 1 → Phase 4 顺序推进；每个 phase 都要在进入下一 phase 前通过本 phase 的回归命令并单独提交。

---

## File Structure Lock-In

### Phase 1（后端核心收敛）

**Create:**
- `go-backend/internal/app/tasks/types.go`
- `go-backend/internal/app/tasks/status_catalog.go`
- `go-backend/internal/app/tasks/metadata.go`
- `go-backend/internal/app/tasks/service.go`
- `go-backend/internal/app/tasks/legacy_bridge.go`
- `go-backend/internal/app/tasks/run_task.go`
- `go-backend/internal/app/tasks/artifact_access.go`
- `go-backend/internal/app/tasks/status_catalog_test.go`
- `go-backend/internal/app/tasks/metadata_test.go`
- `go-backend/internal/app/tasks/service_test.go`
- `go-backend/internal/app/tasks/legacy_bridge_test.go`
- `go-backend/internal/app/tasks/run_task_test.go`
- `go-backend/internal/app/tasks/artifact_access_test.go`

**Modify:**
- `go-backend/internal/httpv2/tasks_handler.go`
- `go-backend/internal/httpv2/tasks_handler_test.go`
- `go-backend/internal/httpv2/contract_test.go`
- `go-backend/internal/httpapi/api.go`
- `go-backend/internal/httpapi/api_test.go`
- `go-backend/internal/httpapi/legacy_adapter.go`
- `go-backend/internal/httpapi/legacy_adapter_test.go`
- `go-backend/internal/service/v2_artifact_service.go`
- `go-backend/internal/service/v2_artifact_service_test.go`
- `go-backend/internal/worker/v2_executor.go`
- `go-backend/internal/worker/v2_executor_test.go`

### Phase 2（前端边界收敛）

**Create:**
- `frontend/src/shared/api/tasks_api.js`
- `frontend/src/shared/models/status_catalog.js`
- `frontend/src/home/form_submission.js`
- `frontend/src/home/summary_panel.js`
- `frontend/src/home/startup_recovery_banner.js`
- `frontend/src/logs/status_filters.js`
- `frontend/src/logs/table_render.js`
- `frontend/src/logs/error_modal.js`
- `frontend/src/logs/download_preflight.js`
- `frontend/src/tests/tasks_api.test.mjs`
- `frontend/src/tests/status_catalog.test.mjs`
- `frontend/src/tests/home_form_submission.test.mjs`
- `frontend/src/tests/home_summary_panel.test.mjs`
- `frontend/src/tests/logs_status_filters.test.mjs`
- `frontend/src/tests/logs_table_render.test.mjs`
- `frontend/src/tests/logs_download_preflight.test.mjs`

**Modify:**
- `frontend/src/home/index.js`
- `frontend/src/logs/index.js`
- `frontend/src/index.js`
- `frontend/src/logs.js`
- `frontend/src/settings.js`
- `frontend/src/app.js`
- `frontend/src/shared/page_modules.js`
- `frontend/src/shared/polling.js`
- `frontend/src/shared/server_messages.js`
- `package.json`
- `vite.config.js`
- `static/dist/app.bundle.js`
- `static/dist/index.bundle.js`
- `static/dist/logs.bundle.js`
- `static/dist/settings.bundle.js`

### Phase 3（测试/交付链路收敛）

**Create:**
- `docs/runbooks/2026-03-18-project-refactor-rollout.md`
- `docs/runbooks/2026-03-18-project-refactor-rollback.md`
- `scripts/verify_release_gates.sh`

**Modify:**
- `.github/workflows/ci.yml`
- `tests/e2e/run-e2e.sh`
- `tests/e2e/specs/backend-smoke.spec.js`
- `tests/e2e/specs/logs-flow.spec.js`
- `tests/e2e/specs/settings.spec.js`
- `tests/e2e/v2/tasks-flow.spec.ts`
- `go-backend/internal/httpui/handler_test.go`
- `tests/web/test_routes_api_logs.py`
- `README.md`
- `docs/PROJECT_UPDATES.md`
- `docker-compose.yml`

### Phase 4（物理迁移与历史清理）

**Create / Move:**
- `go-backend/go.mod -> go.mod`
- `go-backend/go.sum -> go.sum`
- `go-backend/cmd -> cmd`
- `go-backend/internal -> internal`
- `go-backend/internal/httpui/templates -> web/templates`
- `static/style.css -> web/static/style.css`
- `static/dist -> web/static/dist`
- `go-backend/Dockerfile -> Dockerfile`
- `scripts/verify_root_go_layout.sh`
- `scripts/verify_no_legacy_runtime.sh`
- `docs/legacy/python-bridge-history.md`

**Delete:**
- `app.py`
- `downloader_logic.py`
- `task_store.py`
- `requirements.txt`
- `telegram_downloader/`
- `templates/`
- `static/app.js`
- `static/index.js`
- `static/logs.js`
- `static/v2/`
- `tests/web/test_go_proxy.py`
- `tests/web/test_routes_api_logs.py`
- `go-backend/`

**Modify:**
- `.github/workflows/ci.yml`
- `docker-compose.yml`
- `README.md`
- `docs/PROJECT_UPDATES.md`
- `vite.config.js`
- `scripts/verify_release_gates.sh`

---

## Phase 1: 后端核心收敛

### Task 1: 建立 `internal/app/tasks` 骨架与共享状态/元数据语义

**Files:**
- Create: `go-backend/internal/app/tasks/types.go`
- Create: `go-backend/internal/app/tasks/status_catalog.go`
- Create: `go-backend/internal/app/tasks/metadata.go`
- Create: `go-backend/internal/app/tasks/status_catalog_test.go`
- Create: `go-backend/internal/app/tasks/metadata_test.go`
- Modify: `go-backend/internal/httpv2/tasks_handler.go`
- Modify: `go-backend/internal/httpv2/contract_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestStatusCatalogIncludesCancelableAndDownloadableFlags(t *testing.T) {
    catalog := Catalog()
    if !catalog["QUEUED"].CanCancel {
        t.Fatalf("queued should be cancelable")
    }
    if !catalog["SUCCESS"].CanDownload {
        t.Fatalf("success should be downloadable")
    }
}

func TestNormalizeMetadataTrimsAndNormalizesChineseComma(t *testing.T) {
    got := NormalizeMetadata(MetadataInput{TagsRaw: strPtr("a， b "), GenresRaw: strPtr("冒险， 科幻")})
    if got.TagsNormalized == nil || *got.TagsNormalized != "a, b" {
        t.Fatalf("unexpected tags normalization: %#v", got.TagsNormalized)
    }
}
```

并在 `go-backend/internal/httpv2/contract_test.go` 增加断言：dashboard/list 响应暴露 `status_catalog` 字段和稳定的状态标签。

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd go-backend && go test ./internal/app/tasks ./internal/httpv2 -run 'TestStatusCatalog|TestNormalizeMetadata|TestDashboardSummaryContract' -count=1`
Expected: FAIL（`internal/app/tasks` 包尚不存在，或 contract 断言缺少 `status_catalog`）

- [ ] **Step 3: Write minimal implementation**

在 `status_catalog.go` 和 `metadata.go` 写最小实现：

```go
type StatusMeta struct {
    Code        string `json:"code"`
    Label       string `json:"label"`
    CanCancel   bool   `json:"can_cancel"`
    CanDownload bool   `json:"can_download"`
}

func Catalog() map[string]StatusMeta {
    return map[string]StatusMeta{
        "QUEUED":  {Code: "QUEUED", Label: "排队中", CanCancel: true},
        "RUNNING": {Code: "RUNNING", Label: "进行中", CanCancel: true},
        "SUCCESS": {Code: "SUCCESS", Label: "已完成", CanDownload: true},
    }
}
```

并让 `go-backend/internal/httpv2/tasks_handler.go` 的 summary/list 响应直接引用该 catalog，而不是在 handler 中散写状态含义。

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd go-backend && go test ./internal/app/tasks ./internal/httpv2 -run 'TestStatusCatalog|TestNormalizeMetadata|TestDashboardSummaryContract' -count=1`
Expected: PASS

- [ ] **Step 5: Run the broader Phase 1 regression**

Run: `cd go-backend && go test ./internal/httpv2 ./internal/domain/v2 -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go-backend/internal/app/tasks/types.go go-backend/internal/app/tasks/status_catalog.go go-backend/internal/app/tasks/metadata.go go-backend/internal/app/tasks/status_catalog_test.go go-backend/internal/app/tasks/metadata_test.go go-backend/internal/httpv2/tasks_handler.go go-backend/internal/httpv2/contract_test.go
git commit -m "refactor(tasks): add shared task status and metadata semantics"
```

---

### Task 2: 将 `/v2/tasks*` 入口改为应用服务驱动

**Files:**
- Create: `go-backend/internal/app/tasks/service.go`
- Create: `go-backend/internal/app/tasks/service_test.go`
- Modify: `go-backend/internal/httpv2/tasks_handler.go`
- Modify: `go-backend/internal/httpv2/tasks_handler_test.go`
- Modify: `go-backend/internal/httpv2/contract_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestServiceCreateEnqueuesAndReturnsAcceptedTask(t *testing.T) {
    service := newTestService(t)
    result, err := service.Create(context.Background(), CreateInput{URL: "https://telegra.ph/demo"})
    if err != nil || result.TaskID == "" || result.Status != "QUEUED" {
        t.Fatalf("unexpected create result: %#v err=%v", result, err)
    }
}

func TestServiceCancelMapsFinishedTaskToConflict(t *testing.T) {
    service := newTestService(t)
    _, err := service.Cancel(context.Background(), "task-finished")
    if !errors.Is(err, ErrTaskNotCancelable) {
        t.Fatalf("expected ErrTaskNotCancelable, got %v", err)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd go-backend && go test ./internal/app/tasks ./internal/httpv2 -run 'TestServiceCreate|TestServiceCancel|TestCreateTaskHandler|TestCancelTaskHandler' -count=1`
Expected: FAIL（service 不存在，handler 仍直接依赖 store/queue 细节）

- [ ] **Step 3: Write minimal implementation**

在 `service.go` 定义面向用例的服务：

```go
type Service struct {
    Store TaskStore
    Queue TaskQueue
}

func (s *Service) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
    canonical := normalizeCanonicalURL(in.URL, in.CanonicalURL)
    created, err := s.Store.CreateTask(ctx, CreateTaskInput{ID: uuid.NewString(), URL: in.URL, CanonicalURL: strPtr(canonical), EnqueueToken: uuid.NewString()})
    if err != nil { return CreateResult{}, err }
    if err := s.Queue.Enqueue(ctx, TaskQueueMessage{TaskID: created.ID, Token: created.EnqueueToken}); err != nil { return CreateResult{}, err }
    return CreateResult{TaskID: created.ID, Status: created.Status}, nil
}
```

然后让 `TasksHandler` 只负责 request decode / response encode / HTTP 错误映射。

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd go-backend && go test ./internal/app/tasks ./internal/httpv2 -run 'TestServiceCreate|TestServiceCancel|TestCreateTaskHandler|TestCancelTaskHandler' -count=1`
Expected: PASS

- [ ] **Step 5: Run the broader Phase 1 regression**

Run: `cd go-backend && go test ./internal/httpv2 ./internal/store/postgres -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go-backend/internal/app/tasks/service.go go-backend/internal/app/tasks/service_test.go go-backend/internal/httpv2/tasks_handler.go go-backend/internal/httpv2/tasks_handler_test.go go-backend/internal/httpv2/contract_test.go
git commit -m "refactor(httpv2): route task endpoints through app service"
```

---

### Task 3: 将 legacy `/download` 与日志相关决策复用同一应用层

**Files:**
- Create: `go-backend/internal/app/tasks/legacy_bridge.go`
- Create: `go-backend/internal/app/tasks/legacy_bridge_test.go`
- Modify: `go-backend/internal/httpapi/api.go`
- Modify: `go-backend/internal/httpapi/api_test.go`
- Modify: `go-backend/internal/httpapi/legacy_adapter.go`
- Modify: `go-backend/internal/httpapi/legacy_adapter_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestLegacyBridgeReuseSuccessKeepsExistingDownload(t *testing.T) {
    bridge := newLegacyBridge(t)
    result, err := bridge.Submit(context.Background(), LegacySubmitInput{URL: "https://telegra.ph/demo", ReuseSuccess: true})
    if err != nil || result.Decision != "reuse_success" || result.DownloadURL == "" {
        t.Fatalf("unexpected legacy submit result: %#v err=%v", result, err)
    }
}

func TestLegacyBridgeSummaryUsesSharedStatusCatalog(t *testing.T) {
    bridge := newLegacyBridge(t)
    summary, err := bridge.BuildSummary(context.Background())
    if err != nil || summary.StatusCatalog["SUCCESS"].CanDownload != true {
        t.Fatalf("unexpected summary: %#v err=%v", summary, err)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd go-backend && go test ./internal/app/tasks ./internal/httpapi -run 'TestLegacyBridge|TestSubmitDownload|TestLogsSummary' -count=1`
Expected: FAIL（legacy 路径仍将规则散落在 `api.go` / `legacy_adapter.go`）

- [ ] **Step 3: Write minimal implementation**

在 `legacy_bridge.go` 中实现最小桥接服务：

```go
type LegacyBridge struct {
    Tasks   *Service
    Reader  LegacyReader
    Catalog map[string]StatusMeta
}

func (b *LegacyBridge) Submit(ctx context.Context, in LegacySubmitInput) (LegacySubmitResult, error) {
    normalized := NormalizeMetadata(in.Metadata)
    return b.Reader.ClaimOrReuse(ctx, normalized, in.ReuseSuccess)
}
```

并把 `go-backend/internal/httpapi/api.go` 中的 duplicate / summary / status label 判断迁到 bridge 中，让 handler 只负责 form/json 兼容与 redirect/json 输出分流。

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd go-backend && go test ./internal/app/tasks ./internal/httpapi -run 'TestLegacyBridge|TestSubmitDownload|TestLogsSummary' -count=1`
Expected: PASS

- [ ] **Step 5: Run the broader Phase 1 regression**

Run: `cd go-backend && go test ./internal/httpapi -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go-backend/internal/app/tasks/legacy_bridge.go go-backend/internal/app/tasks/legacy_bridge_test.go go-backend/internal/httpapi/api.go go-backend/internal/httpapi/api_test.go go-backend/internal/httpapi/legacy_adapter.go go-backend/internal/httpapi/legacy_adapter_test.go
git commit -m "refactor(httpapi): reuse application task flows for legacy routes"
```

---

### Task 4: 将 worker 执行与产物访问收敛到 `RunTask` / `ArtifactAccess`

**Files:**
- Create: `go-backend/internal/app/tasks/run_task.go`
- Create: `go-backend/internal/app/tasks/run_task_test.go`
- Create: `go-backend/internal/app/tasks/artifact_access.go`
- Create: `go-backend/internal/app/tasks/artifact_access_test.go`
- Modify: `go-backend/internal/worker/v2_executor.go`
- Modify: `go-backend/internal/worker/v2_executor_test.go`
- Modify: `go-backend/internal/service/v2_artifact_service.go`
- Modify: `go-backend/internal/service/v2_artifact_service_test.go`
- Modify: `go-backend/internal/httpv2/tasks_handler.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestRunTaskTransitionsQueuedToSuccess(t *testing.T) {
    runner := newRunTaskUseCase(t)
    err := runner.Execute(context.Background(), "task-1", "token-1")
    if err != nil {
        t.Fatalf("execute: %v", err)
    }
}

func TestArtifactAccessRejectsMissingFile(t *testing.T) {
    access := newArtifactAccess(t)
    _, err := access.Open("/tmp/missing.cbz")
    if !errors.Is(err, ErrArtifactUnavailable) {
        t.Fatalf("expected ErrArtifactUnavailable, got %v", err)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd go-backend && go test ./internal/app/tasks ./internal/worker ./internal/service -run 'TestRunTask|TestArtifactAccess|TestExecute' -count=1`
Expected: FAIL（执行与产物规则仍散在 `v2_executor.go` / `v2_artifact_service.go`）

- [ ] **Step 3: Write minimal implementation**

在 `run_task.go` 中定义执行用例，在 `artifact_access.go` 中统一文件存在性/扩展名/下载句柄：

```go
func (u *RunTaskUseCase) Execute(ctx context.Context, taskID, token string) error {
    snapshot, err := u.Repo.GetTaskForExecution(ctx, taskID)
    if err != nil { return err }
    artifactPath, err := u.Downloader.DownloadAndPackage(ctx, taskID, snapshot.URL, taskMetadataFromSnapshot(snapshot))
    if err != nil { return u.Repo.MarkFailed(ctx, taskID, err) }
    return u.Repo.MarkSucceeded(ctx, taskID, artifactPath)
}
```

然后让 `V2Executor` 只负责 read/ack/retry/reclaim，`TasksHandler.DownloadArtifact` 只依赖 `ArtifactAccess`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd go-backend && go test ./internal/app/tasks ./internal/worker ./internal/service -run 'TestRunTask|TestArtifactAccess|TestExecute' -count=1`
Expected: PASS

- [ ] **Step 5: Run the broader Phase 1 regression**

Run: `cd go-backend && go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go-backend/internal/app/tasks/run_task.go go-backend/internal/app/tasks/run_task_test.go go-backend/internal/app/tasks/artifact_access.go go-backend/internal/app/tasks/artifact_access_test.go go-backend/internal/worker/v2_executor.go go-backend/internal/worker/v2_executor_test.go go-backend/internal/service/v2_artifact_service.go go-backend/internal/service/v2_artifact_service_test.go go-backend/internal/httpv2/tasks_handler.go
git commit -m "refactor(worker): move execution and artifact access into app layer"
```

---

## Phase 2: 前端边界收敛

### Task 5: 提供 shared API client 与 shared status model

**Files:**
- Create: `frontend/src/shared/api/tasks_api.js`
- Create: `frontend/src/shared/models/status_catalog.js`
- Create: `frontend/src/tests/tasks_api.test.mjs`
- Create: `frontend/src/tests/status_catalog.test.mjs`
- Modify: `frontend/src/home/index.js`
- Modify: `frontend/src/logs/index.js`

- [ ] **Step 1: Write the failing tests**

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { createTasksApi } from '../shared/api/tasks_api.js';
import { getStatusMeta } from '../shared/models/status_catalog.js';

test('createTasksApi.createTask sends JSON body and returns parsed payload', async () => {
  const calls = [];
  const api = createTasksApi(async (url, options) => {
    calls.push({ url, options });
    return new Response(JSON.stringify({ task_id: 't1', status: 'QUEUED' }), { status: 202 });
  });
  const result = await api.createTask({ url: 'https://telegra.ph/demo' });
  assert.equal(result.task_id, 't1');
  assert.equal(calls[0].url, '/v2/tasks');
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `node --test frontend/src/tests/tasks_api.test.mjs frontend/src/tests/status_catalog.test.mjs`
Expected: FAIL（模块尚不存在）

- [ ] **Step 3: Write minimal implementation**

在 `tasks_api.js` 中统一 fetch / JSON / HEAD 预检逻辑，在 `status_catalog.js` 中提供稳定 lookup：

```js
export function createTasksApi(fetchImpl = fetch) {
  return {
    async createTask(payload) {
      const response = await fetchImpl('/v2/tasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify(payload),
      });
      return response.json();
    },
  };
}
```

并让 `home/index.js`、`logs/index.js` 先替换最底层 fetch 调用为 shared api。

- [ ] **Step 4: Run tests to verify they pass**

Run: `node --test frontend/src/tests/tasks_api.test.mjs frontend/src/tests/status_catalog.test.mjs`
Expected: PASS

- [ ] **Step 5: Run the broader Phase 2 regression**

Run: `npm run test:frontend`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add frontend/src/shared/api/tasks_api.js frontend/src/shared/models/status_catalog.js frontend/src/tests/tasks_api.test.mjs frontend/src/tests/status_catalog.test.mjs frontend/src/home/index.js frontend/src/logs/index.js
git commit -m "refactor(frontend): add shared tasks api and status model"
```

---

### Task 6: 将首页行为拆成表单、概览、恢复提示三个模块

**Files:**
- Create: `frontend/src/home/form_submission.js`
- Create: `frontend/src/home/summary_panel.js`
- Create: `frontend/src/home/startup_recovery_banner.js`
- Create: `frontend/src/tests/home_form_submission.test.mjs`
- Create: `frontend/src/tests/home_summary_panel.test.mjs`
- Modify: `frontend/src/home/index.js`
- Modify: `frontend/src/index.js`

- [ ] **Step 1: Write the failing tests**

```js
test('submitDownload maps duplicate response to action buttons', async () => {
  const result = buildDuplicateActions({ download_url: '/api/tasks/t1/download' });
  assert.equal(result.downloadLabel, '立即下载');
});

test('renderSummary writes active and success counters', () => {
  const doc = new JSDOM(`<span id="summary-active"></span><span id="summary-success"></span>`).window.document;
  renderSummary(doc, { active_tasks: 2, success_tasks: 5 });
  assert.equal(doc.getElementById('summary-active').textContent, '2');
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `node --test frontend/src/tests/home_form_submission.test.mjs frontend/src/tests/home_summary_panel.test.mjs`
Expected: FAIL（模块尚不存在）

- [ ] **Step 3: Write minimal implementation**

把 `frontend/src/home/index.js` 中的 submit/summary/banner 逻辑分拆成独立函数：

```js
export function renderSummary(doc, summary) {
  doc.getElementById('summary-active').textContent = String(summary.active_tasks || 0);
  doc.getElementById('summary-success').textContent = String(summary.success_tasks || 0);
}
```

`index.js` 只保留 mount/unmount 和模块装配。

- [ ] **Step 4: Run tests to verify they pass**

Run: `node --test frontend/src/tests/home_form_submission.test.mjs frontend/src/tests/home_summary_panel.test.mjs frontend/src/tests/startup_recovery.test.mjs`
Expected: PASS

- [ ] **Step 5: Run broader regression and rebuild bundles**

Run: `npm run test:frontend && npm run build`
Expected: PASS；`static/dist/*.bundle.js` 更新时间戳/内容变化

- [ ] **Step 6: Commit**

```bash
git add frontend/src/home/form_submission.js frontend/src/home/summary_panel.js frontend/src/home/startup_recovery_banner.js frontend/src/tests/home_form_submission.test.mjs frontend/src/tests/home_summary_panel.test.mjs frontend/src/home/index.js frontend/src/index.js static/dist/index.bundle.js static/dist/app.bundle.js
git commit -m "refactor(frontend): split home page orchestration"
```

---

### Task 7: 将日志页行为拆成筛选、表格、错误弹窗、下载预检模块

**Files:**
- Create: `frontend/src/logs/status_filters.js`
- Create: `frontend/src/logs/table_render.js`
- Create: `frontend/src/logs/error_modal.js`
- Create: `frontend/src/logs/download_preflight.js`
- Create: `frontend/src/tests/logs_status_filters.test.mjs`
- Create: `frontend/src/tests/logs_table_render.test.mjs`
- Create: `frontend/src/tests/logs_download_preflight.test.mjs`
- Modify: `frontend/src/logs/index.js`
- Modify: `frontend/src/logs.js`

- [ ] **Step 1: Write the failing tests**

```js
test('applyStatusCatalog normalizes status options', () => {
  const catalog = applyStatusCatalog({ success: { label: '已完成', can_download: true } });
  assert.equal(catalog.SUCCESS.can_download, true);
});

test('normalizeHeadResult returns inline error for missing artifact', async () => {
  const result = await normalizeHeadResult(new Response('', { status: 404 }));
  assert.equal(result.ok, false);
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `node --test frontend/src/tests/logs_status_filters.test.mjs frontend/src/tests/logs_table_render.test.mjs frontend/src/tests/logs_download_preflight.test.mjs`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

将 `frontend/src/logs/index.js` 中的 catalog/render/modal/head 逻辑提取到独立模块，例如：

```js
export function normalizeHeadResult(response) {
  return { ok: response.ok, status: response.status };
}
```

`logs/index.js` 只负责 state、poll schedule 和模块拼装。

- [ ] **Step 4: Run tests to verify they pass**

Run: `node --test frontend/src/tests/logs_status_filters.test.mjs frontend/src/tests/logs_table_render.test.mjs frontend/src/tests/logs_download_preflight.test.mjs`
Expected: PASS

- [ ] **Step 5: Run broader regression and rebuild bundles**

Run: `npm run test:frontend && npm run build`
Expected: PASS；`static/dist/logs.bundle.js`、`static/dist/settings.bundle.js` 更新

- [ ] **Step 6: Commit**

```bash
git add frontend/src/logs/status_filters.js frontend/src/logs/table_render.js frontend/src/logs/error_modal.js frontend/src/logs/download_preflight.js frontend/src/tests/logs_status_filters.test.mjs frontend/src/tests/logs_table_render.test.mjs frontend/src/tests/logs_download_preflight.test.mjs frontend/src/logs/index.js frontend/src/logs.js static/dist/logs.bundle.js static/dist/settings.bundle.js
git commit -m "refactor(frontend): split logs page orchestration"
```

---

### Task 8: 锁定模板 / bundle 合同并收紧前端入口责任

**Files:**
- Modify: `vite.config.js`
- Modify: `package.json`
- Modify: `go-backend/internal/httpui/templates/base.html`
- Modify: `go-backend/internal/httpui/handler_test.go`
- Modify: `templates/base.html`
- Modify: `tests/web/test_routes_api_logs.py`
- Modify: `static/dist/app.bundle.js`
- Modify: `static/dist/index.bundle.js`
- Modify: `static/dist/logs.bundle.js`
- Modify: `static/dist/settings.bundle.js`

- [ ] **Step 1: Write the failing contract assertions**

在 `go-backend/internal/httpui/handler_test.go` 和 `tests/web/test_routes_api_logs.py` 增加断言：

```go
assertContains(t, body, `/static/dist/app.bundle.js`)
assertNotContains(t, body, `/static/index.js`)
assertNotContains(t, body, `/static/logs.js`)
```

并在 Node 侧补一个简单断言：`npm run build` 后只输出 `*.bundle.js` 入口产物。

- [ ] **Step 2: Run tests to verify they fail if contract is broken**

Run: `cd go-backend && go test ./internal/httpui -count=1 && cd .. && PYTHONPATH=. .venv/bin/pytest tests/web/test_routes_api_logs.py -q`
Expected: PASS now；把它们保留为防回退合同（本步骤是把 guard 先写进去）

- [ ] **Step 3: Tighten the implementation to match the contract**

- 保持模板只引用 `dist/*.bundle.js`
- 保持 `package.json` 的 lint/test/build 只围绕源码入口与构建产物
- 确认 `frontend/src/index.js`、`frontend/src/logs.js`、`frontend/src/settings.js` 只是极薄 entry wrapper

必要时将 `vite.config.js` 明确成：

```js
output: { entryFileNames: '[name].bundle.js' }
```

- [ ] **Step 4: Rebuild and verify**

Run: `npm run test:frontend && npm run lint && npm run build`
Expected: PASS

- [ ] **Step 5: Run compatibility contract regression**

Run: `cd go-backend && go test ./internal/httpui -count=1 && cd .. && PYTHONPATH=. .venv/bin/pytest tests/web/test_routes_api_logs.py tests/web/test_go_proxy.py -q`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add vite.config.js package.json go-backend/internal/httpui/templates/base.html go-backend/internal/httpui/handler_test.go templates/base.html tests/web/test_routes_api_logs.py static/dist/app.bundle.js static/dist/index.bundle.js static/dist/logs.bundle.js static/dist/settings.bundle.js
git commit -m "build(frontend): lock template and bundle contract"
```

---

## Phase 3: 测试、CI、Compose、文档收敛

### Task 9: 把 CI / E2E / Compose / runbook 收敛为发布级门禁

**Files:**
- Create: `scripts/verify_release_gates.sh`
- Create: `docs/runbooks/2026-03-18-project-refactor-rollout.md`
- Create: `docs/runbooks/2026-03-18-project-refactor-rollback.md`
- Modify: `.github/workflows/ci.yml`
- Modify: `tests/e2e/run-e2e.sh`
- Modify: `tests/e2e/specs/backend-smoke.spec.js`
- Modify: `tests/e2e/specs/logs-flow.spec.js`
- Modify: `tests/e2e/specs/settings.spec.js`
- Modify: `tests/e2e/v2/tasks-flow.spec.ts`
- Modify: `docker-compose.yml`
- Modify: `README.md`
- Modify: `scripts/verify_release_gates.sh`
- Modify: `docs/PROJECT_UPDATES.md`

- [ ] **Step 1: Write the failing verification script**

创建 `scripts/verify_release_gates.sh`，先用最小可执行门禁表达期望：

```bash
#!/usr/bin/env bash
set -euo pipefail
(cd go-backend && go test ./... && go test -race ./...)
npm run test:frontend
npm run lint
npm run build
PYTHONPATH=. .venv/bin/pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q
npm run e2e:test
```

- [ ] **Step 2: Run the script to establish the current baseline**

Run: `bash scripts/verify_release_gates.sh`
Expected: PASS（若失败，先按 @superpowers:systematic-debugging 记录并修正，再继续本 task）

- [ ] **Step 3: Update CI, e2e, Compose, and docs to match the script**

- `.github/workflows/ci.yml` 的步骤顺序与脚本一致
- `tests/e2e/run-e2e.sh` 以 `/readyz` 为统一等待条件
- `docker-compose.yml` 只表达 `gateway + go-api + go-worker + postgres + redis`
- README / runbook 写清发布、回滚、健康检查、环境变量要求

- [ ] **Step 4: Re-run the release gate locally**

Run: `bash scripts/verify_release_gates.sh`
Expected: PASS

- [ ] **Step 5: Add explicit Compose smoke**

Run: `INTERNAL_ENQUEUE_TOKEN=test-token docker compose up -d --build && curl -fsS http://localhost:5002/healthz && curl -fsS http://localhost:5002/readyz && docker compose down`
Expected: health/ready both HTTP 200, then clean shutdown

- [ ] **Step 6: Commit**

```bash
git add scripts/verify_release_gates.sh .github/workflows/ci.yml tests/e2e/run-e2e.sh tests/e2e/specs/backend-smoke.spec.js tests/e2e/specs/logs-flow.spec.js tests/e2e/specs/settings.spec.js tests/e2e/v2/tasks-flow.spec.ts docker-compose.yml README.md docs/PROJECT_UPDATES.md docs/runbooks/2026-03-18-project-refactor-rollout.md docs/runbooks/2026-03-18-project-refactor-rollback.md
git commit -m "docs(ci): align release gates with go single-stack runtime"
```

---

## Phase 4: 物理迁移与历史清理

### Task 10: 将 Go 主线与 Web 资产提升到仓库根目录

**Files:**
- Create: `scripts/verify_root_go_layout.sh`
- Move: `go-backend/go.mod -> go.mod`
- Move: `go-backend/go.sum -> go.sum`
- Move: `go-backend/cmd -> cmd`
- Move: `go-backend/internal -> internal`
- Move: `go-backend/internal/httpui/templates -> web/templates`
- Move: `static/style.css -> web/static/style.css`
- Move: `static/dist -> web/static/dist`
- Move: `go-backend/Dockerfile -> Dockerfile`
- Modify: `vite.config.js`
- Modify: `docker-compose.yml`
- Modify: `.github/workflows/ci.yml`
- Modify: `README.md`

- [ ] **Step 1: Write the failing layout verifier**

创建 `scripts/verify_root_go_layout.sh`：

```bash
#!/usr/bin/env bash
set -euo pipefail
[ -f go.mod ]
[ -d cmd ]
[ -d internal ]
[ -d web/templates ]
[ -d web/static/dist ]
rg -n 'module github.com/ryancheng/telegram-downloader' go.mod >/dev/null
```

- [ ] **Step 2: Run the verifier and root go test to see the current failure**

Run: `bash scripts/verify_root_go_layout.sh && go test ./...`
Expected: FAIL（根目录尚无 Go module / cmd / internal / web）

- [ ] **Step 3: Perform the physical promotion**

- 把 Go module 根移动到仓库根目录
- 保持 package 名称不变，批量更新 import 从 `.../go-backend/internal/...` 到 `.../internal/...`
- 将模板和静态资源移动到 `web/`，并更新 `vite.config.js` 的 `outDir` 为 `web/static/dist`
- 更新 `Dockerfile`、`docker-compose.yml`、CI 路径和静态卷挂载
- 同步把 `scripts/verify_release_gates.sh` 从 `cd go-backend && go test ./...` 改成根目录 `go test ./...`，为 Phase 4 之后的最终验收做准备

- [ ] **Step 4: Run targeted build verification**

Run: `go test ./... && npm run build`
Expected: PASS；`web/static/dist/*.bundle.js` 已生成

- [ ] **Step 5: Run Phase 4 smoke before deleting legacy runtime**

Run: `INTERNAL_ENQUEUE_TOKEN=test-token docker compose up -d --build && curl -fsS http://localhost:5002/healthz && curl -fsS http://localhost:5002/readyz && docker compose down`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -A go.mod go.sum cmd internal web Dockerfile vite.config.js docker-compose.yml .github/workflows/ci.yml README.md scripts/verify_root_go_layout.sh scripts/verify_release_gates.sh
git commit -m "refactor(repo): promote go module and web assets to repository root"
```

---

### Task 11: 删除 Python compatibility runtime 与历史静态入口，并完成最终验收

**Files:**
- Create: `scripts/verify_no_legacy_runtime.sh`
- Create: `docs/legacy/python-bridge-history.md`
- Delete: `app.py`
- Delete: `downloader_logic.py`
- Delete: `task_store.py`
- Delete: `requirements.txt`
- Delete: `telegram_downloader/`
- Delete: `templates/`
- Delete: `static/app.js`
- Delete: `static/index.js`
- Delete: `static/logs.js`
- Delete: `static/v2/`
- Delete: `tests/web/test_go_proxy.py`
- Delete: `tests/web/test_routes_api_logs.py`
- Modify: `.github/workflows/ci.yml`
- Modify: `README.md`
- Modify: `docs/PROJECT_UPDATES.md`

- [ ] **Step 1: Write the failing legacy-runtime verifier**

创建 `scripts/verify_no_legacy_runtime.sh`：

```bash
#!/usr/bin/env bash
set -euo pipefail
! test -e app.py
! test -e requirements.txt
! test -d telegram_downloader
! test -d templates
! test -e static/index.js
! test -d static/v2
! rg -n 'pytest tests/web|GO_BACKEND_BASE_URL|PYTHON_WEB_BASE_URL' README.md .github/workflows/ci.yml docker-compose.yml >/dev/null
```

把最后一行改成“若还能搜到 legacy runtime 关键字则退出 1”。

- [ ] **Step 2: Run the verifier to observe failure before cleanup**

Run: `bash scripts/verify_no_legacy_runtime.sh`
Expected: FAIL（legacy 文件与文档引用仍存在）

- [ ] **Step 3: Delete the legacy runtime and update docs/gates**

- 删除 Python runtime/compatibility code
- 删除 Python web tests
- 从 CI、README、PROJECT_UPDATES 以及 `scripts/verify_release_gates.sh` 中移除 Python 主链路描述
- 在 `docs/legacy/python-bridge-history.md` 保留迁移说明与 git 历史回溯提示

- [ ] **Step 4: Run the verifier and full final verification**

Run: `bash scripts/verify_no_legacy_runtime.sh`
Expected: PASS

Run: `go test ./... && go test -race ./... && npm run test:frontend && npm run lint && npm run build && npm run e2e:test`
Expected: PASS

- [ ] **Step 5: Run final Compose smoke**

Run: `INTERNAL_ENQUEUE_TOKEN=test-token docker compose up -d --build && curl -fsS http://localhost:5002/healthz && curl -fsS http://localhost:5002/readyz && docker compose down`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -A scripts/verify_no_legacy_runtime.sh docs/legacy/python-bridge-history.md .github/workflows/ci.yml README.md docs/PROJECT_UPDATES.md scripts/verify_release_gates.sh
git commit -m "chore(cleanup): remove python compatibility runtime and legacy assets"
```

---

## Completion Gate

在宣称“整仓重构完成”前，必须执行 @superpowers:verification-before-completion，并记录以下证据：

- `git status --short` 为空
- `go test ./...`
- `go test -race ./...`
- `npm run test:frontend`
- `npm run lint`
- `npm run build`
- `npm run e2e:test`
- `INTERNAL_ENQUEUE_TOKEN=test-token docker compose up -d --build`
- `curl -fsS http://localhost:5002/healthz`
- `curl -fsS http://localhost:5002/readyz`
- `docker compose down`

若 Task 10（根目录提升）在实现时暴露出超出当前 spec 的迁移复杂度，不要硬顶；暂停在一个绿的提交点，回到设计流程，为“根目录物理迁移”补一份单独 spec/plan 再继续。

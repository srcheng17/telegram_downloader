# Go Mainline Modernization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在保持当前前端界面与交互不变的前提下，完成 Go 单主线收敛、队列可靠性升级、前端工程化、CI 与安全基线落地，并支持 Docker Compose 一次性发布。

**Architecture:** 统一运行面为 `gateway -> go-api -> postgres/redis` 与 `go-worker`。Go API 同时提供页面与 API，旧 Python 运行链路下线。任务统一落在 v2 状态机与事件模型，worker 采用 Redis Streams consumer group + ack + pending reclaim 实现可恢复执行。

**Tech Stack:** Go 1.23、chi、pgx、go-redis、PostgreSQL、Redis Streams、HTML/CSS/Vanilla JS（Vite 工程化）、Playwright、GitHub Actions、Docker Compose

---

**Execution discipline:** @superpowers/test-driven-development + @superpowers/systematic-debugging + @superpowers/verification-before-completion

### Task 1: Go-only Compose/Nginx 拓扑收敛

**Files:**
- Modify: `docker-compose.yml`
- Modify: `deploy/nginx/canary-go-full.conf`
- Modify: `go-backend/internal/config/compose_contract_test.go`
- Test: `go-backend/internal/config/compose_contract_test.go`

**Step 1: Write the failing test**

```go
func TestComposeTopologyMatchesGoBackendOnly(t *testing.T) {
    expectedServices := map[string]struct{}{
        "go-api": {}, "go-worker": {}, "postgres": {}, "redis": {}, "gateway": {},
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config -run TestComposeTopologyMatchesGoBackendOnly -count=1`  
Expected: FAIL（当前 compose 仍包含 `web`，nginx `/` 仍转发 Python）

**Step 3: Write minimal implementation**

```nginx
upstream telegraph_go_api { server go-api:5000; }
location / { proxy_pass http://telegraph_go_api; }
```

```yaml
services:
  go-api:
  go-worker:
  postgres:
  redis:
  gateway:
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config -count=1`  
Expected: PASS

**Step 5: Commit**

```bash
git add docker-compose.yml deploy/nginx/canary-go-full.conf go-backend/internal/config/compose_contract_test.go
git commit -m "refactor: switch compose and nginx to go-only topology"
```

### Task 2: 在 Go API 中提供现有首页/日志/设置页面（UI 不变）

**Files:**
- Create: `go-backend/internal/httpui/handler.go`
- Create: `go-backend/internal/httpui/handler_test.go`
- Create: `go-backend/internal/httpui/templates/base.html`
- Create: `go-backend/internal/httpui/templates/index.html`
- Create: `go-backend/internal/httpui/templates/logs.html`
- Create: `go-backend/internal/httpui/templates/settings.html`
- Modify: `go-backend/cmd/server/main.go`
- Modify: `go-backend/internal/httpapi/api.go`

**Step 1: Write the failing test**

```go
func TestUIRoutesRenderMainPages(t *testing.T) {
    // GET /, /logs, /settings all return 200 and contain expected heading text
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/httpui -run TestUIRoutesRenderMainPages -count=1`  
Expected: FAIL（httpui 包与模板尚不存在）

**Step 3: Write minimal implementation**

```go
r.Get("/", h.Index)
r.Get("/logs", h.Logs)
r.Get("/settings", h.SettingsPage)
r.Handle("/static/*", http.StripPrefix("/static/", staticFS))
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/httpui -count=1`  
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/httpui go-backend/cmd/server/main.go go-backend/internal/httpapi/api.go
git commit -m "feat: serve existing ui pages from go-api"
```

### Task 3: 支持 HTMX 局部渲染，保持导航交互一致

**Files:**
- Modify: `go-backend/internal/httpui/handler.go`
- Modify: `go-backend/internal/httpui/templates/index.html`
- Modify: `go-backend/internal/httpui/templates/logs.html`
- Modify: `go-backend/internal/httpui/templates/settings.html`
- Create: `go-backend/internal/httpui/htmx_test.go`

**Step 1: Write the failing test**

```go
func TestHTMXRequestReturnsPartialContent(t *testing.T) {
    req.Header.Set("HX-Request", "true")
    // body should contain fragment only, not full <html> document
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/httpui -run TestHTMXRequestReturnsPartialContent -count=1`  
Expected: FAIL

**Step 3: Write minimal implementation**

```go
func isHTMX(r *http.Request) bool {
    return strings.EqualFold(strings.TrimSpace(r.Header.Get("HX-Request")), "true")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/httpui -count=1`  
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/httpui
git commit -m "feat: add htmx partial rendering in go ui"
```

### Task 4: 将现有前端依赖的 `/download` 与 `/api/*` 行为切到 v2 数据模型

**Files:**
- Create: `go-backend/internal/httpapi/legacy_adapter.go`
- Create: `go-backend/internal/httpapi/legacy_adapter_test.go`
- Modify: `go-backend/internal/httpapi/api.go`
- Modify: `go-backend/internal/httpapi/api_test.go`
- Modify: `go-backend/internal/httpv2/tasks_handler.go`

**Step 1: Write the failing test**

```go
func TestLegacyDownloadEndpointCreatesV2Task(t *testing.T) {}
func TestLegacyLogsEndpointReadsFromV2Tasks(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/httpapi -run TestLegacyDownloadEndpointCreatesV2Task -count=1`  
Expected: FAIL（当前 `/download` 仍走 legacy `tasks`/bridge 路径）

**Step 3: Write minimal implementation**

```go
// /download -> create v2 task + enqueue
// /api/logs -> v2_tasks 查询并映射为旧前端字段
// /api/tasks/{id}/cancel -> 调用 v2 cancel
// /api/tasks/{id}/download -> 调用 v2 artifact
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/httpapi ./internal/httpv2 -count=1`  
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/httpapi go-backend/internal/httpv2/tasks_handler.go
git commit -m "refactor: back legacy-facing api endpoints with v2 model"
```

### Task 5: 设置页行为迁移到 Go（GET/POST `/settings`）

**Files:**
- Modify: `go-backend/internal/httpui/handler.go`
- Create: `go-backend/internal/httpui/settings_post_test.go`
- Modify: `go-backend/internal/httpv2/settings_handler.go`
- Modify: `go-backend/internal/httpv2/settings_handler_test.go`

**Step 1: Write the failing test**

```go
func TestSettingsPostPersistsAndRedirects(t *testing.T) {
    // POST /settings should persist values and 303 redirect to /settings
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/httpui -run TestSettingsPostPersistsAndRedirects -count=1`  
Expected: FAIL

**Step 3: Write minimal implementation**

```go
r.Post("/settings", h.SettingsPost)
http.Redirect(w, r, "/settings", http.StatusSeeOther)
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/httpui ./internal/httpv2 -count=1`  
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/httpui go-backend/internal/httpv2/settings_handler.go go-backend/internal/httpv2/settings_handler_test.go
git commit -m "feat: support settings page post flow in go ui"
```

### Task 6: v2 队列升级为 consumer group + ack + pending reclaim

**Files:**
- Modify: `go-backend/internal/queue/v2/consumer.go`
- Create: `go-backend/internal/queue/v2/consumer_group_test.go`
- Modify: `go-backend/internal/worker/v2_executor.go`
- Modify: `go-backend/internal/worker/v2_executor_test.go`
- Modify: `go-backend/cmd/worker/main.go`

**Step 1: Write the failing test**

```go
func TestConsumerReadsViaGroupAndAcks(t *testing.T) {}
func TestConsumerReclaimsPendingMessage(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/queue/v2 -run TestConsumerReadsViaGroupAndAcks -count=1`  
Expected: FAIL（当前实现为 `XREAD + in-memory cursor`）

**Step 3: Write minimal implementation**

```go
type TaskMessage struct {
    MessageID string
    TaskID    string
    Token     string
}

func (c *Consumer) ReadGroup(...) ([]TaskMessage, error)
func (c *Consumer) Ack(...) error
func (c *Consumer) ClaimPending(...) ([]TaskMessage, error)
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/queue/v2 ./internal/worker -count=1`  
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/queue/v2 go-backend/internal/worker/v2_executor.go go-backend/internal/worker/v2_executor_test.go go-backend/cmd/worker/main.go
git commit -m "feat: adopt consumer-group queue semantics for v2 worker"
```

### Task 7: v2 取消与恢复语义补齐（CANCEL_REQUESTED + heartbeat）

**Files:**
- Create: `go-backend/internal/store/postgres/migrations/003_v2_reliability.sql`
- Modify: `go-backend/internal/store/postgres/v2_repo.go`
- Modify: `go-backend/internal/httpv2/tasks_handler.go`
- Modify: `go-backend/internal/httpv2/tasks_handler_test.go`
- Modify: `go-backend/internal/worker/v2_executor.go`
- Modify: `go-backend/internal/worker/v2_executor_test.go`

**Step 1: Write the failing test**

```go
func TestCancelTaskTransitionsToCancelRequested(t *testing.T) {}
func TestExecutorStopsRunningTaskWhenCancelRequested(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/httpv2 ./internal/worker -run 'TestCancelTaskTransitionsToCancelRequested|TestExecutorStopsRunningTaskWhenCancelRequested' -count=1`  
Expected: FAIL（当前 cancel 直接终态或不协作终止）

**Step 3: Write minimal implementation**

```sql
ALTER TABLE v2_tasks ADD COLUMN IF NOT EXISTS heartbeat_at TIMESTAMPTZ;
ALTER TABLE v2_tasks ADD COLUMN IF NOT EXISTS cancel_requested_at TIMESTAMPTZ;
ALTER TABLE v2_tasks ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0;
```

```go
// cancel: QUEUED/RUNNING -> CANCEL_REQUESTED
// executor loop: heartbeat + check cancel_requested -> CANCELED
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/httpv2 ./internal/worker ./internal/store/postgres -count=1`  
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/store/postgres/migrations/003_v2_reliability.sql go-backend/internal/store/postgres/v2_repo.go go-backend/internal/httpv2/tasks_handler.go go-backend/internal/httpv2/tasks_handler_test.go go-backend/internal/worker/v2_executor.go go-backend/internal/worker/v2_executor_test.go
git commit -m "feat: add cancel-requested and heartbeat recovery semantics for v2"
```

### Task 8: 前端最小工程化（不改变 UI）

**Files:**
- Create: `package.json`
- Create: `package-lock.json`
- Create: `vite.config.js`
- Create: `.eslintrc.cjs`
- Create: `.prettierrc.json`
- Create: `frontend/src/index.js`
- Create: `frontend/src/logs.js`
- Create: `frontend/src/settings.js`
- Modify: `static/index.js`
- Modify: `static/logs.js`
- Modify: `static/app.js`
- Modify: `templates/base.html` (or `go-backend/internal/httpui/templates/base.html`)

**Step 1: Write the failing test**

```bash
npm run lint
# should fail first because eslint config/files are missing
```

**Step 2: Run test to verify it fails**

Run: `npm run lint`  
Expected: FAIL（缺少 package.json / lint pipeline）

**Step 3: Write minimal implementation**

```json
{
  "scripts": {
    "lint": "eslint frontend/src static/**/*.js",
    "build": "vite build"
  }
}
```

**Step 4: Run test to verify it passes**

Run: `npm run lint && npm run build`  
Expected: PASS

**Step 5: Commit**

```bash
git add package.json package-lock.json vite.config.js .eslintrc.cjs .prettierrc.json frontend/src static templates/base.html go-backend/internal/httpui/templates/base.html
git commit -m "chore: add minimal frontend toolchain without changing ui"
```

### Task 9: 配置与安全默认值收紧

**Files:**
- Modify: `go-backend/internal/config/config.go`
- Modify: `go-backend/internal/config/config_test.go`
- Modify: `docker-compose.yml`
- Create: `.env.example`
- Modify: `scripts/start_local.sh`

**Step 1: Write the failing test**

```go
func TestLoadFromEnvFailsWhenDatabaseURLMissingInProduction(t *testing.T) {}
func TestLoadFromEnvFailsWhenInternalTokenMissingInProduction(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config -run 'TestLoadFromEnvFailsWhenDatabaseURLMissingInProduction|TestLoadFromEnvFailsWhenInternalTokenMissingInProduction' -count=1`  
Expected: FAIL（当前仍有弱默认值）

**Step 3: Write minimal implementation**

```go
if isProdMode && strings.TrimSpace(os.Getenv("DATABASE_URL")) == "" {
    return Config{}, errors.New("DATABASE_URL is required in production")
}
```

```yaml
environment:
  INTERNAL_ENQUEUE_TOKEN: ${INTERNAL_ENQUEUE_TOKEN:?required}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config -count=1`  
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/config/config.go go-backend/internal/config/config_test.go docker-compose.yml .env.example scripts/start_local.sh
git commit -m "security: remove weak defaults and require critical env vars"
```

### Task 10: Schema migration 标准化 + `/readyz` + 启动探测

**Files:**
- Create: `go-backend/internal/store/postgres/migrations/runner.go`
- Create: `go-backend/internal/store/postgres/migrations/runner_test.go`
- Modify: `go-backend/cmd/server/main.go`
- Modify: `go-backend/internal/httpapi/api.go`
- Modify: `go-backend/internal/httpapi/api_test.go`

**Step 1: Write the failing test**

```go
func TestReadyzReturns503WhenMigrationsPending(t *testing.T) {}
func TestRunMigrationsAppliesAllSQLFiles(t *testing.T) {}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/postgres/migrations ./internal/httpapi -run 'TestRunMigrationsAppliesAllSQLFiles|TestReadyzReturns503WhenMigrationsPending' -count=1`  
Expected: FAIL（目前没有标准 migration runner 与 readyz 逻辑）

**Step 3: Write minimal implementation**

```go
r.Get("/readyz", api.handleReadyz)
if cfg.AutoMigrate { migrations.Run(ctx, pool) }
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/store/postgres/migrations ./internal/httpapi -count=1`  
Expected: PASS

**Step 5: Commit**

```bash
git add go-backend/internal/store/postgres/migrations/runner.go go-backend/internal/store/postgres/migrations/runner_test.go go-backend/cmd/server/main.go go-backend/internal/httpapi/api.go go-backend/internal/httpapi/api_test.go
git commit -m "feat: add schema migration runner and readiness endpoint"
```

### Task 11: CI、E2E 可复现、发布文档与最终验收

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `tests/e2e/playwright.config.ts`
- Modify: `tests/e2e/specs/logs-flow.spec.js`
- Modify: `tests/e2e/specs/settings.spec.js`
- Modify: `tests/e2e/v2/tasks-flow.spec.ts`
- Modify: `README.md`
- Modify: `docs/runbooks/v2-cutover-checklist.md`
- Modify: `docs/runbooks/v2-rollback-checklist.md`

**Step 1: Write the failing test**

```bash
npm run e2e:test
# should fail first because playwright config / scripts are incomplete
```

**Step 2: Run test to verify it fails**

Run: `npm run e2e:test`  
Expected: FAIL（缺少项目级可复现配置）

**Step 3: Write minimal implementation**

```yaml
# .github/workflows/ci.yml
- run: go test ./...
- run: go test -race ./...
- run: npm ci && npm run lint && npm run e2e:test
```

**Step 4: Run test to verify it passes**

Run:
- `go test ./...`
- `go test -race ./...`
- `npm ci && npm run lint && npm run e2e:test`

Expected: PASS（全部门禁通过）

**Step 5: Commit**

```bash
git add .github/workflows/ci.yml tests/e2e/playwright.config.ts tests/e2e/specs README.md docs/runbooks/v2-cutover-checklist.md docs/runbooks/v2-rollback-checklist.md
git commit -m "chore: add ci pipeline and reproducible e2e release checklist"
```

### Task 12: 最终整体验证与切换演练记录

**Files:**
- Create: `docs/runbooks/go-mainline-cutover-evidence.md`
- Modify: `docs/PROJECT_UPDATES.md`

**Step 1: Write the failing test (verification checklist)**

```text
- compose topology check
- ui parity smoke
- task lifecycle smoke (create/cancel/download)
- settings persistence smoke
- rollback dry-run
```

**Step 2: Run verification to capture failures (if any)**

Run:
- `docker compose config`
- `docker compose up -d --build`
- `curl -sS http://localhost:5002/healthz`
- `curl -sS http://localhost:5002/readyz`

Expected: 记录任何失败项并修复

**Step 3: Write minimal implementation/fixes**

```text
- 修复验证中暴露的配置与脚本问题
- 回填证据文档中的命令输出与时间戳
```

**Step 4: Run full verification to verify it passes**

Run:
- `go test ./...`
- `go test -race ./...`
- `npm run lint && npm run e2e:test`
- `docker compose up -d --build && docker compose ps`

Expected: PASS + 证据文件完整

**Step 5: Commit**

```bash
git add docs/runbooks/go-mainline-cutover-evidence.md docs/PROJECT_UPDATES.md
git commit -m "docs: record go mainline cutover verification evidence"
```


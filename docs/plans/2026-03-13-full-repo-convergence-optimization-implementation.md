# Full Repo Convergence & Optimization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在保持现有页面、接口和部署入口基本兼容的前提下，把仓库进一步收敛到 Go 主线，优先解决 Go API 巨型文件、前端源码/产物脱节、日志/任务查询热路径和 Python compatibility 边界不清的问题。

**Architecture:** 先把纯逻辑从 `go-backend/internal/httpapi/api.go` 中拆出来并补齐单测，再把首页/日志页脚本从 `static/*.js` 迁回 `frontend/src/`，随后让 Vite bundle 真正进入模板运行链路；最后再优化 v2 任务筛选查询并把 Python web 侧降级为显式 compatibility bridge。

**Tech Stack:** Go (chi/pgx/redis), PostgreSQL, Vite/ES modules, Node built-in test runner, Python/Flask compatibility layer, Go test, pytest, Playwright, Docker Compose.

**Skills:** 执行时遵循 @using-git-worktrees、@test-driven-development、@verification-before-completion。

**Preflight:** 当前主工作区已有大量未提交改动，且覆盖本计划涉及的 Go/UI 文件。执行本计划前，先在目标基线提交或暂存这些改动，然后用 `@using-git-worktrees` 从目标分支/提交创建独立 worktree；以下所有命令默认都在该 worktree 中运行。

---

### Task 1: 从 `api.go` 提炼日志查询与摘要构建纯逻辑

**Files:**
- Create: `go-backend/internal/httpapi/logs_query.go`
- Create: `go-backend/internal/httpapi/logs_query_test.go`
- Create: `go-backend/internal/httpapi/summary_builder.go`
- Create: `go-backend/internal/httpapi/summary_builder_test.go`
- Modify: `go-backend/internal/httpapi/api.go`
- Modify: `go-backend/internal/httpapi/api_test.go`
- Modify: `go-backend/internal/httpapi/legacy_adapter.go`

**Step 1: 先写失败测试，固定查询归一化与摘要派生规则**

在 `logs_query_test.go` 新增 `TestNormalizeLogQueryClampsAndNormalizes`，断言：

```go
query := normalizeLogQuery("0", "999", "unknown", "  abc  ")
if query.Page != 1 || query.PerPage != 100 || query.Status != "" || query.Keyword != "abc" {
    t.Fatalf("unexpected normalized query: %#v", query)
}
```

在 `summary_builder_test.go` 新增 `TestBuildSummaryFromCountsDerivesTotalsAndRate`，断言 `ActiveTasks`、`FinishedTasks`、`SuccessRate` 正确。

**Step 2: 运行测试确认失败（RED）**

Run:
```bash
cd go-backend
go test ./internal/httpapi -run 'TestNormalizeLogQueryClampsAndNormalizes|TestBuildSummaryFromCountsDerivesTotalsAndRate' -count=1
```
Expected:
- FAIL（`normalizeLogQuery` / `buildSummaryFromCounts` 尚不存在）

**Step 3: 写最小实现并替换散落逻辑（GREEN）**

在 `logs_query.go` 增加：

```go
func normalizeLogQuery(rawPage, rawPerPage, rawStatus, rawKeyword string) domain.LogQuery {
    return domain.LogQuery{
        Page:    clampInt(parseInt(rawPage, 1), 1, math.MaxInt),
        PerPage: clampInt(parseInt(rawPerPage, domain.DefaultLogsPerPage), 1, domain.MaxLogsPerPage),
        Status:  normalizeTaskStatus(rawStatus),
        Keyword: strings.TrimSpace(rawKeyword),
    }
}
```

在 `summary_builder.go` 增加：

```go
func buildSummaryFromCounts(counts map[string]int, recovery domain.StartupRecovery) domain.Summary {
    // 统一计算 total/active/finished/success_rate
}
```

然后让 `api.go` 与 `legacy_adapter.go` 都复用这两个 helper，删掉原有重复摘要计算与查询归一化片段。

**Step 4: 运行回归测试**

Run:
```bash
cd go-backend
go test ./internal/httpapi -run 'TestNormalizeLogQueryClampsAndNormalizes|TestBuildSummaryFromCountsDerivesTotalsAndRate|TestGetLogsNormalizesFiltersAndReturnsCatalog|TestGetSummaryBuildsDerivedFields|TestV2LogsEndpointUsesV2StoreWhenConfigured' -count=1
```
Expected:
- PASS

**Step 5: 提交**

```bash
git add go-backend/internal/httpapi/logs_query.go go-backend/internal/httpapi/logs_query_test.go go-backend/internal/httpapi/summary_builder.go go-backend/internal/httpapi/summary_builder_test.go go-backend/internal/httpapi/api.go go-backend/internal/httpapi/api_test.go go-backend/internal/httpapi/legacy_adapter.go
git commit -m "refactor(httpapi): extract logs query and summary builders"
```

---

### Task 2: 统一下载请求解析与 decision 响应映射，压缩 `api.go` 重复分支

**Files:**
- Create: `go-backend/internal/httpapi/download_request.go`
- Create: `go-backend/internal/httpapi/download_request_test.go`
- Create: `go-backend/internal/httpapi/download_response.go`
- Create: `go-backend/internal/httpapi/download_response_test.go`
- Modify: `go-backend/internal/httpapi/api.go`
- Modify: `go-backend/internal/httpapi/api_test.go`

**Step 1: 先写失败测试，锁定表单/JSON 解析与 response 映射**

在 `download_request_test.go` 新增：

```go
req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fabc&force=1&tags=a%EF%BC%8Cb"))
req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
rawURL, force, metadata, err := extractDownloadRequest(req)
if err != nil || rawURL != "https://telegra.ph/abc" || !force {
    t.Fatalf("unexpected request parse result")
}
if metadata.tagsNormalized == nil || *metadata.tagsNormalized != "a,b" {
    t.Fatalf("expected tags to normalize comma variants")
}
```

在 `download_response_test.go` 新增 `TestBuildDownloadDecisionResponse`，分别覆盖 `created / reuse_active / reuse_success`。

**Step 2: 运行测试确认失败（RED）**

Run:
```bash
cd go-backend
go test ./internal/httpapi -run 'TestExtractDownloadRequest|TestBuildDownloadDecisionResponse' -count=1
```
Expected:
- FAIL（新的 helper 尚不存在，或旧逻辑未被提炼）

**Step 3: 提炼请求/响应 helper 并消除 URL 上传与 archive 上传重复代码**

在 `download_response.go` 增加类似：

```go
func buildDownloadDecisionResponse(taskID string, decision domain.ClaimDecision, forceApplied bool) (int, map[string]any, error) {
    switch decision {
    case domain.ClaimDecisionReuseSuccess:
        return http.StatusOK, map[string]any{ ... }, nil
    case domain.ClaimDecisionReuseActive:
        return http.StatusOK, map[string]any{ ... }, nil
    case domain.ClaimDecisionCreated:
        return http.StatusAccepted, map[string]any{ ... }, nil
    default:
        return 0, nil, fmt.Errorf("unknown claim decision: %s", decision)
    }
}
```

同时把 `handleDownload` 与 `handleArchiveUpload` 里重复的 response 分支改成共用 helper；把 enqueue error 响应也收敛到单点函数。

**Step 4: 运行回归测试**

Run:
```bash
cd go-backend
go test ./internal/httpapi -run 'TestExtractDownloadRequest|TestBuildDownloadDecisionResponse|TestDownloadCreatesTaskAndSubmitsJob|TestUploadArchiveCreatesTaskAndSubmitsJob|TestDownloadHandlesExistingSuccessDuplicate|TestUploadArchiveHandlesExistingSuccessDuplicate' -count=1
```
Expected:
- PASS

**Step 5: 提交**

```bash
git add go-backend/internal/httpapi/download_request.go go-backend/internal/httpapi/download_request_test.go go-backend/internal/httpapi/download_response.go go-backend/internal/httpapi/download_response_test.go go-backend/internal/httpapi/api.go go-backend/internal/httpapi/api_test.go
git commit -m "refactor(httpapi): centralize download request and response mapping"
```

---

### Task 3: 把首页/日志脚本迁回 `frontend/src` 并为共享纯逻辑补前端单测

**Files:**
- Create: `frontend/src/app.js`
- Create: `frontend/src/home/index.js`
- Create: `frontend/src/logs/index.js`
- Create: `frontend/src/shared/polling.js`
- Create: `frontend/src/shared/archive_upload.js`
- Create: `frontend/src/shared/page_modules.js`
- Create: `frontend/src/tests/polling.test.mjs`
- Create: `frontend/src/tests/archive_upload.test.mjs`
- Modify: `frontend/src/index.js`
- Modify: `frontend/src/logs.js`
- Modify: `frontend/src/settings.js`
- Modify: `package.json`

**Step 1: 先写失败测试，固定轮询与上传快照纯逻辑**

在 `frontend/src/tests/polling.test.mjs` 新增：

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { resolvePollDelay } from '../shared/polling.js';

test('resolvePollDelay backs off and respects visibility state', () => {
  assert.equal(resolvePollDelay({ hidden: true, failures: 0, active: false }), 30000);
  assert.equal(resolvePollDelay({ hidden: false, failures: 3, active: true }), 8000);
});
```

在 `frontend/src/tests/archive_upload.test.mjs` 新增：

```js
import { normalizeUploadSnapshot } from '../shared/archive_upload.js';
assert.deepEqual(normalizeUploadSnapshot(null), {
  status: 'idle',
  loadedBytes: 0,
  totalBytes: 0,
  fileName: '',
  errorMessage: '',
  canCancel: false,
});
```

**Step 2: 运行测试确认失败（RED）**

Run:
```bash
node --test frontend/src/tests/*.test.mjs
```
Expected:
- FAIL（模块尚不存在）

**Step 3: 迁移共享纯逻辑并让前端入口成为真正源码入口**

目标结构：

```js
// frontend/src/index.js
import { createHomeModule } from './home/index.js';
window.TelegraphDownloaderHome = createHomeModule(window, document);
```

```js
// frontend/src/logs.js
import { createLogsModule } from './logs/index.js';
window.TelegraphDownloaderLogs = createLogsModule(window, document);
```

```js
// frontend/src/app.js
import { mountPageModules, unmountPageModules, syncActiveNav } from './shared/page_modules.js';
```

把 `static/index.js`、`static/logs.js`、`static/app.js` 中可抽离的纯函数先迁到 `frontend/src/shared/*`；入口文件不再只做“import 旧静态脚本”的薄包装。

**Step 4: 运行前端校验**

Run:
```bash
node --test frontend/src/tests/*.test.mjs
npm run lint
npm run build
```
Expected:
- PASS
- `static/dist/index.bundle.js`、`static/dist/logs.bundle.js`、`static/dist/settings.bundle.js` 更新

**Step 5: 提交**

```bash
git add frontend/src/app.js frontend/src/home/index.js frontend/src/logs/index.js frontend/src/shared/polling.js frontend/src/shared/archive_upload.js frontend/src/shared/page_modules.js frontend/src/tests/polling.test.mjs frontend/src/tests/archive_upload.test.mjs frontend/src/index.js frontend/src/logs.js frontend/src/settings.js package.json static/dist/index.bundle.js static/dist/logs.bundle.js static/dist/settings.bundle.js
git commit -m "refactor(frontend): move home and logs logic into frontend source modules"
```

---

### Task 4: 让模板正式使用 Vite bundle，消除 `static/*.js` 与 `static/dist` 脱节

**Files:**
- Modify: `vite.config.js`
- Modify: `go-backend/internal/httpui/templates/base.html`
- Modify: `templates/base.html`
- Modify: `go-backend/internal/httpui/handler_test.go`
- Modify: `tests/web/test_routes_api_logs.py`
- Create/Update (generated): `static/dist/app.bundle.js`
- Update (generated): `static/dist/index.bundle.js`
- Update (generated): `static/dist/logs.bundle.js`
- Update (generated): `static/dist/settings.bundle.js`

**Step 1: 先写失败测试，固定模板资产契约**

在 `go-backend/internal/httpui/handler_test.go` 新增 `TestBaseTemplateLoadsDistBundles`，断言页面 HTML 包含：

```go
assertContains(t, body, `/static/dist/app.bundle.js`)
assertContains(t, body, `/static/dist/index.bundle.js`)
assertContains(t, body, `/static/dist/logs.bundle.js`)
assertNotContains(t, body, `/static/index.js`)
```

在 `tests/web/test_routes_api_logs.py` 新增一个轻量测试，请求 `/` 后断言根模板同样引用 `dist/*.bundle.js`。

**Step 2: 运行测试确认失败（RED）**

Run:
```bash
cd go-backend
go test ./internal/httpui -run TestBaseTemplateLoadsDistBundles -count=1
cd ..
pytest tests/web/test_routes_api_logs.py -k dist_bundles -q
```
Expected:
- FAIL（模板仍在加载 `/static/index.js`、`/static/logs.js`、`/static/app.js`）

**Step 3: 增加 `app` bundle 入口并切换模板**

在 `vite.config.js` 把 `frontend/src/app.js` 加入 entry：

```js
input: {
  app: resolve(__dirname, 'frontend/src/app.js'),
  index: resolve(__dirname, 'frontend/src/index.js'),
  logs: resolve(__dirname, 'frontend/src/logs.js'),
  settings: resolve(__dirname, 'frontend/src/settings.js'),
},
```

模板改为：

```html
<script type="module" src="/static/dist/app.bundle.js?v={{ .StaticVersion }}"></script>
<script type="module" src="/static/dist/index.bundle.js?v={{ .StaticVersion }}"></script>
<script type="module" src="/static/dist/logs.bundle.js?v={{ .StaticVersion }}"></script>
<script type="module" src="/static/dist/settings.bundle.js?v={{ .StaticVersion }}"></script>
```

根目录 Flask 模板使用 `url_for_static_bust_cache('dist/app.bundle.js')` 同步切换。

**Step 4: 运行模板与构建校验**

Run:
```bash
npm run build
cd go-backend
go test ./internal/httpui -run 'TestBaseTemplateLoadsDistBundles|TestUIRoutesRenderMainPages' -count=1
cd ..
pytest tests/web/test_routes_api_logs.py -k dist_bundles -q
```
Expected:
- PASS

**Step 5: 提交**

```bash
git add vite.config.js go-backend/internal/httpui/templates/base.html templates/base.html go-backend/internal/httpui/handler_test.go tests/web/test_routes_api_logs.py static/dist/app.bundle.js static/dist/index.bundle.js static/dist/logs.bundle.js static/dist/settings.bundle.js
git commit -m "build(ui): switch templates to vite dist bundles"
```

---

### Task 5: 收敛 v2 任务筛选 SQL 并为关键词搜索补索引

**Files:**
- Create: `go-backend/internal/httpv2/task_filters.go`
- Create: `go-backend/internal/httpv2/task_filters_test.go`
- Modify: `go-backend/internal/httpv2/tasks_handler.go`
- Modify: `go-backend/internal/httpv2/tasks_handler_test.go`
- Create: `go-backend/internal/store/postgres/migrations/008_task_search_trgm_indexes.sql`
- Modify: `go-backend/internal/store/postgres/migrations/runner_test.go`

**Step 1: 先写失败测试，固定筛选子句生成规则**

在 `task_filters_test.go` 新增：

```go
sql, args := buildTaskListFilter("RUNNING", " demo ")
if !strings.Contains(sql, "status = $1") || !strings.Contains(sql, "ILIKE") {
    t.Fatalf("unexpected filter SQL: %s", sql)
}
if len(args) != 2 || args[0] != "RUNNING" || args[1] != "demo" {
    t.Fatalf("unexpected args: %#v", args)
}
```

在 `tasks_handler_test.go` 增加一个 handler 级测试，确保 `q` 两端空格被裁剪后仍传给 store。

**Step 2: 运行测试确认失败（RED）**

Run:
```bash
cd go-backend
go test ./internal/httpv2 -run 'TestBuildTaskListFilter|TestListTasksTrimsKeyword' -count=1
```
Expected:
- FAIL

**Step 3: 提取 filter builder 并补搜索索引 migration**

在 `task_filters.go` 实现：

```go
func buildTaskListFilter(status, q string) (string, []any) {
    // 只负责 where clause 与 args，避免 count/list 各写一遍字符串
}
```

让 `tasks_handler.go` 的 `ListTasks` 复用这个 builder 生成 `COUNT(*)` 和 `SELECT ... LIMIT/OFFSET`。

新增 migration `008_task_search_trgm_indexes.sql`：

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_tasks_url_trgm ON tasks USING gin (url gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_tasks_canonical_url_trgm ON tasks USING gin (canonical_url gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_v2_tasks_url_trgm ON v2_tasks USING gin (url gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_v2_tasks_canonical_url_trgm ON v2_tasks USING gin (canonical_url gin_trgm_ops);
```

**Step 4: 运行 Go 回归测试**

Run:
```bash
cd go-backend
go test ./internal/httpv2 ./internal/store/postgres/migrations -count=1
```
Expected:
- PASS

**Step 5: 提交**

```bash
git add go-backend/internal/httpv2/task_filters.go go-backend/internal/httpv2/task_filters_test.go go-backend/internal/httpv2/tasks_handler.go go-backend/internal/httpv2/tasks_handler_test.go go-backend/internal/store/postgres/migrations/008_task_search_trgm_indexes.sql go-backend/internal/store/postgres/migrations/runner_test.go
git commit -m "perf(tasks): add reusable filters and trigram search indexes"
```

---

### Task 6: 把 Python Web 侧显式收敛为 Go compatibility bridge

**Files:**
- Create: `telegram_downloader/web/go_proxy.py`
- Create: `tests/web/test_go_proxy.py`
- Modify: `telegram_downloader/web/routes.py`
- Modify: `README.md`

**Step 1: 先写失败测试，固定 Go proxy 行为**

在 `tests/web/test_go_proxy.py` 新增：

```python
def test_proxy_builds_default_go_backend_url():
    assert build_go_backend_base_url({}) == "http://go-api:5000"

def test_proxy_head_response_has_no_body():
    response = make_proxy_response("HEAD", status_code=200, headers={"Content-Length": "123"})
    assert response.get_data() == b""
```

再补一个失败路径测试：`requests.RequestException` 被映射成 502 JSON。

**Step 2: 运行测试确认失败（RED）**

Run:
```bash
pytest tests/web/test_go_proxy.py -q
```
Expected:
- FAIL（`go_proxy.py` 尚不存在）

**Step 3: 提取 proxy helper 并让 `routes.py` 只负责路由绑定**

在 `telegram_downloader/web/go_proxy.py` 中实现：

```python
def build_go_backend_base_url(env):
    return (env.get("GO_BACKEND_BASE_URL") or "http://go-api:5000").strip().rstrip("/")

def proxy_v2_request(request, path, logger, env=None):
    # 复制当前 _proxy_v2_request 逻辑：header 透传、body 转发、HEAD 空 body、异常映射 502
```

然后在 `telegram_downloader/web/routes.py` 中改成调用 `proxy_v2_request(...)`，并删除内联 `_proxy_v2_request`。

README 同步明确：Python/Flask 仅保留 compatibility/迁移参考角色，不再作为默认主线实现。

**Step 4: 运行 Python 回归测试**

Run:
```bash
pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q
```
Expected:
- PASS

**Step 5: 提交**

```bash
git add telegram_downloader/web/go_proxy.py tests/web/test_go_proxy.py telegram_downloader/web/routes.py README.md
git commit -m "refactor(python): extract go proxy compatibility bridge"
```

---

### Task 7: 做全链路验证并清理遗留入口引用

**Files:**
- Modify: `README.md`
- Modify: `docs/PROJECT_UPDATES.md`
- Verify only: `static/index.js`
- Verify only: `static/logs.js`
- Verify only: `static/app.js`

**Step 1: 先补文档验收清单**

在 `README.md` 和 `docs/PROJECT_UPDATES.md` 增补：
- 当前模板与前端源码事实来源
- Python compatibility-only 说明
- 前端构建与 Go/UI 验证命令

**Step 2: 运行主验证命令**

Run:
```bash
cd go-backend
go test ./...
go test -race ./...
cd ..
node --test frontend/src/tests/*.test.mjs
npm run lint
npm run build
pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q
```
Expected:
- PASS

**Step 3: 运行关键 E2E**

Run:
```bash
npm run e2e:test
```
Expected:
- PASS

**Step 4: 确认旧静态入口不再被模板直接引用**

Run:
```bash
grep -R \"/static/index.js\\|/static/logs.js\\|/static/app.js\" -n go-backend/internal/httpui/templates templates || true
```
Expected:
- 无输出（或仅剩注释/历史文档）

**Step 5: 提交**

```bash
git add README.md docs/PROJECT_UPDATES.md
git commit -m "docs: document go mainline and frontend source-of-truth convergence"
```

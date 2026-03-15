# Fullstack Refactor & Stability Convergence Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 以“发布级验收全绿”为前提，完成前后端共享逻辑收敛、错误模型统一、查询热路径优化与兼容层回归修复。

**Architecture:** 按“先稳定再重构再收口”推进：先通过可重复门禁锁定当前问题，再将前端重复逻辑与后端错误处理统一到共享模块，并在 Postgres 查询热路径补齐索引；最后更新契约测试、E2E 与运行文档，完成破坏性变更迁移说明。

**Tech Stack:** Go (chi/pgx), PostgreSQL migrations, Redis Streams, Vite + native JS modules, Python compatibility tests, Playwright E2E, Docker Compose.

**Skills:** 全程执行 @test-driven-development 与 @verification-before-completion；实现阶段在独立 worktree `/.worktrees/fullstack-refactor-stability` 内完成。

---

### Task 1: 前端启动恢复逻辑去重（Home/Logs 共用模块）

**Files:**
- Create: `frontend/src/shared/startup_recovery.js`
- Modify: `frontend/src/home/index.js`
- Modify: `frontend/src/logs/index.js`
- Test: `frontend/src/tests/startup_recovery.test.mjs`

**Step 1: Write the failing test**

新增 `startup_recovery.test.mjs`，覆盖：
- `parseStartupRecovery` 可兼容 `recovered_total/failed_tasks/canceled_tasks` 混合输入。
- `buildStartupRecoveryMessage` 在 `recoveredTotal=0` 与 `>0` 时输出不同文案。
- `getStartupRecoveryDismissKey` 保持稳定前缀。

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { parseStartupRecovery, buildStartupRecoveryMessage, getStartupRecoveryDismissKey } from '../shared/startup_recovery.js';

test('parseStartupRecovery normalizes mixed payload fields', () => {
  const parsed = parseStartupRecovery({ startup_recovery: { failed_tasks: 2, canceled_tasks: 1, happened: true } });
  assert.equal(parsed.recoveredTotal, 3);
  assert.equal(parsed.recoveredFailed, 2);
  assert.equal(parsed.recoveredCanceled, 1);
});
```

**Step 2: Run test to verify it fails**

Run:
```bash
npm run test:frontend
```
Expected:
- FAIL，提示 `Cannot find module '../shared/startup_recovery.js'` 或导出缺失。

**Step 3: Write minimal implementation**

在 `startup_recovery.js` 提取并导出：
```js
export function parseStartupRecovery(summary) { /* 复用 home/logs 现有算法 */ }
export function getStartupRecoveryDismissKey(prefix, signature) { return `${prefix}${signature}`; }
export function buildStartupRecoveryMessage(recovery) { /* 统一文案 */ }
```
并在 `home/index.js` 与 `logs/index.js` 删除重复实现，改为调用共享函数。

**Step 4: Run test to verify it passes**

Run:
```bash
npm run test:frontend
```
Expected:
- PASS，且原有 `polling/page_modules/archive_upload` 测试不回归。

**Step 5: Commit**

```bash
git add frontend/src/shared/startup_recovery.js frontend/src/home/index.js frontend/src/logs/index.js frontend/src/tests/startup_recovery.test.mjs
git commit -m "refactor(frontend): share startup recovery helpers across pages"
```

---

### Task 2: 前端服务端消息本地化映射统一

**Files:**
- Create: `frontend/src/shared/server_messages.js`
- Modify: `frontend/src/home/index.js`
- Modify: `frontend/src/logs/index.js`
- Test: `frontend/src/tests/server_messages.test.mjs`

**Step 1: Write the failing test**

新增测试覆盖：
- Home 与 Logs 的既有英文消息都能映射中文。
- 可配置前缀规则（如 `Task already finished with status ...`）。

```js
import { localizeServerMessage } from '../shared/server_messages.js';

test('localizeServerMessage supports prefix based mapping', () => {
  const text = localizeServerMessage('Task already finished with status SUCCESS', 'logs');
  assert.equal(text, '任务已结束，无法取消。');
});
```

**Step 2: Run test to verify it fails**

Run:
```bash
npm run test:frontend
```
Expected:
- FAIL，提示缺少 `server_messages.js` 或断言不匹配。

**Step 3: Write minimal implementation**

实现统一映射：
```js
const CATALOG = {
  home: { 'Please provide a Telegraph URL.': '请先输入 Telegraph 链接。' },
  logs: { 'Stored file is unavailable.': '缓存文件不可用。' },
};

export function localizeServerMessage(message, domain = 'common') {
  // 先 exact match，再 prefix match，最后 fallback 原文
}
```
并替换 Home/Logs 内部各自字典。

**Step 4: Run test to verify it passes**

Run:
```bash
npm run test:frontend
npm run lint
```
Expected:
- PASS，lint 无新增错误。

**Step 5: Commit**

```bash
git add frontend/src/shared/server_messages.js frontend/src/home/index.js frontend/src/logs/index.js frontend/src/tests/server_messages.test.mjs
git commit -m "refactor(frontend): centralize server message localization"
```

---

### Task 3: Go API 错误响应统一（legacy / v2 共用）

**Files:**
- Create: `go-backend/internal/httpapi/error_response.go`
- Create: `go-backend/internal/httpapi/error_response_test.go`
- Modify: `go-backend/internal/httpapi/api.go`
- Modify: `go-backend/internal/httpapi/download_response.go`
- Modify: `go-backend/internal/httpapi/contract_test.go`

**Step 1: Write the failing test**

新增 `error_response_test.go`：
- 给定领域错误类型，返回稳定 `code/message/details` JSON。
- 未知错误统一映射为 `internal_error`。

```go
func TestWriteAPIErrorResponse_Validation(t *testing.T) {
    rec := httptest.NewRecorder()
    writeAPIErrorResponse(rec, http.StatusBadRequest, "validation_error", "invalid url", map[string]any{"field": "url"})
    if rec.Code != http.StatusBadRequest {
        t.Fatalf("expected 400, got %d", rec.Code)
    }
    assertBodyContains(t, rec.Body.String(), `"code":"validation_error"`)
}
```

并在 `contract_test.go` 新增断言：错误响应必须同时包含 `error` 与 `code`（兼容旧前端 + 新前端）。

**Step 2: Run test to verify it fails**

Run:
```bash
cd go-backend
go test ./internal/httpapi -run 'TestWriteAPIErrorResponse_Validation|TestContractErrorPayloadContainsCode' -count=1
```
Expected:
- FAIL，当前返回结构不稳定或缺少 `code` 字段。

**Step 3: Write minimal implementation**

新增统一写响应函数：
```go
type apiErrorPayload struct {
    Error   string         `json:"error"`
    Code    string         `json:"code"`
    Message string         `json:"message,omitempty"`
    Details map[string]any `json:"details,omitempty"`
}
```
在 `api.go` / `download_response.go` 统一调用，移除散落的 `writeJSON(...{"error": ...})`。

**Step 4: Run test to verify it passes**

Run:
```bash
cd go-backend
go test ./internal/httpapi -count=1
```
Expected:
- PASS，既有 contract test 全部通过。

**Step 5: Commit**

```bash
git add go-backend/internal/httpapi/error_response.go go-backend/internal/httpapi/error_response_test.go go-backend/internal/httpapi/api.go go-backend/internal/httpapi/download_response.go go-backend/internal/httpapi/contract_test.go
git commit -m "refactor(go): unify legacy api error envelope"
```

---

### Task 4: v2 任务查询热路径优化（状态+更新时间索引）

**Files:**
- Create: `go-backend/internal/store/postgres/migrations/007_v2_tasks_status_updated_index.sql`
- Modify: `go-backend/internal/store/postgres/migrations/runner_test.go`
- Modify: `go-backend/internal/httpv2/tasks_handler_test.go`
- Modify: `go-backend/internal/httpv2/task_filters.go`

**Step 1: Write the failing test**

在 `tasks_handler_test.go` 新增 `TestBuildTaskListFilterSupportsStatusOnlyFastPath`，断言仅 status 过滤时不引入不必要 `ILIKE` 条件。

```go
func TestBuildTaskListFilterSupportsStatusOnlyFastPath(t *testing.T) {
    clause, args := buildTaskListFilter("SUCCESS", "")
    if strings.Contains(clause, "ILIKE") {
        t.Fatalf("expected status-only clause without ilike, got %q", clause)
    }
    if len(args) != 1 || args[0] != "SUCCESS" {
        t.Fatalf("unexpected args: %#v", args)
    }
}
```

并在 `runner_test.go` 断言新 migration 被执行。

**Step 2: Run test to verify it fails**

Run:
```bash
cd go-backend
go test ./internal/httpv2 ./internal/store/postgres/migrations -run 'TestBuildTaskListFilterSupportsStatusOnlyFastPath|TestRunnerApplies007V2TasksStatusUpdatedIndex' -count=1
```
Expected:
- FAIL，007 migration 不存在。

**Step 3: Write minimal implementation**

新增 migration：
```sql
CREATE INDEX IF NOT EXISTS idx_v2_tasks_status_updated_at ON v2_tasks (status, updated_at DESC);
```
并在 filter builder 增加 `status` 为空/非空时的明确分支，避免拼接冗余条件。

**Step 4: Run test to verify it passes**

Run:
```bash
cd go-backend
go test ./internal/httpv2 ./internal/store/postgres/migrations -count=1
```
Expected:
- PASS

**Step 5: Commit**

```bash
git add go-backend/internal/store/postgres/migrations/007_v2_tasks_status_updated_index.sql go-backend/internal/store/postgres/migrations/runner_test.go go-backend/internal/httpv2/tasks_handler_test.go go-backend/internal/httpv2/task_filters.go
git commit -m "perf(go): add v2 status-updated index and filter fast-path"
```

---

### Task 5: 兼容层与前端契约回归（错误码透传 + 下载预检）

**Files:**
- Modify: `telegram_downloader/web/go_proxy.py`
- Modify: `telegram_downloader/web/routes.py`
- Modify: `tests/web/test_go_proxy.py`
- Modify: `tests/web/test_routes_api_logs.py`
- Modify: `tests/e2e/specs/logs-flow.spec.js`

**Step 1: Write the failing test**

新增 Python 测试断言：
- Go 返回 `{error, code}` 时，Flask bridge 不丢 `code`。
- 下载预检失败时维持页内反馈语义（HTTP 状态与 JSON 一致）。

```python
def test_go_proxy_preserves_error_code(client, monkeypatch):
    monkeypatch.setattr(...)
    response = client.get('/api/tasks/demo/download', headers={'X-Test': '1'})
    assert response.status_code == 404
    assert response.get_json()['code'] == 'artifact_missing'
```

**Step 2: Run test to verify it fails**

Run:
```bash
PYTHONPATH=. ../.venv/bin/pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q
```
Expected:
- FAIL，`code` 字段未透传或被覆盖。

**Step 3: Write minimal implementation**

在 `go_proxy.py` / `routes.py` 代理响应时补齐：
```python
payload = upstream_json if isinstance(upstream_json, dict) else {'error': fallback}
if 'code' in upstream_json:
    payload['code'] = upstream_json['code']
```
确保 HEAD 预检失败仍返回 JSON 结构给前端消费。

**Step 4: Run test to verify it passes**

Run:
```bash
PYTHONPATH=. ../.venv/bin/pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q
npm run e2e:test -- --grep "logs"
```
Expected:
- PASS

**Step 5: Commit**

```bash
git add telegram_downloader/web/go_proxy.py telegram_downloader/web/routes.py tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py tests/e2e/specs/logs-flow.spec.js
git commit -m "fix(compat): preserve go error codes through python bridge"
```

---

### Task 6: 发布级验收与迁移文档收口

**Files:**
- Modify: `README.md`
- Create: `docs/runbooks/2026-03-16-fullstack-refactor-migration.md`
- Modify: `docs/PROJECT_UPDATES.md`

**Step 1: Write the failing test/check**

先执行发布级门禁并记录任何失败项：

```bash
cd go-backend && go test ./... && go test -race ./...
cd .. && npm run test:frontend && npm run lint && npm run build
PYTHONPATH=. ../.venv/bin/pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q
npm run e2e:test
docker compose up -d --build && curl -fsS http://localhost:5002/healthz && curl -fsS http://localhost:5002/readyz && docker compose down
```

Expected:
- 若有失败，记录到迁移文档“Known Issues”并回到对应任务补修。

**Step 2: Run verification loop until all pass**

重复执行上述命令直到全部 PASS。

**Step 3: Write minimal documentation implementation**

在迁移文档中写明：
- 破坏性变更清单（旧字段/旧接口 -> 新字段/新接口）。
- 升级步骤与回滚步骤。
- 生产验收命令与预期输出。

**Step 4: Re-run release gate to verify docs align with reality**

Run:
```bash
grep -n "发布级验收" README.md docs/runbooks/2026-03-16-fullstack-refactor-migration.md docs/PROJECT_UPDATES.md
```
Expected:
- 三处文档命令一致，且与实际执行命令一致。

**Step 5: Commit**

```bash
git add README.md docs/runbooks/2026-03-16-fullstack-refactor-migration.md docs/PROJECT_UPDATES.md
git commit -m "docs: finalize migration and release-gate evidence for fullstack refactor"
```

---

### Task 7: 最终合并前验证与状态报告

**Files:**
- Modify: `docs/runbooks/go-mainline-cutover-evidence.md` (append 2026-03-16 evidence)

**Step 1: Write the verification checklist**

在文档追加 checklist：
- Go tests/race
- Frontend test/lint/build
- Python compatibility
- E2E
- Compose smoke

**Step 2: Run the complete verification command set**

Run:
```bash
cd go-backend && go test ./... && go test -race ./...
cd .. && npm run test:frontend && npm run lint && npm run build
PYTHONPATH=. ../.venv/bin/pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q
npm run e2e:test
docker compose up -d --build && docker compose ps && curl -fsS http://localhost:5002/healthz && curl -fsS -w '\nHTTP %{http_code}\n' http://localhost:5002/readyz && docker compose down
```
Expected:
- 全部 PASS 并可复制到交付说明。

**Step 3: Commit**

```bash
git add docs/runbooks/go-mainline-cutover-evidence.md
git commit -m "docs(runbook): append fullstack refactor verification evidence"
```


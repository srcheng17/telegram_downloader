# Go Runtime Stability & Maintainability Modernization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在保持对外行为兼容的前提下，完成运行时 Go 主线收敛，并优先提升稳定性/资源效率与代码可维护性。

**Architecture:** 以 `go-api + go-worker + postgres + redis` 作为唯一运行路径；把执行策略（并发、重试、进度写回）从流程代码中提炼为独立策略组件；逐步移除 legacy v1 运行时耦合并将 Python 标记为归档参考。

**Tech Stack:** Go (chi/pgx/redis), PostgreSQL, Redis Streams, Go test + race, Playwright E2E, Docker Compose。

**Skills:** 全流程遵循 @test-driven-development 与 @verification-before-completion。

---

### Task 1: 引入执行遥测抽象（稳定性基线）

**Files:**
- Create: `go-backend/internal/worker/telemetry.go`
- Create: `go-backend/internal/worker/telemetry_test.go`
- Modify: `go-backend/internal/worker/v2_executor.go`
- Modify: `go-backend/internal/worker/v2_executor_test.go`

**Step 1: 先写失败测试，定义遥测事件契约（RED）**

在 `v2_executor_test.go` 新增测试（建议命名 `TestV2ExecutorReportsLifecycleTelemetry`），断言执行一次任务后按顺序上报：`started -> download_succeeded -> terminal_transition_succeeded`。

示例断言片段：
```go
if len(reporter.events) != 3 {
    t.Fatalf("expected 3 telemetry events, got %d", len(reporter.events))
}
if reporter.events[0].Name != "started" || reporter.events[2].Name != "terminal_transition_succeeded" {
    t.Fatalf("unexpected event sequence: %#v", reporter.events)
}
```

**Step 2: 运行测试确认失败（RED）**

Run:
```bash
go test ./internal/worker -run TestV2ExecutorReportsLifecycleTelemetry -count=1
```
Expected:
- FAIL（当前还没有 telemetry reporter）

**Step 3: 实现最小遥测组件（GREEN）**

在 `telemetry.go` 增加接口与 no-op：
```go
type TaskTelemetryEvent struct {
    TaskID    string
    Worker    string
    Name      string
    Attempt   int
    Duration  time.Duration
    ErrorText string
}

type TaskTelemetryReporter interface {
    ReportTaskEvent(ctx context.Context, event TaskTelemetryEvent)
}

type noopTaskTelemetryReporter struct{}
func (noopTaskTelemetryReporter) ReportTaskEvent(context.Context, TaskTelemetryEvent) {}
```

并在 `V2ExecutorConfig` 与 `V2Executor` 中接入 `Reporter TaskTelemetryReporter`，在关键节点调用 `ReportTaskEvent`。

**Step 4: 运行测试转绿**

Run:
```bash
go test ./internal/worker -run TestV2ExecutorReportsLifecycleTelemetry -count=1
```
Expected:
- PASS

**Step 5: 提交**

```bash
git add go-backend/internal/worker/telemetry.go go-backend/internal/worker/telemetry_test.go go-backend/internal/worker/v2_executor.go go-backend/internal/worker/v2_executor_test.go
git commit -m "feat(worker): add v2 executor telemetry lifecycle reporter"
```

---

### Task 2: 增加任务级并发预算器（资源效率）

**Files:**
- Create: `go-backend/internal/worker/concurrency_limiter.go`
- Create: `go-backend/internal/worker/concurrency_limiter_test.go`
- Modify: `go-backend/internal/worker/v2_executor.go`
- Modify: `go-backend/internal/worker/v2_executor_test.go`
- Modify: `go-backend/internal/config/config.go`
- Modify: `go-backend/internal/config/config_test.go`
- Modify: `go-backend/cmd/worker/main.go`

**Step 1: 先写失败测试，锁定“最多 N 个任务并行执行”（RED）**

在 `concurrency_limiter_test.go` 新增测试 `TestTaskLimiterCapsConcurrentRuns`，用 3 个 goroutine 竞争 `limit=2`，断言同一时刻最大并发不会超过 2。

示例断言片段：
```go
if maxSeen > 2 {
    t.Fatalf("expected max concurrency <= 2, got %d", maxSeen)
}
```

**Step 2: 运行测试确认失败（RED）**

Run:
```bash
go test ./internal/worker -run TestTaskLimiterCapsConcurrentRuns -count=1
```
Expected:
- FAIL（尚未实现 limiter）

**Step 3: 实现最小 limiter 并接入 v2 executor（GREEN）**

在 `concurrency_limiter.go` 实现：
```go
type TaskLimiter struct { sem chan struct{} }

func NewTaskLimiter(max int) *TaskLimiter {
    if max <= 0 { max = 1 }
    return &TaskLimiter{sem: make(chan struct{}, max)}
}

func (l *TaskLimiter) Acquire(ctx context.Context) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case l.sem <- struct{}{}:
        return nil
    }
}

func (l *TaskLimiter) Release() {
    select { case <-l.sem: default: }
}
```

在 `V2ExecutorConfig` 新增 `TaskLimiter *TaskLimiter`，在 `executeWithRetry` 进入前 `Acquire`，退出时 `Release`。

**Step 4: 接入配置项并补测试**

- `config.go` 增加 `TaskConcurrency int` 与 `GO_TASK_CONCURRENCY`（默认 `2`，边界 `1..64`）。
- `cmd/worker/main.go` 使用 `worker.NewTaskLimiter(cfg.TaskConcurrency)` 注入 executor。
- `config_test.go` 增加环境变量解析断言。

**Step 5: 运行相关测试**

Run:
```bash
go test ./internal/config ./internal/worker -count=1
```
Expected:
- PASS

**Step 6: 提交**

```bash
git add go-backend/internal/worker/concurrency_limiter.go go-backend/internal/worker/concurrency_limiter_test.go go-backend/internal/worker/v2_executor.go go-backend/internal/worker/v2_executor_test.go go-backend/internal/config/config.go go-backend/internal/config/config_test.go go-backend/cmd/worker/main.go
git commit -m "feat(worker): add global task concurrency limiter"
```

---

### Task 3: 重试策略收敛为可测试策略对象（稳定性）

**Files:**
- Create: `go-backend/internal/worker/retry_policy.go`
- Create: `go-backend/internal/worker/retry_policy_test.go`
- Modify: `go-backend/internal/worker/v2_executor.go`
- Modify: `go-backend/internal/worker/v2_executor_test.go`

**Step 1: 先写失败测试，锁定指数退避行为（RED）**

在 `retry_policy_test.go` 新增测试 `TestRetryPolicyBackoffIsExponentialAndCapped`，断言延迟序列为 `100ms, 200ms, 400ms, 800ms` 且不超过上限。

示例代码：
```go
policy := RetryPolicy{Base: 100 * time.Millisecond, Max: 800 * time.Millisecond}
if got := policy.DelayForAttempt(4); got != 800*time.Millisecond {
    t.Fatalf("expected capped delay 800ms, got %s", got)
}
```

**Step 2: 运行测试确认失败（RED）**

Run:
```bash
go test ./internal/worker -run TestRetryPolicyBackoffIsExponentialAndCapped -count=1
```
Expected:
- FAIL

**Step 3: 实现策略对象并替换散落逻辑（GREEN）**

`retry_policy.go` 最小实现：
```go
type RetryPolicy struct {
    Base time.Duration
    Max  time.Duration
}

func (p RetryPolicy) DelayForAttempt(attempt int) time.Duration {
    if attempt < 1 { attempt = 1 }
    base := p.Base
    if base <= 0 { base = 100 * time.Millisecond }
    max := p.Max
    if max <= 0 { max = 2 * time.Second }
    d := base << (attempt - 1)
    if d > max { return max }
    return d
}
```

在 `v2_executor.go` 用 `RetryPolicy.DelayForAttempt(attempt)` 替换 `runRetryBackoffDuration` 的线性退避逻辑。

**Step 4: 运行相关测试**

Run:
```bash
go test ./internal/worker -run 'TestRetryPolicyBackoffIsExponentialAndCapped|TestExecutorMarksFailedAfterRetryExhausted' -count=1
```
Expected:
- PASS

**Step 5: 提交**

```bash
git add go-backend/internal/worker/retry_policy.go go-backend/internal/worker/retry_policy_test.go go-backend/internal/worker/v2_executor.go go-backend/internal/worker/v2_executor_test.go
git commit -m "refactor(worker): centralize retry backoff policy"
```

---

### Task 4: 进度写回节流，降低数据库写放大（资源效率）

**Files:**
- Create: `go-backend/internal/worker/progress_throttle.go`
- Create: `go-backend/internal/worker/progress_throttle_test.go`
- Modify: `go-backend/internal/worker/v2_executor.go`
- Modify: `go-backend/internal/worker/v2_executor_test.go`

**Step 1: 先写失败测试，定义“同进度不重复写 + 高频回调限流”（RED）**

新增测试 `TestProgressThrottleSuppressesDuplicateWrites`，给同样的进度回调 10 次，断言 repo 仅写 1 次。

示例断言：
```go
if len(repo.progressUpdates) != 1 {
    t.Fatalf("expected 1 persisted progress update, got %d", len(repo.progressUpdates))
}
```

**Step 2: 运行测试确认失败（RED）**

Run:
```bash
go test ./internal/worker -run TestProgressThrottleSuppressesDuplicateWrites -count=1
```
Expected:
- FAIL

**Step 3: 实现最小节流器并接入（GREEN）**

`progress_throttle.go` 最小实现：
```go
type ProgressThrottle struct {
    lastDownloaded int
    lastTotal      int
}

func (t *ProgressThrottle) ShouldPersist(downloaded, total int) bool {
    if downloaded == t.lastDownloaded && total == t.lastTotal {
        return false
    }
    t.lastDownloaded = downloaded
    t.lastTotal = total
    return true
}
```

在 `v2_executor.go` 的 progress callback 前调用 `ShouldPersist`，仅在返回 `true` 时写 DB。

**Step 4: 运行相关测试**

Run:
```bash
go test ./internal/worker -run 'TestProgressThrottleSuppressesDuplicateWrites|TestExecutorPersistsProgressUpdatesFromDownloader' -count=1
```
Expected:
- PASS

**Step 5: 提交**

```bash
git add go-backend/internal/worker/progress_throttle.go go-backend/internal/worker/progress_throttle_test.go go-backend/internal/worker/v2_executor.go go-backend/internal/worker/v2_executor_test.go
git commit -m "perf(worker): throttle duplicate progress persistence"
```

---

### Task 5: 运行时收敛为 v2-only（移除 v1 消费路径耦合）

**Files:**
- Modify: `go-backend/cmd/worker/main.go`
- Modify: `go-backend/cmd/worker/main_test.go`
- Modify: `go-backend/internal/config/config.go`
- Modify: `go-backend/internal/config/config_test.go`
- Modify: `go-backend/cmd/server/main.go`
- Modify: `go-backend/cmd/server/main_test.go`
- Modify: `go-backend/internal/httpapi/api.go`
- Modify: `go-backend/internal/httpapi/api_test.go`

**Step 1: 先写失败测试，锁定 worker 只启动 v2 消费链路（RED）**

在 `cmd/worker/main_test.go` 增加测试 `TestWorkerMainUsesOnlyV2ConsumerPipeline`（通过抽取 `buildWorkerRuntime` 工厂函数后注入 fake 检查组件数量）。

断言：只创建 1 个 run loop（v2），不再创建 legacy consumer。

**Step 2: 运行测试确认失败（RED）**

Run:
```bash
go test ./cmd/worker -run TestWorkerMainUsesOnlyV2ConsumerPipeline -count=1
```
Expected:
- FAIL

**Step 3: 实现 v2-only 运行时最小改动（GREEN）**

在 `cmd/worker/main.go` 删除 legacy stream/consumer 启动分支，保留：
```go
go func() {
    errCh <- v2Executor.Run(runCtx, v2Consumer)
}()
if err := <-errCh; err != nil {
    stop()
    log.Fatalf("worker pipeline exited with error: %v", err)
}
```

并移除对 `cfg.StreamName` 的运行时依赖。

**Step 4: 收敛 API 入队分支，默认走 v2 task queue**

在 `httpapi/api.go` 删除 `downloadQueue/downloadSubmitter` 双分支中的 legacy fallback，统一走 `legacyAdapter.CreateOrReuseDownloadTask(...)` 触发 v2 入队（已有路径）。

**Step 5: 运行相关测试**

Run:
```bash
go test ./cmd/server ./cmd/worker ./internal/config ./internal/httpapi -count=1
```
Expected:
- PASS

**Step 6: 提交**

```bash
git add go-backend/cmd/worker/main.go go-backend/cmd/worker/main_test.go go-backend/internal/config/config.go go-backend/internal/config/config_test.go go-backend/cmd/server/main.go go-backend/cmd/server/main_test.go go-backend/internal/httpapi/api.go go-backend/internal/httpapi/api_test.go
git commit -m "refactor(runtime): run v2-only worker and queue pipeline"
```

---

### Task 6: Python 运行时归档化（保留代码，不参与默认流程）

**Files:**
- Create: `docs/runbooks/python-legacy-archive.md`
- Create: `legacy/python/README.md`
- Modify: `README.md`
- Modify: `docs/PROJECT_UPDATES.md`
- Modify: `scripts/start_local.sh`
- Create: `scripts/start_local_go.sh`

**Step 1: 先写失败文档断言测试（RED）**

在 `go-backend/internal/config/compose_contract_test.go` 新增断言：`README.md` 必须包含“Go 主线默认运行”与“Python 仅 legacy 归档”说明。

示例断言：
```go
if !strings.Contains(readmeText, "Python 仅作为 legacy 归档参考") {
    t.Fatalf("README must state python runtime is archived")
}
```

**Step 2: 运行测试确认失败（RED）**

Run:
```bash
go test ./internal/config -run TestComposeTopologyMatchesGoBackendOnly -count=1
```
Expected:
- FAIL（文档文案尚未补齐）

**Step 3: 实现最小文档与脚本收敛（GREEN）**

- `scripts/start_local.sh` 改为提示并转发到 `scripts/start_local_go.sh`。
- 新增 `scripts/start_local_go.sh`：
```bash
#!/usr/bin/env bash
set -euo pipefail
docker compose up -d --build
docker compose ps
```
- README 与 runbook 写清楚：Python 不参与默认运行。

**Step 4: 运行测试**

Run:
```bash
go test ./internal/config -count=1
```
Expected:
- PASS

**Step 5: 提交**

```bash
git add docs/runbooks/python-legacy-archive.md legacy/python/README.md README.md docs/PROJECT_UPDATES.md scripts/start_local.sh scripts/start_local_go.sh go-backend/internal/config/compose_contract_test.go
git commit -m "docs(runtime): mark python as legacy archive and promote go startup path"
```

---

### Task 7: 全量验证与性能回归基线

**Files:**
- Create: `docs/runbooks/go-runtime-optimization-evidence-2026-03-13.md`

**Step 1: 运行 Go 全量测试**

Run:
```bash
cd go-backend && go test ./... -count=1
```
Expected:
- PASS

**Step 2: 运行 Go 竞态测试**

Run:
```bash
cd go-backend && go test -race ./... -count=1
```
Expected:
- PASS

**Step 3: 运行前端 lint + E2E**

Run:
```bash
npm ci
npm run lint
npm run e2e:test
```
Expected:
- PASS

**Step 4: 记录证据文档**

将关键命令与输出摘要记录到：`docs/runbooks/go-runtime-optimization-evidence-2026-03-13.md`。

**Step 5: 最终提交**

```bash
git add docs/runbooks/go-runtime-optimization-evidence-2026-03-13.md
git commit -m "docs(verification): record go runtime optimization validation evidence"
```

---

## 执行注意事项

- 每个 Task 严格走 TDD：先红后绿再重构。
- 每个 Task 完成后执行最小回归，再提交。
- 不在同一提交中混合“功能改动 + 大规模重命名/迁移”。
- 若 Task 5（v2-only）中出现兼容风险，先用特性开关保护，再推进默认切换。

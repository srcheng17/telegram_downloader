# Worker 路径整合 + HTTP Handler 拆分实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 下线 worker 端两条死路径（旧 stream `worker.Consumer/Executor` + V2 stream `v2Executor`），收敛到唯一 canonical 路径 `taskCoreExecutor`；同时拆分两个超过 1k 行的巨型 HTTP handler 文件。

**Architecture:** 当前 `cmd/worker/main.go` 启动 4 个 goroutine（旧 consumer、v2Executor、taskCoreExecutor、taskCoreRecovery）。审计发现前端只产生 task_core 流量（POST /download → `httpapi/taskcore_handlers.go`），httpv2 `/v2/tasks*` 端点仅被 e2e backend-smoke 调用，旧 `download_tasks` Redis stream 在生产无人写入。本计划按"先拆分（无破坏）后删除（有破坏）"顺序推进，每一步保证 `go test ./...` + `npm run build` + e2e 全绿。

**Tech Stack:** Go 1.23 / chi / pgx / go-redis / Vite / Playwright / Docker Compose

**前置约束（critical）：**
- 拆分文件用纯 Go 编译器验证：`go build ./...` + `go test ./...` 必须全过。不允许"看起来对了就提交"。
- 删除代码必须先 grep 确认无任何 import 或字符串引用，再删除。任何无法理解的引用先暂停问主程序。
- 跨层共享：`internal/httpv2/metadata_history_store.go` 和 `upload_task_store.go` 被 `internal/httpapi/upload_handlers.go` 调用 — 删 v2 tasks 路由时**不要**误删这两个文件。
- 仓库根有 `web/static/dist/` bundle 同步约束（README 第 94-96 行），本计划不改前端 .js，无需重 build。
- 提交频率：每个 task 结束 commit 一次，分支命名 `feat/worker-consolidation-handler-split`。

---

## Stage 0：分支与基线

### Task 0：建分支 + 跑基线测试

**Files:**
- 无修改

- [ ] **Step 1：创建特性分支**

```bash
git checkout -b feat/worker-consolidation-handler-split
```

- [ ] **Step 2：跑 Go 全套测试，记录基线**

```bash
go test ./... 2>&1 | tail -20
go build ./...
```
预期：全部 PASS，无 build error。

- [ ] **Step 3：跑前端测试与 lint**

```bash
npm run test:frontend
npm run lint
```
预期：PASS。

- [ ] **Step 4：保存基线测试数**

```bash
go test ./... 2>&1 | grep -c "^ok" > /tmp/baseline_passing_pkgs.txt
cat /tmp/baseline_passing_pkgs.txt
```
预期：输出一个整数（如 30+），后续每个 task 完成后此数应**只增不减**。

> 不提交。本 task 仅建分支 + 基线，不产生 diff。

---

## Stage 1：拆分 `internal/httpapi/api.go`（1262 行 → 4 文件）

**重要前提：** 因为 `cmd/server/main.go:76` 总是注入 `TaskCoreService`，`api.go` 中所有 `taskCoreService == nil` 的 fallback 分支在生产实际跑不到。但 **Stage 1 不删它们**，只做物理拆分以降低单文件认知负荷；删除 fallback 留给 Stage 6。

### Task 1：拆分 api.go — 抽出 logs/summary handlers

**Files:**
- Create: `internal/httpapi/logs_handlers.go`
- Modify: `internal/httpapi/api.go`（删除被搬走的函数）

- [ ] **Step 1：把以下函数原样剪切到新文件 `internal/httpapi/logs_handlers.go`**

要搬动的方法（receiver 仍是 `*API`）：
- `handleSummary`（约第 436-463 行）
- `handleLogs`（约第 465-510 行）
- `buildSummary`（如果存在，是 logs 路径的辅助）
- `normalizeLogQuery`（如果它仅被 logs 用）

新文件头：

```go
package httpapi

import (
	"net/http"
	// 必要的 import 由编译器报错时补全
)
```

注意：`handleMetadataHistory` 当前在 `upload_handlers.go:122`，**不要动它**。

- [ ] **Step 2：编译验证**

```bash
go build ./internal/httpapi/...
```
预期：no error。若有 unused import，删之；若有 undefined symbol，把对应函数也搬过来或保留在原处。

- [ ] **Step 3：跑 httpapi 包测试**

```bash
go test ./internal/httpapi/...
```
预期：PASS，测试数与基线一致。

- [ ] **Step 4：跑全量测试**

```bash
go test ./...
```
预期：PASS。

- [ ] **Step 5：commit**

```bash
git add internal/httpapi/logs_handlers.go internal/httpapi/api.go
git commit -m "refactor(httpapi): extract logs/summary handlers from api.go"
```

### Task 2：拆分 api.go — 抽出 dashboard/health handlers

**Files:**
- Create: `internal/httpapi/dashboard_handlers.go`
- Modify: `internal/httpapi/api.go`

- [ ] **Step 1：把以下方法剪切到 `internal/httpapi/dashboard_handlers.go`**

- `handleRoot`（约第 392-394 行）
- `handleLogsPageRedirect`（第 396-398 行）
- `handleV2DashboardPage`（第 400-402 行）
- `handleV2TasksPage`（第 404-406 行）
- `handleHealthz`（第 408-410 行）
- `handleReadyz`（第 412-434 行）

并把对应的 HTML 常量（`v2DashboardPageHTML`、`v2TasksPageHTML` — 在 api.go 顶部，约第 40-260 行的字符串字面量）一起搬过去。

- [ ] **Step 2：编译验证**

```bash
go build ./internal/httpapi/...
```

- [ ] **Step 3：跑测试**

```bash
go test ./internal/httpapi/...
```

注意：`internal/httpapi/api_test.go:571` 有 `/v2/tasks-ui` 路径断言，需保持不变。

- [ ] **Step 4：commit**

```bash
git add internal/httpapi/dashboard_handlers.go internal/httpapi/api.go
git commit -m "refactor(httpapi): extract dashboard/health handlers from api.go"
```

### Task 3：拆分 api.go — 抽出 download handlers（fallback 分支）

**Files:**
- Create: `internal/httpapi/download_handlers.go`
- Modify: `internal/httpapi/api.go`

- [ ] **Step 1：把以下 fallback 分支专用方法搬到 `internal/httpapi/download_handlers.go`**

- `handleDownload`（POST /download 的旧 fallback；只在 `TaskCoreService == nil` 时注册，但代码当前仍在）
- `handleTaskCancel`（第 663+）
- `handleTaskRetry`
- `handleTaskCopyToKomga`
- `handleTaskDownload`（GET/HEAD /api/tasks/{id}/download）

保留 `api.go` 中的路由注册逻辑（NewRouterWithOptions），它仍引用这些方法名。

- [ ] **Step 2：编译验证**

```bash
go build ./...
```

- [ ] **Step 3：测试**

```bash
go test ./internal/httpapi/...
```
注意：因为这些方法在 fallback 分支注册，若 api_test.go 测试用 `TaskCoreService = nil` 的 router，需确认仍能找到方法。

- [ ] **Step 4：commit**

```bash
git add internal/httpapi/download_handlers.go internal/httpapi/api.go
git commit -m "refactor(httpapi): extract legacy download/cancel/retry handlers from api.go"
```

### Task 4：验证 api.go 已瘦身

- [ ] **Step 1：核对 api.go 行数**

```bash
wc -l internal/httpapi/api.go
```
目标：从原 1262 行降到 < 600 行。若仍超 600，再拆一次（看哪个 handler 群最大）。

- [ ] **Step 2：全量测试 + build**

```bash
go test ./... && go build ./...
```

- [ ] **Step 3：无 commit**（如有微调，合并到上一个 commit 用 `git commit --amend` — 仅当本 step 之前无新 commit；否则单独 commit）。

---

## Stage 2：拆分 `internal/httpv2/tasks_handler.go`（1137 行 → 5 文件）

**重要提示：** 这个文件整体会在 Stage 5 被删除。但 Stage 2 先做拆分是为了：（a）拆分后再删，diff 更清晰；（b）拆分过程中若发现 task_core 路径意外依赖 v2 tasks_handler 内部 helper，能提前发现。

> **可选优化：** 主程序可决定跳过 Stage 2 直接进入 Stage 5（整体删除）。若选择跳过，Stage 5 task 描述里要更细致追踪 import。**默认建议不跳过**。

### Task 5：拆分 tasks_handler.go — 抽出 CreateTask + helpers

**Files:**
- Create: `internal/httpv2/create_handler.go`
- Modify: `internal/httpv2/tasks_handler.go`

- [ ] **Step 1：把 `CreateTask` 方法 + 仅它用到的私有 helper（如请求 body 解析、validation）搬到 `create_handler.go`**

保留 `TasksHandler` struct 定义在 `tasks_handler.go`（这是个共享类型）。

- [ ] **Step 2：编译验证**

```bash
go build ./internal/httpv2/...
go test ./internal/httpv2/...
```

- [ ] **Step 3：commit**

```bash
git add internal/httpv2/create_handler.go internal/httpv2/tasks_handler.go
git commit -m "refactor(httpv2): extract CreateTask from tasks_handler.go"
```

### Task 6：拆分 tasks_handler.go — 抽出 ListTasks/GetTask

**Files:**
- Create: `internal/httpv2/list_handler.go`
- Modify: `internal/httpv2/tasks_handler.go`

- [ ] **Step 1：搬 `ListTasks`、`GetTask`、`GetDashboardSummary` 及其专属 helper**

- [ ] **Step 2：build + test**

```bash
go build ./... && go test ./internal/httpv2/...
```

- [ ] **Step 3：commit**

```bash
git add internal/httpv2/list_handler.go internal/httpv2/tasks_handler.go
git commit -m "refactor(httpv2): extract ListTasks/GetTask/Dashboard from tasks_handler.go"
```

### Task 7：拆分 tasks_handler.go — 抽出 CancelTask + DownloadArtifact

**Files:**
- Create: `internal/httpv2/cancel_artifact_handler.go`
- Modify: `internal/httpv2/tasks_handler.go`

- [ ] **Step 1：搬 `CancelTask`、`DownloadArtifact` 及其 helper**

- [ ] **Step 2：验证**

```bash
go build ./... && go test ./...
```

- [ ] **Step 3：commit**

```bash
git add internal/httpv2/cancel_artifact_handler.go internal/httpv2/tasks_handler.go
git commit -m "refactor(httpv2): extract CancelTask/DownloadArtifact from tasks_handler.go"
```

### Task 8：验证 tasks_handler.go 已瘦身

- [ ] **Step 1：核对行数**

```bash
wc -l internal/httpv2/tasks_handler.go
```
目标：< 400 行（应只剩 struct 定义、构造器、共用 helper）。

- [ ] **Step 2：全量测试**

```bash
go test ./...
```

---

## Stage 3：识别并隔离跨层共享代码

### Task 9：把 httpv2 的共享 store 移到中性包

**Files:**
- Move: `internal/httpv2/metadata_history_store.go` → `internal/store/postgres/metadata_history_store.go`
- Move: `internal/httpv2/upload_task_store.go` → `internal/store/postgres/upload_task_store.go`
- Modify: `internal/httpapi/upload_handlers.go`（更新 import + 类型引用）
- Modify: `internal/httpapi/legacy_adapter.go`（更新 import — 此文件 Stage 5 会删除，先临时改）

- [ ] **Step 1：确认这两个文件的所有使用方**

```bash
grep -rn "httpv2\.MetadataHistoryEntry\|httpv2\.UploadTaskStore" --include="*.go" .
```
预期：列表里有 `internal/httpapi/upload_handlers.go`、`internal/httpapi/legacy_adapter.go`、可能还有 `cmd/server/main.go`。

- [ ] **Step 2：搬文件 + 改 package 名**

```bash
git mv internal/httpv2/metadata_history_store.go internal/store/postgres/metadata_history_store.go
git mv internal/httpv2/metadata_history_store_test.go internal/store/postgres/metadata_history_store_test.go
git mv internal/httpv2/upload_task_store.go internal/store/postgres/upload_task_store.go
git mv internal/httpv2/upload_task_store_test.go internal/store/postgres/upload_task_store_test.go
```

把这四个文件首行的 `package httpv2` 改成 `package postgres`。**重要：** 检查它们是否依赖 `httpv2` 内部其它类型（比如 task model）；如有，把那些类型也一并搬过去或暴露为 export。

- [ ] **Step 3：更新所有引用**

把 `httpv2.MetadataHistoryEntry` 改成 `postgres.MetadataHistoryEntry`（用 sed 或 IDE 全局替换），同理处理 `UploadTaskStore`。

```bash
grep -rn "httpv2\.MetadataHistoryEntry\|httpv2\.UploadTaskStore" --include="*.go" .
```
预期：替换后 grep 返回空。

- [ ] **Step 4：build + test**

```bash
go build ./... && go test ./...
```

- [ ] **Step 5：commit**

```bash
git add -A
git commit -m "refactor(store): move metadata_history/upload_task stores out of httpv2"
```

> 这一步若发现循环依赖或共享类型太多，立即停止并向主程序汇报，不要硬塞。

---

## Stage 4：死代码删除（按依赖顺序，从叶子到根）

### Task 10：删除 `internal/worker/v2_executor.go` + v2 Redis stream 消费

**前置验证：**

- [ ] **Step 1：grep 确认 v2Executor 的所有引用**

```bash
grep -rn "V2Executor\|v2_executor\|v2Executor\|V2ExecutorConfig\|V2PostgresExecutionRepo\|V2ServiceDownloader" --include="*.go" .
```
预期：所有匹配都在 `internal/worker/v2_executor*.go`、`cmd/worker/main.go` 本身。若有其它生产文件依赖，**停下来汇报**。

- [ ] **Step 2：从 `cmd/worker/main.go` 移除 v2 路径**

打开 `cmd/worker/main.go`，做以下编辑：

- 删除 import `queuev2 "github.com/ryancheng/telegram-downloader/internal/queue/v2"`
- 删除第 129-139 行：`v2Repo`、`v2Executor`、`ensureV2ConsumerGroup`、`v2Consumer` 初始化
- 删除第 156-158 行的 `v2Executor.Run(...)` goroutine
- 删除函数 `ensureV2ConsumerGroup`（约第 183-189 行）
- 把 `errCh := make(chan error, 4)` 改成 `errCh := make(chan error, 3)`
- 把 `for i := 0; i < 4` 改成 `for i := 0; i < 3`
- 把 log.Printf 日志里的 `and v2_stream=%s` + `cfg.V2StreamName` 移除

- [ ] **Step 3：删除 v2_executor 文件**

```bash
git rm internal/worker/v2_executor.go internal/worker/v2_executor_test.go
```

- [ ] **Step 4：build**

```bash
go build ./...
```
若报"undefined V2ServiceDownloader"等，继续 grep 清理被遗漏的引用。

- [ ] **Step 5：跑 test**

```bash
go test ./...
```

- [ ] **Step 6：commit**

```bash
git add -A
git commit -m "refactor(worker): remove dead v2 stream executor and consumer"
```

### Task 11：删除 `internal/queue/v2/`（v2 stream producer + consumer 包）

- [ ] **Step 1：grep v2 queue 引用**

```bash
grep -rn "queue/v2\|queuev2\b" --include="*.go" .
```
预期：所有匹配应都在 `internal/queue/v2/` 内部，或被 Task 10 已经移除的位置。若 `cmd/server/main.go:68` 仍引用，需先在 server 端去掉 v2Queue 初始化。

- [ ] **Step 2：从 `cmd/server/main.go` 移除 v2 producer**

打开 `cmd/server/main.go`，做：
- 删除 import `queuev2 "github.com/ryancheng/telegram-downloader/internal/queue/v2"`
- 删除第 68 行 `v2Queue := httpv2.NewV2TaskQueue(queuev2.NewProducer(redisClient, cfg.V2StreamName))`
- 删除 `legacyRouterOptions.V2TaskQueue` 字段赋值（如有）
- 删除 `httpv2.RegisterRoutes(rootRouter, httpv2.NewTasksHandler(v2Store, v2Queue))` 第二个参数 — 暂时改成 `nil` 占位，下一个 task 会处理这条路由本身

```bash
go build ./...
```
若 build 红，按报错继续修。

- [ ] **Step 3：删除目录**

```bash
git rm -r internal/queue/v2
```

- [ ] **Step 4：test**

```bash
go test ./...
```

- [ ] **Step 5：commit**

```bash
git add -A
git commit -m "refactor(queue): remove dead v2 stream queue package"
```

### Task 12：删除 `internal/worker/consumer.go + executor.go + task_downloader.go`（旧 stream 消费）

- [ ] **Step 1：grep 旧 worker 入口的引用**

```bash
grep -rn "worker\.Consumer\|worker\.Executor\|worker\.NewConsumer\|worker\.NewTaskDownloader\|worker\.NewRedisStream\|worker\.TaskDownloaderConfig" --include="*.go" .
```
预期：所有命中都在 `internal/worker/` 内部（自己引用自己）或 `cmd/worker/main.go`。如果有 `internal/app/...` 引用，**停下来汇报**。

注意：`internal/worker/taskcore/` 子包里的 `taskcore.NewTaskDownloader` 是另一个东西，**不要混淆**。

- [ ] **Step 2：从 `cmd/worker/main.go` 移除旧 consumer**

删除：
- `stream := worker.NewRedisStream(...)` + `stream.CreateGroup(...)`
- `taskDownloader := worker.NewTaskDownloader(...)`
- `executor := &worker.Executor{...}`
- `consumer := worker.NewConsumer(...)`
- 对应 goroutine：`errCh <- consumer.Run(runCtx)`
- 把 errCh buffer 从 3 改成 2，循环改成 `for i := 0; i < 2`
- 移除日志中 `cfg.StreamName` 的引用，只保留 task-core 工作描述

- [ ] **Step 3：删文件**

```bash
git rm internal/worker/consumer.go internal/worker/consumer_test.go
git rm internal/worker/executor.go internal/worker/executor_test.go
git rm internal/worker/task_downloader.go internal/worker/task_downloader_test.go
git rm internal/worker/output_filename.go internal/worker/output_filename_test.go
```

- [ ] **Step 4：build + test**

```bash
go build ./... && go test ./...
```

- [ ] **Step 5：commit**

```bash
git add -A
git commit -m "refactor(worker): remove dead legacy stream consumer/executor"
```

### Task 13：删除 `internal/queue/redisstream/` 与 `internal/queue/queue.go`

- [ ] **Step 1：grep 引用**

```bash
grep -rn "redisstream\|queue\.DownloadQueue\|queue\.EnqueueMessage" --include="*.go" .
```

如果 `internal/httpapi/api.go` 仍然引用（约第 289 行 `downloadQueue queue.DownloadQueue`），先在 api.go 里把 `downloadQueue` 字段和相关 `EnqueueDownload` 调用注释或删除（这部分 Task 14 会彻底清理 fallback 分支，先临时让它编译通过即可 — 删字段、删 RouterOptions.DownloadQueue 字段、删 cmd/server/main.go 中 `downloadQueue` 参数）。

- [ ] **Step 2：精简 cmd/server/main.go**

- 删除 import `"github.com/ryancheng/telegram-downloader/internal/queue"` 和 `"github.com/ryancheng/telegram-downloader/internal/queue/redisstream"`
- 删除第 66 行 `downloadQueue := redisstream.NewProducer(...)`
- 删除 `buildLegacyRouterOptions` 函数中 `downloadQueue` 参数与 `DownloadQueue` 字段
- 删除调用处对应的实参

- [ ] **Step 3：精简 httpapi 包**

- 在 `internal/httpapi/api.go` 删除 `downloadQueue` 字段、`DownloadQueue` RouterOptions 字段、构造函数中的赋值
- 在 `internal/httpapi/download_handlers.go`（Task 3 拆分出来的）中删除 `a.downloadQueue != nil` 分支
- `apiErrorCodeEnqueueFailed` 错误码暂时保留（fallback 路径删除后才完全失效，Task 14 处理）

- [ ] **Step 4：删文件**

```bash
git rm -r internal/queue/redisstream
git rm internal/queue/queue.go
rmdir internal/queue 2>/dev/null || true
```

- [ ] **Step 5：build + test**

```bash
go build ./... && go test ./...
```

- [ ] **Step 6：commit**

```bash
git add -A
git commit -m "refactor(queue): remove dead redisstream producer and queue package"
```

### Task 14：删除 `internal/httpapi` 里的 legacy_adapter + LegacyBridge

- [ ] **Step 1：grep 引用**

```bash
grep -rn "legacyAdapter\|LegacyAdapter\|LegacyBridge\|legacy_adapter\|legacyBridge" --include="*.go" .
```

- [ ] **Step 2：从 `api.go` 移除 fallback 分支**

打开 `internal/httpapi/api.go`，做以下修改：
- 删除 `legacyAdapter` struct 字段
- 删除 `LegacyAdapterOptions` / `LegacyV2TaskStore` / `LegacyV2TaskQueue` 等 RouterOptions 中已经无用的字段
- `NewRouterWithOptions` 函数中：删除 `if options.TaskCoreService != nil { ... } else { ... }` 的 else 分支（即所有 fallback 路由注册），因为生产中 `TaskCoreService` 始终非空
- 同样在 `handleSummary` / `handleLogs` 中删除 `a.legacyAdapter != nil && a.legacyAdapter.SupportsXxx()` 的整个分支（约 api.go 第 447-484 行附近 + 拆分后的 logs_handlers.go）
- 把降级到 `a.buildSummary` / 直接读 store 的旧路径删除（若它们仅在 fallback 分支被调用）

- [ ] **Step 3：删文件**

```bash
git rm internal/httpapi/legacy_adapter.go
git rm internal/httpapi/legacy_adapter_logs.go
# 找到并删除相关测试
ls internal/httpapi/legacy_adapter*_test.go 2>/dev/null && git rm internal/httpapi/legacy_adapter*_test.go
git rm internal/app/tasks/legacy_bridge.go
git rm internal/app/tasks/legacy_bridge_test.go
```

- [ ] **Step 4：清理 `internal/app/tasks/` 里专门给 legacy bridge 用的类型**

grep 一遍 `LegacyDownloadDecision`、`LegacySubmitInput`、`LegacyClaimInput` 等类型，确认只在已删除的文件里出现。如果还有外部引用（不太可能），停下来汇报。

- [ ] **Step 5：build + test**

```bash
go build ./... && go test ./...
```
预期：`internal/httpapi/api_test.go` 中测试 legacy 路径的 case 可能要删（如 `TestRouter_FallbackHandlersRegisteredWhenNoTaskCore`），按编译/测试报错精确删除。

- [ ] **Step 6：commit**

```bash
git add -A
git commit -m "refactor(httpapi): remove dead legacy adapter and fallback branch"
```

### Task 15：删除 `internal/httpv2/tasks_handler.go` + 拆出的 v2 task handlers + 路由

- [ ] **Step 1：grep `/v2/tasks` 和相关 handler 的非测试引用**

```bash
grep -rn "TasksHandler\|NewTasksHandler\|httpv2\.RegisterRoutes\b" --include="*.go" .
```
预期：只在 `internal/httpv2/` 内、`cmd/server/main.go` 出现。

- [ ] **Step 2：精简 `internal/httpv2/router.go`**

把 `router.go` 改成只注册 `/v2/settings` 子路由：

```go
package httpv2

import (
	"github.com/go-chi/chi/v5"
)

func RegisterSettings(r chi.Router, h *SettingsHandler) {
	RegisterSettingsRoutes(r, h)
}
```

（或保留 `RegisterRoutes` 名字但只调 settings — 看 main.go 怎么调用更顺）。把原 `/v2/dashboard/summary`、`/v2/tasks*` 这些行全部删除。

- [ ] **Step 3：调整 `cmd/server/main.go`**

把：
```go
httpv2.RegisterRoutes(rootRouter, httpv2.NewTasksHandler(v2Store, v2Queue))
```
改成：
```go
httpv2.RegisterSettings(rootRouter, httpv2.NewSettingsHandler(v2Store))
```

并移除 `v2Store := httpv2.NewPostgresTaskStore(pool)` 这一行（如果它只给 tasks_handler 用）— 但要先确认 settings_handler 是否也用它。如果用，保留；如果不用，删。

- [ ] **Step 4：删 tasks_handler 文件群**

```bash
git rm internal/httpv2/tasks_handler.go
git rm internal/httpv2/tasks_handler_test.go
git rm internal/httpv2/tasks_handler_claim_store_test.go
git rm internal/httpv2/contract_test.go
git rm internal/httpv2/task_filters.go internal/httpv2/task_filters_test.go
# 以及 Task 5-7 拆出来的：
git rm internal/httpv2/create_handler.go
git rm internal/httpv2/list_handler.go
git rm internal/httpv2/cancel_artifact_handler.go
```

注意：保留 `internal/httpv2/settings_handler.go` + 测试。

- [ ] **Step 5：build + test**

```bash
go build ./... && go test ./...
```
预期：编译需要解决一些遗漏引用。test 中 `api_test.go:571,591` 测试 `/v2/tasks-ui` 重定向，**这个保留**（任务列表 UI 页面不是 /v2/tasks JSON API）。

- [ ] **Step 6：commit**

```bash
git add -A
git commit -m "refactor(httpv2): remove dead /v2/tasks JSON API, keep /v2/settings"
```

### Task 16：清理 `internal/app/tasks` 里仅 fallback 用的代码

- [ ] **Step 1：grep 看哪些 app/tasks 文件还在用**

```bash
grep -rn "apptasks\.\|app/tasks\b" --include="*.go" . | grep -v "_test.go" | awk -F: '{print $1}' | sort -u
```
列出仍引用 `internal/app/tasks` 的非测试文件。

- [ ] **Step 2：按需删除**

`internal/app/tasks/` 下许多文件是给 legacy_adapter 用的（`create.go`、`upload.go`、`cancel.go`、`retry.go`、`copy_result.go`、`komga_copy.go`、`metadata.go`、`run_task.go`、`artifact_access.go`、`list_logs.go`、`service.go`、`status_catalog.go`、`types.go`）。

判断流程：对每个文件，grep 其导出的 type/func 名，看是否还有非自身、非测试引用。如果没有，删除该文件 + 其测试。

注意：`internal/app/taskcore/` 是 task_core 路径用的，**不要碰**。

举例：
```bash
grep -rn "apptasks\.CreateInput\|apptasks\.Service" --include="*.go" . | grep -v "internal/app/tasks/" | grep -v "_test.go"
```

逐个判断后批量 `git rm`。如果不确定某个文件是否还在用，**保留它**，标注 TODO 留待后续验证。

- [ ] **Step 3：build + test**

```bash
go build ./... && go test ./...
```

- [ ] **Step 4：commit**

```bash
git add -A
git commit -m "refactor(app): remove unused legacy task service files"
```

---

## Stage 5：测试与 e2e 更新

### Task 17：处理 `tests/e2e/specs/backend-smoke.spec.js`

这个 e2e 测试现在依赖已删除的 `/v2/tasks` POST/GET/cancel 端点。

- [ ] **Step 1：阅读现有 spec**

```bash
cat tests/e2e/specs/backend-smoke.spec.js
```

- [ ] **Step 2：决定改写还是删除**

判断：该 spec 是否覆盖了 `upload-flow.spec.js`、`logs-flow.spec.js` 等其他 spec 没覆盖的内容？
- 如果是冗余（task 创建走 /v2/tasks，等价于 home 页 /download 提交，已被 upload-flow 等覆盖），**删除整个文件**：
  ```bash
  git rm tests/e2e/specs/backend-smoke.spec.js
  ```
- 如果它覆盖独特路径（如纯 API 层 smoke 不经过 UI），改写为通过 `POST /download` form-encoded：
  ```js
  const createResponse = await request.post('/download', {
    form: { url: 'https://telegra.ph/demo' },
    headers: { Accept: 'application/json' },
  });
  ```
  并把后续 `/v2/tasks/{id}/cancel` 改成 `/api/tasks/{id}/cancel`。

**默认推荐：删除**（其它 spec 已覆盖创建/取消流程）。

- [ ] **Step 3：本地 e2e 验证（可选，需 Docker）**

```bash
npm run e2e:test
```
若 Docker 不可用，跳过此步，等 CI 跑。

- [ ] **Step 4：commit**

```bash
git add -A
git commit -m "test(e2e): drop backend-smoke spec covered by upload/logs flows"
```

### Task 18：清理 `frontend/src/shared/api/tasks_api.js` 中的 `createTask` 死方法

- [ ] **Step 1：再次确认前端无调用**

```bash
grep -rn "\.createTask(\|api\.createTask\b" frontend/src --include="*.js" --include="*.mjs"
```
预期：除 `tasks_api.js:71` 定义本身和 `tests/tasks_api.test.mjs` 外，无其它命中。

- [ ] **Step 2：删除 createTask 方法**

打开 `frontend/src/shared/api/tasks_api.js`，删除第 71-81 行的 `async createTask(payload) { ... }` 整段（包括前面的逗号）。

- [ ] **Step 3：更新或删除对应测试**

```bash
cat frontend/src/tests/tasks_api.test.mjs
```
删除其中所有断言 `/v2/tasks` 或 `createTask` 的测试 case。

- [ ] **Step 4：重新 build 前端 bundle（README 第 94-96 行的约束）**

```bash
npm run lint
npm run test:frontend
npm run build
```
build 后 `web/static/dist/*.bundle.js` 应有 diff。

- [ ] **Step 5：commit（含 bundle）**

```bash
git add frontend/src/shared/api/tasks_api.js frontend/src/tests/tasks_api.test.mjs web/static/dist/
git commit -m "chore(frontend): drop unused createTask method targeting /v2/tasks"
```

---

## Stage 6：发布前验证

### Task 19：跑完整 release gate

- [ ] **Step 1：Go 全套**

```bash
go test ./... && go test -race ./...
```

- [ ] **Step 2：前端 + build**

```bash
npm run test:frontend && npm run lint && npm run build
```

- [ ] **Step 3：e2e（如本地 Docker 可用）**

```bash
npm run e2e:test
```
如本地不可用，标注"待 CI 验证"，**不要跳过**记录。

- [ ] **Step 4：体检统计**

```bash
git log --oneline main..HEAD | wc -l
git diff main --stat | tail -5
```
记录提交数和净 diff 行数（应净减少 3000+ 行）。

- [ ] **Step 5：核对 worker 启动日志（手动）**

如本地 Docker 可用，启动 compose，看 worker 日志：

```bash
docker compose up -d --build go-worker
docker compose logs go-worker | head -20
docker compose down
```
预期：日志显示 `task-core` 路径在跑，没有 `consuming stream=download_tasks` 字样。

- [ ] **Step 6（无 commit，仅汇报）：** 把 Step 4-5 的输出贴给主程序，由主程序决定是否 PR。

---

## 任务依赖关系

```
Task 0 (建分支)
  ↓
Task 1-3 (拆 api.go) → Task 4 (验证)
  ↓
Task 5-7 (拆 tasks_handler.go) → Task 8 (验证)
  ↓
Task 9 (搬共享 store)
  ↓
Task 10 (删 v2_executor)
  ↓
Task 11 (删 queue/v2)
  ↓
Task 12 (删 legacy worker)
  ↓
Task 13 (删 redisstream)
  ↓
Task 14 (删 legacy_adapter)
  ↓
Task 15 (删 httpv2 tasks_handler)
  ↓
Task 16 (清理 app/tasks)
  ↓
Task 17 (e2e backend-smoke)
  ↓
Task 18 (前端 createTask)
  ↓
Task 19 (release gate)
```

每个 task 必须独立编译通过 + 测试通过 + commit，才能开始下一个。

---

## Self-review note

- 所有删除前都有 grep 验证步骤。
- 跨层共享代码（`metadata_history_store`、`upload_task_store`）在 Stage 3 单独搬迁，避免 Stage 5 误删。
- 前端 bundle 同步约束（Task 18 Step 4）已显式列出。
- e2e backend-smoke 删除/改写在 Task 17 明确给出两个选项。
- 每个 task 含完整 git 命令、build/test 命令、expected 输出。
- 风险点（任务大小、不确定的 app/tasks 文件）通过 "停下来汇报" 显式标注。

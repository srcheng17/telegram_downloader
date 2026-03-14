# Archive Upload & Stability Optimization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为项目增加 ZIP/CBZ 异步上传主链路，并让上传任务与 URL 任务在日志、重试、下载、元数据回溯上保持一致，同时清理残留代码并增强稳定性。

**Architecture:** 保持现有 `go-api + go-worker + postgres + redis + legacy-facing logs/UI` 主线不变，在任务模型中引入 `source_type=archive_upload`、`phase`、统一进度字段与三层元数据；API 负责创建上传任务与接收文件流，worker 负责校验压缩包、提取图片/元数据、合并首页填写信息并重打包最终 `cbz`。legacy adapter 与前端日志页继续作为统一入口，但不再丢弃 v2 任务的进度、阶段和元数据。

**Tech Stack:** Go (chi/pgx), PostgreSQL migrations, Redis Streams, Vite frontend modules, Node test, Go test, Playwright E2E, Docker Compose.

**Skills:** 全流程遵循 @test-driven-development 与 @verification-before-completion。执行时请先在独立 worktree 中运行本计划。

---

### Task 1: 扩展任务模型与数据库契约（source_type / phase / progress / metadata）

**Files:**
- Create: `go-backend/internal/store/postgres/migrations/007_archive_upload_support.sql`
- Modify: `go-backend/internal/store/postgres/migrations/runner_test.go`
- Modify: `go-backend/internal/httpv2/tasks_handler.go`
- Modify: `go-backend/internal/store/postgres/v2_repo.go`
- Modify: `go-backend/internal/store/postgres/v2_repo_test.go`
- Modify: `go-backend/internal/domain/types.go`
- Modify: `go-backend/internal/domain/status.go`
- Modify: `go-backend/internal/httpapi/contract_test.go`

**Step 1: Write the failing test**

先在 `v2_repo_test.go` 增加测试 `TestCreateTaskPersistsArchiveUploadFields`，断言 `CreateTaskInput` 写入后能保留：
- `source_type=archive_upload`
- `phase=UPLOADING`
- `progress_mode=bytes`
- `progress_current/progress_total`
- `request_metadata_json/archive_metadata_json/effective_metadata_json`

示例断言：
```go
if persisted.sourceType != "archive_upload" {
    t.Fatalf("expected archive source type, got %q", persisted.sourceType)
}
if persisted.phase != "UPLOADING" {
    t.Fatalf("expected phase UPLOADING, got %q", persisted.phase)
}
if persisted.progressMode != "bytes" || persisted.progressTotal != 1024 {
    t.Fatalf("unexpected progress snapshot: %#v", persisted)
}
```

并在 `contract_test.go` 先补充日志返回字段契约断言（当前会失败）：
```go
assertPayloadIsString(t, firstLog, "source_type")
assertPayloadOptionalString(t, firstLog, "phase")
assertPayloadOptionalString(t, firstLog, "progress_mode")
assertPayloadIsNumber(t, firstLog, "progress_current")
assertPayloadIsNumber(t, firstLog, "progress_total")
```

**Step 2: Run test to verify it fails**

Run:
```bash
cd go-backend
go test ./internal/store/postgres ./internal/httpapi -run 'TestCreateTaskPersistsArchiveUploadFields|TestLogsContractIncludesArchiveProgressFields' -count=1
```
Expected:
- FAIL，提示缺少新字段或扫描/JSON 契约不匹配。

**Step 3: Write minimal implementation**

在 migration 中新增列：
```sql
ALTER TABLE v2_tasks
  ADD COLUMN IF NOT EXISTS source_type TEXT NOT NULL DEFAULT 'url',
  ADD COLUMN IF NOT EXISTS phase TEXT,
  ADD COLUMN IF NOT EXISTS progress_mode TEXT,
  ADD COLUMN IF NOT EXISTS progress_current BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS progress_total BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS progress_label TEXT,
  ADD COLUMN IF NOT EXISTS source_file_name TEXT,
  ADD COLUMN IF NOT EXISTS source_content_type TEXT,
  ADD COLUMN IF NOT EXISTS source_file_size BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS source_sha256 TEXT,
  ADD COLUMN IF NOT EXISTS source_archive_path TEXT,
  ADD COLUMN IF NOT EXISTS retry_parent_task_id TEXT,
  ADD COLUMN IF NOT EXISTS request_metadata_json JSONB,
  ADD COLUMN IF NOT EXISTS archive_metadata_json JSONB,
  ADD COLUMN IF NOT EXISTS effective_metadata_json JSONB;
```

在 `tasks_handler.go` / `v2_repo.go` 增加对应字段：
```go
type Task struct {
    ID               string  `json:"id"`
    URL              string  `json:"url"`
    SourceType       string  `json:"source_type"`
    Phase            *string `json:"phase,omitempty"`
    ProgressMode     *string `json:"progress_mode,omitempty"`
    ProgressCurrent  int64   `json:"progress_current"`
    ProgressTotal    int64   `json:"progress_total"`
    ProgressLabel    *string `json:"progress_label,omitempty"`
    SourceFileName   *string `json:"source_file_name,omitempty"`
    RetryParentTaskID *string `json:"retry_parent_task_id,omitempty"`
}
```

在 `domain.StatusMeta` 增加 `CanRetry bool`，并先让 `FAILED` 返回 `CanRetry: true`。

**Step 4: Run test to verify it passes**

Run:
```bash
cd go-backend
go test ./internal/store/postgres ./internal/httpapi -run 'TestCreateTaskPersistsArchiveUploadFields|TestLogsContractIncludesArchiveProgressFields' -count=1
```
Expected:
- PASS

**Step 5: Commit**

```bash
git add go-backend/internal/store/postgres/migrations/007_archive_upload_support.sql go-backend/internal/store/postgres/migrations/runner_test.go go-backend/internal/httpv2/tasks_handler.go go-backend/internal/store/postgres/v2_repo.go go-backend/internal/store/postgres/v2_repo_test.go go-backend/internal/domain/types.go go-backend/internal/domain/status.go go-backend/internal/httpapi/contract_test.go
git commit -m "feat(go): add archive upload task schema and progress fields"
```

---

### Task 2: 增加上传任务创建接口（不再全局强制 URL）

**Files:**
- Create: `go-backend/internal/httpapi/archive_request.go`
- Create: `go-backend/internal/httpapi/archive_request_test.go`
- Modify: `go-backend/internal/httpapi/api.go`
- Modify: `go-backend/internal/httpapi/api_test.go`
- Modify: `go-backend/internal/httpapi/contract_test.go`

**Step 1: Write the failing test**

新增测试 `TestCreateArchiveTaskAcceptsMetadataWithoutURL`，断言：
- `POST /api/archive-tasks` 不要求 `url`
- 只要 metadata + file metadata 合法，就返回 `202`
- 创建任务后状态是 `PENDING`，阶段是 `UPLOADING`

示例测试片段：
```go
body := strings.NewReader(`{"file_name":"demo.zip","file_size":2048,"content_type":"application/zip","author":"作者A"}`)
req := httptest.NewRequest(http.MethodPost, "/api/archive-tasks", body)
req.Header.Set("Content-Type", "application/json")

rec := httptest.NewRecorder()
router.ServeHTTP(rec, req)

if rec.Code != http.StatusAccepted {
    t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
}
```

再补一个失败测试 `TestDownloadStillRejectsMissingURLForURLMode`，锁定 `/download` 仍只在 URL 模式下要求 URL。

**Step 2: Run test to verify it fails**

Run:
```bash
cd go-backend
go test ./internal/httpapi -run 'TestCreateArchiveTaskAcceptsMetadataWithoutURL|TestDownloadStillRejectsMissingURLForURLMode' -count=1
```
Expected:
- FAIL，当前还没有 `/api/archive-tasks`。

**Step 3: Write minimal implementation**

在 `archive_request.go` 增加解析：
```go
type archiveTaskCreateRequest struct {
    FileName    string `json:"file_name"`
    FileSize    int64  `json:"file_size"`
    ContentType string `json:"content_type"`
    Author      string `json:"author"`
    SeriesName  string `json:"series_name"`
    ComicName   string `json:"comic_name"`
    Summary     string `json:"summary"`
    Tags        string `json:"tags"`
    Genres      string `json:"genres"`
}
```

在 `api.go` 注册并实现：
```go
router.Post("/api/archive-tasks", api.handleCreateArchiveTask)
```

创建任务时写入：
```go
createInput := httpv2.CreateTaskInput{
    ID:           uuid.NewString(),
    URL:          fmt.Sprintf("archive://%s", fileName),
    SourceType:   "archive_upload",
    Phase:        stringPtr("UPLOADING"),
    ProgressMode: stringPtr("bytes"),
    ProgressTotal: fileSize,
    RequestMetadataJSON: requestMetadataJSON,
}
```

**Step 4: Run test to verify it passes**

Run:
```bash
cd go-backend
go test ./internal/httpapi -run 'TestCreateArchiveTaskAcceptsMetadataWithoutURL|TestDownloadStillRejectsMissingURLForURLMode' -count=1
```
Expected:
- PASS

**Step 5: Commit**

```bash
git add go-backend/internal/httpapi/archive_request.go go-backend/internal/httpapi/archive_request_test.go go-backend/internal/httpapi/api.go go-backend/internal/httpapi/api_test.go go-backend/internal/httpapi/contract_test.go
git commit -m "feat(api): add archive task creation endpoint"
```

---

### Task 3: 增加上传内容接收与上传进度持久化

**Files:**
- Create: `go-backend/internal/service/archive_upload_store.go`
- Create: `go-backend/internal/service/archive_upload_store_test.go`
- Modify: `go-backend/internal/httpapi/api.go`
- Modify: `go-backend/internal/httpapi/api_test.go`
- Modify: `go-backend/internal/httpv2/tasks_handler.go`
- Modify: `go-backend/internal/store/postgres/v2_repo.go`
- Modify: `go-backend/internal/store/postgres/v2_repo_test.go`

**Step 1: Write the failing test**

先在 `archive_upload_store_test.go` 写 `TestReceiveArchiveStreamPersistsFileAndProgress`，断言：
- 以 `io.Reader` 流式写入临时目录
- 回调中能得到逐步增长的字节数
- 最终文件原子落盘

示例：
```go
store := NewArchiveUploadStore(ArchiveUploadStoreConfig{RootDir: tempDir})
progresses := []int64{}
meta, err := store.Receive(ctx, "task-1", strings.NewReader("payload"), 7, func(written, total int64) {
    progresses = append(progresses, written)
})
if err != nil { t.Fatalf("receive: %v", err) }
if meta.Size != 7 || len(progresses) == 0 || progresses[len(progresses)-1] != 7 {
    t.Fatalf("unexpected upload result: %#v %#v", meta, progresses)
}
```

再在 `api_test.go` 写 `TestUploadArchiveContentUpdatesTaskProgressAndEnqueuesWorker`，断言 `PUT /api/archive-tasks/{id}/content`：
- 返回 `202`
- 更新 `progress_current`
- 写入 `source_archive_path`
- 调用队列 enqueue

**Step 2: Run test to verify it fails**

Run:
```bash
cd go-backend
go test ./internal/service ./internal/httpapi ./internal/store/postgres -run 'TestReceiveArchiveStreamPersistsFileAndProgress|TestUploadArchiveContentUpdatesTaskProgressAndEnqueuesWorker' -count=1
```
Expected:
- FAIL

**Step 3: Write minimal implementation**

在 `archive_upload_store.go` 实现：
```go
type ArchiveUploadStore struct { rootDir string }

type ArchiveUploadMeta struct {
    Path   string
    Size   int64
    SHA256 string
}

func (s *ArchiveUploadStore) Receive(ctx context.Context, taskID string, body io.Reader, total int64, onProgress func(written, total int64)) (ArchiveUploadMeta, error)
```

在 `api.go` 增加：
```go
router.Put("/api/archive-tasks/{task_id}/content", api.handleUploadArchiveContent)
```

并在 handler 中：
```go
meta, err := a.archiveUploadStore.Receive(...)
_ = a.v2Store.UpdateArchiveUploadProgress(ctx, taskID, progress)
_ = a.v2Store.MarkArchiveUploaded(ctx, taskID, meta.Path, meta.Size, meta.SHA256)
_ = a.legacyAdapter.EnqueueCreatedTask(...) // 或等价 queue enqueue
```

**Step 4: Run test to verify it passes**

Run:
```bash
cd go-backend
go test ./internal/service ./internal/httpapi ./internal/store/postgres -run 'TestReceiveArchiveStreamPersistsFileAndProgress|TestUploadArchiveContentUpdatesTaskProgressAndEnqueuesWorker' -count=1
```
Expected:
- PASS

**Step 5: Commit**

```bash
git add go-backend/internal/service/archive_upload_store.go go-backend/internal/service/archive_upload_store_test.go go-backend/internal/httpapi/api.go go-backend/internal/httpapi/api_test.go go-backend/internal/httpv2/tasks_handler.go go-backend/internal/store/postgres/v2_repo.go go-backend/internal/store/postgres/v2_repo_test.go
git commit -m "feat(api): stream archive uploads into task progress and queue"
```

---

### Task 4: 实现 ComicInfo 解析、元数据合并与归一化

**Files:**
- Create: `go-backend/internal/downloader/comicinfo.go`
- Create: `go-backend/internal/downloader/comicinfo_test.go`
- Modify: `go-backend/internal/downloader/cbz_writer.go`
- Modify: `go-backend/internal/downloader/cbz_writer_test.go`

**Step 1: Write the failing test**

在 `comicinfo_test.go` 增加：
- `TestParseComicInfoXMLReadsExistingMetadata`
- `TestMergeTaskMetadataPrefersRequestOverArchive`

示例：
```go
archiveMeta := TaskMetadata{Writer: "包内作者", Title: "包内标题"}
requestMeta := TaskMetadata{Writer: "首页作者"}
merged := MergeTaskMetadata(requestMeta, archiveMeta)
if merged.Writer != "首页作者" || merged.Title != "包内标题" {
    t.Fatalf("unexpected merge result: %#v", merged)
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
cd go-backend
go test ./internal/downloader -run 'TestParseComicInfoXMLReadsExistingMetadata|TestMergeTaskMetadataPrefersRequestOverArchive' -count=1
```
Expected:
- FAIL

**Step 3: Write minimal implementation**

在 `comicinfo.go` 实现：
```go
type ComicInfo struct {
    Writer  string `xml:"Writer"`
    Series  string `xml:"Series"`
    Title   string `xml:"Title"`
    Summary string `xml:"Summary"`
    Tags    string `xml:"Tags"`
    Genre   string `xml:"Genre"`
}

func ParseComicInfoXML(data []byte) (TaskMetadata, error)
func MergeTaskMetadata(request TaskMetadata, archive TaskMetadata) TaskMetadata
```

在 `cbz_writer.go` 让写入逻辑可复用 merged metadata。

**Step 4: Run test to verify it passes**

Run:
```bash
cd go-backend
go test ./internal/downloader -run 'TestParseComicInfoXMLReadsExistingMetadata|TestMergeTaskMetadataPrefersRequestOverArchive' -count=1
```
Expected:
- PASS

**Step 5: Commit**

```bash
git add go-backend/internal/downloader/comicinfo.go go-backend/internal/downloader/comicinfo_test.go go-backend/internal/downloader/cbz_writer.go go-backend/internal/downloader/cbz_writer_test.go
git commit -m "feat(downloader): parse and merge archive comic metadata"
```

---

### Task 5: 实现上传任务 worker 导入链路（校验、解压、重打包）

**Files:**
- Create: `go-backend/internal/worker/archive_importer.go`
- Create: `go-backend/internal/worker/archive_importer_test.go`
- Modify: `go-backend/internal/worker/v2_executor.go`
- Modify: `go-backend/internal/worker/v2_executor_test.go`
- Modify: `go-backend/internal/worker/task_downloader.go`
- Modify: `go-backend/internal/worker/task_downloader_test.go`

**Step 1: Write the failing test**

在 `archive_importer_test.go` 新增 `TestArchiveImporterReadsComicInfoAndRepackagesCBZ`，断言：
- 输入 `cbz/zip`
- 读取已有 `ComicInfo.xml`
- 与 request metadata 合并
- 输出标准 `cbz`
- 回写阶段进度

示例：
```go
result, err := importer.Import(ctx, task)
if err != nil { t.Fatalf("import: %v", err) }
if result.OutputPath == "" || result.EffectiveMetadata.Writer != "首页作者" {
    t.Fatalf("unexpected import result: %#v", result)
}
```

在 `v2_executor_test.go` 新增 `TestV2ExecutorProcessesArchiveUploadTask`，断言 `source_type=archive_upload` 时不会走 URL 下载器，而会走 importer。

**Step 2: Run test to verify it fails**

Run:
```bash
cd go-backend
go test ./internal/worker -run 'TestArchiveImporterReadsComicInfoAndRepackagesCBZ|TestV2ExecutorProcessesArchiveUploadTask' -count=1
```
Expected:
- FAIL

**Step 3: Write minimal implementation**

在 `archive_importer.go` 实现骨架：
```go
type ArchiveImporter interface {
    Import(ctx context.Context, snapshot V2TaskSnapshot) (ArchiveImportResult, error)
}

type ArchiveImportResult struct {
    OutputPath         string
    ArchiveMetadataJSON []byte
    EffectiveMetadataJSON []byte
}
```

导入器职责：
- 校验 `source_archive_path`
- 防 ZIP Slip / 图片数量 / 展开体积超限
- 读取 `ComicInfo.xml`
- 解压图片
- 合并 metadata
- 调用 `PackageCBZ`

在 `v2_executor.go` 中按 `snapshot.SourceType` 分流：
```go
if snapshot.SourceType == "archive_upload" {
    result, err := e.archiveImporter.Import(downloadCtx, snapshot)
    artifactPath = result.OutputPath
} else {
    artifactPath, err = e.downloader.DownloadAndPackage(...)
}
```

**Step 4: Run test to verify it passes**

Run:
```bash
cd go-backend
go test ./internal/worker -run 'TestArchiveImporterReadsComicInfoAndRepackagesCBZ|TestV2ExecutorProcessesArchiveUploadTask' -count=1
```
Expected:
- PASS

**Step 5: Commit**

```bash
git add go-backend/internal/worker/archive_importer.go go-backend/internal/worker/archive_importer_test.go go-backend/internal/worker/v2_executor.go go-backend/internal/worker/v2_executor_test.go go-backend/internal/worker/task_downloader.go go-backend/internal/worker/task_downloader_test.go
git commit -m "feat(worker): process archive upload tasks asynchronously"
```

---

### Task 6: 增加统一重试接口并支持上传任务复用源文件重跑

**Files:**
- Create: `go-backend/internal/httpapi/retry_task_test.go`
- Modify: `go-backend/internal/httpapi/api.go`
- Modify: `go-backend/internal/httpapi/api_test.go`
- Modify: `go-backend/internal/httpapi/legacy_adapter.go`
- Modify: `go-backend/internal/httpapi/legacy_adapter_test.go`
- Modify: `go-backend/internal/store/postgres/v2_repo.go`
- Modify: `go-backend/internal/store/postgres/v2_repo_test.go`

**Step 1: Write the failing test**

新增：
- `TestRetryFailedURLTaskCreatesNewQueuedTask`
- `TestRetryFailedArchiveTaskReusesSourceArchiveAndMetadata`

示例：
```go
req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-archive-failed/retry", nil)
rec := httptest.NewRecorder()
router.ServeHTTP(rec, req)
if rec.Code != http.StatusAccepted {
    t.Fatalf("expected 202, got %d", rec.Code)
}
```

并断言新任务：
- `retry_parent_task_id` 指向旧任务
- `source_archive_path` 复用
- `request_metadata_json` 复用
- 状态回到 `PENDING`

**Step 2: Run test to verify it fails**

Run:
```bash
cd go-backend
go test ./internal/httpapi ./internal/store/postgres -run 'TestRetryFailedURLTaskCreatesNewQueuedTask|TestRetryFailedArchiveTaskReusesSourceArchiveAndMetadata' -count=1
```
Expected:
- FAIL

**Step 3: Write minimal implementation**

在 `api.go` 注册：
```go
router.Post("/api/tasks/{task_id}/retry", api.handleTaskRetry)
```

在 handler 中：
```go
original, _ := a.legacyAdapter.GetTaskForRetry(...)
createInput := cloneTaskForRetry(original)
createInput.ID = uuid.NewString()
createInput.EnqueueToken = uuid.NewString()
createInput.RetryParentTaskID = stringPtr(original.ID)
```

对失败状态做限制：
```go
if status != domain.StatusFailed && status != domain.StatusCanceled {
    writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "message": "Task is not retryable."})
    return
}
```

**Step 4: Run test to verify it passes**

Run:
```bash
cd go-backend
go test ./internal/httpapi ./internal/store/postgres -run 'TestRetryFailedURLTaskCreatesNewQueuedTask|TestRetryFailedArchiveTaskReusesSourceArchiveAndMetadata' -count=1
```
Expected:
- PASS

**Step 5: Commit**

```bash
git add go-backend/internal/httpapi/api.go go-backend/internal/httpapi/api_test.go go-backend/internal/httpapi/retry_task_test.go go-backend/internal/httpapi/legacy_adapter.go go-backend/internal/httpapi/legacy_adapter_test.go go-backend/internal/store/postgres/v2_repo.go go-backend/internal/store/postgres/v2_repo_test.go
git commit -m "feat(api): add task retry for url and archive uploads"
```

---

### Task 7: 修复 legacy 日志映射，透出 phase/progress/metadata/actions

**Files:**
- Modify: `go-backend/internal/httpapi/legacy_adapter.go`
- Modify: `go-backend/internal/httpapi/legacy_adapter_test.go`
- Modify: `go-backend/internal/httpapi/contract_test.go`
- Modify: `go-backend/internal/domain/types.go`
- Modify: `go-backend/internal/domain/status.go`

**Step 1: Write the failing test**

新增 `TestMapV2TaskToLegacyLogPreservesArchiveProgressAndMetadata`，断言：
- `source_type`
- `phase`
- `progress_*`
- `author/series_name/comic_name`
- `can_retry`

示例：
```go
log := mapV2TaskToLegacyLog(task)
if log.SourceType != "archive_upload" || stringValue(log.Phase) != "UPLOADING" {
    t.Fatalf("unexpected log projection: %#v", log)
}
if log.ProgressCurrent != 512 || log.ProgressTotal != 1024 {
    t.Fatalf("unexpected progress projection: %#v", log)
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
cd go-backend
go test ./internal/httpapi -run TestMapV2TaskToLegacyLogPreservesArchiveProgressAndMetadata -count=1
```
Expected:
- FAIL，当前映射会清空这些字段。

**Step 3: Write minimal implementation**

在 `domain.TaskLog` 增加：
```go
SourceType      string  `json:"source_type"`
Phase           *string `json:"phase"`
ProgressMode    *string `json:"progress_mode"`
ProgressCurrent int64   `json:"progress_current"`
ProgressTotal   int64   `json:"progress_total"`
ProgressLabel   *string `json:"progress_label"`
```

更新 `mapV2TaskToLegacyLog`：
```go
return domain.TaskLog{
    ID:              strings.TrimSpace(task.ID),
    URL:             strings.TrimSpace(task.URL),
    SourceType:      strings.TrimSpace(task.SourceType),
    Phase:           task.Phase,
    ProgressMode:    task.ProgressMode,
    ProgressCurrent: task.ProgressCurrent,
    ProgressTotal:   task.ProgressTotal,
    ProgressLabel:   task.ProgressLabel,
    Author:          task.Author,
    SeriesName:      task.SeriesName,
    ComicName:       task.ComicName,
}
```

并让 `StatusCatalog[FAILED]` 返回 `CanRetry: true`。

**Step 4: Run test to verify it passes**

Run:
```bash
cd go-backend
go test ./internal/httpapi -run TestMapV2TaskToLegacyLogPreservesArchiveProgressAndMetadata -count=1
```
Expected:
- PASS

**Step 5: Commit**

```bash
git add go-backend/internal/httpapi/legacy_adapter.go go-backend/internal/httpapi/legacy_adapter_test.go go-backend/internal/httpapi/contract_test.go go-backend/internal/domain/types.go go-backend/internal/domain/status.go
git commit -m "fix(logs): preserve v2 archive progress metadata and retry actions"
```

---

### Task 8: 首页改为双模式提交（URL / ZIP-CBZ 上传）

**Files:**
- Modify: `go-backend/internal/httpui/templates/index.html`
- Modify: `templates/index.html`
- Modify: `frontend/src/home/index.js`
- Modify: `frontend/src/shared/archive_upload.js`
- Create: `frontend/src/tests/home_upload.test.mjs`
- Modify: `frontend/src/tests/archive_upload.test.mjs`

**Step 1: Write the failing test**

在 `home_upload.test.mjs` 新增测试：
- `submitDownload` 在上传模式下不校验 URL
- 选择文件后会先请求 `/api/archive-tasks`，再请求 `/api/archive-tasks/{id}/content`
- 上传阶段显示“正在上传压缩包...”

示例：
```js
assert.equal(requests[0].url, '/api/archive-tasks');
assert.match(feedback.textContent, /正在上传/);
```

**Step 2: Run test to verify it fails**

Run:
```bash
npm run test:frontend -- --test-name-pattern="upload mode"
```
Expected:
- FAIL

**Step 3: Write minimal implementation**

模板增加模式切换与文件输入：
```html
<label><input type="radio" name="task_mode" value="url" checked> Telegraph 链接</label>
<label><input type="radio" name="task_mode" value="archive"> 上传 ZIP/CBZ</label>
<input type="file" id="archive-file" name="archive_file" accept=".zip,.cbz,application/zip" />
```

前端状态模块扩展：
```js
export function normalizeUploadSnapshot(snapshot) {
  return {
    status: normalizeText(snapshot.status) || 'idle',
    loadedBytes: normalizeCount(snapshot.loadedBytes),
    totalBytes: normalizeCount(snapshot.totalBytes),
    fileName: normalizeText(snapshot.fileName),
    errorMessage: normalizeText(snapshot.errorMessage),
    canCancel: Boolean(snapshot.canCancel),
    taskId: normalizeText(snapshot.taskId),
  };
}
```

首页提交分支：
```js
if (taskMode === 'archive') {
    await createArchiveTaskAndUpload(file, metadata);
    return;
}
return submitURLDownload(metadata);
```

**Step 4: Run test to verify it passes**

Run:
```bash
npm run test:frontend
```
Expected:
- PASS，至少新增的上传模式测试转绿。

**Step 5: Commit**

```bash
git add go-backend/internal/httpui/templates/index.html templates/index.html frontend/src/home/index.js frontend/src/shared/archive_upload.js frontend/src/tests/home_upload.test.mjs frontend/src/tests/archive_upload.test.mjs
git commit -m "feat(frontend): add dual-mode home submit for url and archive uploads"
```

---

### Task 9: 日志页增加上传进度渲染与失败重试按钮

**Files:**
- Modify: `frontend/src/logs/index.js`
- Create: `frontend/src/tests/logs_actions.test.mjs`
- Modify: `frontend/src/tests/page_modules.test.mjs`
- Modify: `tests/e2e/specs/logs-flow.spec.js`

**Step 1: Write the failing test**

在 `logs_actions.test.mjs` 新增：
- `FAILED + can_retry=true` 渲染“重试”按钮
- `archive_upload + progress_mode=bytes` 渲染字节进度文本
- `phase_only` 渲染 `progress_label`

示例：
```js
assert.match(row.textContent, /512 \/ 1024/);
assert.equal(retryButton.textContent, '重试');
```

**Step 2: Run test to verify it fails**

Run:
```bash
npm run test:frontend
```
Expected:
- FAIL

**Step 3: Write minimal implementation**

在 `logs/index.js` 的动作列逻辑中加入：
```js
const canRetry = Boolean(statusMeta && statusMeta.can_retry && log.id);
if (canRetry) {
  const retryButton = doc.createElement('button');
  retryButton.textContent = '重试';
  retryButton.onclick = () => retryTask(log.id, retryButton);
}
```

在进度渲染中按模式显示：
```js
if (log.progress_mode === 'bytes' && log.progress_total > 0) {
  return `${log.progress_current} / ${log.progress_total}`;
}
if (log.progress_mode === 'phase_only') {
  return log.progress_label || '处理中';
}
```

补充 `retryTask()`：
```js
await fetch(`/api/tasks/${encodeURIComponent(taskId)}/retry`, { method: 'POST' })
```

**Step 4: Run test to verify it passes**

Run:
```bash
npm run test:frontend
```
Expected:
- PASS

**Step 5: Commit**

```bash
git add frontend/src/logs/index.js frontend/src/tests/logs_actions.test.mjs frontend/src/tests/page_modules.test.mjs tests/e2e/specs/logs-flow.spec.js
git commit -m "feat(frontend): render archive progress and retry actions in logs"
```

---

### Task 10: 更新打包产物、补 E2E、清理残留代码并完成验证

**Files:**
- Modify: `tests/e2e/specs/backend-smoke.spec.js`
- Create: `tests/e2e/specs/archive-upload-flow.spec.js`
- Modify: `README.md`
- Modify: `docs/PROJECT_UPDATES.md`
- Modify: `static/dist/app.bundle.js`
- Modify: `static/dist/index.bundle.js`
- Modify: `static/dist/logs.bundle.js`
- Modify: `static/dist/settings.bundle.js`
- Optional cleanup/modify: `static/index.js`
- Optional cleanup/modify: `static/logs.js`

**Step 1: Write the failing test**

新增 Playwright 用例 `archive-upload-flow.spec.js`：
- 选择 zip 文件
- 首页提交
- 跳到日志页看到上传进度
- 任务成功后出现下载按钮
- 人为模拟失败任务时出现“重试”按钮

示例：
```js
await page.setInputFiles('#archive-file', 'tests/e2e/fixtures/demo-upload.zip');
await page.getByRole('button', { name: '开始上传' }).click();
await expect(page.locator('#log-body')).toContainText('上传中');
await expect(page.getByRole('button', { name: '下载' })).toBeVisible();
```

**Step 2: Run tests to verify they fail**

Run:
```bash
npm run e2e:test
```
Expected:
- FAIL（当前还没有上传端到端流程）

**Step 3: Build assets, finish docs, and clean residual code**

- 运行前端构建：
```bash
npm run build
```
- 如 `frontend/src/shared/archive_upload.js` 已成为主模块，保留并清理旧无用逻辑；
- 如 `static/index.js` / `static/logs.js` 已彻底失去价值，最小化或删除其无用上传残留，确保 lint 不受历史代码牵制；
- 更新 `README.md` 与 `docs/PROJECT_UPDATES.md`，写明：
  - 首页双模式
  - 上传任务接口
  - 日志中的重试/下载行为
  - 元数据优先级与回溯策略

**Step 4: Run full verification**

Run:
```bash
cd go-backend && go test ./... && go test -race ./...
cd /Users/ryancheng/project/telegram-downloader-src && npm run test:frontend
cd /Users/ryancheng/project/telegram-downloader-src && npm run lint
cd /Users/ryancheng/project/telegram-downloader-src && npm run build
cd /Users/ryancheng/project/telegram-downloader-src && npm run e2e:test
```
Expected:
- 全部 PASS
- 新增上传流程 E2E 通过
- 不再出现“必须填写 Telegraph URL 才能上传压缩包”的阻塞问题

**Step 5: Commit**

```bash
git add tests/e2e/specs/backend-smoke.spec.js tests/e2e/specs/archive-upload-flow.spec.js README.md docs/PROJECT_UPDATES.md static/dist/app.bundle.js static/dist/index.bundle.js static/dist/logs.bundle.js static/dist/settings.bundle.js static/index.js static/logs.js
git commit -m "feat: ship archive upload flow with unified logs retry and download"
```


# Upload CBZ、首页历史与日志动作切换 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为现有 URL 下载主线增加 ZIP/RAR/7Z 上传转 CBZ、首页最近 20 条元数据历史、日志页上传任务进度/重试/取消，以及基于设置切换的 Komga 复制动作，同时不回归现有 URL 下载与日志流程。

**Architecture:** 后端继续以 v2 任务存储为真实任务来源，新增上传任务字段、元数据历史表和 `download_action_mode` 设置；首页上传走“两阶段握手”（`upload/init` 创建正式任务，`upload-source` 传输文件），这样日志页在上传过程中就能轮询真实任务。worker 侧把当前“只会从 URL 产生产物”的执行链路泛化成“根据任务类型生成 CBZ”，成功任务的日志按钮再按设置选择浏览器下载或复制到 Komga。

**Tech Stack:** Go 1.23、Chi、pgx/PostgreSQL、Redis Streams、HTML templates、Vite/ESM、Node test runner、Playwright、本地文件系统复制/清理。

**Skills:** 执行时使用 @superpowers:using-git-worktrees、@superpowers:test-driven-development、@superpowers:systematic-debugging、@superpowers:verification-before-completion。

**Spec:** `docs/superpowers/specs/2026-03-18-upload-cbz-home-logs-design.md`

---

## File Structure Lock-In

### Persistence / shared contracts

**Create:**
- `internal/store/postgres/migrations/008_download_action_mode.sql`
- `internal/store/postgres/migrations/009_upload_tasks_and_metadata_history.sql`
- `internal/httpv2/upload_task_store.go`
- `internal/httpv2/upload_task_store_test.go`
- `internal/httpv2/metadata_history_store.go`
- `internal/httpv2/metadata_history_store_test.go`

**Modify:**
- `internal/config/config.go`
- `internal/httpv2/settings_handler.go`
- `internal/httpv2/settings_handler_test.go`
- `internal/httpui/handler.go`
- `internal/httpui/handler_test.go`
- `web/templates/settings.html`
- `internal/domain/types.go`
- `internal/domain/status.go`
- `internal/httpv2/tasks_handler.go`
- `internal/store/postgres/v2_repo.go`
- `internal/httpapi/legacy_adapter.go`

### Upload / artifact runtime

**Create:**
- `internal/archive/extractor.go`
- `internal/archive/extractor_test.go`
- `internal/archive/external_extractor.go`
- `internal/app/tasks/upload_source.go`
- `internal/app/tasks/upload_source_test.go`
- `internal/app/tasks/komga_copy.go`
- `internal/app/tasks/komga_copy_test.go`

**Modify:**
- `internal/app/tasks/metadata.go`
- `internal/app/tasks/metadata_test.go`
- `internal/app/tasks/run_task.go`
- `internal/app/tasks/run_task_test.go`
- `internal/worker/v2_executor.go`
- `internal/worker/v2_executor_test.go`
- `internal/worker/output_filename.go`
- `internal/worker/output_filename_test.go`

### Legacy-facing API / UI handlers

**Create:**
- `internal/httpapi/upload_handlers.go`
- `internal/httpapi/upload_handlers_test.go`
- `internal/httpapi/history_handlers.go`
- `internal/httpapi/history_handlers_test.go`
- `internal/httpapi/task_actions_test.go`

**Modify:**
- `internal/httpapi/api.go`
- `internal/httpapi/api_test.go`
- `internal/httpapi/download_request.go`
- `internal/httpapi/download_request_test.go`
- `internal/httpapi/legacy_adapter.go`
- `internal/httpapi/legacy_adapter_test.go`

### Frontend source / tests

**Create:**
- `frontend/src/home/input_mode.js`
- `frontend/src/home/metadata_history.js`
- `frontend/src/home/upload_submission.js`
- `frontend/src/home/field_hints.js`
- `frontend/src/logs/task_actions.js`
- `frontend/src/tests/home_input_mode.test.mjs`
- `frontend/src/tests/home_metadata_history.test.mjs`
- `frontend/src/tests/home_upload_submission.test.mjs`
- `frontend/src/tests/home_field_hints.test.mjs`
- `frontend/src/tests/logs_task_actions.test.mjs`
- `tests/e2e/specs/upload-flow.spec.js`

**Modify:**
- `frontend/src/home/index.js`
- `frontend/src/home/form_submission.js`
- `frontend/src/logs/index.js`
- `frontend/src/logs/table_render.js`
- `frontend/src/settings.js`
- `frontend/src/shared/api/tasks_api.js`
- `frontend/src/shared/archive_upload.js`
- `frontend/src/tests/archive_upload.test.mjs`
- `frontend/src/tests/home_form_submission.test.mjs`
- `frontend/src/tests/logs_table_render.test.mjs`
- `tests/e2e/specs/logs-flow.spec.js`
- `tests/e2e/specs/settings.spec.js`
- `web/templates/index.html`
- `web/templates/logs.html`
- `web/templates/settings.html`
- `web/static/style.css`
- `README.md`

---

### Task 1: Persist `download_action_mode` through config, settings API, and settings UI

**Files:**
- Create: `internal/store/postgres/migrations/008_download_action_mode.sql`
- Modify: `internal/config/config.go`
- Modify: `internal/httpv2/settings_handler.go`
- Modify: `internal/httpv2/settings_handler_test.go`
- Modify: `internal/httpui/handler.go`
- Modify: `internal/httpui/handler_test.go`
- Modify: `web/templates/settings.html`

- [ ] **Step 1: Write the failing tests**

```go
func TestUpdateSettingsPersistsDownloadActionMode(t *testing.T) {
    store := &fakeSettingsStore{}
    req := httptest.NewRequest(
        http.MethodPut,
        "/v2/settings",
        bytes.NewBufferString(`{"timeout":45,"retries":6,"image_concurrency":7,"download_action_mode":"komga_copy"}`),
    )
    req.Header.Set("Content-Type", "application/json")

    recorder := httptest.NewRecorder()
    chi.NewRouter().Put("/v2/settings", NewSettingsHandler(store).UpdateSettings).ServeHTTP(recorder, req)

    if recorder.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", recorder.Code)
    }
    if got := store.updateCalls[0].DownloadActionMode; got != "komga_copy" {
        t.Fatalf("expected komga_copy, got %q", got)
    }
}

func TestSettingsPageRendersAndPostsDownloadActionMode(t *testing.T) {
    // GET must render radio/select state; POST must parse download_action_mode and persist it.
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/httpv2 ./internal/httpui -run 'Test(UpdateSettingsPersistsDownloadActionMode|SettingsPageRendersAndPostsDownloadActionMode)' -count=1`
Expected: FAIL because `SettingsSnapshot` has no `download_action_mode`, `/v2/settings` rejects the extra field, and the settings form has no mode control.

- [ ] **Step 3: Write minimal implementation**

Add the new settings field and normalizer:

```go
type SettingsSnapshot struct {
    Timeout            int    `json:"timeout"`
    Retries            int    `json:"retries"`
    ImageConcurrency   int    `json:"image_concurrency"`
    DownloadActionMode string `json:"download_action_mode"`
}

func normalizeDownloadActionMode(raw string) string {
    switch strings.TrimSpace(raw) {
    case "komga_copy":
        return "komga_copy"
    default:
        return "browser"
    }
}
```

Update `app_settings` SQL read/write paths, then teach `internal/httpui/handler.go` and `web/templates/settings.html` to render and post the mode alongside timeout/retries/image concurrency.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/httpv2 ./internal/httpui -run 'Test(UpdateSettingsPersistsDownloadActionMode|SettingsPageRendersAndPostsDownloadActionMode)' -count=1`
Expected: PASS

- [ ] **Step 5: Run the broader settings regression**

Run: `go test ./internal/httpv2 ./internal/httpui -run 'Test(UpdateSettings|GetSettings|SettingsPage)' -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store/postgres/migrations/008_download_action_mode.sql internal/config/config.go internal/httpv2/settings_handler.go internal/httpv2/settings_handler_test.go internal/httpui/handler.go internal/httpui/handler_test.go web/templates/settings.html
git commit -m "feat(settings): persist download action mode"
```

---

### Task 2: Extend v2 task/history storage and legacy log mappings for upload tasks

**Files:**
- Create: `internal/store/postgres/migrations/009_upload_tasks_and_metadata_history.sql`
- Create: `internal/httpv2/upload_task_store.go`
- Create: `internal/httpv2/upload_task_store_test.go`
- Create: `internal/httpv2/metadata_history_store.go`
- Create: `internal/httpv2/metadata_history_store_test.go`
- Modify: `internal/domain/types.go`
- Modify: `internal/domain/status.go`
- Modify: `internal/httpv2/tasks_handler.go`
- Modify: `internal/store/postgres/v2_repo.go`
- Modify: `internal/httpapi/legacy_adapter.go`
- Modify: `internal/httpapi/legacy_adapter_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestPostgresTaskStoreListTasksReturnsUploadFields(t *testing.T) {
    // Insert a v2 row with task_type/source_archive_name/upload_loaded_bytes/retryable
    // and assert ListTasks returns them.
}

func TestPostgresTaskStoreMetadataHistoryReturnsLatestTwenty(t *testing.T) {
    // Insert 25 history rows and assert only the latest 20 come back in descending order.
}

func TestLegacyAdapterReadLogsIncludesUploadTaskMetadata(t *testing.T) {
    log := mapV2TaskToLegacyLog(httpv2.Task{
        ID: "task-upload-1",
        URL: "",
        Status: "UPLOADING",
        TaskType: "upload",
        SourceArchiveName: stringPtr("demo.7z"),
        UploadLoadedBytes: 12,
        UploadTotalBytes: 40,
        Retryable: true,
        Author: stringPtr("作者A"),
    })
    if log.TaskType == nil || *log.TaskType != "upload" {
        t.Fatalf("expected upload task type, got %#v", log.TaskType)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/httpv2 ./internal/httpapi -run 'Test(PostgresTaskStoreListTasksReturnsUploadFields|PostgresTaskStoreMetadataHistoryReturnsLatestTwenty|LegacyAdapterReadLogsIncludesUploadTaskMetadata)' -count=1`
Expected: FAIL because `v2_tasks` has no upload columns, there is no metadata history store, and `mapV2TaskToLegacyLog` currently drops all v2 metadata.

- [ ] **Step 3: Write minimal implementation**

Add the new task fields to the shared structs:

```go
type TaskLog struct {
    ID                string  `json:"id"`
    URL               string  `json:"url"`
    Status            string  `json:"status"`
    TaskType          *string `json:"task_type,omitempty"`
    SourceArchiveName *string `json:"source_archive_name,omitempty"`
    UploadLoadedBytes int64   `json:"upload_loaded_bytes"`
    UploadTotalBytes  int64   `json:"upload_total_bytes"`
    Retryable         bool    `json:"retryable"`
}
```

Then:
- add `UPLOADING` to `internal/domain/status.go`
- extend `httpv2.Task` / `CreateTaskInput` / SQL SELECTs and INSERTs
- add `PostgresTaskStore` helpers for upload progress/history in dedicated files
- update `internal/httpapi/legacy_adapter.go` so logs preserve task type, archive name, upload bytes, retryability, and metadata instead of zeroing them out

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/httpv2 ./internal/httpapi -run 'Test(PostgresTaskStoreListTasksReturnsUploadFields|PostgresTaskStoreMetadataHistoryReturnsLatestTwenty|LegacyAdapterReadLogsIncludesUploadTaskMetadata)' -count=1`
Expected: PASS

- [ ] **Step 5: Run the broader contract regression**

Run: `go test ./internal/httpv2 ./internal/httpapi -run 'Test(ListTasks|ReadLogs|GetDashboardSummary)' -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store/postgres/migrations/009_upload_tasks_and_metadata_history.sql internal/httpv2/upload_task_store.go internal/httpv2/upload_task_store_test.go internal/httpv2/metadata_history_store.go internal/httpv2/metadata_history_store_test.go internal/domain/types.go internal/domain/status.go internal/httpv2/tasks_handler.go internal/store/postgres/v2_repo.go internal/httpapi/legacy_adapter.go internal/httpapi/legacy_adapter_test.go
git commit -m "feat(tasks): add upload task fields and metadata history storage"
```

---

### Task 3: Add upload init/upload-source/history API flow and unify metadata normalization

**Files:**
- Create: `internal/app/tasks/upload_source.go`
- Create: `internal/app/tasks/upload_source_test.go`
- Create: `internal/httpapi/upload_handlers.go`
- Create: `internal/httpapi/upload_handlers_test.go`
- Create: `internal/httpapi/history_handlers.go`
- Create: `internal/httpapi/history_handlers_test.go`
- Modify: `internal/httpapi/api.go`
- Modify: `internal/httpapi/api_test.go`
- Modify: `internal/httpapi/download_request.go`
- Modify: `internal/httpapi/download_request_test.go`
- Modify: `internal/app/tasks/metadata.go`
- Modify: `internal/app/tasks/metadata_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestNormalizeMetadataSupportsHashAndWhitespaceSeparators(t *testing.T) {
    got := NormalizeMetadata(MetadataInput{
        Author:   strPtr(" 作者A # 作者B ＃ 作者A "),
        TagsRaw:  strPtr("剧情 # 动作， 热血"),
        GenresRaw:strPtr("青年 ＃ 悬疑"),
    })
    if got.Author == nil || *got.Author != "作者A,作者B" {
        t.Fatalf("unexpected author normalization: %#v", got.Author)
    }
}

func TestHandleUploadInitCreatesUploadingTaskAndHistoryEntry(t *testing.T) {
    // POST /api/tasks/upload/init should create task_type=upload, status=UPLOADING and record metadata history.
}

func TestHandleUploadSourceStreamsBytesAndTransitionsToQueued(t *testing.T) {
    // PUT /api/tasks/{id}/upload-source should update upload_loaded_bytes and persist temp_uploads/<task-id>/source.ext.
}

func TestHandleMetadataHistoryReturnsLatestTwenty(t *testing.T) {
    // GET /api/metadata-history?limit=20 should return upload/url entries in descending order.
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/app/tasks ./internal/httpapi -run 'Test(NormalizeMetadataSupportsHashAndWhitespaceSeparators|HandleUploadInitCreatesUploadingTaskAndHistoryEntry|HandleUploadSourceStreamsBytesAndTransitionsToQueued|HandleMetadataHistoryReturnsLatestTwenty)' -count=1`
Expected: FAIL because author normalization ignores `#`/`＃`, the upload routes do not exist, and metadata history is not exposed.

- [ ] **Step 3: Write minimal implementation**

Normalize list fields once and reuse them from HTTP parsing:

```go
func normalizeDelimitedList(raw string, isDelimiter func(rune) bool) string {
    items := strings.FieldsFunc(raw, isDelimiter)
    seen := map[string]struct{}{}
    out := make([]string, 0, len(items))
    for _, item := range items {
        trimmed := strings.TrimSpace(item)
        if trimmed == "" {
            continue
        }
        if _, ok := seen[trimmed]; ok {
            continue
        }
        seen[trimmed] = struct{}{}
        out = append(out, trimmed)
    }
    return strings.Join(out, ",")
}
```

Implement:
- `POST /api/tasks/upload/init`
- `PUT /api/tasks/{task_id}/upload-source`
- `GET /api/metadata-history`

`upload_source.go` should:
- validate extension (`.zip`, `.rar`, `.7z`)
- write into `temp_uploads/<task-id>/source.<ext>`
- update `upload_loaded_bytes` / `upload_total_bytes` with throttled store writes
- move task from `UPLOADING` to `QUEUED` only after a successful write

Also update the existing `/download` path to record metadata history after a URL task has been accepted into the queue/claim flow.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/app/tasks ./internal/httpapi -run 'Test(NormalizeMetadataSupportsHashAndWhitespaceSeparators|HandleUploadInitCreatesUploadingTaskAndHistoryEntry|HandleUploadSourceStreamsBytesAndTransitionsToQueued|HandleMetadataHistoryReturnsLatestTwenty)' -count=1`
Expected: PASS

- [ ] **Step 5: Run the broader API regression**

Run: `go test ./internal/httpapi -run 'TestHandle(Download|Upload|MetadataHistory)' -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/app/tasks/upload_source.go internal/app/tasks/upload_source_test.go internal/httpapi/upload_handlers.go internal/httpapi/upload_handlers_test.go internal/httpapi/history_handlers.go internal/httpapi/history_handlers_test.go internal/httpapi/api.go internal/httpapi/api_test.go internal/httpapi/download_request.go internal/httpapi/download_request_test.go internal/app/tasks/metadata.go internal/app/tasks/metadata_test.go
git commit -m "feat(api): add upload handshake and metadata history endpoints"
```

---

### Task 4: Generalize the worker/runtime pipeline to build CBZ from URL tasks or uploaded archives

**Files:**
- Create: `internal/archive/extractor.go`
- Create: `internal/archive/extractor_test.go`
- Create: `internal/archive/external_extractor.go`
- Modify: `internal/app/tasks/run_task.go`
- Modify: `internal/app/tasks/run_task_test.go`
- Modify: `internal/worker/v2_executor.go`
- Modify: `internal/worker/v2_executor_test.go`
- Modify: `internal/worker/output_filename.go`
- Modify: `internal/worker/output_filename_test.go`
- Modify: `internal/httpv2/tasks_handler.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestRunTaskTransitionsUploadTaskToSuccess(t *testing.T) {
    repo := &fakeRunTaskRepo{snapshot: RunTaskSnapshot{
        ID:               "task-upload",
        Status:           StatusQueued,
        EnqueueToken:     "token-upload",
        TaskType:         stringPtr("upload"),
        SourceArchivePath:stringPtr("temp_uploads/task-upload/source.zip"),
        SourceArchiveName:stringPtr("source.zip"),
    }}
    builder := &fakeArtifactBuilder{artifactPath: "/tmp/task-upload.cbz"}
    runner := NewRunTaskUseCase(RunTaskConfig{Repo: repo, Worker: "worker-a", ArtifactBuilder: builder})
    if err := runner.Execute(context.Background(), "task-upload", "token-upload"); err != nil {
        t.Fatalf("execute: %v", err)
    }
}

func TestExtractorSupportsZipRarAnd7z(t *testing.T) {
    // table-driven test over .zip/.rar/.7z fixtures, expecting ordered image entries and no path traversal.
}

func TestBuildDownloadFilenameOmitsEmptySeries(t *testing.T) {
    // existing filename test remains green with the new naming contract.
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/app/tasks ./internal/archive ./internal/worker -run 'Test(RunTaskTransitionsUploadTaskToSuccess|ExtractorSupportsZipRarAnd7z|BuildDownloadFilenameOmitsEmptySeries)' -count=1`
Expected: FAIL because `RunTaskSnapshot` has no upload source fields in the execution path and there is no archive extractor.

- [ ] **Step 3: Write minimal implementation**

Replace the URL-only builder contract with a task-aware artifact builder:

```go
type ArtifactBuildInput struct {
    TaskID            string
    TaskType          string
    PageURL           string
    SourceArchivePath string
    SourceArchiveName string
    Metadata          downloader.TaskMetadata
}

type ArtifactBuilder interface {
    Build(ctx context.Context, input ArtifactBuildInput) (string, error)
}
```

Implement builder branches:
- `url`: current `Service.Download` + `PackageCBZ`
- `upload`: extract images from the source archive, package them into a new `.cbz`

Update terminal transitions so upload tasks:
- clear `source_archive_path` on success/cancel
- keep `source_archive_path` and mark `retryable=true` on failure

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/app/tasks ./internal/archive ./internal/worker -run 'Test(RunTaskTransitionsUploadTaskToSuccess|ExtractorSupportsZipRarAnd7z|BuildDownloadFilenameOmitsEmptySeries)' -count=1`
Expected: PASS

- [ ] **Step 5: Run the broader worker regression**

Run: `go test ./internal/app/tasks ./internal/worker -run 'Test(RunTask|Executor)' -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/archive/extractor.go internal/archive/extractor_test.go internal/archive/external_extractor.go internal/app/tasks/run_task.go internal/app/tasks/run_task_test.go internal/worker/v2_executor.go internal/worker/v2_executor_test.go internal/worker/output_filename.go internal/worker/output_filename_test.go internal/httpv2/tasks_handler.go
git commit -m "feat(worker): build cbz from uploaded archives"
```

---

### Task 5: Add backend task actions for failed-upload retry and Komga copy

**Files:**
- Create: `internal/app/tasks/komga_copy.go`
- Create: `internal/app/tasks/komga_copy_test.go`
- Create: `internal/httpapi/task_actions_test.go`
- Modify: `internal/httpapi/api.go`
- Modify: `internal/httpapi/api_test.go`
- Modify: `internal/httpapi/legacy_adapter.go`
- Modify: `internal/httpapi/legacy_adapter_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestRetryUploadTaskRequeuesFailedTaskWithoutCreatingNewID(t *testing.T) {
    // POST /api/tasks/{id}/retry should keep the same task ID and move FAILED(upload) back to QUEUED.
}

func TestCopyToKomgaUsesSeriesFolderOrTanbokon(t *testing.T) {
    copier := NewKomgaCopier(KomgaCopyConfig{
        Root: "/Users/ryancheng/docker_data/komga/data/myReadingManga",
    })
    got, err := copier.TargetPath("demo.cbz", "系列A")
    if err != nil {
        t.Fatalf("target path: %v", err)
    }
    if !strings.Contains(got, "/myReadingManga/系列A/demo.cbz") {
        t.Fatalf("unexpected target path %q", got)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/app/tasks ./internal/httpapi -run 'Test(RetryUploadTaskRequeuesFailedTaskWithoutCreatingNewID|CopyToKomgaUsesSeriesFolderOrTanbokon)' -count=1`
Expected: FAIL because there is no retry route or Komga copy helper.

- [ ] **Step 3: Write minimal implementation**

Implement:
- `POST /api/tasks/{task_id}/retry`
  - only for `task_type=upload`, `status=FAILED`, `retryable=true`
  - reset `error`, clear terminal-only fields, move to `QUEUED`, and enqueue the same `task_id`
- `POST /api/tasks/{task_id}/copy-to-komga`
  - open the success artifact
  - resolve destination as:

```go
func targetSubdir(seriesName string) string {
    if strings.TrimSpace(seriesName) != "" {
        return sanitizeOptionalFilenamePart(seriesName)
    }
    return "tanbokon"
}
```

  - create missing directories
  - overwrite existing target file atomically

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/app/tasks ./internal/httpapi -run 'Test(RetryUploadTaskRequeuesFailedTaskWithoutCreatingNewID|CopyToKomgaUsesSeriesFolderOrTanbokon)' -count=1`
Expected: PASS

- [ ] **Step 5: Run the broader action regression**

Run: `go test ./internal/httpapi -run 'TestHandle(TaskCancel|TaskDownload|TaskRetry|CopyToKomga)' -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/app/tasks/komga_copy.go internal/app/tasks/komga_copy_test.go internal/httpapi/task_actions_test.go internal/httpapi/api.go internal/httpapi/api_test.go internal/httpapi/legacy_adapter.go internal/httpapi/legacy_adapter_test.go
git commit -m "feat(api): add upload retry and komga copy actions"
```

---

### Task 6: Rework the homepage for mode switching, upload submission, history recall, and hidden field hints

**Files:**
- Create: `frontend/src/home/input_mode.js`
- Create: `frontend/src/home/metadata_history.js`
- Create: `frontend/src/home/upload_submission.js`
- Create: `frontend/src/home/field_hints.js`
- Modify: `frontend/src/home/index.js`
- Modify: `frontend/src/home/form_submission.js`
- Modify: `frontend/src/shared/api/tasks_api.js`
- Modify: `frontend/src/shared/archive_upload.js`
- Modify: `frontend/src/tests/archive_upload.test.mjs`
- Modify: `frontend/src/tests/home_form_submission.test.mjs`
- Create: `frontend/src/tests/home_input_mode.test.mjs`
- Create: `frontend/src/tests/home_metadata_history.test.mjs`
- Create: `frontend/src/tests/home_upload_submission.test.mjs`
- Create: `frontend/src/tests/home_field_hints.test.mjs`
- Modify: `web/templates/index.html`
- Modify: `web/static/style.css`

- [ ] **Step 1: Write the failing frontend tests**

```js
test('normalizeUploadSnapshot preserves loaded/total bytes for upload tasks', () => {
  assert.equal(normalizeUploadSnapshot({ loadedBytes: 12, totalBytes: 40 }).loadedBytes, 12);
});

test('clicking a URL history item restores mode, metadata, and url', () => {
  // metadata_history.js should rehydrate url tasks fully.
});

test('upload submission initializes task then streams file with xhr progress', async () => {
  // upload_submission.js should call /api/tasks/upload/init before PUT /upload-source.
});

test('field hints toggle hidden help text without changing input values', () => {
  // field_hints.js should collapse/expand the author/tags/genres help panels.
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `node --test frontend/src/tests/archive_upload.test.mjs frontend/src/tests/home_form_submission.test.mjs frontend/src/tests/home_input_mode.test.mjs frontend/src/tests/home_metadata_history.test.mjs frontend/src/tests/home_upload_submission.test.mjs frontend/src/tests/home_field_hints.test.mjs`
Expected: FAIL because the current homepage only knows URL form submission, has no history module, and the field hints are static paragraphs.

- [ ] **Step 3: Write minimal implementation**

Add a small orchestrator in `home/index.js` that delegates to focused modules:

```js
const mode = getSelectedMode(doc); // "url" | "upload"
if (mode === 'upload') {
  await submitArchive({
    api,
    file: archiveInput.files[0],
    metadata: collectMetadata(form),
    onProgress: (snapshot) => renderUploadFeedback(snapshot),
  });
} else {
  await submitUrlMode({
    api,
    form,
    onSuccess: (payload, submittedPayload) => handleDownloadSuccess(payload, submittedPayload),
  });
}
```

Requirements for the implementation:
- single radio group controls `url` vs `upload`
- upload mode disables/hides the URL input and enables the file picker
- history list loads `/api/metadata-history?limit=20` on mount
- upload source uses `XMLHttpRequest` (or an equivalent API that exposes upload progress) for the second stage so progress events fire before completion
- author/tags/genres are normalized before either URL or upload submission

- [ ] **Step 4: Run tests to verify they pass**

Run: `node --test frontend/src/tests/archive_upload.test.mjs frontend/src/tests/home_form_submission.test.mjs frontend/src/tests/home_input_mode.test.mjs frontend/src/tests/home_metadata_history.test.mjs frontend/src/tests/home_upload_submission.test.mjs frontend/src/tests/home_field_hints.test.mjs`
Expected: PASS

- [ ] **Step 5: Build the frontend bundle for the homepage changes**

Run: `npm run build`
Expected: PASS and updated `web/static/dist/index.bundle.js` (plus any hashed asset changes under `web/static/dist/assets/`).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/home/input_mode.js frontend/src/home/metadata_history.js frontend/src/home/upload_submission.js frontend/src/home/field_hints.js frontend/src/home/index.js frontend/src/home/form_submission.js frontend/src/shared/api/tasks_api.js frontend/src/shared/archive_upload.js frontend/src/tests/archive_upload.test.mjs frontend/src/tests/home_form_submission.test.mjs frontend/src/tests/home_input_mode.test.mjs frontend/src/tests/home_metadata_history.test.mjs frontend/src/tests/home_upload_submission.test.mjs frontend/src/tests/home_field_hints.test.mjs web/templates/index.html web/static/style.css web/static/dist
git commit -m "feat(home): add upload mode and metadata history"
```

---

### Task 7: Rework logs/settings frontend for task type, upload progress, retry/copy actions, then finish with E2E + docs verification

**Files:**
- Create: `frontend/src/logs/task_actions.js`
- Create: `frontend/src/tests/logs_task_actions.test.mjs`
- Modify: `frontend/src/logs/index.js`
- Modify: `frontend/src/logs/table_render.js`
- Modify: `frontend/src/settings.js`
- Modify: `frontend/src/tests/logs_table_render.test.mjs`
- Modify: `tests/e2e/specs/logs-flow.spec.js`
- Modify: `tests/e2e/specs/settings.spec.js`
- Create: `tests/e2e/specs/upload-flow.spec.js`
- Modify: `web/templates/logs.html`
- Modify: `web/templates/settings.html`
- Modify: `web/static/style.css`
- Modify: `README.md`

- [ ] **Step 1: Write the failing frontend and E2E tests**

```js
test('logs table renders upload task type and byte progress', () => {
  const row = renderLogRow({
    id: 'upload-1',
    task_type: 'upload',
    status: 'UPLOADING',
    upload_loaded_bytes: 12,
    upload_total_bytes: 40,
  });
  assert.match(row.textContent, /上传/);
  assert.match(row.textContent, /12/);
});

test('logs task actions choose copy endpoint when settings mode is komga_copy', async () => {
  // task_actions.js should call /api/tasks/:id/copy-to-komga instead of /download.
});
```

In Playwright:
- extend `logs-flow.spec.js` for upload rows and retry
- extend `settings.spec.js` for the mode toggle
- add `upload-flow.spec.js` covering upload init, progress, cancel, retry, and history recall

- [ ] **Step 2: Run tests to verify they fail**

Run: `node --test frontend/src/tests/logs_table_render.test.mjs frontend/src/tests/logs_task_actions.test.mjs`
Expected: FAIL because logs rows have no task type/byte progress and settings mode is not wired into logs actions.

- [ ] **Step 3: Write minimal implementation**

Move logs-side imperative logic into `logs/task_actions.js`:

```js
export async function runSuccessAction({ api, taskId, mode }) {
  if (mode === 'komga_copy') {
    return api.postJson(`/api/tasks/${encodeURIComponent(taskId)}/copy-to-komga`, {});
  }
  return precheckAndDownload(api, taskId);
}
```

Update the logs renderer so:
- it displays `URL` / `上传`
- upload rows format `upload_loaded_bytes` / `upload_total_bytes`
- failed upload rows show a `重试` button when `retryable === true`

Update `frontend/src/settings.js` to hydrate and persist `download_action_mode` through `/v2/settings`, then cache the current mode for logs/home modules to reuse without reloading the page.

- [ ] **Step 4: Run the frontend unit tests and rebuild**

Run: `node --test frontend/src/tests/logs_table_render.test.mjs frontend/src/tests/logs_task_actions.test.mjs && npm run build`
Expected: PASS

- [ ] **Step 5: Run end-to-end and full-stack verification**

Run:

```bash
go test ./...
npm run test:frontend
npm run build
npx playwright test tests/e2e/specs/logs-flow.spec.js tests/e2e/specs/settings.spec.js tests/e2e/specs/upload-flow.spec.js
```

Expected:
- all Go tests PASS
- all frontend node tests PASS
- Vite build PASS
- Playwright specs PASS, including upload init/progress/cancel/retry/history and Komga mode switching

- [ ] **Step 6: Commit**

```bash
git add frontend/src/logs/task_actions.js frontend/src/tests/logs_task_actions.test.mjs frontend/src/logs/index.js frontend/src/logs/table_render.js frontend/src/settings.js frontend/src/tests/logs_table_render.test.mjs tests/e2e/specs/logs-flow.spec.js tests/e2e/specs/settings.spec.js tests/e2e/specs/upload-flow.spec.js web/templates/logs.html web/templates/settings.html web/static/style.css README.md web/static/dist
git commit -m "feat(ui): add upload-aware logs and komga action mode"
```

---

## Final verification checklist

- [ ] `go test ./...`
- [ ] `npm run test:frontend`
- [ ] `npm run build`
- [ ] `npx playwright test tests/e2e/specs/logs-flow.spec.js tests/e2e/specs/settings.spec.js tests/e2e/specs/upload-flow.spec.js`
- [ ] Manually verify `/Users/ryancheng/docker_data/komga/data/myReadingManga` remains the active fixed root and that `系列名/` vs `tanbokon/` routing matches the stored metadata
- [ ] Confirm successful upload tasks delete `source_archive_path`, while failed upload tasks keep it until cleanup/retry

## Handoff notes

- Keep generated bundle outputs (`web/static/dist/`) in each frontend-affecting commit so templates and runtime stay in sync.
- Do not collapse upload init + upload-source into one request during implementation; the plan intentionally uses the two-step handshake so logs can observe the task before bytes finish uploading.
- If RAR/7Z support requires an external binary or third-party extractor with platform caveats, keep that dependency isolated inside `internal/archive/external_extractor.go` and cover it with feature-detection tests before wiring it into the runtime path.

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/queue"
)

type fakeTaskReader struct {
	statusCounts map[string]int
	logResult    domain.LogListResult
	hasActive    bool
	lastQuery    domain.LogQuery

	claimCalls      []domain.ClaimDownloadTaskInput
	claimResponses  []domain.ClaimDownloadTaskResult
	clearResultByID []string
	markFailedCalls []markFailedCall
	tasks           map[string]domain.TaskLog
	cancelCalls     []string
}

type markFailedCall struct {
	taskID  string
	message string
}

func (f *fakeTaskReader) ClaimDownloadTask(
	_ context.Context,
	input domain.ClaimDownloadTaskInput,
) (domain.ClaimDownloadTaskResult, error) {
	f.claimCalls = append(f.claimCalls, input)
	if len(f.claimResponses) == 0 {
		return domain.ClaimDownloadTaskResult{}, nil
	}
	next := f.claimResponses[0]
	f.claimResponses = f.claimResponses[1:]
	return next, nil
}

func (f *fakeTaskReader) ClearResultZipPath(_ context.Context, taskID string) error {
	f.clearResultByID = append(f.clearResultByID, taskID)
	return nil
}

func (f *fakeTaskReader) MarkTaskFailed(_ context.Context, taskID, message string) error {
	f.markFailedCalls = append(f.markFailedCalls, markFailedCall{taskID: taskID, message: message})
	return nil
}

func (f *fakeTaskReader) GetTask(_ context.Context, taskID string) (*domain.TaskLog, error) {
	if f.tasks == nil {
		return nil, nil
	}
	task, ok := f.tasks[taskID]
	if !ok {
		return nil, nil
	}
	copied := task
	return &copied, nil
}

func (f *fakeTaskReader) RequestTaskCancel(_ context.Context, taskID string, cancelError string) error {
	f.cancelCalls = append(f.cancelCalls, taskID)
	if f.tasks == nil {
		return nil
	}
	task, ok := f.tasks[taskID]
	if !ok {
		return nil
	}
	task.Status = domain.StatusCancelRequested
	task.ResultZipPath = nil
	task.Error = stringPtr(cancelError)
	f.tasks[taskID] = task
	return nil
}

type fakeDownloadSubmitter struct {
	calls []DownloadSubmitRequest
	err   error
}

func (f *fakeDownloadSubmitter) SubmitDownload(_ context.Context, request DownloadSubmitRequest) error {
	f.calls = append(f.calls, request)
	return f.err
}

type fakeDownloadQueue struct {
	calls []queue.EnqueueMessage
	err   error
}

func (f *fakeDownloadQueue) EnqueueDownload(_ context.Context, msg queue.EnqueueMessage) error {
	f.calls = append(f.calls, msg)
	return f.err
}

func (f *fakeTaskReader) GetStatusCounts(_ context.Context) (map[string]int, error) {
	return f.statusCounts, nil
}

func (f *fakeTaskReader) ListLogs(_ context.Context, query domain.LogQuery) (domain.LogListResult, error) {
	f.lastQuery = query
	return f.logResult, nil
}

func (f *fakeTaskReader) HasActiveTasks(_ context.Context, _ []string) (bool, error) {
	return f.hasActive, nil
}

func TestGetSummaryBuildsDerivedFields(t *testing.T) {
	repo := &fakeTaskReader{
		statusCounts: map[string]int{
			"PENDING":          2,
			"IN_PROGRESS":      3,
			"CANCEL_REQUESTED": 1,
			"SUCCESS":          9,
			"FAILED":           1,
			"CANCELED":         2,
		},
	}

	handler := NewRouter(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	var payload domain.Summary
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}

	if payload.TotalTasks != 18 {
		t.Fatalf("expected total_tasks=18, got %d", payload.TotalTasks)
	}
	if payload.ActiveTasks != 6 {
		t.Fatalf("expected active_tasks=6, got %d", payload.ActiveTasks)
	}
	if payload.FinishedTasks != 12 {
		t.Fatalf("expected finished_tasks=12, got %d", payload.FinishedTasks)
	}
	if payload.SuccessRate == nil || *payload.SuccessRate != 75 {
		t.Fatalf("expected success_rate=75.0, got %#v", payload.SuccessRate)
	}
	if payload.StartupRecovery.Happened {
		t.Fatalf("expected startup_recovery.happened=false by default")
	}
}

func TestGetLogsNormalizesFiltersAndReturnsCatalog(t *testing.T) {
	repo := &fakeTaskReader{
		statusCounts: map[string]int{},
		logResult: domain.LogListResult{
			Logs: []domain.TaskLog{
				{
					ID:          "task-1",
					URL:         "https://telegra.ph/a",
					Status:      "SUCCESS",
					StartTime:   1700000000,
					Progress:    3,
					TotalImages: 3,
				},
			},
			Total:      1,
			Page:       1,
			PerPage:    100,
			TotalPages: 1,
		},
		hasActive: true,
	}

	handler := NewRouter(repo)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/logs?page=0&per_page=999&status=unknown&q=abc",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	if repo.lastQuery.Page != 1 {
		t.Fatalf("expected page normalized to 1, got %d", repo.lastQuery.Page)
	}
	if repo.lastQuery.PerPage != 100 {
		t.Fatalf("expected per_page clamped to 100, got %d", repo.lastQuery.PerPage)
	}
	if repo.lastQuery.Status != "" {
		t.Fatalf("expected unknown status to normalize to empty, got %q", repo.lastQuery.Status)
	}

	var payload domain.LogsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal logs response: %v", err)
	}

	if payload.Total != 1 {
		t.Fatalf("expected total=1, got %d", payload.Total)
	}
	if !payload.HasActiveTasks {
		t.Fatalf("expected has_active_tasks=true")
	}
	meta, ok := payload.StatusCatalog["SUCCESS"]
	if !ok {
		t.Fatalf("expected status catalog to contain SUCCESS")
	}
	if meta.Label != "成功" {
		t.Fatalf("expected SUCCESS label to be 成功, got %q", meta.Label)
	}
	if payload.Filters.Status != "" {
		t.Fatalf("expected filters.status empty, got %q", payload.Filters.Status)
	}
	if payload.Filters.Query != "abc" {
		t.Fatalf("expected filters.q=abc, got %q", payload.Filters.Query)
	}
}

func TestHealthz(t *testing.T) {
	repo := &fakeTaskReader{}
	handler := NewRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal health payload: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true, got %#v", payload["ok"])
	}
}

func TestDownloadCreatesTaskAndSubmitsJob(t *testing.T) {
	repo := &fakeTaskReader{}
	repo.claimResponses = []domain.ClaimDownloadTaskResult{
		{
			Decision: domain.ClaimDecisionCreated,
			Task: domain.TaskLog{
				ID: "created-task-1",
			},
		},
	}
	submitter := &fakeDownloadSubmitter{}
	handler := NewRouterWithOptions(
		repo,
		RouterOptions{
			DownloadSubmitter: submitter,
		},
	)

	req := httptest.NewRequest(
		http.MethodPost,
		"/download",
		strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fabc&author=A&tags=t1%EF%BC%8Ct2"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", recorder.Code)
	}
	if len(repo.claimCalls) != 1 {
		t.Fatalf("expected one claim call, got %d", len(repo.claimCalls))
	}
	if repo.claimCalls[0].Task.CanonicalURL == nil || *repo.claimCalls[0].Task.CanonicalURL != "https://telegra.ph/abc" {
		t.Fatalf("expected canonical_url normalized, got %#v", repo.claimCalls[0].Task.CanonicalURL)
	}
	if repo.claimCalls[0].Task.TagsNormalized == nil || *repo.claimCalls[0].Task.TagsNormalized != "t1,t2" {
		t.Fatalf("expected tags normalized with comma replacement")
	}
	if len(submitter.calls) != 1 {
		t.Fatalf("expected one submit call, got %d", len(submitter.calls))
	}
	if submitter.calls[0].TaskID != "created-task-1" {
		t.Fatalf("expected submit task_id created-task-1, got %q", submitter.calls[0].TaskID)
	}
}

func TestDownloadCreatedTaskEnqueuesStreamMessage(t *testing.T) {
	repo := &fakeTaskReader{}
	repo.claimResponses = []domain.ClaimDownloadTaskResult{
		{
			Decision: domain.ClaimDecisionCreated,
			Task: domain.TaskLog{
				ID: "created-task-queue",
			},
		},
	}
	downloadQueue := &fakeDownloadQueue{}
	handler := NewRouterWithOptions(
		repo,
		RouterOptions{
			DownloadQueue: downloadQueue,
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fabc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(downloadQueue.calls) != 1 {
		t.Fatalf("expected one enqueue call, got %d", len(downloadQueue.calls))
	}
	if downloadQueue.calls[0].TaskID != "created-task-queue" {
		t.Fatalf("expected enqueued task_id created-task-queue, got %q", downloadQueue.calls[0].TaskID)
	}
	if strings.TrimSpace(downloadQueue.calls[0].EnqueueToken) == "" {
		t.Fatalf("expected enqueue_token to be set")
	}
}

func TestDownloadCreatedTaskPersistsEnqueueTokenAndQueuePayload(t *testing.T) {
	repo := &fakeTaskReader{}
	repo.claimResponses = []domain.ClaimDownloadTaskResult{
		{
			Decision: domain.ClaimDecisionCreated,
			Task: domain.TaskLog{
				ID: "created-task-token",
			},
		},
	}
	downloadQueue := &fakeDownloadQueue{}
	handler := NewRouterWithOptions(
		repo,
		RouterOptions{
			DownloadQueue: downloadQueue,
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fabc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(repo.claimCalls) != 1 {
		t.Fatalf("expected one claim call, got %d", len(repo.claimCalls))
	}
	persistedToken := strings.TrimSpace(repo.claimCalls[0].EnqueueToken)
	if persistedToken == "" {
		t.Fatalf("expected claim call enqueue_token to be set")
	}
	if len(downloadQueue.calls) != 1 {
		t.Fatalf("expected one enqueue call, got %d", len(downloadQueue.calls))
	}
	if downloadQueue.calls[0].TaskID != "created-task-token" {
		t.Fatalf("expected enqueued task_id created-task-token, got %q", downloadQueue.calls[0].TaskID)
	}
	if downloadQueue.calls[0].EnqueueToken != persistedToken {
		t.Fatalf(
			"expected queue enqueue_token %q to match claim enqueue_token %q",
			downloadQueue.calls[0].EnqueueToken,
			persistedToken,
		)
	}
}

func TestDownloadCreatedTaskQueueFailureMarksTaskFailed(t *testing.T) {
	repo := &fakeTaskReader{}
	repo.claimResponses = []domain.ClaimDownloadTaskResult{
		{
			Decision: domain.ClaimDecisionCreated,
			Task: domain.TaskLog{
				ID: "created-task-queue-fail",
			},
		},
	}
	downloadQueue := &fakeDownloadQueue{
		err: errors.New("redis down"),
	}
	handler := NewRouterWithOptions(
		repo,
		RouterOptions{
			DownloadQueue: downloadQueue,
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fabc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(downloadQueue.calls) != 1 {
		t.Fatalf("expected one enqueue call, got %d", len(downloadQueue.calls))
	}
	if len(repo.markFailedCalls) != 1 {
		t.Fatalf("expected mark failed called once, got %d", len(repo.markFailedCalls))
	}
	if repo.markFailedCalls[0].taskID != "created-task-queue-fail" {
		t.Fatalf("expected mark failed task id created-task-queue-fail, got %q", repo.markFailedCalls[0].taskID)
	}
	if !strings.Contains(repo.markFailedCalls[0].message, "failed to enqueue task") {
		t.Fatalf("expected mark failed message to include enqueue failure context, got %q", repo.markFailedCalls[0].message)
	}
}

func TestDownloadReuseSuccessReturnsConfirmationWithoutSubmit(t *testing.T) {
	downloadDir := t.TempDir()
	t.Setenv("DOWNLOAD_PATH", downloadDir)
	existingFile := filepath.Join(downloadDir, "x.cbz")
	if err := os.WriteFile(existingFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("create existing file: %v", err)
	}

	repo := &fakeTaskReader{}
	repo.claimResponses = []domain.ClaimDownloadTaskResult{
		{
			Decision: domain.ClaimDecisionReuseSuccess,
			Task: domain.TaskLog{
				ID:            "existing-task",
				ResultZipPath: stringPtr(existingFile),
			},
		},
	}
	submitter := &fakeDownloadSubmitter{}
	handler := NewRouterWithOptions(
		repo,
		RouterOptions{
			DownloadSubmitter: submitter,
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fabc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if len(submitter.calls) != 0 {
		t.Fatalf("expected no submit call for reuse_success")
	}
	if !strings.Contains(recorder.Body.String(), "\"needs_confirmation\":true") {
		t.Fatalf("expected confirmation payload, got %s", recorder.Body.String())
	}
}

func TestDownloadRejectsInvalidURL(t *testing.T) {
	repo := &fakeTaskReader{}
	handler := NewRouter(repo)

	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Fexample.com%2Fx"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Only telegra.ph or graph.org URLs are supported.") {
		t.Fatalf("expected invalid-domain message, got %s", recorder.Body.String())
	}
}

func TestDownloadReuseSuccessWithMissingFileClearsAndReclaims(t *testing.T) {
	downloadDir := t.TempDir()
	t.Setenv("DOWNLOAD_PATH", downloadDir)
	stalePath := filepath.Join(downloadDir, "missing.cbz")

	repo := &fakeTaskReader{}
	repo.claimResponses = []domain.ClaimDownloadTaskResult{
		{
			Decision: domain.ClaimDecisionReuseSuccess,
			Task: domain.TaskLog{
				ID:            "stale-task",
				ResultZipPath: stringPtr(stalePath),
			},
		},
		{
			Decision: domain.ClaimDecisionCreated,
			Task: domain.TaskLog{
				ID: "fresh-task",
			},
		},
	}
	submitter := &fakeDownloadSubmitter{}
	handler := NewRouterWithOptions(
		repo,
		RouterOptions{
			DownloadSubmitter: submitter,
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fabc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202 after stale cleanup, got %d", recorder.Code)
	}
	if len(repo.clearResultByID) != 1 || repo.clearResultByID[0] != "stale-task" {
		t.Fatalf("expected stale task zip path to be cleared, got %#v", repo.clearResultByID)
	}
	if len(repo.claimCalls) != 2 {
		t.Fatalf("expected claim retry after stale file, got %d calls", len(repo.claimCalls))
	}
	if len(submitter.calls) != 1 || submitter.calls[0].TaskID != "fresh-task" {
		t.Fatalf("expected submit for fresh task, got %#v", submitter.calls)
	}
}

func TestDownloadSubmitFailureMarksTaskFailed(t *testing.T) {
	repo := &fakeTaskReader{}
	repo.claimResponses = []domain.ClaimDownloadTaskResult{
		{
			Decision: domain.ClaimDecisionCreated,
			Task: domain.TaskLog{
				ID: "task-submit-fail",
			},
		},
	}
	submitter := &fakeDownloadSubmitter{
		err: os.ErrDeadlineExceeded,
	}
	handler := NewRouterWithOptions(
		repo,
		RouterOptions{
			DownloadSubmitter: submitter,
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fabc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 when submit fails, got %d", recorder.Code)
	}
	if len(repo.markFailedCalls) != 1 || repo.markFailedCalls[0].taskID != "task-submit-fail" {
		t.Fatalf("expected mark failed call, got %#v", repo.markFailedCalls)
	}
}

func TestCancelTaskReturns404WhenTaskMissing(t *testing.T) {
	repo := &fakeTaskReader{tasks: map[string]domain.TaskLog{}}
	handler := NewRouter(repo)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/not-found/cancel", nil)
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
}

func TestCancelTaskUpdatesPendingTask(t *testing.T) {
	repo := &fakeTaskReader{
		tasks: map[string]domain.TaskLog{
			"task-pending": {ID: "task-pending", Status: domain.StatusPending},
		},
	}
	handler := NewRouter(repo)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-pending/cancel", nil)
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", recorder.Code)
	}
	if len(repo.cancelCalls) != 1 || repo.cancelCalls[0] != "task-pending" {
		t.Fatalf("expected cancel call for task-pending, got %#v", repo.cancelCalls)
	}
}

func TestCancelTaskReturnsConflictForFinishedTask(t *testing.T) {
	repo := &fakeTaskReader{
		tasks: map[string]domain.TaskLog{
			"task-done": {ID: "task-done", Status: domain.StatusSuccess},
		},
	}
	handler := NewRouter(repo)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-done/cancel", nil)
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", recorder.Code)
	}
}

func TestDownloadTaskReturnsFileWhenReady(t *testing.T) {
	downloadDir := t.TempDir()
	t.Setenv("DOWNLOAD_PATH", downloadDir)
	filePath := filepath.Join(downloadDir, "demo.cbz")
	if err := os.WriteFile(filePath, []byte("cbz"), 0o644); err != nil {
		t.Fatalf("write demo file: %v", err)
	}

	repo := &fakeTaskReader{
		tasks: map[string]domain.TaskLog{
			"task-success": {
				ID:            "task-success",
				Status:        domain.StatusSuccess,
				ResultZipPath: stringPtr(filePath),
			},
		},
	}
	handler := NewRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-success/download", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Header().Get("Content-Type"), "application/vnd.comicbook+zip") {
		t.Fatalf("expected cbz mimetype, got %s", recorder.Header().Get("Content-Type"))
	}
}

func TestDownloadTaskReturns409WhenNotFinished(t *testing.T) {
	repo := &fakeTaskReader{
		tasks: map[string]domain.TaskLog{
			"task-running": {ID: "task-running", Status: domain.StatusInProgress},
		},
	}
	handler := NewRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-running/download", nil)
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", recorder.Code)
	}
}

func TestDownloadUsesRuntimeSettingsSnapshotWhenBridgeConfigured(t *testing.T) {
	repo := &fakeTaskReader{}
	repo.claimResponses = []domain.ClaimDownloadTaskResult{
		{
			Decision: domain.ClaimDecisionCreated,
			Task: domain.TaskLog{
				ID: "task-runtime-settings",
			},
		},
	}

	var capturedSubmit DownloadSubmitRequest
	var submitCalled bool
	var mu sync.Mutex

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/settings-snapshot":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"timeout":66,"retries":7,"image_concurrency":5}`))
		case "/api/internal/enqueue-download":
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&capturedSubmit); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			submitCalled = true
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	handler := NewRouterWithOptions(
		repo,
		RouterOptions{
			UpstreamBaseURL:  upstream.URL,
			DownloadTimeout:  30,
			DownloadRetries:  10,
			ImageConcurrency: 2,
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fabc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(repo.claimCalls) != 1 {
		t.Fatalf("expected one claim call, got %d", len(repo.claimCalls))
	}
	if repo.claimCalls[0].Task.ImageConcurrency != 5 {
		t.Fatalf("expected claimed task image_concurrency=5 from runtime settings, got %d", repo.claimCalls[0].Task.ImageConcurrency)
	}
	mu.Lock()
	wasSubmitCalled := submitCalled
	mu.Unlock()
	if !wasSubmitCalled {
		t.Fatalf("expected enqueue api to be called")
	}
	if capturedSubmit.Timeout != 66 || capturedSubmit.Retries != 7 || capturedSubmit.ImageConcurrency != 5 {
		t.Fatalf(
			"expected submit request to use runtime settings, got timeout=%d retries=%d image_concurrency=%d",
			capturedSubmit.Timeout,
			capturedSubmit.Retries,
			capturedSubmit.ImageConcurrency,
		)
	}
}

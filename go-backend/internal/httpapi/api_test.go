package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/httpv2"
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

type cancelTaskCall struct {
	taskID     string
	fromStatus string
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

type fakeLegacyV2Store struct {
	createCalls []httpv2.CreateTaskInput
	createTask  httpv2.Task
	createErr   error

	listCalls  []httpv2.ListTasksQuery
	listResult httpv2.ListTasksResult
	listErr    error

	getCalls []string
	getTask  *httpv2.Task
	getTasks []*httpv2.Task
	getErr   error

	cancelCalls []cancelTaskCall
	cancelErr   error
	cancelErrs  []error

	markFailedCalls []markFailedCall
	markFailedErr   error

	statusCounts map[string]int
}

func (f *fakeLegacyV2Store) CreateTask(_ context.Context, in httpv2.CreateTaskInput) (httpv2.Task, error) {
	f.createCalls = append(f.createCalls, in)
	if f.createErr != nil {
		return httpv2.Task{}, f.createErr
	}
	if strings.TrimSpace(f.createTask.ID) != "" {
		return f.createTask, nil
	}
	return httpv2.Task{
		ID:           in.ID,
		URL:          in.URL,
		CanonicalURL: in.CanonicalURL,
		Status:       "QUEUED",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}, nil
}

func (f *fakeLegacyV2Store) ListTasks(_ context.Context, in httpv2.ListTasksQuery) (httpv2.ListTasksResult, error) {
	f.listCalls = append(f.listCalls, in)
	if f.listErr != nil {
		return httpv2.ListTasksResult{}, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeLegacyV2Store) GetTask(_ context.Context, taskID string) (*httpv2.Task, error) {
	f.getCalls = append(f.getCalls, taskID)
	if f.getErr != nil {
		return nil, f.getErr
	}
	if len(f.getTasks) > 0 {
		index := len(f.getCalls) - 1
		if index < len(f.getTasks) {
			return f.getTasks[index], nil
		}
		return f.getTasks[len(f.getTasks)-1], nil
	}
	return f.getTask, nil
}

func (f *fakeLegacyV2Store) CancelTask(_ context.Context, taskID, fromStatus string) error {
	f.cancelCalls = append(f.cancelCalls, cancelTaskCall{
		taskID:     taskID,
		fromStatus: fromStatus,
	})
	if len(f.cancelErrs) > 0 {
		next := f.cancelErrs[0]
		f.cancelErrs = f.cancelErrs[1:]
		return next
	}
	return f.cancelErr
}

func (f *fakeLegacyV2Store) MarkTaskFailed(_ context.Context, taskID, message string) error {
	f.markFailedCalls = append(f.markFailedCalls, markFailedCall{taskID: taskID, message: message})
	return f.markFailedErr
}

func (f *fakeLegacyV2Store) GetTaskStatusCounts(_ context.Context) (map[string]int, error) {
	if f.statusCounts == nil {
		return map[string]int{}, nil
	}
	return f.statusCounts, nil
}

type fakeLegacyV2Queue struct {
	calls []httpv2.TaskQueueMessage
	err   error
}

func (f *fakeLegacyV2Queue) Enqueue(_ context.Context, message httpv2.TaskQueueMessage) error {
	f.calls = append(f.calls, message)
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

func TestReadyzReturns503WhenMigrationsPending(t *testing.T) {
	handler := NewRouterWithOptions(
		&fakeTaskReader{},
		RouterOptions{
			ReadyzChecker: func(context.Context) (bool, error) {
				return false, nil
			},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal readyz payload: %v", err)
	}
	if payload["ready"] != false {
		t.Fatalf("expected ready=false, got %#v", payload["ready"])
	}
	if payload["reason"] != "migrations_pending" {
		t.Fatalf("expected reason migrations_pending, got %#v", payload["reason"])
	}
}

func TestReadyzReturns200WhenReady(t *testing.T) {
	handler := NewRouterWithOptions(
		&fakeTaskReader{},
		RouterOptions{
			ReadyzChecker: func(context.Context) (bool, error) {
				return true, nil
			},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestReadyzReturns500OnCheckerError(t *testing.T) {
	handler := NewRouterWithOptions(
		&fakeTaskReader{},
		RouterOptions{
			ReadyzChecker: func(context.Context) (bool, error) {
				return false, errors.New("database unavailable")
			},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestV2PageEntrypoints(t *testing.T) {
	handler := NewRouter(&fakeTaskReader{})

	t.Run("root redirects to v2", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusFound {
			t.Fatalf("expected 302, got %d", recorder.Code)
		}
		if got := recorder.Header().Get("Location"); got != "/v2" {
			t.Fatalf("expected redirect /v2, got %q", got)
		}
	})

	t.Run("logs redirects to v2 tasks", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/logs", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusFound {
			t.Fatalf("expected 302, got %d", recorder.Code)
		}
		if got := recorder.Header().Get("Location"); got != "/v2/tasks-ui" {
			t.Fatalf("expected redirect /v2/tasks-ui, got %q", got)
		}
	})

	t.Run("v2 dashboard page is available", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", recorder.Code)
		}
		body := recorder.Body.String()
		if !strings.Contains(body, "v2-create-task-form") {
			t.Fatalf("expected v2 dashboard form in html, got body=%q", body)
		}
	})

	t.Run("v2 tasks page is available", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/tasks-ui", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", recorder.Code)
		}
		body := recorder.Body.String()
		if !strings.Contains(body, "v2-tasks-table-body") {
			t.Fatalf("expected v2 tasks table in html, got body=%q", body)
		}
	})
}

func TestLegacyDownloadEndpointCreatesV2Task(t *testing.T) {
	legacyStore := &fakeTaskReader{}
	v2Store := &fakeLegacyV2Store{
		createTask: httpv2.Task{
			ID:           "task-v2-from-legacy-download",
			URL:          "https://telegra.ph/legacy-download",
			CanonicalURL: stringPtr("https://telegra.ph/legacy-download"),
			Status:       "QUEUED",
		},
	}
	v2Queue := &fakeLegacyV2Queue{}
	handler := NewRouterWithOptions(
		legacyStore,
		RouterOptions{
			V2TaskStore: v2Store,
			V2TaskQueue: v2Queue,
		},
	)

	req := httptest.NewRequest(
		http.MethodPost,
		"/download",
		strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Flegacy-download&author=%E4%BD%9C%E8%80%85A&series_name=%E7%B3%BB%E5%88%97B&comic_name=%E6%BC%AB%E7%94%BBC"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(v2Store.createCalls) != 1 {
		t.Fatalf("expected one v2 create call, got %d", len(v2Store.createCalls))
	}
	if v2Store.createCalls[0].Author == nil || *v2Store.createCalls[0].Author != "作者A" {
		t.Fatalf("expected author forwarded into v2 create call, got %#v", v2Store.createCalls[0].Author)
	}
	if v2Store.createCalls[0].SeriesName == nil || *v2Store.createCalls[0].SeriesName != "系列B" {
		t.Fatalf("expected series_name forwarded into v2 create call, got %#v", v2Store.createCalls[0].SeriesName)
	}
	if v2Store.createCalls[0].ComicName == nil || *v2Store.createCalls[0].ComicName != "漫画C" {
		t.Fatalf("expected comic_name forwarded into v2 create call, got %#v", v2Store.createCalls[0].ComicName)
	}
	if len(v2Queue.calls) != 1 {
		t.Fatalf("expected one v2 queue call, got %d", len(v2Queue.calls))
	}
	if v2Queue.calls[0].TaskID != "task-v2-from-legacy-download" {
		t.Fatalf("expected v2 queue task id task-v2-from-legacy-download, got %q", v2Queue.calls[0].TaskID)
	}
	if len(legacyStore.claimCalls) != 0 {
		t.Fatalf("expected legacy claim path not called, got %d calls", len(legacyStore.claimCalls))
	}
}

func TestLegacyLogsEndpointReadsFromV2Tasks(t *testing.T) {
	legacyStore := &fakeTaskReader{
		logResult: domain.LogListResult{
			Logs: []domain.TaskLog{
				{ID: "legacy-task-row", URL: "https://telegra.ph/legacy"},
			},
			Total:      1,
			Page:       1,
			PerPage:    25,
			TotalPages: 1,
		},
	}
	v2Store := &fakeLegacyV2Store{
		listResult: httpv2.ListTasksResult{
			Tasks: []httpv2.Task{
				{
					ID:           "task-v2-log-row",
					URL:          "https://telegra.ph/v2",
					CanonicalURL: stringPtr("https://telegra.ph/v2"),
					Status:       "QUEUED",
					CreatedAt:    time.Unix(1700000100, 0).UTC(),
					UpdatedAt:    time.Unix(1700000100, 0).UTC(),
				},
			},
			Total:      1,
			Page:       1,
			PerPage:    25,
			TotalPages: 1,
		},
		statusCounts: map[string]int{
			"QUEUED": 1,
		},
	}
	handler := NewRouterWithOptions(
		legacyStore,
		RouterOptions{
			V2TaskStore: v2Store,
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/logs?page=1&per_page=25", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload domain.LogsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal logs response: %v", err)
	}
	if len(payload.Logs) != 1 {
		t.Fatalf("expected one log row, got %d", len(payload.Logs))
	}
	if payload.Logs[0].ID != "task-v2-log-row" {
		t.Fatalf("expected v2 task row id task-v2-log-row, got %q", payload.Logs[0].ID)
	}
	if payload.Logs[0].Status != domain.StatusPending {
		t.Fatalf("expected mapped status %q, got %q", domain.StatusPending, payload.Logs[0].Status)
	}
	if payload.Total != 1 {
		t.Fatalf("expected total=1, got %d", payload.Total)
	}
	if !payload.HasActiveTasks {
		t.Fatalf("expected has_active_tasks=true")
	}
	if payload.Summary.PendingTasks != 1 {
		t.Fatalf("expected summary.pending_tasks=1, got %d", payload.Summary.PendingTasks)
	}
	if payload.Logs[0].ID == "legacy-task-row" {
		t.Fatalf("expected logs from v2 model, got legacy row")
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

func TestDownloadNormalizesAuthorByCommaOnly(t *testing.T) {
	repo := &fakeTaskReader{}
	repo.claimResponses = []domain.ClaimDownloadTaskResult{
		{
			Decision: domain.ClaimDecisionCreated,
			Task: domain.TaskLog{
				ID: "created-task-author-normalization",
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

	form := url.Values{}
	form.Set("url", "https://telegra.ph/author-normalization")
	form.Set("author", "  Jane Doe #1 ,  John Smith，Alice  Bob  ")
	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader(form.Encode()))
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
	if repo.claimCalls[0].Task.Author == nil {
		t.Fatalf("expected author metadata to be forwarded")
	}
	if *repo.claimCalls[0].Task.Author != "Jane Doe #1,John Smith,Alice  Bob" {
		t.Fatalf("expected author to normalize only comma delimiters, got %q", *repo.claimCalls[0].Task.Author)
	}
}

func TestDownloadNormalizesTagsAndGenresByCommaSpaceAndHash(t *testing.T) {
	repo := &fakeTaskReader{}
	repo.claimResponses = []domain.ClaimDownloadTaskResult{
		{
			Decision: domain.ClaimDecisionCreated,
			Task: domain.TaskLog{
				ID: "created-task-tag-genre-normalization",
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

	form := url.Values{}
	form.Set("url", "https://telegra.ph/tag-genre-normalization")
	form.Set("tags", " tag1  tag2,#tag3，tag2   # tag4,,tag1  ")
	form.Set("genres", " 类型A #类型B，类型C   类型A ## 类型D  ")
	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader(form.Encode()))
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
	if repo.claimCalls[0].Task.TagsNormalized == nil {
		t.Fatalf("expected tags_normalized to be forwarded")
	}
	if *repo.claimCalls[0].Task.TagsNormalized != "tag1,tag2,tag3,tag4" {
		t.Fatalf("expected tags to normalize by comma, space, and hash with dedupe, got %q", *repo.claimCalls[0].Task.TagsNormalized)
	}
	if repo.claimCalls[0].Task.GenresNormalized == nil {
		t.Fatalf("expected genres_normalized to be forwarded")
	}
	if *repo.claimCalls[0].Task.GenresNormalized != "类型A,类型B,类型C,类型D" {
		t.Fatalf("expected genres to normalize by comma, space, and hash with dedupe, got %q", *repo.claimCalls[0].Task.GenresNormalized)
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

func decodeJSONResponse(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal json response: %v body=%s", err, string(body))
	}
	return payload
}

func assertExactJSONKeys(t *testing.T, payload map[string]any, expected ...string) {
	t.Helper()

	expectedSet := make(map[string]struct{}, len(expected))
	for _, key := range expected {
		expectedSet[key] = struct{}{}
	}

	if len(payload) != len(expectedSet) {
		actualKeys := make([]string, 0, len(payload))
		for key := range payload {
			actualKeys = append(actualKeys, key)
		}
		sort.Strings(actualKeys)
		t.Fatalf("expected %d keys, got %d (%v)", len(expectedSet), len(payload), actualKeys)
	}

	for key := range payload {
		if _, ok := expectedSet[key]; !ok {
			t.Fatalf("unexpected key %q in payload %#v", key, payload)
		}
	}
}

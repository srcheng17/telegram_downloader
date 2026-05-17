package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/domain"
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

func TestLegacyPageEntrypoints(t *testing.T) {
	handler := NewRouter(&fakeTaskReader{})

	t.Run("root redirects to healthz", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusFound {
			t.Fatalf("expected 302, got %d", recorder.Code)
		}
		if got := recorder.Header().Get("Location"); got != "/healthz" {
			t.Fatalf("expected redirect /healthz, got %q", got)
		}
	})

	t.Run("logs redirects to logs api", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/logs", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusFound {
			t.Fatalf("expected 302, got %d", recorder.Code)
		}
		if got := recorder.Header().Get("Location"); got != "/api/logs" {
			t.Fatalf("expected redirect /api/logs, got %q", got)
		}
	})

	t.Run("v2 dashboard page is removed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", recorder.Code)
		}
	})

	t.Run("v2 tasks page is removed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/tasks-ui", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", recorder.Code)
		}
	})
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

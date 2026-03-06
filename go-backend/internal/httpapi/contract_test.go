package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
)

func TestContract_DownloadResponsesMatchFrozenSchema(t *testing.T) {
	t.Run("created task response", func(t *testing.T) {
		repo := &fakeTaskReader{
			claimResponses: []domain.ClaimDownloadTaskResult{
				{
					Decision: domain.ClaimDecisionCreated,
					Task:     domain.TaskLog{ID: "task-created"},
				},
			},
		}
		handler := NewRouterWithOptions(repo, RouterOptions{DownloadSubmitter: &fakeDownloadSubmitter{}})

		req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fcreated"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
		}
		payload := decodeJSONResponse(t, rec.Body.Bytes())
		assertExactJSONKeys(t, payload, "ok", "duplicate", "active", "task_id", "logs_url", "force_applied")
		assertPayloadBool(t, payload, "ok", true)
		assertPayloadBool(t, payload, "duplicate", false)
		assertPayloadBool(t, payload, "active", false)
		assertPayloadBool(t, payload, "force_applied", false)
		taskID := assertPayloadNonEmptyString(t, payload, "task_id")
		if taskID != "task-created" {
			t.Fatalf("expected task_id=task-created, got %q", taskID)
		}
		logsURL := assertPayloadString(t, payload, "logs_url")
		if logsURL != "/logs" {
			t.Fatalf("expected logs_url=/logs, got %q", logsURL)
		}
		if payload["duplicate"] != false || payload["active"] != false || payload["force_applied"] != false {
			t.Fatalf("unexpected created payload booleans: %#v", payload)
		}
	})

	t.Run("reuse success response", func(t *testing.T) {
		downloadDir := t.TempDir()
		t.Setenv("DOWNLOAD_PATH", downloadDir)
		existingPath := filepath.Join(downloadDir, "existing.cbz")
		if err := os.WriteFile(existingPath, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write existing file: %v", err)
		}

		repo := &fakeTaskReader{
			claimResponses: []domain.ClaimDownloadTaskResult{
				{
					Decision: domain.ClaimDecisionReuseSuccess,
					Task:     domain.TaskLog{ID: "task-reuse-success", ResultZipPath: stringPtr(existingPath)},
				},
			},
		}
		handler := NewRouterWithOptions(repo, RouterOptions{DownloadSubmitter: &fakeDownloadSubmitter{}})

		req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Freuse-success"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		payload := decodeJSONResponse(t, rec.Body.Bytes())
		assertExactJSONKeys(t, payload, "ok", "duplicate", "needs_confirmation", "task_id", "download_url", "force_applied")
		assertPayloadBool(t, payload, "ok", true)
		assertPayloadBool(t, payload, "duplicate", true)
		assertPayloadBool(t, payload, "needs_confirmation", true)
		assertPayloadBool(t, payload, "force_applied", false)
		taskID := assertPayloadNonEmptyString(t, payload, "task_id")
		if taskID != "task-reuse-success" {
			t.Fatalf("expected task_id=task-reuse-success, got %q", taskID)
		}
		downloadURL := assertPayloadString(t, payload, "download_url")
		if downloadURL != "/api/tasks/task-reuse-success/download" {
			t.Fatalf("expected frozen download_url, got %q", downloadURL)
		}
		if payload["duplicate"] != true || payload["needs_confirmation"] != true {
			t.Fatalf("unexpected reuse_success payload booleans: %#v", payload)
		}
	})

	t.Run("reuse active response", func(t *testing.T) {
		repo := &fakeTaskReader{
			claimResponses: []domain.ClaimDownloadTaskResult{
				{
					Decision: domain.ClaimDecisionReuseActive,
					Task:     domain.TaskLog{ID: "task-reuse-active"},
				},
			},
		}
		handler := NewRouterWithOptions(repo, RouterOptions{DownloadSubmitter: &fakeDownloadSubmitter{}})

		req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Freuse-active"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		payload := decodeJSONResponse(t, rec.Body.Bytes())
		assertExactJSONKeys(t, payload, "ok", "duplicate", "active", "task_id", "logs_url", "force_applied")
		assertPayloadBool(t, payload, "ok", true)
		assertPayloadBool(t, payload, "duplicate", true)
		assertPayloadBool(t, payload, "active", true)
		assertPayloadBool(t, payload, "force_applied", false)
		taskID := assertPayloadNonEmptyString(t, payload, "task_id")
		if taskID != "task-reuse-active" {
			t.Fatalf("expected task_id=task-reuse-active, got %q", taskID)
		}
		logsURL := assertPayloadString(t, payload, "logs_url")
		if logsURL != "/logs" {
			t.Fatalf("expected logs_url=/logs, got %q", logsURL)
		}
		if payload["duplicate"] != true || payload["active"] != true {
			t.Fatalf("unexpected reuse_active payload booleans: %#v", payload)
		}
	})

	t.Run("invalid json matches legacy error", func(t *testing.T) {
		repo := &fakeTaskReader{}
		handler := NewRouterWithOptions(repo, RouterOptions{DownloadSubmitter: &fakeDownloadSubmitter{}})

		req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("{"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
		}
		payload := decodeJSONResponse(t, rec.Body.Bytes())
		assertExactJSONKeys(t, payload, "ok", "message")
		assertPayloadBool(t, payload, "ok", false)
		if payload["message"] != "Please provide a Telegraph URL." {
			t.Fatalf("expected legacy invalid-json message, got %#v", payload["message"])
		}
	})
}

func TestContract_LogsResponseFrozenSchema(t *testing.T) {
	repo := &fakeTaskReader{
		statusCounts: map[string]int{
			domain.StatusSuccess: 1,
		},
		logResult: domain.LogListResult{
			Logs: []domain.TaskLog{
				{
					ID:          "task-logs-contract",
					URL:         "https://telegra.ph/contract-logs",
					Status:      domain.StatusSuccess,
					StartTime:   1700000000,
					Progress:    5,
					TotalImages: 5,
				},
			},
			Total:      1,
			Page:       1,
			PerPage:    25,
			TotalPages: 1,
		},
		hasActive: false,
	}
	handler := NewRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/logs?page=1&per_page=25&status=SUCCESS&q=contract", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONResponse(t, rec.Body.Bytes())
	assertExactJSONKeys(
		t,
		payload,
		"logs",
		"total",
		"page",
		"per_page",
		"total_pages",
		"has_active_tasks",
		"filters",
		"status_catalog",
		"summary",
	)

	logs, ok := payload["logs"].([]any)
	if !ok {
		t.Fatalf("expected logs []any, got %#v", payload["logs"])
	}
	if len(logs) != 1 {
		t.Fatalf("expected one log row, got %d", len(logs))
	}
	assertPayloadNumber(t, payload, "total", 1)
	assertPayloadNumber(t, payload, "page", 1)
	assertPayloadNumber(t, payload, "per_page", 25)
	assertPayloadNumber(t, payload, "total_pages", 1)
	assertPayloadBool(t, payload, "has_active_tasks", false)

	filters, ok := payload["filters"].(map[string]any)
	if !ok {
		t.Fatalf("expected filters object, got %#v", payload["filters"])
	}
	assertExactJSONKeys(t, filters, "status", "q")
	if assertPayloadString(t, filters, "status") != "SUCCESS" {
		t.Fatalf("expected filters.status=SUCCESS, got %#v", filters["status"])
	}
	if assertPayloadString(t, filters, "q") != "contract" {
		t.Fatalf("expected filters.q=contract, got %#v", filters["q"])
	}

	statusCatalog, ok := payload["status_catalog"].(map[string]any)
	if !ok {
		t.Fatalf("expected status_catalog object, got %#v", payload["status_catalog"])
	}
	if len(statusCatalog) == 0 {
		t.Fatalf("expected at least one status_catalog entry")
	}

	foundCatalogWithRequiredFields := false
	for _, rawMeta := range statusCatalog {
		meta, ok := rawMeta.(map[string]any)
		if !ok {
			continue
		}
		if _, hasLabel := meta["label"]; !hasLabel {
			continue
		}
		if _, hasCanCancel := meta["can_cancel"]; !hasCanCancel {
			continue
		}
		if _, hasCanDownload := meta["can_download"]; !hasCanDownload {
			continue
		}
		if _, hasTerminal := meta["terminal"]; !hasTerminal {
			continue
		}
		assertPayloadString(t, meta, "label")
		if _, ok := meta["can_cancel"].(bool); !ok {
			continue
		}
		if _, ok := meta["can_download"].(bool); !ok {
			continue
		}
		if _, ok := meta["terminal"].(bool); !ok {
			continue
		}
		foundCatalogWithRequiredFields = true
		break
	}
	if !foundCatalogWithRequiredFields {
		t.Fatalf("expected at least one status_catalog entry with label/can_cancel/can_download/terminal")
	}

	if _, ok := payload["summary"].(map[string]any); !ok {
		t.Fatalf("expected summary object, got %#v", payload["summary"])
	}
}

func TestContract_SummaryAllowsNullSuccessRate(t *testing.T) {
	repo := &fakeTaskReader{
		statusCounts: map[string]int{
			domain.StatusPending: 2,
		},
	}
	handler := NewRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	payload := decodeJSONResponse(t, rec.Body.Bytes())
	if _, ok := payload["success_rate"]; !ok {
		t.Fatalf("expected success_rate key in summary payload")
	}
	if payload["success_rate"] != nil {
		t.Fatalf("expected success_rate=null when no finished tasks, got %#v", payload["success_rate"])
	}

	startupRecovery, ok := payload["startup_recovery"].(map[string]any)
	if !ok {
		t.Fatalf("expected startup_recovery object, got %#v", payload["startup_recovery"])
	}
	if startupRecovery["occurred_at"] != nil {
		t.Fatalf("expected startup_recovery.occurred_at=null by default, got %#v", startupRecovery["occurred_at"])
	}
}

func TestContract_CancelAndDownloadStatusCodes(t *testing.T) {
	downloadDir := t.TempDir()
	t.Setenv("DOWNLOAD_PATH", downloadDir)
	realFile := filepath.Join(downloadDir, "ready.cbz")
	if err := os.WriteFile(realFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write ready file: %v", err)
	}

	repo := &fakeTaskReader{
		tasks: map[string]domain.TaskLog{
			"cancel-pending":     {ID: "cancel-pending", Status: domain.StatusPending},
			"cancel-finished":    {ID: "cancel-finished", Status: domain.StatusSuccess},
			"download-running":   {ID: "download-running", Status: domain.StatusInProgress},
			"download-no-output": {ID: "download-no-output", Status: domain.StatusSuccess},
			"download-ready":     {ID: "download-ready", Status: domain.StatusSuccess, ResultZipPath: stringPtr(realFile)},
		},
	}
	handler := NewRouter(repo)

	assertStatusAndMessage := func(method, target string, expectedStatus int, expectedMessage string) {
		t.Helper()

		req := httptest.NewRequest(method, target, nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != expectedStatus {
			t.Fatalf("expected %d for %s %s, got %d body=%s", expectedStatus, method, target, rec.Code, rec.Body.String())
		}
		payload := decodeJSONResponse(t, rec.Body.Bytes())
		assertExactJSONKeys(t, payload, "ok", "message")
		assertPayloadBool(t, payload, "ok", false)
		if payload["message"] != expectedMessage {
			t.Fatalf("expected message %q for %s %s, got %#v", expectedMessage, method, target, payload["message"])
		}
	}

	assertSuccessShape := func(method, target string, expectedStatus int, expectedMessage string) {
		t.Helper()

		req := httptest.NewRequest(method, target, nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != expectedStatus {
			t.Fatalf("expected %d for %s %s, got %d body=%s", expectedStatus, method, target, rec.Code, rec.Body.String())
		}
		payload := decodeJSONResponse(t, rec.Body.Bytes())
		assertExactJSONKeys(t, payload, "ok", "message")
		assertPayloadBool(t, payload, "ok", true)
		if payload["message"] != expectedMessage {
			t.Fatalf("expected message %q for %s %s, got %#v", expectedMessage, method, target, payload["message"])
		}
	}

	assertStatusAndMessage(http.MethodPost, "/api/tasks/not-found/cancel", http.StatusNotFound, "Task not found.")
	assertStatusAndMessage(http.MethodPost, "/api/tasks/cancel-finished/cancel", http.StatusConflict, "Task already finished with status SUCCESS.")
	assertSuccessShape(http.MethodPost, "/api/tasks/cancel-pending/cancel", http.StatusAccepted, "Cancellation requested.")
	assertSuccessShape(http.MethodPost, "/api/tasks/cancel-pending/cancel", http.StatusOK, "Cancellation already requested.")

	assertStatusAndMessage(http.MethodGet, "/api/tasks/unknown/download", http.StatusNotFound, "Task not found.")
	assertStatusAndMessage(http.MethodGet, "/api/tasks/download-running/download", http.StatusConflict, "Task is not completed yet.")
	assertStatusAndMessage(http.MethodGet, "/api/tasks/download-no-output/download", http.StatusNotFound, "Output file not found for this task.")

	headReq := httptest.NewRequest(http.MethodHead, "/api/tasks/download-ready/download", nil)
	headRec := httptest.NewRecorder()
	handler.ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusOK {
		t.Fatalf("expected HEAD precheck 200, got %d", headRec.Code)
	}
	if headRec.Body.Len() != 0 {
		t.Fatalf("expected HEAD body empty, got %q", headRec.Body.String())
	}
}

func assertPayloadBool(t *testing.T, payload map[string]any, key string, expected bool) {
	t.Helper()

	value, ok := payload[key].(bool)
	if !ok {
		t.Fatalf("expected payload[%q] bool, got %#v", key, payload[key])
	}
	if value != expected {
		t.Fatalf("expected payload[%q]=%t, got %t", key, expected, value)
	}
}

func assertPayloadString(t *testing.T, payload map[string]any, key string) string {
	t.Helper()

	value, ok := payload[key].(string)
	if !ok {
		t.Fatalf("expected payload[%q] string, got %#v", key, payload[key])
	}
	return value
}

func assertPayloadNonEmptyString(t *testing.T, payload map[string]any, key string) string {
	t.Helper()

	value := assertPayloadString(t, payload, key)
	if strings.TrimSpace(value) == "" {
		t.Fatalf("expected payload[%q] non-empty string", key)
	}
	return value
}

func assertPayloadNumber(t *testing.T, payload map[string]any, key string, expected float64) {
	t.Helper()

	value, ok := payload[key].(float64)
	if !ok {
		t.Fatalf("expected payload[%q] number, got %#v", key, payload[key])
	}
	if value != expected {
		t.Fatalf("expected payload[%q]=%v, got %v", key, expected, value)
	}
}

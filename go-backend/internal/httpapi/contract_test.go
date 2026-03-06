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
		if payload["message"] != "Please provide a Telegraph URL." {
			t.Fatalf("expected legacy invalid-json message, got %#v", payload["message"])
		}
	})
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
		if payload["message"] != expectedMessage {
			t.Fatalf("expected message %q for %s %s, got %#v", expectedMessage, method, target, payload["message"])
		}
	}

	assertStatusAndMessage(http.MethodPost, "/api/tasks/not-found/cancel", http.StatusNotFound, "Task not found.")
	assertStatusAndMessage(http.MethodPost, "/api/tasks/cancel-finished/cancel", http.StatusConflict, "Task already finished with status SUCCESS.")
	assertStatusAndMessage(http.MethodPost, "/api/tasks/cancel-pending/cancel", http.StatusAccepted, "Cancellation requested.")
	assertStatusAndMessage(http.MethodPost, "/api/tasks/cancel-pending/cancel", http.StatusOK, "Cancellation already requested.")

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

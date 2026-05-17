package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/domain"
)

func TestContract_LogsResponseFrozenSchema(t *testing.T) {
	repo := &fakeTaskReader{
		statusCounts: map[string]int{
			domain.StatusSuccess: 1,
		},
		logResult: domain.LogListResult{
			Logs: []domain.TaskLog{
				{
					ID:               "task-logs-contract",
					URL:              "https://telegra.ph/contract-logs",
					CanonicalURL:     stringPtr("https://telegra.ph/contract-logs"),
					Status:           domain.StatusSuccess,
					StartTime:        1700000000,
					Progress:         5,
					TotalImages:      5,
					ImageConcurrency: 2,
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
	firstLog, ok := logs[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first logs row object, got %#v", logs[0])
	}
	assertExactJSONKeys(
		t,
		firstLog,
		"id",
		"url",
		"canonical_url",
		"status",
		"task_type",
		"source_archive_name",
		"upload_loaded_bytes",
		"upload_total_bytes",
		"retryable",
		"start_time",
		"error",
		"progress",
		"total_images",
		"image_concurrency",
		"result_zip_path",
		"author",
		"series_name",
		"comic_name",
		"summary",
		"tags_raw",
		"tags_normalized",
		"genres_raw",
		"genres_normalized",
	)
	assertPayloadNonEmptyString(t, firstLog, "id")
	assertPayloadNonEmptyString(t, firstLog, "url")
	assertPayloadNonEmptyString(t, firstLog, "status")
	assertPayloadIsNumber(t, firstLog, "start_time")
	assertPayloadIsNumber(t, firstLog, "upload_loaded_bytes")
	assertPayloadIsNumber(t, firstLog, "upload_total_bytes")
	assertPayloadIsNumber(t, firstLog, "progress")
	assertPayloadIsNumber(t, firstLog, "total_images")
	assertPayloadIsNumber(t, firstLog, "image_concurrency")
	assertPayloadOptionalString(t, firstLog, "canonical_url")
	assertPayloadOptionalString(t, firstLog, "task_type")
	assertPayloadOptionalString(t, firstLog, "source_archive_name")
	assertPayloadOptionalString(t, firstLog, "error")
	assertPayloadOptionalString(t, firstLog, "result_zip_path")
	assertPayloadOptionalString(t, firstLog, "author")
	assertPayloadOptionalString(t, firstLog, "series_name")
	assertPayloadOptionalString(t, firstLog, "comic_name")
	assertPayloadOptionalString(t, firstLog, "summary")
	assertPayloadOptionalString(t, firstLog, "tags_raw")
	assertPayloadOptionalString(t, firstLog, "tags_normalized")
	assertPayloadOptionalString(t, firstLog, "genres_raw")
	assertPayloadOptionalString(t, firstLog, "genres_normalized")
	assertPayloadBool(t, firstLog, "retryable", false)
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

	for statusCode, rawMeta := range statusCatalog {
		if strings.TrimSpace(statusCode) == "" {
			t.Fatalf("status_catalog contains empty status code key")
		}
		meta, ok := rawMeta.(map[string]any)
		if !ok {
			t.Fatalf("status_catalog[%q] expected object, got %#v", statusCode, rawMeta)
		}
		assertExactJSONKeys(t, meta, "label", "can_cancel", "can_download", "terminal")
		assertPayloadString(t, meta, "label")
		assertPayloadIsBool(t, meta, "can_cancel")
		assertPayloadIsBool(t, meta, "can_download")
		assertPayloadIsBool(t, meta, "terminal")
	}

	summary, ok := payload["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary object, got %#v", payload["summary"])
	}
	assertExactJSONKeys(
		t,
		summary,
		"total_tasks",
		"pending_tasks",
		"in_progress_tasks",
		"cancel_requested_tasks",
		"canceled_tasks",
		"success_tasks",
		"failed_tasks",
		"active_tasks",
		"finished_tasks",
		"success_rate",
		"startup_recovery",
	)
	assertPayloadIsNumber(t, summary, "total_tasks")
	assertPayloadIsNumber(t, summary, "pending_tasks")
	assertPayloadIsNumber(t, summary, "in_progress_tasks")
	assertPayloadIsNumber(t, summary, "cancel_requested_tasks")
	assertPayloadIsNumber(t, summary, "canceled_tasks")
	assertPayloadIsNumber(t, summary, "success_tasks")
	assertPayloadIsNumber(t, summary, "failed_tasks")
	assertPayloadIsNumber(t, summary, "active_tasks")
	assertPayloadIsNumber(t, summary, "finished_tasks")
	assertPayloadOptionalNumber(t, summary, "success_rate")

	startupRecovery, ok := summary["startup_recovery"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary.startup_recovery object, got %#v", summary["startup_recovery"])
	}
	assertExactJSONKeys(t, startupRecovery, "happened", "recovered_total", "recovered_failed", "recovered_canceled", "occurred_at")
	assertPayloadIsBool(t, startupRecovery, "happened")
	assertPayloadIsNumber(t, startupRecovery, "recovered_total")
	assertPayloadIsNumber(t, startupRecovery, "recovered_failed")
	assertPayloadIsNumber(t, startupRecovery, "recovered_canceled")
	assertPayloadOptionalString(t, startupRecovery, "occurred_at")
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

func assertPayloadIsBool(t *testing.T, payload map[string]any, key string) bool {
	t.Helper()

	value, ok := payload[key].(bool)
	if !ok {
		t.Fatalf("expected payload[%q] bool, got %#v", key, payload[key])
	}
	return value
}

func assertPayloadIsNumber(t *testing.T, payload map[string]any, key string) float64 {
	t.Helper()

	value, ok := payload[key].(float64)
	if !ok {
		t.Fatalf("expected payload[%q] number, got %#v", key, payload[key])
	}
	return value
}

func assertPayloadOptionalString(t *testing.T, payload map[string]any, key string) {
	t.Helper()

	value, ok := payload[key]
	if !ok {
		t.Fatalf("expected payload[%q] key to exist", key)
	}
	if value == nil {
		return
	}
	if _, ok := value.(string); !ok {
		t.Fatalf("expected payload[%q] nil or string, got %#v", key, value)
	}
}

func assertPayloadOptionalNumber(t *testing.T, payload map[string]any, key string) {
	t.Helper()

	value, ok := payload[key]
	if !ok {
		t.Fatalf("expected payload[%q] key to exist", key)
	}
	if value == nil {
		return
	}
	if _, ok := value.(float64); !ok {
		t.Fatalf("expected payload[%q] nil or number, got %#v", key, value)
	}
}

func assertErrorPayloadKeys(t *testing.T, payload map[string]any) {
	t.Helper()

	if _, hasMessage := payload["message"]; hasMessage {
		assertExactJSONKeys(t, payload, "error", "code", "message")
		return
	}
	assertExactJSONKeys(t, payload, "error", "code")
}

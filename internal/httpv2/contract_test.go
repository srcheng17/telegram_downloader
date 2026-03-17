package httpv2

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestContract_V2CreateTaskSchema(t *testing.T) {
	store := &fakeTaskStore{
		createdTask: Task{
			ID:           "task-contract-create",
			URL:          "https://telegra.ph/contract-create",
			CanonicalURL: stringPtr("https://telegra.ph/contract-create"),
			Status:       "QUEUED",
		},
	}
	handler := NewRouter(store, &fakeTaskQueue{})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v2/tasks",
		strings.NewReader(`{"url":"https://telegra.ph/contract-create"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	assertExactJSONKeys(t, payload, "task_id", "status")
	assertJSONValueType(t, payload, "task_id", "string")
	assertJSONValueType(t, payload, "status", "string")

	badReq := httptest.NewRequest(http.MethodPost, "/v2/tasks", strings.NewReader("{"))
	badReq.Header.Set("Content-Type", "application/json")
	badRecorder := httptest.NewRecorder()
	handler.ServeHTTP(badRecorder, badReq)

	if badRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected bad json to return 400, got %d body=%s", badRecorder.Code, badRecorder.Body.String())
	}
	assertErrorResponseShape(t, badRecorder.Body.Bytes())
}

func TestContract_V2SummarySchema(t *testing.T) {
	store := &fakeTaskStore{
		summaryCounts: map[string]int{
			"QUEUED":   3,
			"RUNNING":  2,
			"SUCCESS":  5,
			"FAILED":   1,
			"CANCELED": 1,
		},
	}
	handler := NewRouter(store, &fakeTaskQueue{})

	req := httptest.NewRequest(http.MethodGet, "/v2/dashboard/summary", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode summary response: %v", err)
	}
	assertExactJSONKeys(
		t,
		payload,
		"total_tasks",
		"queued_tasks",
		"running_tasks",
		"success_tasks",
		"failed_tasks",
		"canceled_tasks",
		"active_tasks",
		"finished_tasks",
		"success_rate",
		"status_catalog",
	)
	assertJSONValueType(t, payload, "total_tasks", "number")
	assertJSONValueType(t, payload, "queued_tasks", "number")
	assertJSONValueType(t, payload, "running_tasks", "number")
	assertJSONValueType(t, payload, "success_tasks", "number")
	assertJSONValueType(t, payload, "failed_tasks", "number")
	assertJSONValueType(t, payload, "canceled_tasks", "number")
	assertJSONValueType(t, payload, "active_tasks", "number")
	assertJSONValueType(t, payload, "finished_tasks", "number")
	assertJSONValueType(t, payload, "success_rate", "number")
	assertJSONValueType(t, payload, "status_catalog", "object")

	statusCatalog := payload["status_catalog"].(map[string]any)
	successMeta := statusCatalog["SUCCESS"].(map[string]any)
	if successMeta["label"] != "已完成" {
		t.Fatalf("expected success label 已完成, got %#v", successMeta["label"])
	}

	errorStore := &fakeTaskStore{summaryErr: errors.New("summary broken")}
	errorHandler := NewRouter(errorStore, &fakeTaskQueue{})
	errReq := httptest.NewRequest(http.MethodGet, "/v2/dashboard/summary", nil)
	errRecorder := httptest.NewRecorder()
	errorHandler.ServeHTTP(errRecorder, errReq)
	if errRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected summary store error to return 500, got %d body=%s", errRecorder.Code, errRecorder.Body.String())
	}
	assertErrorResponseShape(t, errRecorder.Body.Bytes())
}

func TestContract_V2ListTasksSchema(t *testing.T) {
	store := &fakeTaskStore{
		listResult: ListTasksResult{
			Tasks: []Task{{
				ID:           "task-contract-list",
				URL:          "https://telegra.ph/contract-list",
				CanonicalURL: stringPtr("https://telegra.ph/contract-list"),
				Status:       "RUNNING",
			}},
			Total:      1,
			Page:       1,
			PerPage:    20,
			TotalPages: 1,
		},
	}
	handler := NewRouter(store, &fakeTaskQueue{})

	req := httptest.NewRequest(http.MethodGet, "/v2/tasks?page=1&per_page=20", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	assertExactJSONKeys(t, payload, "tasks", "total", "page", "per_page", "total_pages", "status_catalog")
	assertJSONValueType(t, payload, "status_catalog", "object")
	statusCatalog := payload["status_catalog"].(map[string]any)
	runningMeta := statusCatalog["RUNNING"].(map[string]any)
	if runningMeta["label"] != "进行中" {
		t.Fatalf("expected running label 进行中, got %#v", runningMeta["label"])
	}
}

func TestContract_V2TaskDetailSchema(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	store := &fakeTaskStore{
		getTask: &Task{
			ID:            "task-contract-detail",
			URL:           "https://telegra.ph/contract-detail",
			CanonicalURL:  stringPtr("https://telegra.ph/contract-detail"),
			Status:        "SUCCESS",
			Error:         stringPtr("none"),
			ResultZipPath: stringPtr("downloaded_images/task-contract-detail.cbz"),
			CreatedAt:     now,
			UpdatedAt:     now.Add(5 * time.Minute),
		},
	}
	handler := NewRouter(store, &fakeTaskQueue{})

	req := httptest.NewRequest(http.MethodGet, "/v2/tasks/task-contract-detail", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode task detail response: %v", err)
	}
	assertExactJSONKeys(
		t,
		payload,
		"id",
		"url",
		"canonical_url",
		"status",
		"error",
		"result_zip_path",
		"created_at",
		"updated_at",
	)
	assertJSONValueType(t, payload, "id", "string")
	assertJSONValueType(t, payload, "url", "string")
	assertJSONValueType(t, payload, "canonical_url", "string")
	assertJSONValueType(t, payload, "status", "string")
	assertJSONValueType(t, payload, "error", "string")
	assertJSONValueType(t, payload, "result_zip_path", "string")
	assertJSONValueType(t, payload, "created_at", "string")
	assertJSONValueType(t, payload, "updated_at", "string")
	assertRFC3339String(t, payload["created_at"])
	assertRFC3339String(t, payload["updated_at"])

	store.getTask = nil
	notFoundReq := httptest.NewRequest(http.MethodGet, "/v2/tasks/not-found", nil)
	notFoundRecorder := httptest.NewRecorder()
	handler.ServeHTTP(notFoundRecorder, notFoundReq)
	if notFoundRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", notFoundRecorder.Code, notFoundRecorder.Body.String())
	}
	assertErrorResponseShape(t, notFoundRecorder.Body.Bytes())
}

func assertRFC3339String(t *testing.T, value any) {
	t.Helper()

	raw, ok := value.(string)
	if !ok {
		t.Fatalf("expected RFC3339 value to be string, got %T (%#v)", value, value)
	}
	if strings.TrimSpace(raw) == "" {
		t.Fatalf("expected non-empty RFC3339 string")
	}
	if _, err := time.Parse(time.RFC3339, raw); err != nil {
		t.Fatalf("expected RFC3339 timestamp, got %q err=%v", raw, err)
	}
}

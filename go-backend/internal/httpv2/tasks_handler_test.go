package httpv2

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeTaskStore struct {
	createCalls []CreateTaskInput
	createdTask Task
	createErr   error

	listCalls  []ListTasksQuery
	listResult ListTasksResult
	listErr    error

	getCalls []string
	getTask  *Task
	getErr   error
}

func (f *fakeTaskStore) CreateTask(_ context.Context, in CreateTaskInput) (Task, error) {
	f.createCalls = append(f.createCalls, in)
	if f.createErr != nil {
		return Task{}, f.createErr
	}
	if f.createdTask.ID != "" {
		return f.createdTask, nil
	}
	return Task{ID: in.ID, URL: in.URL, CanonicalURL: in.CanonicalURL, Status: "QUEUED"}, nil
}

func (f *fakeTaskStore) ListTasks(_ context.Context, in ListTasksQuery) (ListTasksResult, error) {
	f.listCalls = append(f.listCalls, in)
	if f.listErr != nil {
		return ListTasksResult{}, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeTaskStore) GetTask(_ context.Context, taskID string) (*Task, error) {
	f.getCalls = append(f.getCalls, taskID)
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getTask, nil
}

type fakeTaskQueue struct {
	messages []TaskQueueMessage
	err      error
}

func (f *fakeTaskQueue) Enqueue(_ context.Context, message TaskQueueMessage) error {
	f.messages = append(f.messages, message)
	return f.err
}

func TestCreateTaskReturns202AndTaskID(t *testing.T) {
	repo := &fakeTaskStore{
		createdTask: Task{
			ID:           "task-v2-created",
			URL:          "https://telegra.ph/demo",
			CanonicalURL: stringPtr("https://telegra.ph/demo"),
			Status:       "QUEUED",
		},
	}
	queue := &fakeTaskQueue{}
	handler := NewRouter(repo, queue)

	req := httptest.NewRequest(http.MethodPost, "/v2/tasks", strings.NewReader(`{"url":"https://telegra.ph/demo"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(repo.createCalls) != 1 {
		t.Fatalf("expected one create call, got %d", len(repo.createCalls))
	}
	if repo.createCalls[0].URL != "https://telegra.ph/demo" {
		t.Fatalf("expected create url to be forwarded, got %q", repo.createCalls[0].URL)
	}
	if len(queue.messages) != 1 {
		t.Fatalf("expected one queue message, got %d", len(queue.messages))
	}
	if queue.messages[0].TaskID != "task-v2-created" {
		t.Fatalf("expected queue task id task-v2-created, got %q", queue.messages[0].TaskID)
	}
	if strings.TrimSpace(queue.messages[0].Token) == "" {
		t.Fatalf("expected non-empty queue token")
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["task_id"] != "task-v2-created" {
		t.Fatalf("expected task_id task-v2-created, got %#v", payload["task_id"])
	}
	if payload["status"] != "QUEUED" {
		t.Fatalf("expected status QUEUED, got %#v", payload["status"])
	}
}

func TestListTasksReturnsPagination(t *testing.T) {
	repo := &fakeTaskStore{
		listResult: ListTasksResult{
			Tasks: []Task{
				{
					ID:           "task-v2-page-2",
					URL:          "https://telegra.ph/page-2",
					CanonicalURL: stringPtr("https://telegra.ph/page-2"),
					Status:       "RUNNING",
				},
			},
			Total:      3,
			Page:       2,
			PerPage:    1,
			TotalPages: 3,
		},
	}
	handler := NewRouter(repo, &fakeTaskQueue{})

	req := httptest.NewRequest(http.MethodGet, "/v2/tasks?page=2&per_page=1&status=RUNNING&q=page", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(repo.listCalls) != 1 {
		t.Fatalf("expected one list call, got %d", len(repo.listCalls))
	}
	query := repo.listCalls[0]
	if query.Page != 2 {
		t.Fatalf("expected page=2, got %d", query.Page)
	}
	if query.PerPage != 1 {
		t.Fatalf("expected per_page=1, got %d", query.PerPage)
	}
	if query.Status != "RUNNING" {
		t.Fatalf("expected status RUNNING, got %q", query.Status)
	}
	if query.Query != "page" {
		t.Fatalf("expected q=page, got %q", query.Query)
	}

	var payload struct {
		Tasks      []Task `json:"tasks"`
		Total      int    `json:"total"`
		Page       int    `json:"page"`
		PerPage    int    `json:"per_page"`
		TotalPages int    `json:"total_pages"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if payload.Total != 3 {
		t.Fatalf("expected total=3, got %d", payload.Total)
	}
	if payload.Page != 2 {
		t.Fatalf("expected page=2, got %d", payload.Page)
	}
	if payload.PerPage != 1 {
		t.Fatalf("expected per_page=1, got %d", payload.PerPage)
	}
	if payload.TotalPages != 3 {
		t.Fatalf("expected total_pages=3, got %d", payload.TotalPages)
	}
	if len(payload.Tasks) != 1 {
		t.Fatalf("expected one task row, got %d", len(payload.Tasks))
	}
	if payload.Tasks[0].ID != "task-v2-page-2" {
		t.Fatalf("expected task id task-v2-page-2, got %q", payload.Tasks[0].ID)
	}
}

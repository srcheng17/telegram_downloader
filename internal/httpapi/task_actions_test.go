package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/httpv2"
	"github.com/ryancheng/telegram-downloader/internal/service"
)

func TestRetryUploadTaskRequeuesFailedTaskWithoutCreatingNewID(t *testing.T) {
	store := &fakeLegacyV2Store{
		getTask: &httpv2.Task{
			ID:                "task-upload-retry",
			Status:            httpv2.TaskStatusFailed,
			TaskType:          stringPtr("upload"),
			SourceArchivePath: stringPtr("/tmp/task-upload-retry/source.zip"),
			Retryable:         true,
		},
	}
	queue := &fakeLegacyV2Queue{}
	handler := NewRouterWithOptions(&fakeTaskReader{}, RouterOptions{
		V2TaskStore: store,
		V2TaskQueue: queue,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-upload-retry/retry", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(store.retryUploadCalls) != 1 {
		t.Fatalf("expected one retry upload call, got %d", len(store.retryUploadCalls))
	}
	if store.retryUploadCalls[0].taskID != "task-upload-retry" {
		t.Fatalf("unexpected retried task id %#v", store.retryUploadCalls[0])
	}
	if len(queue.calls) != 1 || queue.calls[0].TaskID != "task-upload-retry" {
		t.Fatalf("expected re-enqueue of task-upload-retry, got %#v", queue.calls)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode retry response: %v", err)
	}
	if payload["task_id"] != "task-upload-retry" {
		t.Fatalf("expected response task_id task-upload-retry, got %#v", payload["task_id"])
	}
}

func TestRetryUrlTaskRequeuesFailedTaskWithoutCreatingNewID(t *testing.T) {
	store := &fakeLegacyV2Store{
		getTask: &httpv2.Task{
			ID:        "task-url-retry",
			Status:    httpv2.TaskStatusFailed,
			TaskType:  stringPtr("url"),
			URL:       "https://telegra.ph/retry-me",
			Retryable: true,
		},
	}
	queue := &fakeLegacyV2Queue{}
	handler := NewRouterWithOptions(&fakeTaskReader{}, RouterOptions{
		V2TaskStore: store,
		V2TaskQueue: queue,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-url-retry/retry", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(store.retryUploadCalls) != 1 {
		t.Fatalf("expected one retry call, got %d", len(store.retryUploadCalls))
	}
	if store.retryUploadCalls[0].taskID != "task-url-retry" {
		t.Fatalf("unexpected retried task id %#v", store.retryUploadCalls[0])
	}
	if len(queue.calls) != 1 || queue.calls[0].TaskID != "task-url-retry" {
		t.Fatalf("expected re-enqueue of task-url-retry, got %#v", queue.calls)
	}
}

func TestCopyToKomgaCopiesArtifactIntoSeriesFolder(t *testing.T) {
	downloadRoot := t.TempDir()
	komgaRoot := t.TempDir()
	artifactPath := filepath.Join(downloadRoot, "demo.cbz")
	if err := os.WriteFile(artifactPath, []byte("cbz-data"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	store := &fakeLegacyV2Store{
		getTask: &httpv2.Task{
			ID:            "task-copy-komga",
			Status:        httpv2.TaskStatusSuccess,
			ResultZipPath: &artifactPath,
			SeriesName:    stringPtr("系列A"),
		},
	}
	artifactService := service.NewV2ArtifactService(service.V2ArtifactServiceConfig{DownloadRoot: downloadRoot})
	handler := NewRouterWithOptions(&fakeTaskReader{}, RouterOptions{
		V2TaskStore:       store,
		V2TaskQueue:       &fakeLegacyV2Queue{},
		V2ArtifactService: artifactService,
		KomgaRootDir:      komgaRoot,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-copy-komga/copy-to-komga", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	targetPath := filepath.Join(komgaRoot, "系列A", "demo.cbz")
	if content, err := os.ReadFile(targetPath); err != nil {
		t.Fatalf("read copied artifact: %v", err)
	} else if string(content) != "cbz-data" {
		t.Fatalf("unexpected copied artifact content %q", string(content))
	}
}

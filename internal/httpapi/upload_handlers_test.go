package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/httpv2"
)

func TestHandleUploadInitCreatesUploadingTaskAndHistoryEntry(t *testing.T) {
	store := &fakeLegacyV2Store{
		createUploadTask: httpv2.Task{
			ID:                "task-upload-init",
			Status:            httpv2.TaskStatusUploading,
			TaskType:          stringPtr("upload"),
			SourceArchiveName: stringPtr("demo.7z"),
		},
	}
	handler := NewRouterWithOptions(&fakeTaskReader{}, RouterOptions{
		V2TaskStore: store,
		V2TaskQueue: &fakeLegacyV2Queue{},
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/tasks/upload/init",
		strings.NewReader(`{"file_name":"demo.7z","file_size":40,"author":"作者A","series_name":"系列B","comic_name":"漫画C","tags":"剧情 # 动作","genres":"青年 ＃ 悬疑"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(store.createUploadCalls) != 1 {
		t.Fatalf("expected one create upload call, got %d", len(store.createUploadCalls))
	}
	if store.createUploadCalls[0].TaskType == nil || *store.createUploadCalls[0].TaskType != "upload" {
		t.Fatalf("expected task_type upload, got %#v", store.createUploadCalls[0].TaskType)
	}
	if len(store.metadataHistoryInsertCalls) != 1 {
		t.Fatalf("expected one metadata history insert, got %d", len(store.metadataHistoryInsertCalls))
	}
	if store.metadataHistoryInsertCalls[0].TaskType != "upload" {
		t.Fatalf("expected metadata history task_type upload, got %q", store.metadataHistoryInsertCalls[0].TaskType)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["task_id"] != "task-upload-init" {
		t.Fatalf("expected task_id task-upload-init, got %#v", payload["task_id"])
	}
	if _, ok := payload["upload_token"].(string); !ok {
		t.Fatalf("expected upload_token string, got %#v", payload["upload_token"])
	}
	if payload["upload_url"] != "/api/tasks/task-upload-init/upload-source" {
		t.Fatalf("expected upload_url for task-upload-init, got %#v", payload["upload_url"])
	}
}

func TestHandleUploadSourceStreamsBytesAndTransitionsToQueued(t *testing.T) {
	tempDir := t.TempDir()
	store := &fakeLegacyV2Store{
		getTask: &httpv2.Task{
			ID:                "task-upload-source",
			Status:            httpv2.TaskStatusUploading,
			TaskType:          stringPtr("upload"),
			SourceArchiveName: stringPtr("demo.zip"),
			UploadTotalBytes:  8,
		},
	}
	queue := &fakeLegacyV2Queue{}
	handler := NewRouterWithOptions(&fakeTaskReader{}, RouterOptions{
		V2TaskStore:   store,
		V2TaskQueue:   queue,
		UploadTempDir: tempDir,
	})

	req := httptest.NewRequest(
		http.MethodPut,
		"/api/tasks/task-upload-source/upload-source",
		bytes.NewReader([]byte("zip-data")),
	)
	req.Header.Set("X-Upload-Token", "upload-token-1")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(store.uploadProgressCalls) == 0 {
		t.Fatalf("expected upload progress updates")
	}
	if len(store.markUploadQueuedCalls) != 1 {
		t.Fatalf("expected one mark upload queued call, got %d", len(store.markUploadQueuedCalls))
	}
	targetPath := filepath.Join(tempDir, "task-upload-source", "source.zip")
	if store.markUploadQueuedCalls[0].path != targetPath {
		t.Fatalf("expected queued path %q, got %q", targetPath, store.markUploadQueuedCalls[0].path)
	}
	if content, err := os.ReadFile(targetPath); err != nil {
		t.Fatalf("read uploaded source: %v", err)
	} else if string(content) != "zip-data" {
		t.Fatalf("expected uploaded content zip-data, got %q", string(content))
	}
	if len(queue.calls) != 1 {
		t.Fatalf("expected one queue call, got %d", len(queue.calls))
	}
	if queue.calls[0].TaskID != "task-upload-source" || queue.calls[0].Token != "upload-token-1" {
		t.Fatalf("unexpected queue call: %#v", queue.calls[0])
	}
}

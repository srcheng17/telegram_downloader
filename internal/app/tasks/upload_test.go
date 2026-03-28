package tasks

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeUploadStore struct {
	createUploadTask    UploadTaskRecord
	createUploadCalls   []UploadInitRecord
	getTask             *UploadTaskRecord
	getErr              error
	progressCalls       []uploadProgressRecord
	queuedTaskID        string
	queuedPath          string
	queuedBytes         int64
	markFailedTaskID    string
	markFailedMessage   string
	metadataHistory     []MetadataHistoryRecord
}

type uploadProgressRecord struct {
	taskID string
	loaded int64
	total  int64
}

func (f *fakeUploadStore) CreateUploadTask(_ context.Context, in UploadInitRecord) (UploadTaskRecord, error) {
	f.createUploadCalls = append(f.createUploadCalls, in)
	if f.createUploadTask.ID != "" {
		return f.createUploadTask, nil
	}
	return UploadTaskRecord{
		ID:                in.ID,
		Status:            "UPLOADING",
		TaskType:          "upload",
		SourceArchiveName: in.SourceArchiveName,
		UploadTotalBytes:  in.UploadTotalBytes,
	}, nil
}

func (f *fakeUploadStore) InsertMetadataHistory(_ context.Context, in MetadataHistoryRecord) error {
	f.metadataHistory = append(f.metadataHistory, in)
	return nil
}

func (f *fakeUploadStore) GetUploadTask(_ context.Context, taskID string) (*UploadTaskRecord, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getTask == nil {
		return nil, nil
	}
	copy := *f.getTask
	copy.ID = taskID
	return &copy, nil
}

func (f *fakeUploadStore) UpdateUploadProgress(_ context.Context, taskID string, loadedBytes, totalBytes int64) error {
	f.progressCalls = append(f.progressCalls, uploadProgressRecord{taskID: taskID, loaded: loadedBytes, total: totalBytes})
	return nil
}

func (f *fakeUploadStore) MarkUploadTaskQueued(_ context.Context, taskID, sourceArchivePath string, totalBytes int64) error {
	f.queuedTaskID = taskID
	f.queuedPath = sourceArchivePath
	f.queuedBytes = totalBytes
	return nil
}

func (f *fakeUploadStore) MarkTaskFailed(_ context.Context, taskID, message string) error {
	f.markFailedTaskID = taskID
	f.markFailedMessage = message
	return nil
}

type fakeUploadQueue struct {
	messages []QueueMessage
	err      error
}

func (f *fakeUploadQueue) Enqueue(_ context.Context, msg QueueMessage) error {
	if f.err != nil {
		return f.err
	}
	f.messages = append(f.messages, msg)
	return nil
}

func TestUploadInitServiceCreatesUploadingTaskAndMetadataHistory(t *testing.T) {
	store := &fakeUploadStore{
		createUploadTask: UploadTaskRecord{ID: "task-upload-init", Status: "UPLOADING"},
	}
	svc := NewUploadInitService(store)
	svc.IDGenerator = func() string { return "task-upload-init" }
	svc.TokenGenerator = func() string { return "upload-token-1" }

	result, err := svc.Init(context.Background(), UploadInitInput{
		FileName: "demo.7z",
		FileSize: 40,
		Metadata: MetadataInput{
			Author:     stringPtr("作者A"),
			SeriesName: stringPtr("系列B"),
			ComicName:  stringPtr("漫画C"),
			TagsRaw:    stringPtr("剧情 # 动作"),
			GenresRaw:  stringPtr("青年 ＃ 悬疑"),
		},
	})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if result.TaskID != "task-upload-init" || result.UploadToken != "upload-token-1" {
		t.Fatalf("unexpected init result %#v", result)
	}
	if len(store.createUploadCalls) != 1 {
		t.Fatalf("expected one create upload call, got %d", len(store.createUploadCalls))
	}
	if len(store.metadataHistory) != 1 || store.metadataHistory[0].TaskType != "upload" {
		t.Fatalf("expected upload metadata history entry, got %#v", store.metadataHistory)
	}
}

func TestUploadSourceServiceStoresArchiveAndQueuesTask(t *testing.T) {
	tempDir := t.TempDir()
	store := &fakeUploadStore{
		getTask: &UploadTaskRecord{
			ID:                "task-upload-source",
			Status:            "UPLOADING",
			TaskType:          "upload",
			SourceArchiveName: stringPtr("demo.zip"),
			UploadTotalBytes:  8,
		},
	}
	queue := &fakeUploadQueue{}
	svc := NewUploadSourceService(store, queue, tempDir)

	result, err := svc.Attach(context.Background(), UploadSourceInput{
		TaskID:      "task-upload-source",
		UploadToken: "upload-token-1",
		Body:        bytes.NewReader([]byte("zip-data")),
		ContentSize: 8,
	})
	if err != nil {
		t.Fatalf("Attach returned error: %v", err)
	}
	if result.TaskID != "task-upload-source" || result.Status != StatusQueued {
		t.Fatalf("unexpected attach result %#v", result)
	}
	targetPath := filepath.Join(tempDir, "task-upload-source", "source.zip")
	if store.queuedPath != targetPath {
		t.Fatalf("expected queued path %q, got %q", targetPath, store.queuedPath)
	}
	if content, err := os.ReadFile(targetPath); err != nil {
		t.Fatalf("read uploaded file: %v", err)
	} else if string(content) != "zip-data" {
		t.Fatalf("unexpected uploaded content %q", string(content))
	}
	if len(queue.messages) != 1 || queue.messages[0].TaskID != "task-upload-source" || queue.messages[0].Token != "upload-token-1" {
		t.Fatalf("unexpected queue messages %#v", queue.messages)
	}
}

func TestUploadSourceServiceMarksTaskFailedWhenEnqueueFails(t *testing.T) {
	tempDir := t.TempDir()
	store := &fakeUploadStore{
		getTask: &UploadTaskRecord{
			ID:                "task-upload-source",
			Status:            "UPLOADING",
			TaskType:          "upload",
			SourceArchiveName: stringPtr("demo.zip"),
		},
	}
	queue := &fakeUploadQueue{err: errors.New("queue unavailable")}
	svc := NewUploadSourceService(store, queue, tempDir)

	_, err := svc.Attach(context.Background(), UploadSourceInput{
		TaskID:      "task-upload-source",
		UploadToken: "upload-token-1",
		Body:        bytes.NewReader([]byte("zip-data")),
		ContentSize: 8,
	})
	if err == nil {
		t.Fatalf("expected enqueue failure")
	}
	if store.markFailedTaskID != "task-upload-source" || store.markFailedMessage == "" {
		t.Fatalf("expected mark failed on enqueue error, got %#v", store)
	}
}

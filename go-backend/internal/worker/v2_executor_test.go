package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres"
)

func TestExecutorMarksSuccessAndStoresArtifact(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-success"] = v2ExecutorTaskRecord{
		id:           "task-success",
		url:          "https://telegra.ph/demo-success",
		status:       "QUEUED",
		enqueueToken: "token-success",
	}

	downloader := &fakeV2ExecutorDownloader{artifactPath: "/tmp/task-success.cbz"}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:     repo,
		Worker:   "worker-v2-a",
		Download: downloader,
	})

	err := executor.Execute(context.Background(), "task-success", "token-success")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if downloader.calls != 1 {
		t.Fatalf("expected downloader called once, got %d", downloader.calls)
	}
	if downloader.lastURL != "https://telegra.ph/demo-success" {
		t.Fatalf("expected downloader url https://telegra.ph/demo-success, got %q", downloader.lastURL)
	}
	if len(repo.statusUpdates) != 2 {
		t.Fatalf("expected 2 status updates (RUNNING and SUCCESS), got %d", len(repo.statusUpdates))
	}

	running := repo.statusUpdates[0]
	if running.from != "QUEUED" || running.to != "RUNNING" {
		t.Fatalf("expected QUEUED->RUNNING, got %s->%s", running.from, running.to)
	}
	if running.patch.ClaimedBy == nil || *running.patch.ClaimedBy != "worker-v2-a" {
		t.Fatalf("expected claimed_by worker-v2-a, got %#v", running.patch.ClaimedBy)
	}

	success := repo.statusUpdates[1]
	if success.from != "RUNNING" || success.to != "SUCCESS" {
		t.Fatalf("expected RUNNING->SUCCESS, got %s->%s", success.from, success.to)
	}
	if success.patch.ResultZipPath == nil || *success.patch.ResultZipPath != "/tmp/task-success.cbz" {
		t.Fatalf("expected result path /tmp/task-success.cbz, got %#v", success.patch.ResultZipPath)
	}
	if success.patch.Error != nil {
		t.Fatalf("expected success error nil, got %#v", success.patch.Error)
	}

	if len(repo.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(repo.events))
	}
	if repo.events[0].fromStatus == nil || *repo.events[0].fromStatus != "QUEUED" || repo.events[0].toStatus == nil || *repo.events[0].toStatus != "RUNNING" {
		t.Fatalf("expected first event QUEUED->RUNNING, got %#v", repo.events[0])
	}
	if repo.events[1].fromStatus == nil || *repo.events[1].fromStatus != "RUNNING" || repo.events[1].toStatus == nil || *repo.events[1].toStatus != "SUCCESS" {
		t.Fatalf("expected second event RUNNING->SUCCESS, got %#v", repo.events[1])
	}

	persisted := repo.tasks["task-success"]
	if persisted.status != "SUCCESS" {
		t.Fatalf("expected persisted status SUCCESS, got %q", persisted.status)
	}
	if persisted.resultZipPath == nil || *persisted.resultZipPath != "/tmp/task-success.cbz" {
		t.Fatalf("expected persisted artifact path, got %#v", persisted.resultZipPath)
	}
}

func TestExecutorMarksFailedAfterRetryExhausted(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-failed"] = v2ExecutorTaskRecord{
		id:           "task-failed",
		url:          "https://telegra.ph/demo-failed",
		status:       "QUEUED",
		enqueueToken: "token-failed",
	}

	downloader := &fakeV2ExecutorDownloader{err: errRetryableDownload}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:           repo,
		Worker:         "worker-v2-b",
		Download:       downloader,
		TransientRetry: 2,
	})

	err := executor.Execute(context.Background(), "task-failed", "token-failed")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if downloader.calls != 3 {
		t.Fatalf("expected downloader called 3 times (1+2 retries), got %d", downloader.calls)
	}
	if len(repo.statusUpdates) != 2 {
		t.Fatalf("expected 2 status updates (RUNNING and FAILED), got %d", len(repo.statusUpdates))
	}

	failed := repo.statusUpdates[1]
	if failed.from != "RUNNING" || failed.to != "FAILED" {
		t.Fatalf("expected RUNNING->FAILED, got %s->%s", failed.from, failed.to)
	}
	if failed.patch.Error == nil || !strings.Contains(*failed.patch.Error, "temporary upstream failure") {
		t.Fatalf("expected failed patch error to contain retry cause, got %#v", failed.patch.Error)
	}
	if failed.patch.ResultZipPath != nil {
		t.Fatalf("expected failed result path nil, got %#v", failed.patch.ResultZipPath)
	}

	if len(repo.events) != 2 {
		t.Fatalf("expected 2 transition events, got %d", len(repo.events))
	}
	if repo.events[1].fromStatus == nil || *repo.events[1].fromStatus != "RUNNING" || repo.events[1].toStatus == nil || *repo.events[1].toStatus != "FAILED" {
		t.Fatalf("expected event RUNNING->FAILED, got %#v", repo.events[1])
	}

	persisted := repo.tasks["task-failed"]
	if persisted.status != "FAILED" {
		t.Fatalf("expected persisted status FAILED, got %q", persisted.status)
	}
	if persisted.error == nil || !strings.Contains(*persisted.error, "temporary upstream failure") {
		t.Fatalf("expected persisted error to include retry cause, got %#v", persisted.error)
	}
}

type fakeV2ExecutorDownloader struct {
	artifactPath string
	err          error

	calls   int
	lastURL string
}

func (f *fakeV2ExecutorDownloader) DownloadAndPackage(_ context.Context, taskID, pageURL string) (string, error) {
	f.calls++
	f.lastURL = pageURL
	if f.err != nil {
		return "", f.err
	}
	if f.artifactPath != "" {
		return f.artifactPath, nil
	}
	return "/tmp/" + taskID + ".cbz", nil
}

type fakeV2ExecutorRepo struct {
	tasks         map[string]v2ExecutorTaskRecord
	statusUpdates []v2ExecutorStatusUpdate
	events        []v2ExecutorTaskEvent
}

type v2ExecutorTaskRecord struct {
	id            string
	url           string
	status        string
	enqueueToken  string
	error         *string
	resultZipPath *string
	claimedBy     *string
}

type v2ExecutorStatusUpdate struct {
	id    string
	from  string
	to    string
	patch postgres.StatusPatch
}

type v2ExecutorTaskEvent struct {
	taskID      string
	fromStatus  *string
	toStatus    *string
	payloadJSON string
}

func newFakeV2ExecutorRepo() *fakeV2ExecutorRepo {
	return &fakeV2ExecutorRepo{tasks: map[string]v2ExecutorTaskRecord{}}
}

func (f *fakeV2ExecutorRepo) GetTaskForExecution(_ context.Context, taskID string) (V2TaskSnapshot, error) {
	task, ok := f.tasks[taskID]
	if !ok {
		return V2TaskSnapshot{}, errors.New("task not found")
	}
	return V2TaskSnapshot{
		ID:           task.id,
		URL:          task.url,
		Status:       task.status,
		EnqueueToken: task.enqueueToken,
	}, nil
}

func (f *fakeV2ExecutorRepo) UpdateTaskStatus(_ context.Context, id string, from, to string, patch postgres.StatusPatch) error {
	task, ok := f.tasks[id]
	if !ok {
		return errors.New("task not found")
	}
	if task.status != from {
		return postgres.ErrV2TaskStatusMismatchOrNotFound
	}

	task.status = to
	if patch.Error != nil {
		message := *patch.Error
		task.error = &message
	} else {
		task.error = nil
	}
	if patch.ResultZipPath != nil {
		path := *patch.ResultZipPath
		task.resultZipPath = &path
	} else {
		task.resultZipPath = nil
	}
	if patch.ClaimedBy != nil {
		worker := *patch.ClaimedBy
		task.claimedBy = &worker
	}
	f.tasks[id] = task

	f.statusUpdates = append(f.statusUpdates, v2ExecutorStatusUpdate{
		id:    id,
		from:  from,
		to:    to,
		patch: patch,
	})
	return nil
}

func (f *fakeV2ExecutorRepo) AppendTaskEvent(_ context.Context, event postgres.TaskEvent) error {
	f.events = append(f.events, v2ExecutorTaskEvent{
		taskID:      event.TaskID,
		fromStatus:  cloneStringForV2Executor(event.FromStatus),
		toStatus:    cloneStringForV2Executor(event.ToStatus),
		payloadJSON: event.PayloadJSON,
	})
	return nil
}

func cloneStringForV2Executor(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

var errRetryableDownload = &temporaryExecutionNetError{message: "temporary upstream failure"}

type temporaryExecutionNetError struct {
	message string
}

func (e *temporaryExecutionNetError) Error() string {
	return e.message
}

func (e *temporaryExecutionNetError) Timeout() bool {
	return true
}

func (e *temporaryExecutionNetError) Temporary() bool {
	return true
}

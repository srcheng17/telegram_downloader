package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/downloader"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
)

type fakeRunTaskRepo struct {
	snapshot       RunTaskSnapshot
	transitions    []postgres.TransitionTaskWithEventInput
	heartbeatCalls int
}

func (f *fakeRunTaskRepo) GetTaskForExecution(context.Context, string) (RunTaskSnapshot, error) {
	return f.snapshot, nil
}

func (f *fakeRunTaskRepo) UpdateTaskHeartbeat(context.Context, string, string) error {
	f.heartbeatCalls++
	return nil
}

func (f *fakeRunTaskRepo) TransitionTaskWithEvent(_ context.Context, in postgres.TransitionTaskWithEventInput) error {
	f.transitions = append(f.transitions, in)
	f.snapshot.Status = in.ToStatus
	if in.Patch.SourceArchivePath != nil {
		f.snapshot.SourceArchivePath = in.Patch.SourceArchivePath
	}
	if in.Patch.ClearSourceArchivePath {
		f.snapshot.SourceArchivePath = nil
	}
	return nil
}

type fakeRunTaskDownloader struct {
	artifactPath string
	calls        int
	lastSource   string
}

func (f *fakeRunTaskDownloader) DownloadAndPackage(_ context.Context, taskID, source string, _ downloader.TaskMetadata) (string, error) {
	f.calls++
	return f.downloadAndPackage(taskID, source)
}

func (f *fakeRunTaskDownloader) downloadAndPackage(taskID, source string) (string, error) {
	f.lastSource = source
	return f.artifactPath, nil
}

func TestRunTaskTransitionsQueuedToSuccess(t *testing.T) {
	repo := &fakeRunTaskRepo{snapshot: RunTaskSnapshot{
		ID:           "task-1",
		URL:          "https://telegra.ph/demo-run-task",
		Status:       StatusQueued,
		EnqueueToken: "token-1",
	}}
	downloader := &fakeRunTaskDownloader{artifactPath: "/tmp/task-1.cbz"}
	runner := NewRunTaskUseCase(RunTaskConfig{
		Repo:              repo,
		Worker:            "worker-run-task",
		Download:          downloader,
		HeartbeatInterval: time.Second,
	})

	if err := runner.Execute(context.Background(), "task-1", "token-1"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if downloader.calls != 1 {
		t.Fatalf("expected downloader called once, got %d", downloader.calls)
	}
	if len(repo.transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(repo.transitions))
	}
	if repo.transitions[0].ToStatus != StatusRunning {
		t.Fatalf("expected first transition to RUNNING, got %q", repo.transitions[0].ToStatus)
	}
	if repo.transitions[1].ToStatus != StatusSuccess {
		t.Fatalf("expected second transition to SUCCESS, got %q", repo.transitions[1].ToStatus)
	}
}

func TestRunTaskUploadUsesSourceArchivePathAndClearsItOnSuccess(t *testing.T) {
	repo := &fakeRunTaskRepo{snapshot: RunTaskSnapshot{
		ID:                "task-upload-1",
		Status:            StatusQueued,
		EnqueueToken:      "token-upload-1",
		TaskType:          strPtr("upload"),
		SourceArchivePath: strPtr("/tmp/task-upload-1/source.zip"),
	}}
	downloader := &fakeRunTaskDownloader{artifactPath: "/tmp/task-upload-1.cbz"}
	runner := NewRunTaskUseCase(RunTaskConfig{
		Repo:              repo,
		Worker:            "worker-upload",
		Download:          downloader,
		HeartbeatInterval: time.Second,
	})

	if err := runner.Execute(context.Background(), "task-upload-1", "token-upload-1"); err != nil {
		t.Fatalf("execute upload task: %v", err)
	}
	if downloader.calls != 1 {
		t.Fatalf("expected downloader called once, got %d", downloader.calls)
	}
	if downloader.lastSource != "/tmp/task-upload-1/source.zip" {
		t.Fatalf("expected source archive path /tmp/task-upload-1/source.zip, got %q", downloader.lastSource)
	}
	if len(repo.transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(repo.transitions))
	}
	if !repo.transitions[1].Patch.ClearSourceArchivePath {
		t.Fatalf("expected success patch to clear source archive path")
	}
	if repo.transitions[1].Patch.Retryable == nil || *repo.transitions[1].Patch.Retryable {
		t.Fatalf("expected success patch retryable=false, got %#v", repo.transitions[1].Patch.Retryable)
	}
}

func TestBuildCanceledStatusPatchKeepsTaskRetryable(t *testing.T) {
	patch := buildCanceledStatusPatch(RunTaskSnapshot{TaskType: strPtr("upload")}, "Cancellation requested by user.")
	if patch.Retryable == nil || !*patch.Retryable {
		t.Fatalf("expected canceled patch retryable=true, got %#v", patch.Retryable)
	}
	if patch.ClearSourceArchivePath {
		t.Fatalf("expected canceled patch not to clear source archive path")
	}
}

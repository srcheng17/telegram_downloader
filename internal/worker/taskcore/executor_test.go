package taskcore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
)

func TestExecutorCompletesClaimedTask(t *testing.T) {
	svc := &fakeService{claim: &app.Task{ID: "task-1", Attempt: 1, Generation: 1}}
	downloader := &fakeDownloader{path: fakeArtifact(t)}
	executor := Executor{Service: svc, Downloader: downloader, WorkerID: "worker-a", HeartbeatInterval: time.Hour}

	processed, err := executor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne processed=%v err=%v", processed, err)
	}
	if svc.completed.WorkerID != "worker-a" || svc.completed.Attempt != 1 {
		t.Fatalf("complete = %#v", svc.completed)
	}
	if svc.completed.TaskID != "task-1" || svc.completed.ArtifactPath != downloader.path || svc.completed.ArtifactName != "out.cbz" {
		t.Fatalf("complete artifact = %#v", svc.completed)
	}
}

func TestExecutorFailsClaimedTaskOnDownloaderError(t *testing.T) {
	svc := &fakeService{claim: &app.Task{ID: "task-1", Attempt: 1, Generation: 1}}
	downloader := &fakeDownloader{err: errors.New("download failed")}
	executor := Executor{Service: svc, Downloader: downloader, WorkerID: "worker-a", HeartbeatInterval: time.Hour}

	processed, err := executor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne processed=%v err=%v", processed, err)
	}
	if svc.failed.Message != "download failed" {
		t.Fatalf("fail = %#v", svc.failed)
	}
	if svc.failed.TaskID != "task-1" || svc.failed.WorkerID != "worker-a" || svc.failed.Attempt != 1 {
		t.Fatalf("fail metadata = %#v", svc.failed)
	}
}

func TestExecutorAcknowledgesCancelOnHeartbeatSignal(t *testing.T) {
	svc := &fakeService{claim: &app.Task{ID: "task-1", Attempt: 1, Generation: 1}, heartbeat: app.HeartbeatResult{CancelRequested: true}}
	downloader := &blockingDownloader{}
	executor := Executor{Service: svc, Downloader: downloader, WorkerID: "worker-a", HeartbeatInterval: time.Millisecond}

	processed, err := executor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne processed=%v err=%v", processed, err)
	}
	if !svc.cancelAcknowledged {
		t.Fatalf("expected cancel acknowledgement")
	}
	if svc.cancelTaskID != "task-1" || svc.cancelWorkerID != "worker-a" || svc.cancelAttempt != 1 {
		t.Fatalf("cancel acknowledgement = taskID %q workerID %q attempt %d", svc.cancelTaskID, svc.cancelWorkerID, svc.cancelAttempt)
	}
	if !downloader.cancelObserved {
		t.Fatalf("downloader should observe cancellation")
	}
}

func TestExecutorAcknowledgesCancelWhenSuccessResultRacesWithCancel(t *testing.T) {
	svc := &fakeService{
		claim:     &app.Task{ID: "task-1", Attempt: 1, Generation: 1},
		heartbeat: app.HeartbeatResult{CancelRequested: true},
	}
	downloader := &fakeDownloader{path: fakeArtifact(t)}
	svc.onCancel = func() {
		if _, err := os.Stat(downloader.path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("cancel acknowledged before artifact cleanup: %v", err)
		}
	}
	executor := Executor{Service: svc, Downloader: downloader, WorkerID: "worker-a", HeartbeatInterval: time.Hour}

	processed, err := executor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne processed=%v err=%v", processed, err)
	}
	if !svc.cancelAcknowledged {
		t.Fatalf("expected cancel acknowledgement")
	}
	if svc.completed.TaskID != "" {
		t.Fatalf("task should not complete after cancel race: %#v", svc.completed)
	}
}

func TestExecutorAcknowledgesCancelWhenCompleteConflictsAfterCancel(t *testing.T) {
	svc := &fakeService{
		claim: &app.Task{ID: "task-1", Attempt: 1, Generation: 1},
		heartbeats: []app.HeartbeatResult{
			{CancelRequested: false},
			{CancelRequested: true},
		},
		completeErr: app.ErrConflict,
	}
	downloader := &fakeDownloader{path: fakeArtifact(t)}
	svc.onCancel = func() {
		if _, err := os.Stat(downloader.path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("complete conflict cancel acknowledged before artifact cleanup: %v", err)
		}
	}
	executor := Executor{Service: svc, Downloader: downloader, WorkerID: "worker-a", HeartbeatInterval: time.Hour}

	processed, err := executor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne processed=%v err=%v", processed, err)
	}
	if !svc.cancelAcknowledged {
		t.Fatalf("expected cancel acknowledgement")
	}
	if svc.completed.TaskID != "task-1" {
		t.Fatalf("expected Complete to race and return conflict, got %#v", svc.completed)
	}
}

func TestExecutorAcknowledgesCancelWhenFailConflictsAfterCancel(t *testing.T) {
	svc := &fakeService{
		claim: &app.Task{ID: "task-1", Attempt: 1, Generation: 1},
		heartbeats: []app.HeartbeatResult{
			{CancelRequested: false},
			{CancelRequested: true},
		},
		failErr: app.ErrConflict,
	}
	downloader := &fakeDownloader{err: errors.New("download failed")}
	executor := Executor{Service: svc, Downloader: downloader, WorkerID: "worker-a", HeartbeatInterval: time.Hour}

	processed, err := executor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne processed=%v err=%v", processed, err)
	}
	if !svc.cancelAcknowledged {
		t.Fatalf("expected cancel acknowledgement")
	}
	if svc.failed.TaskID != "task-1" {
		t.Fatalf("expected Fail to race and return conflict, got %#v", svc.failed)
	}
}

func TestExecutorRejectsClaimedTaskMissingRequiredFields(t *testing.T) {
	svc := &fakeService{claim: &app.Task{Attempt: 1, Generation: 1}}
	downloader := &fakeDownloader{path: fakeArtifact(t)}
	executor := Executor{Service: svc, Downloader: downloader, WorkerID: "worker-a"}

	processed, err := executor.ProcessOne(context.Background())
	if err == nil || processed {
		t.Fatalf("ProcessOne processed=%v err=%v, want validation error", processed, err)
	}
	if downloader.called {
		t.Fatalf("downloader should not be called for invalid claimed task")
	}
}

func TestExecutorDoesNothingWhenNoTaskClaimed(t *testing.T) {
	svc := &fakeService{}
	downloader := &fakeDownloader{path: fakeArtifact(t)}
	executor := Executor{Service: svc, Downloader: downloader, WorkerID: "worker-a"}

	processed, err := executor.ProcessOne(context.Background())
	if err != nil || processed {
		t.Fatalf("ProcessOne processed=%v err=%v", processed, err)
	}
	if downloader.called {
		t.Fatalf("downloader should not be called")
	}
}

type fakeService struct {
	onCancel           func()
	claim              *app.Task
	heartbeat          app.HeartbeatResult
	heartbeats         []app.HeartbeatResult
	completeErr        error
	failErr            error
	completed          app.CompleteInput
	failed             app.FailInput
	cancelAcknowledged bool
	cancelTaskID       string
	cancelWorkerID     string
	cancelAttempt      int
}

func (s *fakeService) ClaimNext(ctx context.Context, workerID string) (app.ClaimResult, error) {
	return app.ClaimResult{Task: s.claim}, nil
}

func (s *fakeService) Heartbeat(ctx context.Context, taskID string, workerID string, generation int64) (app.HeartbeatResult, error) {
	if len(s.heartbeats) > 0 {
		heartbeat := s.heartbeats[0]
		s.heartbeats = s.heartbeats[1:]
		return heartbeat, nil
	}
	return s.heartbeat, nil
}

func (s *fakeService) Complete(ctx context.Context, in app.CompleteInput) error {
	s.completed = in
	return s.completeErr
}

func (s *fakeService) Fail(ctx context.Context, in app.FailInput) error {
	s.failed = in
	return s.failErr
}

func (s *fakeService) AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int, generation int64) error {
	if s.onCancel != nil {
		s.onCancel()
	}
	s.cancelAcknowledged = true
	s.cancelTaskID = taskID
	s.cancelWorkerID = workerID
	s.cancelAttempt = attempt
	return nil
}

func (s *fakeService) RecoverExpired(ctx context.Context) (app.RecoveryResult, error) {
	return app.RecoveryResult{}, nil
}

type fakeDownloader struct {
	path   string
	err    error
	called bool
}

func (d *fakeDownloader) Execute(ctx context.Context, task app.Task) (string, error) {
	d.called = true
	return d.path, d.err
}

type blockingDownloader struct {
	cancelObserved bool
}

func (d *blockingDownloader) Execute(ctx context.Context, task app.Task) (string, error) {
	<-ctx.Done()
	d.cancelObserved = true
	return "", ctx.Err()
}

func fakeArtifact(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out.cbz")
	if err := os.WriteFile(path, []byte("artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExecutorWaitsForExecutionExitBeforeAcknowledgingCancel(t *testing.T) {
	ack := make(chan struct{})
	svc := &fakeService{claim: &app.Task{ID: "task-a", Attempt: 1, Generation: 1}, heartbeat: app.HeartbeatResult{CancelRequested: true}}
	d := &gatedCancelDownloader{path: fakeArtifact(t), cancelObserved: make(chan struct{}), release: make(chan struct{})}
	svc.onCancel = func() {
		if _, err := os.Stat(d.path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("heartbeat cancel acknowledged before artifact cleanup: %v", err)
		}
		close(ack)
	}
	executor := Executor{Service: svc, Downloader: d, WorkerID: "worker-a", HeartbeatInterval: time.Millisecond}
	done := make(chan error, 1)
	go func() { _, err := executor.ProcessOne(context.Background()); done <- err }()
	select {
	case <-d.cancelObserved:
	case <-time.After(time.Second):
		t.Fatal("execution never canceled")
	}
	select {
	case <-ack:
		close(d.release)
		t.Fatal("cancel acknowledged before execution exited")
	case <-time.After(150 * time.Millisecond):
	}
	close(d.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("executor did not finish")
	}
	select {
	case <-ack:
	default:
		t.Fatal("cancel not acknowledged after exit")
	}
	if _, err := os.Stat(d.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled artifact not cleaned: %v", err)
	}
}

type gatedCancelDownloader struct {
	path                    string
	cancelObserved, release chan struct{}
}

func (d *gatedCancelDownloader) Execute(ctx context.Context, _ app.Task) (string, error) {
	<-ctx.Done()
	close(d.cancelObserved)
	<-d.release
	return d.path, ctx.Err()
}

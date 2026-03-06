package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	queuev2 "github.com/ryancheng/telegram-downloader/go-backend/internal/queue/v2"
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

func TestExecutorDoesNotLeavePartialStateWhenEventWriteFails(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-atomic"] = v2ExecutorTaskRecord{
		id:           "task-atomic",
		url:          "https://telegra.ph/demo-atomic",
		status:       "QUEUED",
		enqueueToken: "token-atomic",
	}
	repo.eventErrByToStatus["RUNNING"] = errors.New("append event failed")

	executor := NewV2Executor(V2ExecutorConfig{
		Repo:     repo,
		Worker:   "worker-v2-atomic",
		Download: &fakeV2ExecutorDownloader{artifactPath: "/tmp/task-atomic.cbz"},
	})

	err := executor.Execute(context.Background(), "task-atomic", "token-atomic")
	if err == nil {
		t.Fatalf("expected execute to fail when event write fails")
	}

	persisted := repo.tasks["task-atomic"]
	if persisted.status != "QUEUED" {
		t.Fatalf("expected atomic rollback to keep QUEUED, got %q", persisted.status)
	}
	if len(repo.statusUpdates) != 0 {
		t.Fatalf("expected no persisted status update on event failure, got %d", len(repo.statusUpdates))
	}
}

func TestExecutorUsesIndependentTerminalContextWhenRunContextCanceled(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-terminal"] = v2ExecutorTaskRecord{
		id:           "task-terminal",
		url:          "https://telegra.ph/demo-terminal",
		status:       "QUEUED",
		enqueueToken: "token-terminal",
	}
	repo.failTerminalWhenCtxCanceled = true

	runCtx, cancel := context.WithCancel(context.Background())
	downloader := &fakeV2ExecutorDownloader{
		executeFn: func(context.Context, string, string) (string, error) {
			cancel()
			return "", errors.New("download interrupted")
		},
	}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:     repo,
		Worker:   "worker-v2-terminal",
		Download: downloader,
	})

	err := executor.Execute(runCtx, "task-terminal", "token-terminal")
	if err != nil {
		t.Fatalf("expected terminal transition to use independent context, got %v", err)
	}

	persisted := repo.tasks["task-terminal"]
	if persisted.status != "FAILED" {
		t.Fatalf("expected terminal status FAILED, got %q", persisted.status)
	}
}

func TestRunRetriesExecuteUntilSuccess(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-run-retry"] = v2ExecutorTaskRecord{
		id:           "task-run-retry",
		url:          "https://telegra.ph/demo-retry-run",
		status:       "QUEUED",
		enqueueToken: "token-run-retry",
	}
	repo.getTaskErrSequence = []error{errors.New("temporary repo failure")}

	downloader := &fakeV2ExecutorDownloader{artifactPath: "/tmp/task-run-retry.cbz"}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:     repo,
		Worker:   "worker-v2-run",
		Download: downloader,
	})

	consumer := &fakeV2QueueConsumer{
		reads: []fakeV2ReadResult{
			{messages: []queuev2.TaskMessage{{TaskID: "task-run-retry", Token: "token-run-retry"}}},
			{err: context.Canceled},
		},
	}

	err := executor.Run(context.Background(), consumer)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if repo.getTaskCalls != 2 {
		t.Fatalf("expected execute retried once in run loop, got getTaskCalls=%d", repo.getTaskCalls)
	}
	if downloader.calls != 1 {
		t.Fatalf("expected downloader to run once after retry recovery, got %d", downloader.calls)
	}
	persisted := repo.tasks["task-run-retry"]
	if persisted.status != "SUCCESS" {
		t.Fatalf("expected final status SUCCESS after run retry, got %q", persisted.status)
	}
}

func TestRunRetryRecoversRunningTaskAfterTerminalTransitionFailure(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-running-recovery"] = v2ExecutorTaskRecord{
		id:           "task-running-recovery",
		url:          "https://telegra.ph/demo-running-recovery",
		status:       "QUEUED",
		enqueueToken: "token-running-recovery",
	}
	repo.eventErrCountByToStatus["FAILED"] = 1

	downloader := &fakeV2ExecutorDownloader{err: errors.New("non-retryable execution failure")}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:     repo,
		Worker:   "worker-v2-running-recovery",
		Download: downloader,
	})

	consumer := &fakeV2QueueConsumer{
		reads: []fakeV2ReadResult{
			{messages: []queuev2.TaskMessage{{TaskID: "task-running-recovery", Token: "token-running-recovery"}}},
			{err: context.Canceled},
		},
	}

	err := executor.Run(context.Background(), consumer)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	persisted := repo.tasks["task-running-recovery"]
	if persisted.status != "FAILED" {
		t.Fatalf("expected run retry to recover RUNNING task into FAILED, got %q", persisted.status)
	}
	if len(repo.statusUpdates) != 2 {
		t.Fatalf("expected QUEUED->RUNNING and RUNNING->FAILED updates, got %d", len(repo.statusUpdates))
	}
	if repo.statusUpdates[1].from != "RUNNING" || repo.statusUpdates[1].to != "FAILED" {
		t.Fatalf("expected second update RUNNING->FAILED, got %s->%s", repo.statusUpdates[1].from, repo.statusUpdates[1].to)
	}
	if repo.getTaskCalls != 2 {
		t.Fatalf("expected execute retried after terminal failure, got getTaskCalls=%d", repo.getTaskCalls)
	}
}

func TestExecuteFirstAttemptSkipsRunningTaskToAvoidConcurrentReexecution(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-running-first"] = v2ExecutorTaskRecord{
		id:           "task-running-first",
		url:          "https://telegra.ph/demo-running-first",
		status:       "RUNNING",
		enqueueToken: "token-running-first",
	}

	downloader := &fakeV2ExecutorDownloader{artifactPath: "/tmp/task-running-first.cbz"}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:     repo,
		Worker:   "worker-v2-first",
		Download: downloader,
	})

	err := executor.Execute(context.Background(), "task-running-first", "token-running-first")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if downloader.calls != 0 {
		t.Fatalf("expected first-attempt RUNNING task to skip downloader, got %d calls", downloader.calls)
	}
	if len(repo.statusUpdates) != 0 {
		t.Fatalf("expected no transition for first-attempt RUNNING task, got %d", len(repo.statusUpdates))
	}
}

func TestExecuteIgnoresTerminalTaskWithEmptyURL(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-terminal-empty-url"] = v2ExecutorTaskRecord{
		id:           "task-terminal-empty-url",
		url:          "",
		status:       "FAILED",
		enqueueToken: "token-terminal-empty-url",
	}

	downloader := &fakeV2ExecutorDownloader{artifactPath: "/tmp/should-not-run.cbz"}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:     repo,
		Worker:   "worker-v2-terminal-empty-url",
		Download: downloader,
	})

	err := executor.Execute(context.Background(), "task-terminal-empty-url", "token-terminal-empty-url")
	if err != nil {
		t.Fatalf("expected terminal task to be ignored without URL validation noise, got %v", err)
	}
	if downloader.calls != 0 {
		t.Fatalf("expected terminal task to skip downloader, got %d calls", downloader.calls)
	}
}

func TestRunRetryAfterPreflightFailureDoesNotAllowRunningRecovery(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-preflight-running"] = v2ExecutorTaskRecord{
		id:           "task-preflight-running",
		url:          "https://telegra.ph/demo-preflight-running",
		status:       "QUEUED",
		enqueueToken: "token-preflight-running",
	}
	repo.getTaskErrSequence = []error{errors.New("temporary repo read error")}
	repo.getTaskStatusSequence = []string{"RUNNING"}

	downloader := &fakeV2ExecutorDownloader{artifactPath: "/tmp/task-preflight-running.cbz"}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:          repo,
		Worker:        "worker-v2-preflight",
		Download:      downloader,
		RunRetryCount: 1,
	})

	consumer := &fakeV2QueueConsumer{
		reads: []fakeV2ReadResult{
			{messages: []queuev2.TaskMessage{{TaskID: "task-preflight-running", Token: "token-preflight-running"}}},
			{err: context.Canceled},
		},
	}

	err := executor.Run(context.Background(), consumer)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if repo.getTaskCalls != 2 {
		t.Fatalf("expected run retry twice, got getTaskCalls=%d", repo.getTaskCalls)
	}
	if downloader.calls != 0 {
		t.Fatalf("expected preflight-failure retry not to run downloader against RUNNING task, got %d calls", downloader.calls)
	}
	if len(repo.statusUpdates) != 0 {
		t.Fatalf("expected no status transitions in preflight failure scenario, got %d", len(repo.statusUpdates))
	}
}

type fakeV2ExecutorDownloader struct {
	artifactPath string
	err          error
	executeFn    func(ctx context.Context, taskID, pageURL string) (string, error)

	calls   int
	lastURL string
}

func (f *fakeV2ExecutorDownloader) DownloadAndPackage(ctx context.Context, taskID, pageURL string) (string, error) {
	f.calls++
	f.lastURL = pageURL
	if f.executeFn != nil {
		return f.executeFn(ctx, taskID, pageURL)
	}
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

	getTaskErrSequence          []error
	getTaskStatusSequence       []string
	eventErrByToStatus          map[string]error
	eventErrCountByToStatus     map[string]int
	failTerminalWhenCtxCanceled bool
	getTaskCalls                int
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
	return &fakeV2ExecutorRepo{
		tasks:                   map[string]v2ExecutorTaskRecord{},
		eventErrByToStatus:      map[string]error{},
		eventErrCountByToStatus: map[string]int{},
	}
}

func (f *fakeV2ExecutorRepo) GetTaskForExecution(_ context.Context, taskID string) (V2TaskSnapshot, error) {
	f.getTaskCalls++
	if len(f.getTaskErrSequence) > 0 {
		err := f.getTaskErrSequence[0]
		f.getTaskErrSequence = f.getTaskErrSequence[1:]
		if err != nil {
			return V2TaskSnapshot{}, err
		}
	}

	task, ok := f.tasks[taskID]
	if !ok {
		return V2TaskSnapshot{}, errors.New("task not found")
	}
	status := task.status
	if len(f.getTaskStatusSequence) > 0 {
		status = f.getTaskStatusSequence[0]
		f.getTaskStatusSequence = f.getTaskStatusSequence[1:]
	}
	return V2TaskSnapshot{
		ID:           task.id,
		URL:          task.url,
		Status:       status,
		EnqueueToken: task.enqueueToken,
	}, nil
}

func (f *fakeV2ExecutorRepo) TransitionTaskWithEvent(
	ctx context.Context,
	in postgres.TransitionTaskWithEventInput,
) error {
	task, ok := f.tasks[in.TaskID]
	if !ok {
		return errors.New("task not found")
	}
	if task.status != in.FromStatus {
		return postgres.ErrV2TaskStatusMismatchOrNotFound
	}
	if f.failTerminalWhenCtxCanceled && ctx.Err() != nil && (in.ToStatus == "SUCCESS" || in.ToStatus == "FAILED") {
		return ctx.Err()
	}
	toStatus := strings.TrimSpace(in.ToStatus)
	if remaining := f.eventErrCountByToStatus[toStatus]; remaining > 0 {
		f.eventErrCountByToStatus[toStatus] = remaining - 1
		if err, ok := f.eventErrByToStatus[toStatus]; ok {
			return err
		}
		return errors.New("simulated transition failure")
	}
	if err, ok := f.eventErrByToStatus[toStatus]; ok {
		return err
	}

	task.status = in.ToStatus
	if in.Patch.Error != nil {
		message := *in.Patch.Error
		task.error = &message
	} else {
		task.error = nil
	}
	if in.Patch.ResultZipPath != nil {
		path := *in.Patch.ResultZipPath
		task.resultZipPath = &path
	} else {
		task.resultZipPath = nil
	}
	if in.Patch.ClaimedBy != nil {
		worker := *in.Patch.ClaimedBy
		task.claimedBy = &worker
	}
	f.tasks[in.TaskID] = task

	f.statusUpdates = append(f.statusUpdates, v2ExecutorStatusUpdate{
		id:    in.TaskID,
		from:  in.FromStatus,
		to:    in.ToStatus,
		patch: in.Patch,
	})
	fromStatus := in.FromStatus
	toStatusCopy := in.ToStatus
	f.events = append(f.events, v2ExecutorTaskEvent{
		taskID:      in.TaskID,
		fromStatus:  &fromStatus,
		toStatus:    &toStatusCopy,
		payloadJSON: in.PayloadJSON,
	})
	return nil
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

type fakeV2ReadResult struct {
	messages []queuev2.TaskMessage
	err      error
}

type fakeV2QueueConsumer struct {
	reads []fakeV2ReadResult
	index int
}

func (f *fakeV2QueueConsumer) Read(_ context.Context, _ int64, _ time.Duration) ([]queuev2.TaskMessage, error) {
	if f.index >= len(f.reads) {
		return nil, context.Canceled
	}
	result := f.reads[f.index]
	f.index++
	if result.messages == nil {
		return []queuev2.TaskMessage{}, result.err
	}
	return result.messages, result.err
}

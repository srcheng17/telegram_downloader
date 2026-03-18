package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/downloader"
	queuev2 "github.com/ryancheng/telegram-downloader/internal/queue/v2"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
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

func TestExecutorPassesSnapshotMetadataToDownloader(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-meta"] = v2ExecutorTaskRecord{
		id:               "task-meta",
		url:              "https://telegra.ph/demo-meta",
		status:           "QUEUED",
		enqueueToken:     "token-meta",
		author:           stringPtr("作者A"),
		seriesName:       stringPtr("系列B"),
		comicName:        stringPtr("漫画C"),
		summary:          stringPtr("简介D"),
		tagsNormalized:   stringPtr("tag1,tag2"),
		genresNormalized: stringPtr("genre1"),
	}
	downloader := &fakeV2ExecutorDownloader{artifactPath: "/tmp/task-meta.cbz"}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:     repo,
		Worker:   "worker-v2-meta",
		Download: downloader,
	})

	if err := executor.Execute(context.Background(), "task-meta", "token-meta"); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if downloader.lastMetadata.Writer != "作者A" {
		t.Fatalf("expected writer 作者A, got %q", downloader.lastMetadata.Writer)
	}
	if downloader.lastMetadata.Series != "系列B" {
		t.Fatalf("expected series 系列B, got %q", downloader.lastMetadata.Series)
	}
	if downloader.lastMetadata.Title != "漫画C" {
		t.Fatalf("expected title 漫画C, got %q", downloader.lastMetadata.Title)
	}
	if downloader.lastMetadata.Summary != "简介D" {
		t.Fatalf("expected summary 简介D, got %q", downloader.lastMetadata.Summary)
	}
	if downloader.lastMetadata.Tags != "tag1,tag2" {
		t.Fatalf("expected tags tag1,tag2, got %q", downloader.lastMetadata.Tags)
	}
	if downloader.lastMetadata.Genre != "genre1" {
		t.Fatalf("expected genre genre1, got %q", downloader.lastMetadata.Genre)
	}
}

func TestExecutorPassesUploadSourcePathToDownloader(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-upload-meta"] = v2ExecutorTaskRecord{
		id:                "task-upload-meta",
		status:            "QUEUED",
		enqueueToken:      "token-upload-meta",
		taskType:          stringPtr("upload"),
		sourceArchivePath: stringPtr("/tmp/task-upload-meta/source.zip"),
		sourceArchiveName: stringPtr("source.zip"),
		author:            stringPtr("作者A"),
	}
	downloader := &fakeV2ExecutorDownloader{artifactPath: "/tmp/task-upload-meta.cbz"}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:     repo,
		Worker:   "worker-v2-upload",
		Download: downloader,
	})

	if err := executor.Execute(context.Background(), "task-upload-meta", "token-upload-meta"); err != nil {
		t.Fatalf("execute upload task: %v", err)
	}

	if downloader.lastURL != "/tmp/task-upload-meta/source.zip" {
		t.Fatalf("expected upload source path /tmp/task-upload-meta/source.zip, got %q", downloader.lastURL)
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
		executeFn: func(context.Context, string, string, downloader.TaskMetadata) (string, error) {
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
			{messages: []queuev2.TaskMessage{{MessageID: "1-0", TaskID: "task-running-recovery", Token: "token-running-recovery"}}},
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

func TestRunAcksMessageAfterSuccessfulExecution(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-run-ack"] = v2ExecutorTaskRecord{
		id:           "task-run-ack",
		url:          "https://telegra.ph/demo-run-ack",
		status:       "QUEUED",
		enqueueToken: "token-run-ack",
	}

	downloader := &fakeV2ExecutorDownloader{artifactPath: "/tmp/task-run-ack.cbz"}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:     repo,
		Worker:   "worker-v2-run-ack",
		Download: downloader,
	})

	consumer := &fakeV2QueueConsumer{
		reads: []fakeV2ReadResult{
			{messages: []queuev2.TaskMessage{{MessageID: "10-0", TaskID: "task-run-ack", Token: "token-run-ack"}}},
			{err: context.Canceled},
		},
	}

	err := executor.Run(context.Background(), consumer)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(consumer.ackedMessageIDs) != 1 {
		t.Fatalf("expected one ack call, got %d", len(consumer.ackedMessageIDs))
	}
	if consumer.ackedMessageIDs[0] != "10-0" {
		t.Fatalf("expected acked message id 10-0, got %#v", consumer.ackedMessageIDs)
	}
}

func TestRunProcessesClaimedPendingMessageBeforeReadGroup(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-run-claimed"] = v2ExecutorTaskRecord{
		id:           "task-run-claimed",
		url:          "https://telegra.ph/demo-run-claimed",
		status:       "QUEUED",
		enqueueToken: "token-run-claimed",
	}

	downloader := &fakeV2ExecutorDownloader{artifactPath: "/tmp/task-run-claimed.cbz"}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:     repo,
		Worker:   "worker-v2-run-claimed",
		Download: downloader,
	})

	consumer := &fakeV2QueueConsumer{
		claims: []fakeV2ReadResult{
			{messages: []queuev2.TaskMessage{{MessageID: "11-0", TaskID: "task-run-claimed", Token: "token-run-claimed"}}},
			{err: context.Canceled},
		},
	}

	err := executor.Run(context.Background(), consumer)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if consumer.readCalls != 0 {
		t.Fatalf("expected no ReadGroup calls when pending claim provided work, got %d", consumer.readCalls)
	}
	if len(consumer.ackedMessageIDs) != 1 || consumer.ackedMessageIDs[0] != "11-0" {
		t.Fatalf("expected reclaimed message to be acked, got %#v", consumer.ackedMessageIDs)
	}
	if downloader.calls != 1 {
		t.Fatalf("expected downloader called once for reclaimed task, got %d", downloader.calls)
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

func TestRunRetryKeepsRunningRecoveryStickyAcrossPreflightFailure(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-running-sticky"] = v2ExecutorTaskRecord{
		id:           "task-running-sticky",
		url:          "https://telegra.ph/demo-running-sticky",
		status:       "QUEUED",
		enqueueToken: "token-running-sticky",
	}
	repo.eventErrCountByToStatus["FAILED"] = 1
	repo.getTaskErrSequence = []error{nil, errors.New("temporary get task jitter"), nil}

	downloader := &fakeV2ExecutorDownloader{err: errors.New("non-retryable execution failure")}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:          repo,
		Worker:        "worker-v2-sticky",
		Download:      downloader,
		RunRetryCount: 2,
	})

	consumer := &fakeV2QueueConsumer{
		reads: []fakeV2ReadResult{
			{messages: []queuev2.TaskMessage{{TaskID: "task-running-sticky", Token: "token-running-sticky"}}},
			{err: context.Canceled},
		},
	}

	err := executor.Run(context.Background(), consumer)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if repo.getTaskCalls != 3 {
		t.Fatalf("expected three execute attempts, got getTaskCalls=%d", repo.getTaskCalls)
	}
	if downloader.calls != 2 {
		t.Fatalf("expected downloader to run on attempt 1 and 3, got %d calls", downloader.calls)
	}
	persisted := repo.tasks["task-running-sticky"]
	if persisted.status != "FAILED" {
		t.Fatalf("expected sticky running recovery to finish FAILED, got %q", persisted.status)
	}
	if len(repo.statusUpdates) != 2 {
		t.Fatalf("expected QUEUED->RUNNING then RUNNING->FAILED, got %d updates", len(repo.statusUpdates))
	}
}

func TestExecutorStopsRunningTaskWhenCancelRequested(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-cancel-requested"] = v2ExecutorTaskRecord{
		id:           "task-cancel-requested",
		url:          "https://telegra.ph/demo-cancel-requested",
		status:       "QUEUED",
		enqueueToken: "token-cancel-requested",
	}

	downloader := &fakeV2ExecutorDownloader{
		executeFn: func(_ context.Context, taskID, _ string, _ downloader.TaskMetadata) (string, error) {
			task := repo.tasks[taskID]
			task.status = "CANCEL_REQUESTED"
			repo.tasks[taskID] = task
			return "", context.Canceled
		},
	}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:     repo,
		Worker:   "worker-v2-cancel",
		Download: downloader,
	})

	err := executor.Execute(context.Background(), "task-cancel-requested", "token-cancel-requested")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(repo.statusUpdates) != 2 {
		t.Fatalf("expected QUEUED->RUNNING then CANCEL_REQUESTED->CANCELED transitions, got %d", len(repo.statusUpdates))
	}
	second := repo.statusUpdates[1]
	if second.from != "CANCEL_REQUESTED" || second.to != "CANCELED" {
		t.Fatalf("expected CANCEL_REQUESTED->CANCELED transition, got %s->%s", second.from, second.to)
	}
	persisted := repo.tasks["task-cancel-requested"]
	if persisted.status != "CANCELED" {
		t.Fatalf("expected persisted status CANCELED, got %q", persisted.status)
	}
}

func TestExecutorMonitorStopsDownloadWhenCancelRequested(t *testing.T) {
	repo := newFakeV2ExecutorRepo()
	repo.tasks["task-cancel-monitor"] = v2ExecutorTaskRecord{
		id:           "task-cancel-monitor",
		url:          "https://telegra.ph/demo-cancel-monitor",
		status:       "QUEUED",
		enqueueToken: "token-cancel-monitor",
	}
	repo.cancelRequestedAfterHeartbeat = 1

	downloader := &fakeV2ExecutorDownloader{
		executeFn: func(ctx context.Context, _ string, _ string, _ downloader.TaskMetadata) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	executor := NewV2Executor(V2ExecutorConfig{
		Repo:              repo,
		Worker:            "worker-v2-cancel-monitor",
		Download:          downloader,
		HeartbeatInterval: 5 * time.Millisecond,
	})

	err := executor.Execute(context.Background(), "task-cancel-monitor", "token-cancel-monitor")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if repo.heartbeatCalls == 0 {
		t.Fatalf("expected monitor heartbeat updates")
	}
	if len(repo.statusUpdates) != 2 {
		t.Fatalf("expected QUEUED->RUNNING then CANCEL_REQUESTED->CANCELED transitions, got %d", len(repo.statusUpdates))
	}
	if repo.statusUpdates[1].from != "CANCEL_REQUESTED" || repo.statusUpdates[1].to != "CANCELED" {
		t.Fatalf("expected second transition CANCEL_REQUESTED->CANCELED, got %s->%s", repo.statusUpdates[1].from, repo.statusUpdates[1].to)
	}
	persisted := repo.tasks["task-cancel-monitor"]
	if persisted.status != "CANCELED" {
		t.Fatalf("expected persisted status CANCELED, got %q", persisted.status)
	}
}

type fakeV2ExecutorDownloader struct {
	artifactPath string
	err          error
	executeFn    func(ctx context.Context, taskID, pageURL string, metadata downloader.TaskMetadata) (string, error)

	calls        int
	lastURL      string
	lastMetadata downloader.TaskMetadata
}

func (f *fakeV2ExecutorDownloader) DownloadAndPackage(
	ctx context.Context,
	taskID,
	pageURL string,
	metadata downloader.TaskMetadata,
) (string, error) {
	f.calls++
	f.lastURL = pageURL
	f.lastMetadata = metadata
	if f.executeFn != nil {
		return f.executeFn(ctx, taskID, pageURL, metadata)
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

	getTaskErrSequence            []error
	getTaskStatusSequence         []string
	eventErrByToStatus            map[string]error
	eventErrCountByToStatus       map[string]int
	failTerminalWhenCtxCanceled   bool
	getTaskCalls                  int
	heartbeatCalls                int
	cancelRequestedAfterHeartbeat int
}

type v2ExecutorTaskRecord struct {
	id                string
	url               string
	status            string
	enqueueToken      string
	taskType          *string
	sourceArchivePath *string
	sourceArchiveName *string
	error             *string
	resultZipPath     *string
	claimedBy         *string
	author            *string
	seriesName        *string
	comicName         *string
	summary           *string
	tagsRaw           *string
	tagsNormalized    *string
	genresRaw         *string
	genresNormalized  *string
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
		ID:                task.id,
		URL:               task.url,
		Status:            status,
		EnqueueToken:      task.enqueueToken,
		TaskType:          task.taskType,
		SourceArchivePath: task.sourceArchivePath,
		SourceArchiveName: task.sourceArchiveName,
		Author:            task.author,
		SeriesName:        task.seriesName,
		ComicName:         task.comicName,
		Summary:           task.summary,
		TagsRaw:           task.tagsRaw,
		TagsNormalized:    task.tagsNormalized,
		GenresRaw:         task.genresRaw,
		GenresNormalized:  task.genresNormalized,
	}, nil
}

func (f *fakeV2ExecutorRepo) UpdateTaskHeartbeat(_ context.Context, taskID, worker string) error {
	f.heartbeatCalls++
	task, ok := f.tasks[taskID]
	if !ok {
		return errors.New("task not found")
	}
	if task.status != "RUNNING" && task.status != "CANCEL_REQUESTED" {
		return nil
	}
	if task.claimedBy != nil && *task.claimedBy != strings.TrimSpace(worker) {
		return nil
	}
	if f.cancelRequestedAfterHeartbeat > 0 && f.heartbeatCalls >= f.cancelRequestedAfterHeartbeat {
		task.status = "CANCEL_REQUESTED"
		f.tasks[taskID] = task
	}
	return nil
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
	reads  []fakeV2ReadResult
	claims []fakeV2ReadResult

	readIndex  int
	claimIndex int
	readCalls  int
	claimCalls int

	ackedMessageIDs []string
	ackErr          error
}

func (f *fakeV2QueueConsumer) ReadGroup(_ context.Context, _ int64, _ time.Duration) ([]queuev2.TaskMessage, error) {
	f.readCalls++
	if f.readIndex >= len(f.reads) {
		return nil, context.Canceled
	}
	result := f.reads[f.readIndex]
	f.readIndex++
	if result.messages == nil {
		return []queuev2.TaskMessage{}, result.err
	}
	return result.messages, result.err
}

func (f *fakeV2QueueConsumer) ClaimPending(_ context.Context, _ time.Duration, _ int64) ([]queuev2.TaskMessage, error) {
	f.claimCalls++
	if f.claimIndex >= len(f.claims) {
		return []queuev2.TaskMessage{}, nil
	}
	result := f.claims[f.claimIndex]
	f.claimIndex++
	if result.messages == nil {
		return []queuev2.TaskMessage{}, result.err
	}
	return result.messages, result.err
}

func (f *fakeV2QueueConsumer) Ack(_ context.Context, messageIDs ...string) error {
	for _, id := range messageIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		f.ackedMessageIDs = append(f.ackedMessageIDs, trimmed)
	}
	return f.ackErr
}

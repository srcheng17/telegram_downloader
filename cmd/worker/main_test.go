package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	apptaskcore "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	queuev2 "github.com/ryancheng/telegram-downloader/internal/queue/v2"
	workertaskcore "github.com/ryancheng/telegram-downloader/internal/worker/taskcore"
)

func TestEnsureV2ConsumerGroupStartsAtZeroToConsumeBacklog(t *testing.T) {
	ctx := context.Background()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.XAdd(ctx, &redis.XAddArgs{
		Stream: "download_tasks_v2",
		Values: map[string]any{
			"task_id": "task-backlog-1",
			"token":   "token-backlog-1",
		},
	}).Result()
	if err != nil {
		t.Fatalf("seed v2 stream backlog: %v", err)
	}

	if err := ensureV2ConsumerGroup(ctx, client, "download_tasks_v2", "go-workers"); err != nil {
		t.Fatalf("ensure v2 consumer group: %v", err)
	}

	consumer := queuev2.NewConsumer(client, "download_tasks_v2", "go-workers", "worker-a")
	messages, err := consumer.ReadGroup(ctx, 1, time.Millisecond)
	if err != nil {
		t.Fatalf("read group: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one backlog message, got %d", len(messages))
	}
	if messages[0].TaskID != "task-backlog-1" {
		t.Fatalf("expected task_id task-backlog-1, got %q", messages[0].TaskID)
	}
}

func TestRunTaskCoreExecutorLogsAndContinuesAfterProcessError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := &taskCoreLoopFakeService{claimErr: errors.New("claim failed")}
	executor := &workertaskcore.Executor{
		Service:    svc,
		Downloader: &taskCoreLoopFakeDownloader{},
		WorkerID:   "worker-a",
	}

	done := make(chan error, 1)
	go func() { done <- runTaskCoreExecutor(ctx, executor) }()

	select {
	case err := <-done:
		t.Fatalf("runTaskCoreExecutor returned %v before context cancellation", err)
	case <-time.After(20 * time.Millisecond):
		cancel()
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runTaskCoreExecutor returned %v after context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runTaskCoreExecutor did not stop after context cancellation")
	}
}

func TestRunTaskCoreRecoveryLogsAndContinuesAfterRecoverError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := &taskCoreLoopFakeService{recoverErr: errors.New("recover failed")}
	executor := &workertaskcore.Executor{Service: svc}

	done := make(chan error, 1)
	go func() { done <- runTaskCoreRecovery(ctx, executor, time.Millisecond) }()

	select {
	case err := <-done:
		t.Fatalf("runTaskCoreRecovery returned %v before context cancellation", err)
	case <-time.After(20 * time.Millisecond):
		cancel()
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runTaskCoreRecovery returned %v after context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runTaskCoreRecovery did not stop after context cancellation")
	}
}

type taskCoreLoopFakeService struct {
	claimErr   error
	recoverErr error
}

func (s *taskCoreLoopFakeService) ClaimNext(ctx context.Context, workerID string) (apptaskcore.ClaimResult, error) {
	return apptaskcore.ClaimResult{}, s.claimErr
}

func (s *taskCoreLoopFakeService) Heartbeat(ctx context.Context, taskID string, workerID string) (apptaskcore.HeartbeatResult, error) {
	return apptaskcore.HeartbeatResult{}, nil
}

func (s *taskCoreLoopFakeService) Complete(ctx context.Context, in apptaskcore.CompleteInput) error {
	return nil
}

func (s *taskCoreLoopFakeService) Fail(ctx context.Context, in apptaskcore.FailInput) error {
	return nil
}

func (s *taskCoreLoopFakeService) AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int) error {
	return nil
}

func (s *taskCoreLoopFakeService) RecoverExpired(ctx context.Context) (apptaskcore.RecoveryResult, error) {
	return apptaskcore.RecoveryResult{}, s.recoverErr
}

type taskCoreLoopFakeDownloader struct{}

func (d *taskCoreLoopFakeDownloader) Execute(ctx context.Context, taskID string) (string, error) {
	return "", nil
}

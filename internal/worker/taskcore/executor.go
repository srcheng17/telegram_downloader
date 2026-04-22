package taskcore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
)

const defaultHeartbeatInterval = 5 * time.Second

type Service interface {
	ClaimNext(ctx context.Context, workerID string) (app.ClaimResult, error)
	Heartbeat(ctx context.Context, taskID string, workerID string) (app.HeartbeatResult, error)
	Complete(ctx context.Context, in app.CompleteInput) error
	Fail(ctx context.Context, in app.FailInput) error
	AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int) error
	RecoverExpired(ctx context.Context) (app.RecoveryResult, error)
}

type Downloader interface {
	Execute(ctx context.Context, taskID string) (string, error)
}

type Executor struct {
	Service           Service
	Downloader        Downloader
	WorkerID          string
	HeartbeatInterval time.Duration
}

type executionResult struct {
	path string
	err  error
}

func (e *Executor) ProcessOne(ctx context.Context) (bool, error) {
	if err := e.validate(); err != nil {
		return false, err
	}

	claim, err := e.Service.ClaimNext(ctx, e.WorkerID)
	if err != nil || claim.Task == nil {
		return false, err
	}
	task := claim.Task
	if strings.TrimSpace(task.ID) == "" || task.Attempt <= 0 {
		return false, errors.New("task core executor claimed task requires id and attempt")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCh := make(chan executionResult, 1)
	go func() {
		path, runErr := e.Downloader.Execute(runCtx, task.ID)
		resultCh <- executionResult{path: path, err: runErr}
	}()

	ticker := time.NewTicker(e.heartbeatEvery())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			cancel()
			return true, ctx.Err()
		case result := <-resultCh:
			if acknowledged, err := e.acknowledgeCancelIfRequested(ctx, task.ID, task.Attempt); err != nil || acknowledged {
				return true, err
			}

			if result.err != nil {
				failErr := e.Service.Fail(ctx, app.FailInput{TaskID: task.ID, WorkerID: e.WorkerID, Attempt: task.Attempt, Message: result.err.Error()})
				return true, e.resolveReportError(ctx, task.ID, task.Attempt, failErr)
			}
			name := filepath.Base(result.path)
			completeErr := e.Service.Complete(ctx, app.CompleteInput{TaskID: task.ID, WorkerID: e.WorkerID, Attempt: task.Attempt, ArtifactPath: result.path, ArtifactName: name, ArtifactSize: 0})
			return true, e.resolveReportError(ctx, task.ID, task.Attempt, completeErr)
		case <-ticker.C:
			heartbeat, err := e.Service.Heartbeat(ctx, task.ID, e.WorkerID)
			if err != nil {
				cancel()
				return true, err
			}
			if heartbeat.CancelRequested {
				cancel()
				ackErr := e.Service.AcknowledgeCancel(ctx, task.ID, e.WorkerID, task.Attempt)
				e.drainResult(resultCh)
				return true, ackErr
			}
		}
	}
}

func (e *Executor) RecoverExpired(ctx context.Context) (app.RecoveryResult, error) {
	if e == nil || e.Service == nil {
		return app.RecoveryResult{}, errors.New("task core executor requires service")
	}
	return e.Service.RecoverExpired(ctx)
}

func (e *Executor) validate() error {
	if e == nil || e.Service == nil || e.Downloader == nil || strings.TrimSpace(e.WorkerID) == "" {
		return errors.New("task core executor requires service, downloader, and worker id")
	}
	return nil
}

func (e *Executor) acknowledgeCancelIfRequested(ctx context.Context, taskID string, attempt int) (bool, error) {
	heartbeat, err := e.Service.Heartbeat(ctx, taskID, e.WorkerID)
	if err != nil {
		return false, err
	}
	if !heartbeat.CancelRequested {
		return false, nil
	}
	return true, e.Service.AcknowledgeCancel(ctx, taskID, e.WorkerID, attempt)
}

func (e *Executor) resolveReportError(ctx context.Context, taskID string, attempt int, reportErr error) error {
	if reportErr == nil {
		return nil
	}
	if !errors.Is(reportErr, app.ErrConflict) {
		return reportErr
	}

	heartbeat, err := e.Service.Heartbeat(ctx, taskID, e.WorkerID)
	if err != nil || !heartbeat.CancelRequested {
		return reportErr
	}
	return e.Service.AcknowledgeCancel(ctx, taskID, e.WorkerID, attempt)
}

func (e *Executor) heartbeatEvery() time.Duration {
	if e.HeartbeatInterval <= 0 {
		return defaultHeartbeatInterval
	}
	return e.HeartbeatInterval
}

func (e *Executor) drainResult(resultCh <-chan executionResult) {
	select {
	case <-resultCh:
	case <-time.After(100 * time.Millisecond):
	}
}

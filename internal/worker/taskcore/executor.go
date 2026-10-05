package taskcore

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
)

const defaultHeartbeatInterval = 5 * time.Second

type Service interface {
	ClaimNext(ctx context.Context, workerID string) (app.ClaimResult, error)
	Heartbeat(ctx context.Context, taskID string, workerID string, generation int64) (app.HeartbeatResult, error)
	Complete(ctx context.Context, in app.CompleteInput) error
	Fail(ctx context.Context, in app.FailInput) error
	AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int, generation int64) error
	RecoverExpired(ctx context.Context) (app.RecoveryResult, error)
}

type Downloader interface {
	Execute(ctx context.Context, task app.Task) (string, error)
}

type Executor struct {
	Service           Service
	Downloader        Downloader
	WorkerID          string
	HeartbeatInterval time.Duration
}

type executionResult struct {
	path     string
	err      error
	metadata ExecutionOutput
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
	if strings.TrimSpace(task.ID) == "" || task.Attempt <= 0 || task.Generation <= 0 {
		return false, errors.New("task core executor claimed task requires id and attempt")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCh := make(chan executionResult, 1)
	go func() {
		if enriched, ok := e.Downloader.(interface {
			ExecuteWithMetadata(context.Context, app.Task) (ExecutionOutput, error)
		}); ok {
			output, runErr := enriched.ExecuteWithMetadata(runCtx, *task)
			resultCh <- executionResult{path: output.Path, err: runErr, metadata: output}
			return
		}
		path, runErr := e.Downloader.Execute(runCtx, *task)
		resultCh <- executionResult{path: path, err: runErr}
	}()

	ticker := time.NewTicker(e.heartbeatEvery())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			cancel()
			result := <-resultCh
			removeExecutionArtifact(result.path)
			return true, ctx.Err()
		case result := <-resultCh:
			heartbeat, err := e.Service.Heartbeat(ctx, task.ID, e.WorkerID, task.Generation)
			if err != nil {
				removeExecutionArtifact(result.path)
				return true, err
			}
			if heartbeat.CancelRequested {
				removeExecutionArtifact(result.path)
				return true, e.Service.AcknowledgeCancel(ctx, task.ID, e.WorkerID, task.Attempt, task.Generation)
			}

			if result.err != nil {
				failErr := e.Service.Fail(ctx, app.FailInput{TaskID: task.ID, WorkerID: e.WorkerID, Attempt: task.Attempt, Generation: task.Generation, Message: result.err.Error()})
				return true, e.resolveReportError(ctx, task.ID, task.Attempt, task.Generation, failErr)
			}
			info, statErr := os.Stat(result.path)
			if statErr != nil {
				failErr := e.Service.Fail(ctx, app.FailInput{TaskID: task.ID, WorkerID: e.WorkerID, Attempt: task.Attempt, Generation: task.Generation, Message: statErr.Error()})
				return true, e.resolveReportError(ctx, task.ID, task.Attempt, task.Generation, failErr)
			}
			name := filepath.Base(result.path)
			completeErr := e.Service.Complete(ctx, app.CompleteInput{TaskID: task.ID, WorkerID: e.WorkerID, Attempt: task.Attempt, Generation: task.Generation, ArtifactPath: result.path, ArtifactName: name, ArtifactSize: info.Size(), EffectiveMetadataDocument: result.metadata.EffectiveMetadataDocument, RetentionManifest: result.metadata.RetentionManifest, MetadataWarnings: result.metadata.MetadataWarnings, MetadataProfile: result.metadata.MetadataProfile})
			if errors.Is(completeErr, app.ErrConflict) {
				removeExecutionArtifact(result.path)
			}
			if completeErr == nil {
				if cleaner, ok := e.Downloader.(interface {
					CleanupSource(context.Context, app.Task) error
				}); ok {
					if err := cleaner.CleanupSource(ctx, *task); err != nil {
						log.Printf("cleanup completed upload source: %v", err)
					}
				}
			}
			return true, e.resolveReportError(ctx, task.ID, task.Attempt, task.Generation, completeErr)
		case <-ticker.C:
			heartbeat, err := e.Service.Heartbeat(ctx, task.ID, e.WorkerID, task.Generation)
			if err != nil {
				cancel()
				result := <-resultCh
				removeExecutionArtifact(result.path)
				return true, err
			}
			if heartbeat.CancelRequested {
				cancel()
				result := <-resultCh
				removeExecutionArtifact(result.path)
				ackErr := e.Service.AcknowledgeCancel(ctx, task.ID, e.WorkerID, task.Attempt, task.Generation)
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

func (e *Executor) resolveReportError(ctx context.Context, taskID string, attempt int, generation int64, reportErr error) error {
	if reportErr == nil {
		return nil
	}
	if !errors.Is(reportErr, app.ErrConflict) {
		return reportErr
	}

	heartbeat, err := e.Service.Heartbeat(ctx, taskID, e.WorkerID, generation)
	if err != nil || !heartbeat.CancelRequested {
		return reportErr
	}
	return e.Service.AcknowledgeCancel(ctx, taskID, e.WorkerID, attempt, generation)
}

func (e *Executor) heartbeatEvery() time.Duration {
	if e.HeartbeatInterval <= 0 {
		return defaultHeartbeatInterval
	}
	return e.HeartbeatInterval
}

func removeExecutionArtifact(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
	_ = os.Remove(filepath.Dir(path))
}

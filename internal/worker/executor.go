package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/domain"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
)

const (
	defaultHeartbeatInterval = 5 * time.Second
	defaultCancelMessage     = "Cancelled by user."
	cancelDrainTimeout       = 100 * time.Millisecond
)

type Downloader interface {
	Execute(ctx context.Context, taskID string) (string, error)
}

type Executor struct {
	Store             TaskStore
	Downloader        Downloader
	HeartbeatInterval time.Duration
	CancelMessage     string
}

type taskHeartbeatStore interface {
	UpdateTaskHeartbeat(ctx context.Context, taskID, worker string) error
}

type taskStatusStore interface {
	GetTask(ctx context.Context, taskID string) (*domain.TaskLog, error)
}

type executionResult struct {
	resultPath string
	err        error
}

func (e *Executor) Handler(worker string) Handler {
	worker = strings.TrimSpace(worker)
	return func(ctx context.Context, msg Message) error {
		return e.Handle(ctx, worker, msg)
	}
}

func (e *Executor) Handle(ctx context.Context, worker string, msg Message) error {
	if e == nil {
		return errors.New("worker executor is required")
	}
	if e.Store == nil {
		return errors.New("worker executor requires task store")
	}
	if e.Downloader == nil {
		return errors.New("worker executor requires downloader")
	}
	statusStore, err := e.statusStore()
	if err != nil {
		return err
	}
	heartbeatStore, err := e.heartbeatStore()
	if err != nil {
		return err
	}

	worker = strings.TrimSpace(worker)
	if worker == "" {
		return errors.New("worker executor requires worker name")
	}

	taskID := strings.TrimSpace(msg.TaskID)
	if taskID == "" {
		return errors.New("worker executor requires task id")
	}

	cancelRequested, err := e.isCancelRequested(ctx, statusStore, taskID)
	if err != nil {
		return err
	}
	if cancelRequested {
		if err := e.transitionCanceled(ctx, taskID, worker); err != nil {
			return err
		}
		return e.ensureExpectedTerminalState(
			ctx,
			statusStore,
			taskID,
			worker,
			domain.StatusCanceled,
		)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCh := make(chan executionResult, 1)
	go func() {
		resultPath, runErr := e.Downloader.Execute(runCtx, taskID)
		resultCh <- executionResult{resultPath: resultPath, err: runErr}
	}()

	heartbeatTicker := time.NewTicker(e.heartbeatEvery())
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			cancel()
			return ctx.Err()
		case result := <-resultCh:
			if result.err != nil {
				cancelRequested, checkErr := e.isCancelRequested(ctx, statusStore, taskID)
				if checkErr != nil {
					return checkErr
				}
				if cancelRequested {
					if err := e.transitionCanceled(ctx, taskID, worker); err != nil {
						return err
					}
					return e.ensureExpectedTerminalState(
						ctx,
						statusStore,
						taskID,
						worker,
						domain.StatusCanceled,
					)
				}

				if err := e.transitionFailed(ctx, taskID, worker, result.err); err != nil {
					return err
				}
				return e.ensureExpectedTerminalState(
					ctx,
					statusStore,
					taskID,
					worker,
					domain.StatusFailed,
					domain.StatusCanceled,
				)
			}

			cancelRequested, err := e.isCancelRequested(ctx, statusStore, taskID)
			if err != nil {
				return err
			}
			if cancelRequested {
				if err := e.transitionCanceled(ctx, taskID, worker); err != nil {
					return err
				}
				return e.ensureExpectedTerminalState(
					ctx,
					statusStore,
					taskID,
					worker,
					domain.StatusCanceled,
				)
			}

			if err := e.transitionSuccess(ctx, taskID, worker, result.resultPath); err != nil {
				return err
			}
			return e.ensureExpectedTerminalState(
				ctx,
				statusStore,
				taskID,
				worker,
				domain.StatusSuccess,
				domain.StatusCanceled,
			)
		case <-heartbeatTicker.C:
			if err := heartbeatStore.UpdateTaskHeartbeat(ctx, taskID, worker); err != nil {
				return fmt.Errorf("update task heartbeat: %w", err)
			}

			cancelRequested, err := e.isCancelRequested(ctx, statusStore, taskID)
			if err != nil {
				return err
			}
			if !cancelRequested {
				continue
			}

			cancel()
			e.drainDownloadResult(resultCh)
			if err := e.transitionCanceled(ctx, taskID, worker); err != nil {
				return err
			}
			return e.ensureExpectedTerminalState(
				ctx,
				statusStore,
				taskID,
				worker,
				domain.StatusCanceled,
			)
		}
	}
}

func (e *Executor) transitionCanceled(ctx context.Context, taskID, worker string) error {
	cancelMsg := e.cancelMessage()
	return e.Store.TransitionToTerminal(ctx, postgres.TransitionTerminalInput{
		TaskID: taskID,
		Worker: worker,
		Status: domain.StatusCanceled,
		Error:  &cancelMsg,
	})
}

func (e *Executor) transitionFailed(ctx context.Context, taskID, worker string, runErr error) error {
	errMsg := runErr.Error()
	return e.Store.TransitionToTerminal(ctx, postgres.TransitionTerminalInput{
		TaskID: taskID,
		Worker: worker,
		Status: domain.StatusFailed,
		Error:  &errMsg,
	})
}

func (e *Executor) transitionSuccess(ctx context.Context, taskID, worker, resultPath string) error {
	var resultZipPath *string
	trimmed := strings.TrimSpace(resultPath)
	if trimmed != "" {
		resultZipPath = &trimmed
	}

	return e.Store.TransitionToTerminal(ctx, postgres.TransitionTerminalInput{
		TaskID:        taskID,
		Worker:        worker,
		Status:        domain.StatusSuccess,
		ResultZipPath: resultZipPath,
	})
}

func (e *Executor) taskStatus(ctx context.Context, store taskStatusStore, taskID string) (string, error) {
	task, err := store.GetTask(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("load task status: %w", err)
	}
	if task == nil {
		return "", fmt.Errorf("task %s not found while executing", taskID)
	}
	return strings.ToUpper(strings.TrimSpace(task.Status)), nil
}

func (e *Executor) ensureExpectedTerminalState(
	ctx context.Context,
	statusStore taskStatusStore,
	taskID string,
	worker string,
	expectedStatuses ...string,
) error {
	status, err := e.taskStatus(ctx, statusStore, taskID)
	if err != nil {
		return err
	}

	if status == domain.StatusCancelRequested {
		if err := e.transitionCanceled(ctx, taskID, worker); err != nil {
			return err
		}
		status, err = e.taskStatus(ctx, statusStore, taskID)
		if err != nil {
			return err
		}
	}

	if !isTerminalStatus(status) {
		return fmt.Errorf(
			"task %s remained non-terminal after terminal transition: %s",
			taskID,
			status,
		)
	}
	if len(expectedStatuses) > 0 && !statusIn(status, expectedStatuses...) {
		return fmt.Errorf(
			"task %s reached unexpected terminal status: %s",
			taskID,
			status,
		)
	}
	return nil
}

func (e *Executor) isCancelRequested(ctx context.Context, store taskStatusStore, taskID string) (bool, error) {
	status, err := e.taskStatus(ctx, store, taskID)
	if err != nil {
		return false, err
	}
	return status == domain.StatusCancelRequested, nil
}

func (e *Executor) heartbeatEvery() time.Duration {
	if e == nil || e.HeartbeatInterval <= 0 {
		return defaultHeartbeatInterval
	}
	return e.HeartbeatInterval
}

func (e *Executor) cancelMessage() string {
	if e == nil {
		return defaultCancelMessage
	}
	message := strings.TrimSpace(e.CancelMessage)
	if message == "" {
		return defaultCancelMessage
	}
	return message
}

func (e *Executor) heartbeatStore() (taskHeartbeatStore, error) {
	heartbeatStore, ok := e.Store.(taskHeartbeatStore)
	if !ok {
		return nil, errors.New("worker executor store does not support heartbeat updates")
	}
	return heartbeatStore, nil
}

func (e *Executor) statusStore() (taskStatusStore, error) {
	statusStore, ok := e.Store.(taskStatusStore)
	if !ok {
		return nil, errors.New("worker executor store does not support task status reads")
	}
	return statusStore, nil
}

func (e *Executor) drainDownloadResult(resultCh <-chan executionResult) {
	select {
	case <-resultCh:
	case <-time.After(cancelDrainTimeout):
	}
}

func isTerminalStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case domain.StatusSuccess, domain.StatusFailed, domain.StatusCanceled:
		return true
	default:
		return false
	}
}

func statusIn(status string, expectedStatuses ...string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(status))
	for _, expectedStatus := range expectedStatuses {
		if normalized == strings.ToUpper(strings.TrimSpace(expectedStatus)) {
			return true
		}
	}
	return false
}

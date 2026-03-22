package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/downloader"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
)

const (
	defaultRunTaskTerminalWrite = 2 * time.Second
	defaultRunTaskHeartbeat     = 2 * time.Second
	runTaskStatusEventType      = "STATUS_TRANSITION"
)

type RunTaskSnapshot struct {
	ID                string
	URL               string
	Status            string
	EnqueueToken      string
	TaskType          *string
	SourceArchivePath *string
	SourceArchiveName *string
	Author            *string
	SeriesName        *string
	ComicName         *string
	Summary           *string
	TagsRaw           *string
	TagsNormalized    *string
	GenresRaw         *string
	GenresNormalized  *string
}

type RunTaskRepo interface {
	GetTaskForExecution(ctx context.Context, taskID string) (RunTaskSnapshot, error)
	UpdateTaskHeartbeat(ctx context.Context, taskID, worker string) error
	TransitionTaskWithEvent(ctx context.Context, in postgres.TransitionTaskWithEventInput) error
}

type RunTaskDownloader interface {
	DownloadAndPackage(ctx context.Context, taskID, pageURL string, metadata downloader.TaskMetadata) (string, error)
}

type RunTaskConfig struct {
	Repo                 RunTaskRepo
	Worker               string
	Download             RunTaskDownloader
	TransientRetry       int
	TerminalWriteTimeout time.Duration
	HeartbeatInterval    time.Duration
}

type RunTaskUseCase struct {
	repo                 RunTaskRepo
	worker               string
	downloader           RunTaskDownloader
	transientRetry       int
	terminalWriteTimeout time.Duration
	heartbeatInterval    time.Duration
}

func NewRunTaskUseCase(cfg RunTaskConfig) *RunTaskUseCase {
	return &RunTaskUseCase{
		repo:                 cfg.Repo,
		worker:               strings.TrimSpace(cfg.Worker),
		downloader:           cfg.Download,
		transientRetry:       cfg.TransientRetry,
		terminalWriteTimeout: cfg.TerminalWriteTimeout,
		heartbeatInterval:    cfg.HeartbeatInterval,
	}
}

func (u *RunTaskUseCase) Execute(ctx context.Context, taskID, token string) error {
	return u.ExecuteAttempt(ctx, taskID, token, false)
}

func (u *RunTaskUseCase) ExecuteAttempt(ctx context.Context, taskID, token string, allowRunning bool) error {
	if u == nil {
		return errors.New("run task use case is required")
	}
	if u.repo == nil {
		return errors.New("run task use case requires repository")
	}
	if u.downloader == nil {
		return errors.New("run task use case requires downloader")
	}
	if strings.TrimSpace(u.worker) == "" {
		return errors.New("run task use case requires worker name")
	}

	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return errors.New("run task use case requires task id")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("run task use case requires enqueue token")
	}

	snapshot, err := u.repo.GetTaskForExecution(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load v2 task snapshot: %w", err)
	}
	if strings.TrimSpace(snapshot.EnqueueToken) != token {
		return nil
	}

	status := strings.TrimSpace(snapshot.Status)
	switch status {
	case StatusQueued:
	case StatusRunning:
		if !allowRunning {
			return nil
		}
	case StatusCancelRequested:
		return u.transitionCancelRequested(snapshot, taskID, map[string]any{"stage": "preflight"})
	default:
		return nil
	}
	sourceLocation := strings.TrimSpace(snapshot.URL)
	if isUploadTask(snapshot.TaskType) {
		sourceLocation = valueOrEmptyRunTask(snapshot.SourceArchivePath)
		if strings.TrimSpace(sourceLocation) == "" {
			return fmt.Errorf("task %s has empty source archive path", taskID)
		}
	} else if strings.TrimSpace(snapshot.URL) == "" {
		return fmt.Errorf("task %s has empty url", taskID)
	}

	enteredRunning := false
	switch status {
	case StatusQueued:
		if err := u.transition(
			ctx,
			taskID,
			StatusQueued,
			StatusRunning,
			postgres.StatusPatch{ClaimedBy: trimmedStringPtr(u.worker)},
			map[string]any{"worker": u.worker},
		); err != nil {
			if errors.Is(err, postgres.ErrV2TaskStatusMismatchOrNotFound) {
				cancelRequested, checkErr := u.isTaskCancelRequested(ctx, taskID)
				if checkErr != nil {
					return checkErr
				}
				if cancelRequested {
					return u.transitionCancelRequested(snapshot, taskID, map[string]any{"stage": "queued_to_running_mismatch"})
				}
				return nil
			}
			return err
		}
		enteredRunning = true
	case StatusRunning:
		enteredRunning = true
	}

	attempts := u.maxAttempts()
	for attempt := 1; attempt <= attempts; attempt++ {
		downloadCtx := ctx
		cancelRequested := &atomic.Bool{}
		stopMonitor := func() {}
		if enteredRunning {
			downloadCtx, stopMonitor = u.startExecutionMonitor(ctx, taskID, cancelRequested)
		}

		artifactPath, runErr := u.downloader.DownloadAndPackage(
			downloadCtx,
			taskID,
			sourceLocation,
			taskMetadataFromRunTaskSnapshot(snapshot),
		)
		stopMonitor()

		if cancelRequested.Load() {
			return u.transitionCancelRequested(snapshot, taskID, map[string]any{"attempt": attempt, "stage": "monitor"})
		}

		if runErr != nil && errors.Is(runErr, context.Canceled) && ctx.Err() != nil {
			return ctx.Err()
		}

		if runErr == nil {
			terminalErr := u.transitionTerminal(
				taskID,
				StatusRunning,
				StatusSuccess,
				buildSuccessStatusPatch(snapshot, artifactPath),
				map[string]any{
					"artifact_path": strings.TrimSpace(artifactPath),
					"attempt":       attempt,
				},
			)
			if terminalErr != nil && enteredRunning {
				return markRunTaskRunningPhaseError(terminalErr)
			}
			return terminalErr
		}

		if errors.Is(runErr, context.Canceled) {
			cancelRequestedByStatus, checkErr := u.isTaskCancelRequested(ctx, taskID)
			if checkErr != nil {
				return checkErr
			}
			if cancelRequestedByStatus {
				return u.transitionCancelRequested(snapshot, taskID, map[string]any{"attempt": attempt, "stage": "error"})
			}
		}

		if shouldRetryRunTaskError(runErr) && attempt < attempts {
			continue
		}

		errMessage := strings.TrimSpace(runErr.Error())
		if errMessage == "" {
			errMessage = "worker execution failed"
		}
		terminalErr := u.transitionTerminal(
			taskID,
			StatusRunning,
			StatusFailed,
			buildFailureStatusPatch(snapshot, errMessage),
			map[string]any{"attempt": attempt},
		)
		if terminalErr != nil && enteredRunning {
			return markRunTaskRunningPhaseError(terminalErr)
		}
		return terminalErr
	}

	return nil
}

func (u *RunTaskUseCase) transition(
	ctx context.Context,
	taskID, from, to string,
	patch postgres.StatusPatch,
	payload map[string]any,
) error {
	return u.repo.TransitionTaskWithEvent(ctx, postgres.TransitionTaskWithEventInput{
		TaskID:      taskID,
		FromStatus:  from,
		ToStatus:    to,
		Patch:       patch,
		EventType:   runTaskStatusEventType,
		PayloadJSON: marshalRunTaskPayload(payload),
	})
}

func (u *RunTaskUseCase) transitionTerminal(
	taskID, from, to string,
	patch postgres.StatusPatch,
	payload map[string]any,
) error {
	terminalCtx, cancel := context.WithTimeout(context.Background(), u.terminalWriteDuration())
	defer cancel()

	return u.transition(terminalCtx, taskID, from, to, patch, payload)
}

func (u *RunTaskUseCase) terminalWriteDuration() time.Duration {
	if u.terminalWriteTimeout > 0 {
		return u.terminalWriteTimeout
	}
	return defaultRunTaskTerminalWrite
}

func (u *RunTaskUseCase) heartbeatDuration() time.Duration {
	if u.heartbeatInterval > 0 {
		return u.heartbeatInterval
	}
	return defaultRunTaskHeartbeat
}

func (u *RunTaskUseCase) maxAttempts() int {
	if u.transientRetry < 0 {
		return 1
	}
	return u.transientRetry + 1
}

func (u *RunTaskUseCase) startExecutionMonitor(
	parent context.Context,
	taskID string,
	cancelRequested *atomic.Bool,
) (context.Context, func()) {
	monitorCtx, monitorCancel := context.WithCancel(parent)
	done := make(chan struct{})
	heartbeatInterval := u.heartbeatDuration()

	observe := func(ctx context.Context) bool {
		heartbeatCtx, heartbeatCancel := context.WithTimeout(context.Background(), heartbeatInterval)
		heartbeatErr := u.repo.UpdateTaskHeartbeat(heartbeatCtx, taskID, u.worker)
		heartbeatCancel()
		if heartbeatErr != nil {
			log.Printf("v2 worker heartbeat update failed task_id=%s worker=%s: %v", strings.TrimSpace(taskID), strings.TrimSpace(u.worker), heartbeatErr)
		}

		requested, err := u.isTaskCancelRequested(ctx, taskID)
		if err != nil {
			log.Printf("v2 worker cancel check failed task_id=%s: %v", strings.TrimSpace(taskID), err)
			return false
		}
		if requested {
			cancelRequested.Store(true)
			monitorCancel()
			return true
		}
		return false
	}

	go func() {
		defer close(done)
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-monitorCtx.Done():
				return
			case <-ticker.C:
				if observe(monitorCtx) {
					return
				}
			}
		}
	}()

	return monitorCtx, func() {
		monitorCancel()
		<-done
	}
}

func (u *RunTaskUseCase) isTaskCancelRequested(ctx context.Context, taskID string) (bool, error) {
	snapshot, err := u.repo.GetTaskForExecution(ctx, taskID)
	if err != nil {
		return false, fmt.Errorf("load v2 task snapshot: %w", err)
	}
	return strings.TrimSpace(snapshot.Status) == StatusCancelRequested, nil
}

func (u *RunTaskUseCase) transitionCancelRequested(snapshot RunTaskSnapshot, taskID string, payload map[string]any) error {
	reason := "Cancellation requested by user."
	err := u.transitionTerminal(
		taskID,
		StatusCancelRequested,
		StatusCanceled,
		buildCanceledStatusPatch(snapshot, reason),
		payload,
	)
	if err == nil {
		return nil
	}
	if !errors.Is(err, postgres.ErrV2TaskStatusMismatchOrNotFound) {
		return err
	}

	statusCtx, statusCancel := context.WithTimeout(context.Background(), u.terminalWriteDuration())
	defer statusCancel()
	snapshot, statusErr := u.repo.GetTaskForExecution(statusCtx, taskID)
	if statusErr != nil {
		return fmt.Errorf("load v2 task snapshot: %w", statusErr)
	}
	switch strings.TrimSpace(snapshot.Status) {
	case StatusCanceled:
		return nil
	case StatusCancelRequested:
		return err
	default:
		return nil
	}
}

type runTaskRunningPhaseError struct {
	cause error
}

func (e *runTaskRunningPhaseError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *runTaskRunningPhaseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func markRunTaskRunningPhaseError(err error) error {
	if err == nil {
		return nil
	}
	var typed *runTaskRunningPhaseError
	if errors.As(err, &typed) {
		return err
	}
	return &runTaskRunningPhaseError{cause: err}
}

func IsRunningPhaseExecutionError(err error) bool {
	var typed *runTaskRunningPhaseError
	return errors.As(err, &typed)
}

func shouldRetryRunTaskError(err error) bool {
	if err == nil {
		return false
	}
	if downloader.IsLimitExceededError(err) || downloader.IsContextCancellationError(err) {
		return false
	}
	return downloader.ShouldRetryTaskError(err)
}

func marshalRunTaskPayload(payload map[string]any) string {
	if len(payload) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func trimmedStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	copied := trimmed
	return &copied
}

func taskMetadataFromRunTaskSnapshot(snapshot RunTaskSnapshot) downloader.TaskMetadata {
	return downloader.TaskMetadata{
		Writer:  valueOrEmptyRunTask(snapshot.Author),
		Series:  valueOrEmptyRunTask(snapshot.SeriesName),
		Title:   valueOrEmptyRunTask(snapshot.ComicName),
		Summary: valueOrEmptyRunTask(snapshot.Summary),
		Tags: firstNonEmptyRunTask(
			valueOrEmptyRunTask(snapshot.TagsNormalized),
			valueOrEmptyRunTask(snapshot.TagsRaw),
		),
		Genre: firstNonEmptyRunTask(
			valueOrEmptyRunTask(snapshot.GenresNormalized),
			valueOrEmptyRunTask(snapshot.GenresRaw),
		),
	}
}

func isUploadTask(taskType *string) bool {
	if taskType == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(*taskType), "upload")
}

func boolPtr(value bool) *bool {
	copied := value
	return &copied
}

func buildSuccessStatusPatch(snapshot RunTaskSnapshot, artifactPath string) postgres.StatusPatch {
	patch := postgres.StatusPatch{
		ResultZipPath: trimmedStringPtr(artifactPath),
		Retryable:     boolPtr(false),
	}
	if isUploadTask(snapshot.TaskType) {
		patch.ClearSourceArchivePath = true
	}
	return patch
}

func buildFailureStatusPatch(snapshot RunTaskSnapshot, errMessage string) postgres.StatusPatch {
	return postgres.StatusPatch{
		Error:     &errMessage,
		Retryable: boolPtr(true),
	}
}

func buildCanceledStatusPatch(snapshot RunTaskSnapshot, reason string) postgres.StatusPatch {
	patch := postgres.StatusPatch{
		Error:     &reason,
		Retryable: boolPtr(false),
	}
	if isUploadTask(snapshot.TaskType) {
		patch.ClearSourceArchivePath = true
	}
	return patch
}

func valueOrEmptyRunTask(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func firstNonEmptyRunTask(values ...string) string {
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized != "" {
			return normalized
		}
	}
	return ""
}

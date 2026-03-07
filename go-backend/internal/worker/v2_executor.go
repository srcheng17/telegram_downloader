package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	domainv2 "github.com/ryancheng/telegram-downloader/go-backend/internal/domain/v2"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/downloader"
	queuev2 "github.com/ryancheng/telegram-downloader/go-backend/internal/queue/v2"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres"
)

const (
	defaultV2DownloadRoot    = "downloaded_images"
	defaultV2QueueReadBlock  = 2 * time.Second
	defaultV2TerminalWrite   = 2 * time.Second
	defaultV2RunRetryCount   = 2
	defaultV2RunRetryBackoff = 100 * time.Millisecond
	v2StatusTransitionEvent  = "STATUS_TRANSITION"
)

type V2TaskSnapshot struct {
	ID           string
	URL          string
	Status       string
	EnqueueToken string
}

type V2ExecutionRepo interface {
	GetTaskForExecution(ctx context.Context, taskID string) (V2TaskSnapshot, error)
	TransitionTaskWithEvent(ctx context.Context, in postgres.TransitionTaskWithEventInput) error
}

type V2TaskDownloader interface {
	DownloadAndPackage(ctx context.Context, taskID, pageURL string) (string, error)
}

type V2ExecutorConfig struct {
	Repo                 V2ExecutionRepo
	Worker               string
	Download             V2TaskDownloader
	TransientRetry       int
	ReadBlock            time.Duration
	TerminalWriteTimeout time.Duration
	RunRetryCount        int
	RunRetryBackoff      time.Duration
}

type V2Executor struct {
	repo                 V2ExecutionRepo
	worker               string
	downloader           V2TaskDownloader
	transientRetry       int
	readBlock            time.Duration
	terminalWriteTimeout time.Duration
	runRetryCount        int
	runRetryBackoff      time.Duration
}

func NewV2Executor(cfg V2ExecutorConfig) *V2Executor {
	return &V2Executor{
		repo:                 cfg.Repo,
		worker:               strings.TrimSpace(cfg.Worker),
		downloader:           cfg.Download,
		transientRetry:       cfg.TransientRetry,
		readBlock:            cfg.ReadBlock,
		terminalWriteTimeout: cfg.TerminalWriteTimeout,
		runRetryCount:        cfg.RunRetryCount,
		runRetryBackoff:      cfg.RunRetryBackoff,
	}
}

func (e *V2Executor) Execute(ctx context.Context, taskID, token string) error {
	return e.executeAttempt(ctx, taskID, token, false)
}

func (e *V2Executor) executeAttempt(ctx context.Context, taskID, token string, allowRunning bool) error {
	if e == nil {
		return errors.New("v2 executor is required")
	}
	if e.repo == nil {
		return errors.New("v2 executor requires repository")
	}
	if e.downloader == nil {
		return errors.New("v2 executor requires downloader")
	}
	if strings.TrimSpace(e.worker) == "" {
		return errors.New("v2 executor requires worker name")
	}

	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return errors.New("v2 executor requires task id")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("v2 executor requires enqueue token")
	}

	snapshot, err := e.repo.GetTaskForExecution(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load v2 task snapshot: %w", err)
	}
	if strings.TrimSpace(snapshot.EnqueueToken) != token {
		return nil
	}

	status := strings.TrimSpace(snapshot.Status)
	needsExecution := false
	switch status {
	case string(domainv2.StatusQueued):
		needsExecution = true
	case string(domainv2.StatusRunning):
		needsExecution = allowRunning
	default:
		// Terminal/non-recoverable statuses are ignored by this executor path.
		return nil
	}
	if !needsExecution {
		return nil
	}
	if strings.TrimSpace(snapshot.URL) == "" {
		return fmt.Errorf("task %s has empty url", taskID)
	}

	enteredRunning := false
	switch status {
	case string(domainv2.StatusQueued):
		if err := e.transition(
			ctx,
			taskID,
			string(domainv2.StatusQueued),
			string(domainv2.StatusRunning),
			postgres.StatusPatch{ClaimedBy: stringPtrV2Executor(e.worker)},
			map[string]any{"worker": e.worker},
		); err != nil {
			if errors.Is(err, postgres.ErrV2TaskStatusMismatchOrNotFound) {
				return nil
			}
			return err
		}
		enteredRunning = true
	case string(domainv2.StatusRunning):
		// Recoverable state: previous run may have succeeded claim but failed to persist terminal status.
		enteredRunning = true
	}

	attempts := e.maxAttempts()
	for attempt := 1; attempt <= attempts; attempt++ {
		artifactPath, runErr := e.downloader.DownloadAndPackage(ctx, taskID, snapshot.URL)
		if runErr == nil {
			terminalErr := e.transitionTerminal(
				taskID,
				string(domainv2.StatusRunning),
				string(domainv2.StatusSuccess),
				postgres.StatusPatch{ResultZipPath: stringPtrV2Executor(artifactPath)},
				map[string]any{
					"artifact_path": strings.TrimSpace(artifactPath),
					"attempt":       attempt,
				},
			)
			if terminalErr != nil && enteredRunning {
				return markRunningPhaseExecutionError(terminalErr)
			}
			return terminalErr
		}

		if shouldRetryTaskExecutionError(runErr) && attempt < attempts {
			continue
		}

		errMessage := strings.TrimSpace(runErr.Error())
		if errMessage == "" {
			errMessage = "worker execution failed"
		}
		terminalErr := e.transitionTerminal(
			taskID,
			string(domainv2.StatusRunning),
			string(domainv2.StatusFailed),
			postgres.StatusPatch{Error: &errMessage},
			map[string]any{"attempt": attempt},
		)
		if terminalErr != nil && enteredRunning {
			return markRunningPhaseExecutionError(terminalErr)
		}
		return terminalErr
	}

	return nil
}

func (e *V2Executor) Run(ctx context.Context, consumer V2QueueConsumer) error {
	if e == nil {
		return errors.New("v2 executor is required")
	}
	if consumer == nil {
		return errors.New("v2 executor run requires queue consumer")
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		messages, err := consumer.ClaimPending(ctx, e.readBlockDuration(), 1)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("claim pending v2 queue messages: %w", err)
		}
		if len(messages) == 0 {
			messages, err = consumer.ReadGroup(ctx, 1, e.readBlockDuration())
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("read v2 queue messages: %w", err)
			}
		}

		for _, msg := range messages {
			err := e.executeWithRetry(ctx, msg)
			if err != nil {
				log.Printf("v2 worker execute failed task_id=%s: %v", strings.TrimSpace(msg.TaskID), err)
				continue
			}
			if strings.TrimSpace(msg.MessageID) == "" {
				continue
			}

			if err := consumer.Ack(ctx, msg.MessageID); err != nil {
				return fmt.Errorf("ack v2 queue message task_id=%s message_id=%s: %w", strings.TrimSpace(msg.TaskID), strings.TrimSpace(msg.MessageID), err)
			}
		}
	}
}

func (e *V2Executor) transition(
	ctx context.Context,
	taskID, from, to string,
	patch postgres.StatusPatch,
	payload map[string]any,
) error {
	return e.repo.TransitionTaskWithEvent(ctx, postgres.TransitionTaskWithEventInput{
		TaskID:      taskID,
		FromStatus:  from,
		ToStatus:    to,
		Patch:       patch,
		EventType:   v2StatusTransitionEvent,
		PayloadJSON: marshalPayload(payload),
	})
}

func (e *V2Executor) transitionTerminal(
	taskID, from, to string,
	patch postgres.StatusPatch,
	payload map[string]any,
) error {
	terminalCtx, cancel := context.WithTimeout(context.Background(), e.terminalWriteDuration())
	defer cancel()

	return e.transition(terminalCtx, taskID, from, to, patch, payload)
}

func (e *V2Executor) maxAttempts() int {
	if e.transientRetry < 0 {
		return 1
	}
	return e.transientRetry + 1
}

func (e *V2Executor) readBlockDuration() time.Duration {
	if e.readBlock > 0 {
		return e.readBlock
	}
	return defaultV2QueueReadBlock
}

func (e *V2Executor) terminalWriteDuration() time.Duration {
	if e.terminalWriteTimeout > 0 {
		return e.terminalWriteTimeout
	}
	return defaultV2TerminalWrite
}

func (e *V2Executor) runRetryAttempts() int {
	if e.runRetryCount < 0 {
		return 1
	}
	retryCount := e.runRetryCount
	if retryCount == 0 {
		retryCount = defaultV2RunRetryCount
	}
	return retryCount + 1
}

func (e *V2Executor) runRetryBackoffDuration(attempt int) time.Duration {
	base := e.runRetryBackoff
	if base <= 0 {
		base = defaultV2RunRetryBackoff
	}
	if attempt <= 0 {
		attempt = 1
	}
	return time.Duration(attempt) * base
}

func (e *V2Executor) executeWithRetry(ctx context.Context, msg queuev2.TaskMessage) error {
	var lastErr error
	allowRunning := false
	attempts := e.runRetryAttempts()
	for attempt := 1; attempt <= attempts; attempt++ {
		lastErr = e.executeAttempt(ctx, msg.TaskID, msg.Token, allowRunning)
		if lastErr == nil {
			return nil
		}
		if isRunningPhaseExecutionError(lastErr) {
			allowRunning = true
		}
		if attempt >= attempts {
			break
		}

		timer := time.NewTimer(e.runRetryBackoffDuration(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

type runningPhaseExecutionError struct {
	cause error
}

func (e *runningPhaseExecutionError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *runningPhaseExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func markRunningPhaseExecutionError(err error) error {
	if err == nil {
		return nil
	}
	var typed *runningPhaseExecutionError
	if errors.As(err, &typed) {
		return err
	}
	return &runningPhaseExecutionError{cause: err}
}

func isRunningPhaseExecutionError(err error) bool {
	var typed *runningPhaseExecutionError
	return errors.As(err, &typed)
}

func shouldRetryTaskExecutionError(err error) bool {
	if err == nil {
		return false
	}
	if downloader.IsLimitExceededError(err) || downloader.IsContextCancellationError(err) {
		return false
	}
	return downloader.ShouldRetryTaskError(err)
}

func marshalPayload(payload map[string]any) string {
	if len(payload) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func stringPtrV2Executor(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	copied := trimmed
	return &copied
}

type V2QueueConsumer interface {
	ReadGroup(ctx context.Context, count int64, block time.Duration) ([]queuev2.TaskMessage, error)
	ClaimPending(ctx context.Context, minIdle time.Duration, count int64) ([]queuev2.TaskMessage, error)
	Ack(ctx context.Context, messageIDs ...string) error
}

type V2DBTaskReader interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type V2PostgresExecutionRepo struct {
	Reader V2DBTaskReader
	Writer postgres.V2TaskRepo
}

func NewV2PostgresExecutionRepo(reader V2DBTaskReader, writer postgres.V2TaskRepo) *V2PostgresExecutionRepo {
	return &V2PostgresExecutionRepo{
		Reader: reader,
		Writer: writer,
	}
}

func (r *V2PostgresExecutionRepo) GetTaskForExecution(ctx context.Context, taskID string) (V2TaskSnapshot, error) {
	if r == nil || r.Reader == nil {
		return V2TaskSnapshot{}, errors.New("v2 execution repo reader is not configured")
	}

	var snapshot V2TaskSnapshot
	err := r.Reader.QueryRow(
		ctx,
		`
		SELECT
			id,
			url,
			status,
			enqueue_token
		FROM v2_tasks
		WHERE id = $1
		`,
		strings.TrimSpace(taskID),
	).Scan(&snapshot.ID, &snapshot.URL, &snapshot.Status, &snapshot.EnqueueToken)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return V2TaskSnapshot{}, fmt.Errorf("v2 task %s not found", strings.TrimSpace(taskID))
		}
		return V2TaskSnapshot{}, err
	}
	return snapshot, nil
}

func (r *V2PostgresExecutionRepo) TransitionTaskWithEvent(ctx context.Context, in postgres.TransitionTaskWithEventInput) error {
	if r == nil || r.Writer == nil {
		return errors.New("v2 execution repo writer is not configured")
	}
	return r.Writer.TransitionTaskWithEvent(ctx, in)
}

type V2ServiceDownloader struct {
	Service      *downloader.Service
	DownloadRoot string
}

func (d *V2ServiceDownloader) DownloadAndPackage(ctx context.Context, taskID, pageURL string) (string, error) {
	if d == nil {
		return "", errors.New("v2 service downloader is required")
	}
	if d.Service == nil {
		return "", errors.New("v2 service downloader requires download service")
	}

	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", errors.New("v2 service downloader requires task id")
	}
	pageURL = strings.TrimSpace(pageURL)
	if pageURL == "" {
		return "", errors.New("v2 service downloader requires page url")
	}

	result, err := d.Service.Download(ctx, pageURL)
	if err != nil {
		return "", err
	}

	downloadRoot := strings.TrimSpace(d.DownloadRoot)
	if downloadRoot == "" {
		downloadRoot = defaultV2DownloadRoot
	}
	if err := os.MkdirAll(downloadRoot, 0o755); err != nil {
		return "", fmt.Errorf("create v2 download output root: %w", err)
	}

	outputPath := filepath.Join(downloadRoot, taskID+".cbz")
	if err := d.Service.PackageCBZ(result.Images, downloader.TaskMetadata{}, outputPath); err != nil {
		return "", err
	}
	return outputPath, nil
}

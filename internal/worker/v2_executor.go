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
	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
	taskarchive "github.com/ryancheng/telegram-downloader/internal/archive"
	"github.com/ryancheng/telegram-downloader/internal/domain"
	"github.com/ryancheng/telegram-downloader/internal/downloader"
	queuev2 "github.com/ryancheng/telegram-downloader/internal/queue/v2"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
)

const (
	defaultV2DownloadRoot    = "downloaded_images"
	defaultV2QueueReadBlock  = 2 * time.Second
	defaultV2TerminalWrite   = 2 * time.Second
	defaultV2Heartbeat       = 2 * time.Second
	defaultV2RunRetryCount   = 2
	defaultV2RunRetryBackoff = 100 * time.Millisecond
	v2StatusTransitionEvent  = "STATUS_TRANSITION"
)

type V2TaskSnapshot struct {
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

type V2ExecutionRepo interface {
	GetTaskForExecution(ctx context.Context, taskID string) (V2TaskSnapshot, error)
	UpdateTaskHeartbeat(ctx context.Context, taskID, worker string) error
	UpdateTaskProgress(ctx context.Context, taskID string, progress, totalImages int) error
	TransitionTaskWithEvent(ctx context.Context, in postgres.TransitionTaskWithEventInput) error
}

type V2TaskDownloader interface {
	DownloadAndPackage(ctx context.Context, taskID, pageURL string, metadata downloader.TaskMetadata) (string, error)
}

type V2ExecutorConfig struct {
	Repo                 V2ExecutionRepo
	Worker               string
	Download             V2TaskDownloader
	TransientRetry       int
	ReadBlock            time.Duration
	TerminalWriteTimeout time.Duration
	HeartbeatInterval    time.Duration
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
	heartbeatInterval    time.Duration
	runRetryCount        int
	runRetryBackoff      time.Duration
	runner               *apptasks.RunTaskUseCase
}

func NewV2Executor(cfg V2ExecutorConfig) *V2Executor {
	return &V2Executor{
		repo:                 cfg.Repo,
		worker:               strings.TrimSpace(cfg.Worker),
		downloader:           cfg.Download,
		transientRetry:       cfg.TransientRetry,
		readBlock:            cfg.ReadBlock,
		terminalWriteTimeout: cfg.TerminalWriteTimeout,
		heartbeatInterval:    cfg.HeartbeatInterval,
		runRetryCount:        cfg.RunRetryCount,
		runRetryBackoff:      cfg.RunRetryBackoff,
		runner: apptasks.NewRunTaskUseCase(apptasks.RunTaskConfig{
			Repo:                 runTaskRepoAdapter{repo: cfg.Repo},
			Worker:               cfg.Worker,
			Download:             runTaskDownloaderAdapter{downloader: cfg.Download},
			TransientRetry:       cfg.TransientRetry,
			TerminalWriteTimeout: cfg.TerminalWriteTimeout,
			HeartbeatInterval:    cfg.HeartbeatInterval,
		}),
	}
}

func (e *V2Executor) Execute(ctx context.Context, taskID, token string) error {
	return e.executeAttempt(ctx, taskID, token, false)
}

func (e *V2Executor) executeAttempt(ctx context.Context, taskID, token string, allowRunning bool) error {
	if e == nil {
		return errors.New("v2 executor is required")
	}
	if e.runner == nil {
		return errors.New("v2 executor requires run task use case")
	}
	err := e.runner.ExecuteAttempt(ctx, taskID, token, allowRunning)
	if apptasks.IsRunningPhaseExecutionError(err) {
		return markRunningPhaseExecutionError(err)
	}
	return err
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

func (e *V2Executor) heartbeatDuration() time.Duration {
	if e.heartbeatInterval > 0 {
		return e.heartbeatInterval
	}
	return defaultV2Heartbeat
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
	return errors.As(err, &typed) || apptasks.IsRunningPhaseExecutionError(err)
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

type runTaskRepoAdapter struct {
	repo V2ExecutionRepo
}

func (a runTaskRepoAdapter) GetTaskForExecution(ctx context.Context, taskID string) (apptasks.RunTaskSnapshot, error) {
	if a.repo == nil {
		return apptasks.RunTaskSnapshot{}, errors.New("v2 executor requires repository")
	}
	snapshot, err := a.repo.GetTaskForExecution(ctx, taskID)
	if err != nil {
		return apptasks.RunTaskSnapshot{}, err
	}
	return apptasks.RunTaskSnapshot{
		ID:                snapshot.ID,
		URL:               snapshot.URL,
		Status:            snapshot.Status,
		EnqueueToken:      snapshot.EnqueueToken,
		TaskType:          snapshot.TaskType,
		SourceArchivePath: snapshot.SourceArchivePath,
		SourceArchiveName: snapshot.SourceArchiveName,
		Author:            snapshot.Author,
		SeriesName:        snapshot.SeriesName,
		ComicName:         snapshot.ComicName,
		Summary:           snapshot.Summary,
		TagsRaw:           snapshot.TagsRaw,
		TagsNormalized:    snapshot.TagsNormalized,
		GenresRaw:         snapshot.GenresRaw,
		GenresNormalized:  snapshot.GenresNormalized,
	}, nil
}

func (a runTaskRepoAdapter) UpdateTaskHeartbeat(ctx context.Context, taskID, worker string) error {
	if a.repo == nil {
		return errors.New("v2 executor requires repository")
	}
	return a.repo.UpdateTaskHeartbeat(ctx, taskID, worker)
}

func (a runTaskRepoAdapter) UpdateTaskProgress(ctx context.Context, taskID string, progress, totalImages int) error {
	if a.repo == nil {
		return errors.New("v2 executor requires repository")
	}
	return a.repo.UpdateTaskProgress(ctx, taskID, progress, totalImages)
}

func (a runTaskRepoAdapter) TransitionTaskWithEvent(ctx context.Context, in postgres.TransitionTaskWithEventInput) error {
	if a.repo == nil {
		return errors.New("v2 executor requires repository")
	}
	return a.repo.TransitionTaskWithEvent(ctx, in)
}

type runTaskDownloaderAdapter struct {
	downloader V2TaskDownloader
}

func (a runTaskDownloaderAdapter) DownloadAndPackage(ctx context.Context, taskID, pageURL string, metadata downloader.TaskMetadata) (string, error) {
	if a.downloader == nil {
		return "", errors.New("v2 executor requires downloader")
	}
	return a.downloader.DownloadAndPackage(ctx, taskID, pageURL, metadata)
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
			enqueue_token,
			task_type,
			source_archive_path,
			source_archive_name,
			author,
			series_name,
			comic_name,
			summary,
			tags_raw,
			tags_normalized,
			genres_raw,
			genres_normalized
		FROM v2_tasks
		WHERE id = $1
		`,
		strings.TrimSpace(taskID),
	).Scan(
		&snapshot.ID,
		&snapshot.URL,
		&snapshot.Status,
		&snapshot.EnqueueToken,
		&snapshot.TaskType,
		&snapshot.SourceArchivePath,
		&snapshot.SourceArchiveName,
		&snapshot.Author,
		&snapshot.SeriesName,
		&snapshot.ComicName,
		&snapshot.Summary,
		&snapshot.TagsRaw,
		&snapshot.TagsNormalized,
		&snapshot.GenresRaw,
		&snapshot.GenresNormalized,
	)
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

func (r *V2PostgresExecutionRepo) UpdateTaskHeartbeat(ctx context.Context, taskID, worker string) error {
	if r == nil || r.Writer == nil {
		return errors.New("v2 execution repo writer is not configured")
	}
	return r.Writer.UpdateTaskHeartbeat(ctx, strings.TrimSpace(taskID), strings.TrimSpace(worker))
}

func (r *V2PostgresExecutionRepo) UpdateTaskProgress(ctx context.Context, taskID string, progress, totalImages int) error {
	if r == nil || r.Writer == nil {
		return errors.New("v2 execution repo writer is not configured")
	}
	return r.Writer.UpdateTaskProgress(ctx, strings.TrimSpace(taskID), progress, totalImages)
}

type V2ServiceDownloader struct {
	Service          *downloader.Service
	DownloadRoot     string
	Extractor        *taskarchive.Extractor
	ProgressReporter V2ExecutionRepo
}

func (d *V2ServiceDownloader) DownloadAndPackage(
	ctx context.Context,
	taskID,
	source string,
	metadata downloader.TaskMetadata,
) (string, error) {
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
	source = strings.TrimSpace(source)
	if source == "" {
		return "", errors.New("v2 service downloader requires source")
	}

	var images []domain.DownloadedImage
	if isRemotePageSource(source) {
		serviceCopy := *d.Service
		if d.ProgressReporter != nil {
			serviceCopy.OnTotalImagesDiscovered = func(total int) {
				_ = d.ProgressReporter.UpdateTaskProgress(ctx, taskID, 0, total)
			}
			serviceCopy.OnImageDownloaded = func(downloaded, total int) {
				_ = d.ProgressReporter.UpdateTaskProgress(ctx, taskID, downloaded, total)
			}
		}
		result, err := serviceCopy.Download(ctx, source)
		if err != nil {
			return "", err
		}
		images = result.Images
	} else {
		extractor := d.Extractor
		if extractor == nil {
			extractor = taskarchive.NewExtractor(taskarchive.ExtractorConfig{})
		}
		extractedImages, err := extractor.Extract(ctx, source)
		if err != nil {
			return "", err
		}
		images = make([]domain.DownloadedImage, 0, len(extractedImages))
		for _, image := range extractedImages {
			images = append(images, domain.DownloadedImage{
				URL:         image.Name,
				ContentType: image.ContentType,
				Data:        image.Data,
			})
		}
	}

	downloadRoot := strings.TrimSpace(d.DownloadRoot)
	if downloadRoot == "" {
		downloadRoot = defaultV2DownloadRoot
	}
	if err := os.MkdirAll(downloadRoot, 0o755); err != nil {
		return "", fmt.Errorf("create v2 download output root: %w", err)
	}

	outputPath := filepath.Join(downloadRoot, buildDownloadFilename(metadata, time.Now().Unix()))
	if err := d.Service.PackageCBZ(images, metadata, outputPath); err != nil {
		return "", err
	}
	return outputPath, nil
}

func isRemotePageSource(source string) bool {
	normalized := strings.ToLower(strings.TrimSpace(source))
	return strings.HasPrefix(normalized, "http://") || strings.HasPrefix(normalized, "https://")
}

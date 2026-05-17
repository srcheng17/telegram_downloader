package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domainv2 "github.com/ryancheng/telegram-downloader/internal/domain/v2"
)

const (
	TaskTypeURL    = "url"
	TaskTypeUpload = "upload"

	TaskStatusUploading       = "UPLOADING"
	TaskStatusQueued          = string(domainv2.StatusQueued)
	TaskStatusRunning         = string(domainv2.StatusRunning)
	TaskStatusCancelRequested = string(domainv2.StatusCancelRequested)
	TaskStatusSuccess         = string(domainv2.StatusSuccess)
	TaskStatusFailed          = string(domainv2.StatusFailed)
	TaskStatusCanceled        = string(domainv2.StatusCanceled)
)

const v2TaskSelectFields = `
	id,
	url,
	canonical_url,
	status,
	progress,
	total_images,
	task_type,
	source_archive_path,
	source_archive_name,
	upload_loaded_bytes,
	upload_total_bytes,
	retryable,
	error,
	result_zip_path,
	author,
	series_name,
	comic_name,
	summary,
	tags_raw,
	tags_normalized,
	genres_raw,
	genres_normalized,
	created_at,
	updated_at
`

type UploadTaskStore struct {
	writer V2TaskRepo
	db     storeExecutor
}

func NewUploadTaskStore(pool *pgxpool.Pool) *UploadTaskStore {
	if pool == nil {
		return &UploadTaskStore{}
	}
	return &UploadTaskStore{
		writer: NewV2TaskRepo(pool),
		db:     pool,
	}
}

type taskRecordRowScanner interface {
	Scan(dest ...any) error
}

func scanTaskRecord(scanner taskRecordRowScanner) (TaskRecord, error) {
	var task TaskRecord
	if err := scanner.Scan(
		&task.ID,
		&task.URL,
		&task.CanonicalURL,
		&task.Status,
		&task.Progress,
		&task.TotalImages,
		&task.TaskType,
		&task.SourceArchivePath,
		&task.SourceArchiveName,
		&task.UploadLoadedBytes,
		&task.UploadTotalBytes,
		&task.Retryable,
		&task.Error,
		&task.ResultZipPath,
		&task.Author,
		&task.SeriesName,
		&task.ComicName,
		&task.Summary,
		&task.TagsRaw,
		&task.TagsNormalized,
		&task.GenresRaw,
		&task.GenresNormalized,
		&task.CreatedAt,
		&task.UpdatedAt,
	); err != nil {
		return TaskRecord{}, err
	}
	return task, nil
}

func NormalizeTaskType(value *string) string {
	if value == nil {
		return TaskTypeURL
	}
	switch strings.ToLower(strings.TrimSpace(*value)) {
	case TaskTypeUpload:
		return TaskTypeUpload
	default:
		return TaskTypeURL
	}
}

func (s *UploadTaskStore) CreateUploadTask(ctx context.Context, in CreateTaskInput) (TaskRecord, error) {
	if s == nil || s.writer == nil {
		return TaskRecord{}, errors.New("v2 task writer is not configured")
	}
	taskType := stringPtr(TaskTypeUpload)
	in.TaskType = taskType
	in.Retryable = false
	if strings.TrimSpace(in.URL) == "" {
		in.URL = ""
	}
	if in.CanonicalURL == nil || strings.TrimSpace(*in.CanonicalURL) == "" {
		canonicalURL := "upload:" + strings.TrimSpace(in.ID)
		in.CanonicalURL = &canonicalURL
	}

	task, err := s.writer.CreateTask(ctx, in)
	if err != nil {
		return TaskRecord{}, err
	}
	if err := s.writer.UpdateTaskStatus(ctx, task.ID, TaskStatusQueued, TaskStatusUploading, StatusPatch{}); err != nil {
		return TaskRecord{}, err
	}
	task.Status = TaskStatusUploading
	task.TaskType = stringPtr(TaskTypeUpload)
	return task, nil
}

func (s *UploadTaskStore) GetTask(ctx context.Context, taskID string) (*TaskRecord, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("v2 task database is not configured")
	}

	row := s.db.QueryRow(
		ctx,
		`SELECT `+v2TaskSelectFields+` FROM v2_tasks WHERE id = $1`,
		strings.TrimSpace(taskID),
	)
	task, err := scanTaskRecord(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func (s *UploadTaskStore) UpdateUploadProgress(ctx context.Context, taskID string, loadedBytes, totalBytes int64) error {
	if s == nil || s.db == nil {
		return errors.New("v2 task database is not configured")
	}
	_, err := s.db.Exec(
		ctx,
		`
		UPDATE v2_tasks
		SET
			upload_loaded_bytes = $2,
			upload_total_bytes = $3,
			updated_at = NOW()
		WHERE id = $1
		`,
		strings.TrimSpace(taskID),
		normalizeNonNegativeInt64(loadedBytes),
		normalizeNonNegativeInt64(totalBytes),
	)
	return err
}

func (s *UploadTaskStore) UpdateTaskProgress(ctx context.Context, taskID string, progress, totalImages int) error {
	if s == nil || s.db == nil {
		return errors.New("v2 task database is not configured")
	}
	_, err := s.db.Exec(
		ctx,
		`
		UPDATE v2_tasks
		SET
			progress = $2,
			total_images = $3,
			updated_at = NOW()
		WHERE id = $1
		`,
		strings.TrimSpace(taskID),
		normalizeNonNegativeInt(progress),
		normalizeNonNegativeInt(totalImages),
	)
	return err
}

func (s *UploadTaskStore) MarkUploadTaskQueued(ctx context.Context, taskID, sourceArchivePath string, totalBytes int64) error {
	if s == nil || s.db == nil {
		return errors.New("v2 task database is not configured")
	}
	tag, err := s.db.Exec(
		ctx,
		`
		UPDATE v2_tasks
		SET
			status = $2,
			source_archive_path = $3,
			upload_loaded_bytes = $4,
			upload_total_bytes = $4,
			retryable = FALSE,
			updated_at = NOW()
		WHERE
			id = $1
			AND status = $5
		`,
		strings.TrimSpace(taskID),
		TaskStatusQueued,
		strings.TrimSpace(sourceArchivePath),
		normalizeNonNegativeInt64(totalBytes),
		TaskStatusUploading,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: id=%s from=%s to=%s", ErrV2TaskStatusMismatchOrNotFound, strings.TrimSpace(taskID), TaskStatusUploading, TaskStatusQueued)
	}
	return nil
}

func (s *UploadTaskStore) MarkTaskFailed(ctx context.Context, taskID, message string) error {
	if s == nil || s.writer == nil {
		return errors.New("v2 task writer is not configured")
	}

	reason := strings.TrimSpace(message)
	if reason == "" {
		reason = "enqueue failed"
	}
	return s.writer.UpdateTaskStatus(
		ctx,
		strings.TrimSpace(taskID),
		TaskStatusQueued,
		TaskStatusFailed,
		StatusPatch{Error: stringPtr(reason)},
	)
}

func (s *UploadTaskStore) RetryUploadTask(ctx context.Context, taskID, enqueueToken string) error {
	if s == nil || s.db == nil {
		return errors.New("v2 task database is not configured")
	}
	tag, err := s.db.Exec(
		ctx,
		`
		UPDATE v2_tasks
		SET
			status = $2,
			enqueue_token = $3,
			error = NULL,
			result_zip_path = NULL,
			progress = 0,
			total_images = 0,
			retryable = FALSE,
			claimed_by = NULL,
			updated_at = NOW()
		WHERE
			id = $1
			AND status IN ($4, $7)
			AND (
				(task_type = $5 AND retryable = TRUE AND source_archive_path IS NOT NULL AND source_archive_path != '')
				OR
				(task_type = $6 AND url IS NOT NULL AND url != '')
			)
		`,
		strings.TrimSpace(taskID),
		TaskStatusQueued,
		strings.TrimSpace(enqueueToken),
		TaskStatusFailed,
		TaskTypeUpload,
		TaskTypeURL,
		TaskStatusCanceled,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: id=%s from=%s to=%s", ErrV2TaskStatusMismatchOrNotFound, strings.TrimSpace(taskID), TaskStatusFailed, TaskStatusQueued)
	}
	return nil
}

func querySingleTaskRecord(ctx context.Context, queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, sql string, args ...any) (*TaskRecord, error) {
	row := queryer.QueryRow(ctx, sql, args...)
	task, err := scanTaskRecord(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

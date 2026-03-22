package httpv2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
)

const (
	taskTypeURL    = "url"
	taskTypeUpload = "upload"
)

type taskRowScanner interface {
	Scan(dest ...any) error
}

func scanTaskRow(scanner taskRowScanner) (Task, error) {
	var task Task
	if err := scanner.Scan(
		&task.ID,
		&task.URL,
		&task.CanonicalURL,
		&task.Status,
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
		return Task{}, err
	}
	return task, nil
}

func normalizeTaskType(value *string) string {
	if value == nil {
		return taskTypeURL
	}
	switch strings.ToLower(strings.TrimSpace(*value)) {
	case taskTypeUpload:
		return taskTypeUpload
	default:
		return taskTypeURL
	}
}

func (s *PostgresTaskStore) CreateUploadTask(ctx context.Context, in CreateTaskInput) (Task, error) {
	taskType := stringPtr(taskTypeUpload)
	in.TaskType = taskType
	in.Retryable = false
	if strings.TrimSpace(in.URL) == "" {
		in.URL = ""
	}
	if in.CanonicalURL == nil || strings.TrimSpace(*in.CanonicalURL) == "" {
		canonicalURL := "upload:" + strings.TrimSpace(in.ID)
		in.CanonicalURL = &canonicalURL
	}

	task, err := s.CreateTask(ctx, in)
	if err != nil {
		return Task{}, err
	}
	if s.writer == nil {
		return Task{}, errors.New("v2 task writer is not configured")
	}
	if err := s.writer.UpdateTaskStatus(ctx, task.ID, TaskStatusQueued, TaskStatusUploading, postgres.StatusPatch{}); err != nil {
		return Task{}, err
	}
	task.Status = TaskStatusUploading
	task.TaskType = stringPtr(taskTypeUpload)
	return task, nil
}

func (s *PostgresTaskStore) UpdateUploadProgress(ctx context.Context, taskID string, loadedBytes, totalBytes int64) error {
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
		maxInt64(loadedBytes, 0),
		maxInt64(totalBytes, 0),
	)
	return err
}

func (s *PostgresTaskStore) MarkUploadTaskQueued(ctx context.Context, taskID, sourceArchivePath string, totalBytes int64) error {
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
		maxInt64(totalBytes, 0),
		TaskStatusUploading,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: id=%s from=%s to=%s", postgres.ErrV2TaskStatusMismatchOrNotFound, strings.TrimSpace(taskID), TaskStatusUploading, TaskStatusQueued)
	}
	return nil
}

func (s *PostgresTaskStore) RetryUploadTask(ctx context.Context, taskID, enqueueToken string) error {
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
			retryable = FALSE,
			claimed_by = NULL,
			updated_at = NOW()
		WHERE
			id = $1
			AND status = $4
			AND retryable = TRUE
			AND (
				(task_type = $5 AND source_archive_path IS NOT NULL AND source_archive_path != '')
				OR
				(task_type = $6 AND url IS NOT NULL AND url != '')
			)
		`,
		strings.TrimSpace(taskID),
		TaskStatusQueued,
		strings.TrimSpace(enqueueToken),
		TaskStatusFailed,
		taskTypeUpload,
		taskTypeURL,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: id=%s from=%s to=%s", postgres.ErrV2TaskStatusMismatchOrNotFound, strings.TrimSpace(taskID), TaskStatusFailed, TaskStatusQueued)
	}
	return nil
}

func maxInt64(value, minimum int64) int64 {
	if value < minimum {
		return minimum
	}
	return value
}

func querySingleTask(ctx context.Context, queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, sql string, args ...any) (*Task, error) {
	row := queryer.QueryRow(ctx, sql, args...)
	task, err := scanTaskRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

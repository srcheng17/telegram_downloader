package httpv2

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
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

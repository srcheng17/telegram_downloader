package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const statusQueued = "QUEUED"

var ErrV2TaskStatusMismatchOrNotFound = errors.New("v2 task status mismatch or not found")

type CreateTaskInput struct {
	ID                string
	URL               string
	CanonicalURL      *string
	EnqueueToken      string
	TaskType          *string
	SourceArchivePath *string
	SourceArchiveName *string
	UploadLoadedBytes int64
	UploadTotalBytes  int64
	Retryable         bool
	Author            *string
	SeriesName        *string
	ComicName         *string
	Summary           *string
	TagsRaw           *string
	TagsNormalized    *string
	GenresRaw         *string
	GenresNormalized  *string
}

type TaskRecord struct {
	ID                string
	URL               string
	CanonicalURL      *string
	Status            string
	EnqueueToken      string
	TaskType          *string
	SourceArchivePath *string
	SourceArchiveName *string
	UploadLoadedBytes int64
	UploadTotalBytes  int64
	Retryable         bool
	Author            *string
	SeriesName        *string
	ComicName         *string
	Summary           *string
	TagsRaw           *string
	TagsNormalized    *string
	GenresRaw         *string
	GenresNormalized  *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type StatusPatch struct {
	Error         *string
	ResultZipPath *string
	ClaimedBy     *string
}

type TaskEvent struct {
	TaskID      string
	EventType   string
	FromStatus  *string
	ToStatus    *string
	PayloadJSON string
}

type TransitionTaskWithEventInput struct {
	TaskID      string
	FromStatus  string
	ToStatus    string
	Patch       StatusPatch
	EventType   string
	PayloadJSON string
}

type V2TaskRepo interface {
	CreateTask(ctx context.Context, in CreateTaskInput) (TaskRecord, error)
	UpdateTaskStatus(ctx context.Context, id string, from, to string, patch StatusPatch) error
	UpdateTaskHeartbeat(ctx context.Context, id, worker string) error
	AppendTaskEvent(ctx context.Context, event TaskEvent) error
	TransitionTaskWithEvent(ctx context.Context, in TransitionTaskWithEventInput) error
}

type PostgresV2TaskRepo struct {
	db storeExecutor
}

func NewV2TaskRepo(pool *pgxpool.Pool) V2TaskRepo {
	return &PostgresV2TaskRepo{db: pool}
}

func (r *PostgresV2TaskRepo) CreateTask(ctx context.Context, in CreateTaskInput) (TaskRecord, error) {
	url := strings.TrimSpace(in.URL)
	canonicalURL := url
	if in.CanonicalURL != nil {
		normalized := strings.TrimSpace(*in.CanonicalURL)
		if normalized != "" {
			canonicalURL = normalized
		}
	}

	now := time.Now().UTC()
	record := TaskRecord{
		ID:                strings.TrimSpace(in.ID),
		URL:               url,
		CanonicalURL:      stringPtr(canonicalURL),
		Status:            statusQueued,
		EnqueueToken:      strings.TrimSpace(in.EnqueueToken),
		TaskType:          stringPtr(normalizeCreateTaskType(in.TaskType)),
		SourceArchivePath: copyOptionalText(in.SourceArchivePath),
		SourceArchiveName: copyOptionalText(in.SourceArchiveName),
		UploadLoadedBytes: normalizeNonNegativeInt64(in.UploadLoadedBytes),
		UploadTotalBytes:  normalizeNonNegativeInt64(in.UploadTotalBytes),
		Retryable:         in.Retryable,
		Author:            copyOptionalText(in.Author),
		SeriesName:        copyOptionalText(in.SeriesName),
		ComicName:         copyOptionalText(in.ComicName),
		Summary:           copyOptionalText(in.Summary),
		TagsRaw:           copyOptionalText(in.TagsRaw),
		TagsNormalized: copyOptionalText(
			in.TagsNormalized,
		),
		GenresRaw:        copyOptionalText(in.GenresRaw),
		GenresNormalized: copyOptionalText(in.GenresNormalized),
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	_, err := r.db.Exec(
		ctx,
		`
		INSERT INTO v2_tasks (
			id,
			url,
			canonical_url,
			status,
			enqueue_token,
			task_type,
			source_archive_path,
			source_archive_name,
			upload_loaded_bytes,
			upload_total_bytes,
			retryable,
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
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
		)
		`,
		record.ID,
		record.URL,
		canonicalURL,
		record.Status,
		record.EnqueueToken,
		record.TaskType,
		record.SourceArchivePath,
		record.SourceArchiveName,
		record.UploadLoadedBytes,
		record.UploadTotalBytes,
		record.Retryable,
		record.Author,
		record.SeriesName,
		record.ComicName,
		record.Summary,
		record.TagsRaw,
		record.TagsNormalized,
		record.GenresRaw,
		record.GenresNormalized,
		record.CreatedAt,
		record.UpdatedAt,
	)
	if err != nil {
		return TaskRecord{}, err
	}

	return record, nil
}

func normalizeCreateTaskType(value *string) string {
	if value == nil {
		return "url"
	}
	switch strings.ToLower(strings.TrimSpace(*value)) {
	case "upload":
		return "upload"
	default:
		return "url"
	}
}

func normalizeNonNegativeInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func (r *PostgresV2TaskRepo) UpdateTaskStatus(ctx context.Context, id string, from, to string, patch StatusPatch) error {
	return updateV2TaskStatusWithExecutor(ctx, r.db, id, from, to, patch)
}

func (r *PostgresV2TaskRepo) UpdateTaskHeartbeat(ctx context.Context, id, worker string) error {
	_, err := r.db.Exec(
		ctx,
		`
		UPDATE v2_tasks
		SET
			heartbeat_at = NOW(),
			updated_at = NOW()
		WHERE
			id = $1
			AND claimed_by = $2
			AND status IN ('RUNNING', 'CANCEL_REQUESTED')
		`,
		strings.TrimSpace(id),
		strings.TrimSpace(worker),
	)
	return err
}

func (r *PostgresV2TaskRepo) AppendTaskEvent(ctx context.Context, event TaskEvent) error {
	return appendV2TaskEventWithExecutor(ctx, r.db, event)
}

func (r *PostgresV2TaskRepo) TransitionTaskWithEvent(ctx context.Context, in TransitionTaskWithEventInput) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if err := updateV2TaskStatusWithExecutor(
		ctx,
		tx,
		strings.TrimSpace(in.TaskID),
		strings.TrimSpace(in.FromStatus),
		strings.TrimSpace(in.ToStatus),
		in.Patch,
	); err != nil {
		return err
	}

	fromCopy := strings.TrimSpace(in.FromStatus)
	toCopy := strings.TrimSpace(in.ToStatus)
	eventType := strings.TrimSpace(in.EventType)
	if eventType == "" {
		eventType = "STATUS_TRANSITION"
	}

	if err := appendV2TaskEventWithExecutor(ctx, tx, TaskEvent{
		TaskID:      strings.TrimSpace(in.TaskID),
		EventType:   eventType,
		FromStatus:  &fromCopy,
		ToStatus:    &toCopy,
		PayloadJSON: strings.TrimSpace(in.PayloadJSON),
	}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func copyOptionalText(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	copied := normalized
	return &copied
}

type v2TaskExec interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func updateV2TaskStatusWithExecutor(
	ctx context.Context,
	executor v2TaskExec,
	id string,
	from string,
	to string,
	patch StatusPatch,
) error {
	tag, err := executor.Exec(
		ctx,
		`
		UPDATE v2_tasks
		SET
			status = $3,
			error = $4,
			result_zip_path = $5,
			claimed_by = COALESCE($6, claimed_by),
			heartbeat_at = CASE
				WHEN $3 IN ('RUNNING', 'SUCCESS', 'FAILED', 'CANCELED') THEN NOW()
				ELSE heartbeat_at
			END,
			cancel_requested_at = CASE
				WHEN $3 = 'CANCEL_REQUESTED' THEN NOW()
				ELSE cancel_requested_at
			END,
			retry_count = CASE
				WHEN $2 = 'RUNNING' AND $3 = 'FAILED' THEN retry_count + 1
				ELSE retry_count
			END,
			updated_at = NOW()
		WHERE
			id = $1
			AND status = $2
		`,
		id,
		from,
		to,
		patch.Error,
		patch.ResultZipPath,
		patch.ClaimedBy,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf(
			"%w: id=%s from=%s to=%s",
			ErrV2TaskStatusMismatchOrNotFound,
			strings.TrimSpace(id),
			strings.TrimSpace(from),
			strings.TrimSpace(to),
		)
	}
	return nil
}

func appendV2TaskEventWithExecutor(ctx context.Context, executor v2TaskExec, event TaskEvent) error {
	payloadJSON := strings.TrimSpace(event.PayloadJSON)
	if payloadJSON == "" {
		payloadJSON = "{}"
	}

	_, err := executor.Exec(
		ctx,
		`
		INSERT INTO v2_task_events (
			task_id,
			event_type,
			from_status,
			to_status,
			payload_json
		) VALUES (
			$1, $2, $3, $4, $5::jsonb
		)
		`,
		strings.TrimSpace(event.TaskID),
		strings.TrimSpace(event.EventType),
		event.FromStatus,
		event.ToStatus,
		payloadJSON,
	)
	return err
}

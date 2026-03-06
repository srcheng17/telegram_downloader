package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const statusQueued = "QUEUED"

type CreateTaskInput struct {
	ID           string
	URL          string
	CanonicalURL *string
	EnqueueToken string
}

type TaskRecord struct {
	ID           string
	URL          string
	CanonicalURL *string
	Status       string
	EnqueueToken string
	CreatedAt    time.Time
	UpdatedAt    time.Time
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

type V2TaskRepo interface {
	CreateTask(ctx context.Context, in CreateTaskInput) (TaskRecord, error)
	UpdateTaskStatus(ctx context.Context, id string, from, to string, patch StatusPatch) error
	AppendTaskEvent(ctx context.Context, event TaskEvent) error
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
		ID:           strings.TrimSpace(in.ID),
		URL:          url,
		CanonicalURL: stringPtr(canonicalURL),
		Status:       statusQueued,
		EnqueueToken: strings.TrimSpace(in.EnqueueToken),
		CreatedAt:    now,
		UpdatedAt:    now,
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
			created_at,
			updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
		`,
		record.ID,
		record.URL,
		canonicalURL,
		record.Status,
		record.EnqueueToken,
		record.CreatedAt,
		record.UpdatedAt,
	)
	if err != nil {
		return TaskRecord{}, err
	}

	return record, nil
}

func (r *PostgresV2TaskRepo) UpdateTaskStatus(ctx context.Context, id string, from, to string, patch StatusPatch) error {
	_, err := r.db.Exec(
		ctx,
		`
		UPDATE v2_tasks
		SET
			status = $3,
			error = $4,
			result_zip_path = $5,
			claimed_by = COALESCE($6, claimed_by),
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
	return err
}

func (r *PostgresV2TaskRepo) AppendTaskEvent(ctx context.Context, event TaskEvent) error {
	payloadJSON := strings.TrimSpace(event.PayloadJSON)
	if payloadJSON == "" {
		payloadJSON = "{}"
	}

	_, err := r.db.Exec(
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

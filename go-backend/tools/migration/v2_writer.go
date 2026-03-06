package migration

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type V2Task struct {
	ID            string
	URL           string
	CanonicalURL  string
	Status        string
	EnqueueToken  string
	Error         *string
	ResultZipPath *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type V2TaskEvent struct {
	TaskID      string
	EventType   string
	FromStatus  *string
	ToStatus    *string
	PayloadJSON string
	CreatedAt   time.Time
}

type V2Writer interface {
	WriteV2Batch(ctx context.Context, tasks []V2Task, events []V2TaskEvent) error
	CountV2Tasks(ctx context.Context) (int, error)
	CountV2MigratedEvents(ctx context.Context) (int, error)
	ChecksumV2Tasks(ctx context.Context) (string, error)
	ChecksumV2MigratedEvents(ctx context.Context) (string, error)
}

type v2WriterDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

type PostgresV2Writer struct {
	db v2WriterDB
}

func NewPostgresV2Writer(db v2WriterDB) *PostgresV2Writer {
	return &PostgresV2Writer{db: db}
}

func (w *PostgresV2Writer) WriteV2Batch(ctx context.Context, tasks []V2Task, events []V2TaskEvent) error {
	if len(tasks) == 0 {
		return nil
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	for _, task := range tasks {
		canonicalURL := strings.TrimSpace(task.CanonicalURL)
		if canonicalURL == "" {
			canonicalURL = strings.TrimSpace(task.URL)
		}
		enqueueToken := strings.TrimSpace(task.EnqueueToken)
		if enqueueToken == "" {
			enqueueToken = migratedEnqueueToken(task.ID)
		}
		createdAt := task.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		updatedAt := task.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = createdAt
		}

		if _, err := tx.Exec(
			ctx,
			`
			INSERT INTO v2_tasks (
				id,
				url,
				canonical_url,
				status,
				enqueue_token,
				error,
				result_zip_path,
				created_at,
				updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9
			)
			ON CONFLICT (id) DO UPDATE
			SET
				url = EXCLUDED.url,
				canonical_url = EXCLUDED.canonical_url,
				status = EXCLUDED.status,
				enqueue_token = EXCLUDED.enqueue_token,
				error = EXCLUDED.error,
				result_zip_path = EXCLUDED.result_zip_path,
				updated_at = EXCLUDED.updated_at
			`,
			strings.TrimSpace(task.ID),
			strings.TrimSpace(task.URL),
			canonicalURL,
			strings.TrimSpace(task.Status),
			enqueueToken,
			task.Error,
			task.ResultZipPath,
			createdAt,
			updatedAt,
		); err != nil {
			return err
		}
	}

	for _, event := range events {
		eventType := strings.TrimSpace(event.EventType)
		if eventType == "" {
			eventType = "MIGRATED"
		}
		payloadJSON := strings.TrimSpace(event.PayloadJSON)
		if payloadJSON == "" {
			payloadJSON = "{}"
		}
		createdAt := event.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}

		if _, err := tx.Exec(
			ctx,
			`
			INSERT INTO v2_task_events (
				task_id,
				event_type,
				from_status,
				to_status,
				payload_json,
				created_at
			) VALUES (
				$1, $2, $3, $4, $5::jsonb, $6
			)
			`,
			strings.TrimSpace(event.TaskID),
			eventType,
			event.FromStatus,
			event.ToStatus,
			payloadJSON,
			createdAt,
		); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (w *PostgresV2Writer) CountV2Tasks(ctx context.Context) (int, error) {
	var total int
	if err := w.db.QueryRow(ctx, `SELECT COUNT(*) FROM v2_tasks`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (w *PostgresV2Writer) CountV2MigratedEvents(ctx context.Context) (int, error) {
	var total int
	if err := w.db.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM v2_task_events WHERE event_type = 'MIGRATED'`,
	).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (w *PostgresV2Writer) ChecksumV2Tasks(ctx context.Context) (string, error) {
	rows, err := w.db.Query(
		ctx,
		`
		SELECT id, url, canonical_url, status, error, result_zip_path
		FROM v2_tasks
		ORDER BY id ASC
		`,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	builder := newChecksumBuilder()
	for rows.Next() {
		var task V2Task
		if err := rows.Scan(
			&task.ID,
			&task.URL,
			&task.CanonicalURL,
			&task.Status,
			&task.Error,
			&task.ResultZipPath,
		); err != nil {
			return "", err
		}
		if err := builder.AddTask(task); err != nil {
			return "", err
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return builder.SumHex(), nil
}

func (w *PostgresV2Writer) ChecksumV2MigratedEvents(ctx context.Context) (string, error) {
	rows, err := w.db.Query(
		ctx,
		`
		SELECT task_id, event_type, from_status, to_status, payload_json::text
		FROM v2_task_events
		WHERE event_type = 'MIGRATED'
		ORDER BY task_id ASC, id ASC
		`,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	builder := newChecksumBuilder()
	for rows.Next() {
		var event V2TaskEvent
		if err := rows.Scan(
			&event.TaskID,
			&event.EventType,
			&event.FromStatus,
			&event.ToStatus,
			&event.PayloadJSON,
		); err != nil {
			return "", err
		}
		if err := builder.AddMigratedEvent(event); err != nil {
			return "", err
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return builder.SumHex(), nil
}

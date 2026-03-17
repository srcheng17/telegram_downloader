package migration

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

type LegacyTask struct {
	ID            string
	URL           string
	CanonicalURL  *string
	Status        string
	StartTime     float64
	Error         *string
	ResultZipPath *string
}

type LegacyReader interface {
	CountLegacyTasks(ctx context.Context) (int, error)
	ReadLegacyTasksAfterID(ctx context.Context, lastID string, limit int) ([]LegacyTask, error)
}

type legacyTaskQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type PostgresLegacyReader struct {
	db legacyTaskQuerier
}

func NewPostgresLegacyReader(db legacyTaskQuerier) *PostgresLegacyReader {
	return &PostgresLegacyReader{db: db}
}

func (r *PostgresLegacyReader) CountLegacyTasks(ctx context.Context) (int, error) {
	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *PostgresLegacyReader) ReadLegacyTasksAfterID(ctx context.Context, lastID string, limit int) ([]LegacyTask, error) {
	if limit <= 0 {
		limit = 1
	}
	lastID = strings.TrimSpace(lastID)

	rows, err := r.db.Query(
		ctx,
		`
		SELECT
			id,
			url,
			canonical_url,
			status,
			start_time,
			error,
			result_zip_path
		FROM tasks
		WHERE ($1 = '' OR id > $1)
		ORDER BY id ASC
		LIMIT $2
		`,
		lastID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]LegacyTask, 0, limit)
	for rows.Next() {
		var item LegacyTask
		if err := rows.Scan(
			&item.ID,
			&item.URL,
			&item.CanonicalURL,
			&item.Status,
			&item.StartTime,
			&item.Error,
			&item.ResultZipPath,
		); err != nil {
			return nil, err
		}
		item.ID = strings.TrimSpace(item.ID)
		item.URL = strings.TrimSpace(item.URL)
		item.Status = strings.TrimSpace(item.Status)
		tasks = append(tasks, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
)

type storeExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Store struct {
	pool storeExecutor
}

type TransitionTerminalInput struct {
	TaskID        string
	Worker        string
	Status        string
	Error         *string
	ResultZipPath *string
}

var terminalStatuses = map[string]struct{}{
	"SUCCESS":  {},
	"FAILED":   {},
	"CANCELED": {},
}

const taskSelectFields = `
	id,
	url,
	canonical_url,
	status,
	start_time,
	error,
	progress,
	total_images,
	image_concurrency,
	result_zip_path,
	author,
	series_name,
	comic_name,
	summary,
	tags_raw,
	tags_normalized,
	genres_raw,
	genres_normalized
`

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) GetStatusCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT status, COUNT(*) AS count
		FROM tasks
		GROUP BY status
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

func (s *Store) ListLogs(ctx context.Context, query domain.LogQuery) (domain.LogListResult, error) {
	safePage := query.Page
	if safePage < 1 {
		safePage = 1
	}
	safePerPage := query.PerPage
	if safePerPage < 1 {
		safePerPage = domain.DefaultLogsPerPage
	}
	if safePerPage > domain.MaxLogsPerPage {
		safePerPage = domain.MaxLogsPerPage
	}

	whereSQL, whereArgs, placeholderIndex := buildWhereClause(query.Status, query.Keyword)

	countQuery := "SELECT COUNT(*) FROM tasks"
	if whereSQL != "" {
		countQuery += " WHERE " + whereSQL
	}

	var total int
	if err := s.pool.QueryRow(ctx, countQuery, whereArgs...).Scan(&total); err != nil {
		return domain.LogListResult{}, err
	}

	totalPages := 1
	if total > 0 {
		totalPages = (total + safePerPage - 1) / safePerPage
	}
	if safePage > totalPages {
		safePage = totalPages
	}
	offset := (safePage - 1) * safePerPage

	rowsQuery := "SELECT " + taskSelectFields + " FROM tasks"
	if whereSQL != "" {
		rowsQuery += " WHERE " + whereSQL
	}
	rowsQuery += fmt.Sprintf(" ORDER BY start_time DESC LIMIT $%d OFFSET $%d", placeholderIndex, placeholderIndex+1)

	rowsArgs := append(append([]any{}, whereArgs...), safePerPage, offset)
	rows, err := s.pool.Query(ctx, rowsQuery, rowsArgs...)
	if err != nil {
		return domain.LogListResult{}, err
	}
	defer rows.Close()

	logs := make([]domain.TaskLog, 0, safePerPage)
	for rows.Next() {
		logItem, err := scanTaskLog(rows)
		if err != nil {
			return domain.LogListResult{}, err
		}
		logs = append(logs, logItem)
	}
	if err := rows.Err(); err != nil {
		return domain.LogListResult{}, err
	}

	return domain.LogListResult{
		Logs:       logs,
		Total:      total,
		Page:       safePage,
		PerPage:    safePerPage,
		TotalPages: totalPages,
	}, nil
}

func (s *Store) ClaimDownloadTask(
	ctx context.Context,
	input domain.ClaimDownloadTaskInput,
) (domain.ClaimDownloadTaskResult, error) {
	task := input.Task
	canonicalURL := strings.TrimSpace(deref(task.CanonicalURL))
	if canonicalURL == "" {
		canonicalURL = strings.TrimSpace(task.URL)
		task.CanonicalURL = stringPtr(canonicalURL)
	}

	activeStatuses := make([]string, 0, len(input.ActiveStatuses))
	for _, status := range input.ActiveStatuses {
		normalized := strings.TrimSpace(status)
		if normalized == "" {
			continue
		}
		activeStatuses = append(activeStatuses, normalized)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ClaimDownloadTaskResult{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", canonicalURL); err != nil {
		return domain.ClaimDownloadTaskResult{}, err
	}

	if input.ReuseSuccess {
		row := tx.QueryRow(
			ctx,
			`
			SELECT `+taskSelectFields+`
			FROM tasks
			WHERE
				status = 'SUCCESS'
				AND result_zip_path IS NOT NULL
				AND result_zip_path != ''
				AND (
					canonical_url = $1
					OR (canonical_url IS NULL AND url = $1)
				)
			ORDER BY start_time DESC
			LIMIT 1
			`,
			canonicalURL,
		)
		existingTask, scanErr := scanTaskLog(row)
		if scanErr == nil {
			if err := tx.Commit(ctx); err != nil {
				return domain.ClaimDownloadTaskResult{}, err
			}
			return domain.ClaimDownloadTaskResult{
				Decision: domain.ClaimDecisionReuseSuccess,
				Task:     existingTask,
			}, nil
		}
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return domain.ClaimDownloadTaskResult{}, scanErr
		}
	}

	if len(activeStatuses) > 0 {
		row := tx.QueryRow(
			ctx,
			`
			SELECT `+taskSelectFields+`
			FROM tasks
			WHERE
				status = ANY($1)
				AND (
					canonical_url = $2
					OR (canonical_url IS NULL AND url = $2)
				)
			ORDER BY start_time DESC
			LIMIT 1
			`,
			activeStatuses,
			canonicalURL,
		)
		existingTask, scanErr := scanTaskLog(row)
		if scanErr == nil {
			if err := tx.Commit(ctx); err != nil {
				return domain.ClaimDownloadTaskResult{}, err
			}
			return domain.ClaimDownloadTaskResult{
				Decision: domain.ClaimDecisionReuseActive,
				Task:     existingTask,
			}, nil
		}
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return domain.ClaimDownloadTaskResult{}, scanErr
		}
	}

	if _, err := tx.Exec(
		ctx,
		`
		INSERT INTO tasks (
			id,
			url,
			canonical_url,
			status,
			start_time,
			error,
			progress,
			total_images,
			image_concurrency,
			result_zip_path,
			author,
			series_name,
			comic_name,
			summary,
			tags_raw,
			tags_normalized,
			genres_raw,
			genres_normalized
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18
		)
		`,
		task.ID,
		task.URL,
		task.CanonicalURL,
		task.Status,
		task.StartTime,
		task.Error,
		task.Progress,
		task.TotalImages,
		task.ImageConcurrency,
		task.ResultZipPath,
		task.Author,
		task.SeriesName,
		task.ComicName,
		task.Summary,
		task.TagsRaw,
		task.TagsNormalized,
		task.GenresRaw,
		task.GenresNormalized,
	); err != nil {
		return domain.ClaimDownloadTaskResult{}, err
	}

	row := tx.QueryRow(ctx, "SELECT "+taskSelectFields+" FROM tasks WHERE id = $1", task.ID)
	createdTask, err := scanTaskLog(row)
	if err != nil {
		return domain.ClaimDownloadTaskResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ClaimDownloadTaskResult{}, err
	}
	return domain.ClaimDownloadTaskResult{
		Decision: domain.ClaimDecisionCreated,
		Task:     createdTask,
	}, nil
}

func (s *Store) HasActiveTasks(ctx context.Context, statuses []string) (bool, error) {
	filtered := make([]string, 0, len(statuses))
	for _, status := range statuses {
		normalized := strings.TrimSpace(status)
		if normalized == "" {
			continue
		}
		filtered = append(filtered, normalized)
	}
	if len(filtered) == 0 {
		return false, nil
	}

	placeholders := make([]string, 0, len(filtered))
	args := make([]any, 0, len(filtered))
	for idx, status := range filtered {
		placeholders = append(placeholders, fmt.Sprintf("$%d", idx+1))
		args = append(args, status)
	}

	query := fmt.Sprintf("SELECT 1 FROM tasks WHERE status IN (%s) LIMIT 1", strings.Join(placeholders, ", "))
	var marker int
	err := s.pool.QueryRow(ctx, query, args...).Scan(&marker)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (s *Store) ClearResultZipPath(ctx context.Context, taskID string) error {
	_, err := s.pool.Exec(
		ctx,
		"UPDATE tasks SET result_zip_path = NULL WHERE id = $1",
		taskID,
	)
	return err
}

func (s *Store) MarkTaskFailed(ctx context.Context, taskID, message string) error {
	_, err := s.pool.Exec(
		ctx,
		`
		UPDATE tasks
		SET
			status = 'FAILED',
			error = $2,
			result_zip_path = NULL
		WHERE id = $1
		`,
		taskID,
		message,
	)
	return err
}

func (s *Store) GetTask(ctx context.Context, taskID string) (*domain.TaskLog, error) {
	row := s.pool.QueryRow(
		ctx,
		"SELECT "+taskSelectFields+" FROM tasks WHERE id = $1",
		taskID,
	)
	task, err := scanTaskLog(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *Store) RequestTaskCancel(ctx context.Context, taskID string, cancelError string) error {
	_, err := s.pool.Exec(
		ctx,
		`
		UPDATE tasks
		SET
			status = 'CANCEL_REQUESTED',
			cancel_requested_at = NOW(),
			error = $2,
			result_zip_path = NULL
		WHERE id = $1
		`,
		taskID,
		cancelError,
	)
	return err
}

func (s *Store) TransitionPendingToInProgress(ctx context.Context, taskID, token, worker string) (bool, error) {
	tag, err := s.pool.Exec(
		ctx,
		`
		UPDATE tasks
		SET
			status = 'IN_PROGRESS',
			claimed_by = $3,
			claimed_at = NOW(),
			heartbeat_at = NOW(),
			version = version + 1
		WHERE
			id = $1
			AND status = 'PENDING'
			AND enqueue_token = $2
		`,
		taskID,
		token,
		worker,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) UpdateTaskHeartbeat(ctx context.Context, taskID, worker string) error {
	_, err := s.pool.Exec(
		ctx,
		`
		UPDATE tasks
		SET
			heartbeat_at = NOW(),
			version = version + 1
		WHERE
			id = $1
			AND status = 'IN_PROGRESS'
			AND claimed_by = $2
		`,
		taskID,
		worker,
	)
	return err
}

func (s *Store) TransitionToTerminal(ctx context.Context, input TransitionTerminalInput) error {
	normalizedStatus := strings.ToUpper(strings.TrimSpace(input.Status))
	if _, ok := terminalStatuses[normalizedStatus]; !ok {
		return fmt.Errorf("invalid terminal status %q", input.Status)
	}

	_, err := s.pool.Exec(
		ctx,
		`
		UPDATE tasks
		SET
			status = $3,
			error = $4,
			result_zip_path = $5,
			end_time = EXTRACT(EPOCH FROM NOW()),
			heartbeat_at = NOW(),
			version = version + 1
		WHERE
			id = $1
			AND claimed_by = $2
			AND status IN ('IN_PROGRESS', 'CANCEL_REQUESTED')
		`,
		input.TaskID,
		input.Worker,
		normalizedStatus,
		input.Error,
		input.ResultZipPath,
	)
	return err
}

func buildWhereClause(status, keyword string) (string, []any, int) {
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 5)
	placeholderIndex := 1

	normalizedStatus := strings.TrimSpace(strings.ToUpper(status))
	if normalizedStatus != "" {
		clauses = append(clauses, fmt.Sprintf("status = $%d", placeholderIndex))
		args = append(args, normalizedStatus)
		placeholderIndex++
	}

	normalizedKeyword := strings.TrimSpace(keyword)
	if normalizedKeyword != "" {
		likeValue := "%" + normalizedKeyword + "%"
		clauses = append(
			clauses,
			fmt.Sprintf(
				"(id ILIKE $%d OR url ILIKE $%d OR canonical_url ILIKE $%d OR error ILIKE $%d)",
				placeholderIndex,
				placeholderIndex+1,
				placeholderIndex+2,
				placeholderIndex+3,
			),
		)
		args = append(args, likeValue, likeValue, likeValue, likeValue)
		placeholderIndex += 4
	}

	return strings.Join(clauses, " AND "), args, placeholderIndex
}

type taskRowScanner interface {
	Scan(dest ...any) error
}

func scanTaskLog(scanner taskRowScanner) (domain.TaskLog, error) {
	var logItem domain.TaskLog
	err := scanner.Scan(
		&logItem.ID,
		&logItem.URL,
		&logItem.CanonicalURL,
		&logItem.Status,
		&logItem.StartTime,
		&logItem.Error,
		&logItem.Progress,
		&logItem.TotalImages,
		&logItem.ImageConcurrency,
		&logItem.ResultZipPath,
		&logItem.Author,
		&logItem.SeriesName,
		&logItem.ComicName,
		&logItem.Summary,
		&logItem.TagsRaw,
		&logItem.TagsNormalized,
		&logItem.GenresRaw,
		&logItem.GenresNormalized,
	)
	if err != nil {
		return domain.TaskLog{}, err
	}
	return logItem, nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPtr(value string) *string {
	copied := value
	return &copied
}

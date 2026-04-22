package taskcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

var _ app.Repository = (*Store)(nil)

type Store struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, now: time.Now}
}

type executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Store) CreateTask(ctx context.Context, task app.Task, input app.Input, progress domain.Progress) (app.Task, error) {
	now := s.clock()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	if task.Attempt < 0 {
		task.Attempt = 0
	}
	input.TaskID = task.ID

	metadata, err := encodeMetadata(input.Metadata)
	if err != nil {
		return app.Task{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return app.Task{}, err
	}
	defer rollback(ctx, tx)

	var readyAt *time.Time
	if task.Status == domain.StatusReady {
		readyAt = &now
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO task_core_tasks (
			id, kind, status, attempt, created_at, updated_at, ready_at, last_error, lease_owner, lease_expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), $10)
	`, task.ID, string(task.Kind), string(task.Status), task.Attempt, task.CreatedAt, task.UpdatedAt, readyAt, task.LastError, task.LeaseOwner, task.LeaseExpiresAt)
	if err != nil {
		return app.Task{}, mapPgError(err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO task_core_inputs (
			task_id, url, canonical_url, source_archive_name, source_archive_path, source_archive_size, metadata
		) VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $7::jsonb)
	`, input.TaskID, input.URL, input.CanonicalURL, input.SourceArchiveName, input.SourceArchivePath, nullablePositiveSize(input.SourceArchiveSize), string(metadata))
	if err != nil {
		return app.Task{}, mapPgError(err)
	}

	if err := upsertProgress(ctx, tx, task.ID, progress, now); err != nil {
		return app.Task{}, err
	}
	if err := insertEvent(ctx, tx, task.ID, "TASK_CREATED", nil, &task.Status, domain.ActorAPI, "task created", nil, now); err != nil {
		return app.Task{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return app.Task{}, err
	}
	return task, nil
}

func (s *Store) GetTask(ctx context.Context, taskID string) (*app.TaskView, error) {
	row := s.pool.QueryRow(ctx, taskViewQuery()+` WHERE t.id = $1`, taskID)
	view, err := scanTaskView(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, app.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *Store) Transition(ctx context.Context, taskID string, to domain.Status, actor domain.Actor, message string) (app.Task, error) {
	now := s.clock()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return app.Task{}, err
	}
	defer rollback(ctx, tx)

	current, err := lockTaskStatus(ctx, tx, taskID)
	if err != nil {
		return app.Task{}, err
	}
	if err := domain.ValidateTransition(current, to, actor); err != nil {
		return app.Task{}, app.ErrConflict
	}

	from := current
	var task app.Task
	task, err = scanTask(tx.QueryRow(ctx, `
		UPDATE task_core_tasks
		SET status = $2,
		    updated_at = $3,
		    ready_at = CASE WHEN $2 = 'READY' THEN $3 ELSE ready_at END,
		    cancel_requested_at = CASE WHEN $2 = 'CANCELING' THEN $3 ELSE cancel_requested_at END,
		    finished_at = CASE WHEN $2 IN ('SUCCEEDED', 'FAILED', 'CANCELED') THEN $3 ELSE finished_at END,
		    last_error = CASE WHEN $2 = 'FAILED' THEN NULLIF($4, '') ELSE last_error END
		WHERE id = $1
		RETURNING id, kind, status, attempt, last_error, lease_owner, lease_expires_at, created_at, updated_at
	`, taskID, string(to), now, strings.TrimSpace(message)))
	if err != nil {
		return app.Task{}, err
	}
	if err := insertEvent(ctx, tx, taskID, "STATUS_TRANSITION", &from, &to, actor, message, nil, now); err != nil {
		return app.Task{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.Task{}, err
	}
	return task, nil
}

func (s *Store) AttachUploadSource(ctx context.Context, input app.AttachUploadSourceInput, progress domain.Progress) (app.Task, error) {
	if err := domain.ValidateTransition(domain.StatusCreated, domain.StatusReady, domain.ActorAPI); err != nil {
		return app.Task{}, err
	}
	now := s.clock()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return app.Task{}, err
	}
	defer rollback(ctx, tx)

	var kind string
	var currentText string
	err = tx.QueryRow(ctx, `SELECT kind, status FROM task_core_tasks WHERE id = $1 FOR UPDATE`, input.TaskID).Scan(&kind, &currentText)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Task{}, app.ErrNotFound
	}
	if err != nil {
		return app.Task{}, err
	}
	if domain.Kind(kind) != domain.KindUpload {
		return app.Task{}, app.ErrConflict
	}
	current := domain.Status(currentText)
	if err := domain.ValidateTransition(current, domain.StatusReady, domain.ActorAPI); err != nil {
		return app.Task{}, app.ErrConflict
	}

	_, err = tx.Exec(ctx, `
		UPDATE task_core_inputs
		SET source_archive_name = $2,
		    source_archive_path = $3,
		    source_archive_size = $4
		WHERE task_id = $1
	`, input.TaskID, input.Name, input.Path, input.Size)
	if err != nil {
		return app.Task{}, err
	}

	var task app.Task
	task, err = scanTask(tx.QueryRow(ctx, `
		UPDATE task_core_tasks
		SET status = 'READY', ready_at = $2, updated_at = $2
		WHERE id = $1
		RETURNING id, kind, status, attempt, last_error, lease_owner, lease_expires_at, created_at, updated_at
	`, input.TaskID, now))
	if err != nil {
		return app.Task{}, err
	}
	to := domain.StatusReady
	if err := upsertProgress(ctx, tx, input.TaskID, progress, now); err != nil {
		return app.Task{}, err
	}
	if err := insertEvent(ctx, tx, input.TaskID, "UPLOAD_SOURCE_ATTACHED", &current, &to, domain.ActorAPI, "upload source attached", nil, now); err != nil {
		return app.Task{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.Task{}, err
	}
	return task, nil
}

func (s *Store) Retry(ctx context.Context, taskID string, progress domain.Progress) (app.Task, error) {
	now := s.clock()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return app.Task{}, err
	}
	defer rollback(ctx, tx)

	current, err := lockTaskStatus(ctx, tx, taskID)
	if err != nil {
		return app.Task{}, err
	}
	to := domain.StatusReady
	if err := domain.ValidateTransition(current, to, domain.ActorAPI); err != nil {
		return app.Task{}, app.ErrConflict
	}

	_, err = tx.Exec(ctx, `DELETE FROM task_core_results WHERE task_id = $1`, taskID)
	if err != nil {
		return app.Task{}, err
	}

	var task app.Task
	task, err = scanTask(tx.QueryRow(ctx, `
		UPDATE task_core_tasks
		SET status = 'READY',
		    attempt = 0,
		    ready_at = $2,
		    started_at = NULL,
		    finished_at = NULL,
		    cancel_requested_at = NULL,
		    last_error = NULL,
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    heartbeat_at = NULL,
		    updated_at = $2
		WHERE id = $1
		RETURNING id, kind, status, attempt, last_error, lease_owner, lease_expires_at, created_at, updated_at
	`, taskID, now))
	if err != nil {
		return app.Task{}, err
	}
	if err := upsertProgress(ctx, tx, taskID, progress, now); err != nil {
		return app.Task{}, err
	}
	if err := insertEvent(ctx, tx, taskID, "TASK_RETRIED", &current, &to, domain.ActorAPI, "task retried", nil, now); err != nil {
		return app.Task{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.Task{}, err
	}
	return task, nil
}

func (s *Store) ClaimNext(ctx context.Context, workerID string, leaseTTL time.Duration) (*app.Task, error) {
	if err := domain.ValidateTransition(domain.StatusReady, domain.StatusRunning, domain.ActorWorker); err != nil {
		return nil, err
	}
	now := s.clock()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, tx)

	var task app.Task
	task, err = scanTask(tx.QueryRow(ctx, `
		WITH candidate AS (
		    SELECT id, attempt
		    FROM task_core_tasks
		    WHERE status = 'READY'
		    ORDER BY ready_at ASC NULLS LAST, created_at ASC
		    FOR UPDATE SKIP LOCKED
		    LIMIT 1
		)
		UPDATE task_core_tasks t
		SET status = 'RUNNING',
		    attempt = candidate.attempt + 1,
		    started_at = $3::timestamptz,
		    updated_at = $3::timestamptz,
		    lease_owner = $1,
		    lease_expires_at = $3::timestamptz + $2::interval,
		    heartbeat_at = $3::timestamptz
		FROM candidate
		WHERE t.id = candidate.id
		RETURNING t.id, t.kind, t.status, t.attempt, t.last_error, t.lease_owner, t.lease_expires_at, t.created_at, t.updated_at
	`, workerID, intervalString(leaseTTL), now))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	from := domain.StatusReady
	to := domain.StatusRunning
	if err := insertEvent(ctx, tx, task.ID, "TASK_CLAIMED", &from, &to, domain.ActorWorker, "task claimed", map[string]any{"worker_id": workerID, "attempt": task.Attempt}, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *Store) Heartbeat(ctx context.Context, taskID string, workerID string, leaseTTL time.Duration) (app.HeartbeatResult, error) {
	now := s.clock()
	var status string
	err := s.pool.QueryRow(ctx, `
		UPDATE task_core_tasks
		SET lease_expires_at = $4::timestamptz + $3::interval,
		    heartbeat_at = $4::timestamptz,
		    updated_at = $4::timestamptz
		WHERE id = $1 AND lease_owner = $2 AND status IN ('RUNNING', 'CANCELING')
		RETURNING status
	`, taskID, workerID, intervalString(leaseTTL), now).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		if exists, existsErr := taskExists(ctx, s.pool, taskID); existsErr != nil {
			return app.HeartbeatResult{}, existsErr
		} else if !exists {
			return app.HeartbeatResult{}, app.ErrNotFound
		}
		return app.HeartbeatResult{}, app.ErrConflict
	}
	if err != nil {
		return app.HeartbeatResult{}, err
	}
	return app.HeartbeatResult{CancelRequested: domain.Status(status) == domain.StatusCanceling}, nil
}

func (s *Store) UpdateProgress(ctx context.Context, taskID string, progress domain.Progress) error {
	exists, err := taskExists(ctx, s.pool, taskID)
	if err != nil {
		return err
	}
	if !exists {
		return app.ErrNotFound
	}
	return upsertProgress(ctx, s.pool, taskID, progress, s.clock())
}

func (s *Store) Complete(ctx context.Context, in app.CompleteInput) error {
	if err := domain.ValidateTransition(domain.StatusRunning, domain.StatusSucceeded, domain.ActorWorker); err != nil {
		return err
	}
	now := s.clock()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	from := domain.StatusRunning
	to := domain.StatusSucceeded
	if err := updateWorkerTerminal(ctx, tx, in.TaskID, in.WorkerID, in.Attempt, to, "", now); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO task_core_results (task_id, artifact_path, artifact_name, artifact_size, artifact_kind, created_at)
		VALUES ($1, $2, $3, $4, 'cbz', $5)
		ON CONFLICT (task_id) DO UPDATE SET
		    artifact_path = EXCLUDED.artifact_path,
		    artifact_name = EXCLUDED.artifact_name,
		    artifact_size = EXCLUDED.artifact_size,
		    artifact_kind = EXCLUDED.artifact_kind,
		    created_at = EXCLUDED.created_at
	`, in.TaskID, in.ArtifactPath, in.ArtifactName, in.ArtifactSize, now)
	if err != nil {
		return err
	}
	if err := upsertProgress(ctx, tx, in.TaskID, domain.NewProgress(domain.PhaseDone, 1, 1, domain.UnitSteps, "完成"), now); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, in.TaskID, "TASK_SUCCEEDED", &from, &to, domain.ActorWorker, "task completed", map[string]any{"worker_id": in.WorkerID, "attempt": in.Attempt}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Fail(ctx context.Context, in app.FailInput) error {
	if err := domain.ValidateTransition(domain.StatusRunning, domain.StatusFailed, domain.ActorWorker); err != nil {
		return err
	}
	message := strings.TrimSpace(in.Message)
	if message == "" {
		message = "task failed"
	}
	now := s.clock()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	from := domain.StatusRunning
	to := domain.StatusFailed
	if err := updateWorkerTerminal(ctx, tx, in.TaskID, in.WorkerID, in.Attempt, to, message, now); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, in.TaskID, "TASK_FAILED", &from, &to, domain.ActorWorker, message, map[string]any{"worker_id": in.WorkerID, "attempt": in.Attempt}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int) error {
	if err := domain.ValidateTransition(domain.StatusCanceling, domain.StatusCanceled, domain.ActorWorker); err != nil {
		return err
	}
	now := s.clock()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	command, err := tx.Exec(ctx, `
		UPDATE task_core_tasks
		SET status = 'CANCELED',
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    heartbeat_at = NULL,
		    finished_at = $4,
		    updated_at = $4
		WHERE id = $1 AND lease_owner = $2 AND attempt = $3 AND status = 'CANCELING'
	`, taskID, workerID, attempt, now)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return conditionalUpdateError(ctx, tx, taskID)
	}
	from := domain.StatusCanceling
	to := domain.StatusCanceled
	if err := insertEvent(ctx, tx, taskID, "TASK_CANCELED", &from, &to, domain.ActorWorker, "cancel acknowledged", map[string]any{"worker_id": workerID, "attempt": attempt}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RecoverExpired(ctx context.Context, maxAttempts int) (app.RecoveryResult, error) {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	now := s.clock()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return app.RecoveryResult{}, err
	}
	defer rollback(ctx, tx)

	running, err := expiredRunningTasks(ctx, tx, now)
	if err != nil {
		return app.RecoveryResult{}, err
	}
	staleCanceling, err := staleCancelingTasks(ctx, tx, now.Add(-2*time.Minute))
	if err != nil {
		return app.RecoveryResult{}, err
	}

	result := app.RecoveryResult{}
	for _, task := range running {
		if task.attempt < maxAttempts {
			if err := domain.ValidateTransition(domain.StatusRunning, domain.StatusReady, domain.ActorRecovery); err != nil {
				return app.RecoveryResult{}, err
			}
			_, err := tx.Exec(ctx, `
				UPDATE task_core_tasks
				SET status = 'READY',
				    ready_at = $2,
				    lease_owner = NULL,
				    lease_expires_at = NULL,
				    heartbeat_at = NULL,
				    updated_at = $2
				WHERE id = $1
			`, task.id, now)
			if err != nil {
				return app.RecoveryResult{}, err
			}
			from := domain.StatusRunning
			to := domain.StatusReady
			if err := insertEvent(ctx, tx, task.id, "LEASE_EXPIRED_REQUEUED", &from, &to, domain.ActorRecovery, "lease expired; task requeued", map[string]any{"attempt": task.attempt}, now); err != nil {
				return app.RecoveryResult{}, err
			}
			result.Requeued++
			continue
		}

		_, err := tx.Exec(ctx, `
			UPDATE task_core_tasks
			SET status = 'FAILED',
			    last_error = 'lease expired',
			    lease_owner = NULL,
			    lease_expires_at = NULL,
			    heartbeat_at = NULL,
			    finished_at = $2,
			    updated_at = $2
			WHERE id = $1
		`, task.id, now)
		if err != nil {
			return app.RecoveryResult{}, err
		}
		from := domain.StatusRunning
		to := domain.StatusFailed
		if err := insertEvent(ctx, tx, task.id, "LEASE_EXPIRED_FAILED", &from, &to, domain.ActorRecovery, "lease expired; max attempts reached", map[string]any{"attempt": task.attempt, "max_attempts": maxAttempts}, now); err != nil {
			return app.RecoveryResult{}, err
		}
		result.Failed++
	}

	for _, id := range staleCanceling {
		if err := domain.ValidateTransition(domain.StatusCanceling, domain.StatusCanceled, domain.ActorRecovery); err != nil {
			return app.RecoveryResult{}, err
		}
		_, err := tx.Exec(ctx, `
			UPDATE task_core_tasks
			SET status = 'CANCELED',
			    lease_owner = NULL,
			    lease_expires_at = NULL,
			    heartbeat_at = NULL,
			    finished_at = $2,
			    updated_at = $2
			WHERE id = $1
		`, id, now)
		if err != nil {
			return app.RecoveryResult{}, err
		}
		from := domain.StatusCanceling
		to := domain.StatusCanceled
		if err := insertEvent(ctx, tx, id, "CANCELING_TIMED_OUT_CANCELED", &from, &to, domain.ActorRecovery, "cancel timed out", nil, now); err != nil {
			return app.RecoveryResult{}, err
		}
		result.Canceled++
	}

	if err := tx.Commit(ctx); err != nil {
		return app.RecoveryResult{}, err
	}
	return result, nil
}

func (s *Store) ListTasks(ctx context.Context, limit int, offset int) ([]app.TaskView, error) {
	rows, err := s.pool.Query(ctx, taskViewQuery()+` ORDER BY t.created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	views := make([]app.TaskView, 0)
	for rows.Next() {
		view, err := scanTaskView(rows)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return views, nil
}

func (s *Store) clock() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

type runningTask struct {
	id      string
	attempt int
}

func expiredRunningTasks(ctx context.Context, q executor, now time.Time) ([]runningTask, error) {
	rows, err := q.Query(ctx, `
		SELECT id, attempt
		FROM task_core_tasks
		WHERE status = 'RUNNING' AND lease_expires_at < $1
		FOR UPDATE
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []runningTask
	for rows.Next() {
		var task runningTask
		if err := rows.Scan(&task.id, &task.attempt); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func staleCancelingTasks(ctx context.Context, q executor, cutoff time.Time) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT id
		FROM task_core_tasks
		WHERE status = 'CANCELING' AND updated_at < $1
		FOR UPDATE
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func updateWorkerTerminal(ctx context.Context, tx pgx.Tx, taskID, workerID string, attempt int, to domain.Status, lastError string, now time.Time) error {
	command, err := tx.Exec(ctx, `
		UPDATE task_core_tasks
		SET status = $4,
		    last_error = CASE WHEN $4 = 'FAILED' THEN NULLIF($5, '') ELSE last_error END,
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    heartbeat_at = NULL,
		    finished_at = $6,
		    updated_at = $6
		WHERE id = $1 AND lease_owner = $2 AND attempt = $3 AND status = 'RUNNING'
	`, taskID, workerID, attempt, string(to), lastError, now)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return conditionalUpdateError(ctx, tx, taskID)
	}
	return nil
}

func lockTaskStatus(ctx context.Context, tx pgx.Tx, taskID string) (domain.Status, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM task_core_tasks WHERE id = $1 FOR UPDATE`, taskID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", app.ErrNotFound
	}
	return domain.Status(status), err
}

func conditionalUpdateError(ctx context.Context, q executor, taskID string) error {
	exists, err := taskExists(ctx, q, taskID)
	if err != nil {
		return err
	}
	if !exists {
		return app.ErrNotFound
	}
	return app.ErrConflict
}

func taskExists(ctx context.Context, q executor, taskID string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM task_core_tasks WHERE id = $1)`, taskID).Scan(&exists)
	return exists, err
}

func upsertProgress(ctx context.Context, q executor, taskID string, progress domain.Progress, now time.Time) error {
	_, err := q.Exec(ctx, `
		INSERT INTO task_core_progress (task_id, phase, current, total, unit, message, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (task_id) DO UPDATE SET
		    phase = EXCLUDED.phase,
		    current = EXCLUDED.current,
		    total = EXCLUDED.total,
		    unit = EXCLUDED.unit,
		    message = EXCLUDED.message,
		    updated_at = EXCLUDED.updated_at
	`, taskID, string(progress.Phase), progress.Current, progress.Total, string(progress.Unit), progress.Message, now)
	return err
}

func insertEvent(ctx context.Context, q executor, taskID, eventType string, from *domain.Status, to *domain.Status, actor domain.Actor, message string, payload map[string]any, now time.Time) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO task_core_events (task_id, event_type, from_status, to_status, actor, message, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
	`, taskID, eventType, statusString(from), statusString(to), string(actor), strings.TrimSpace(message), string(payloadJSON), now)
	return err
}

func taskViewQuery() string {
	return `
		SELECT
			t.id, t.kind, t.status, t.attempt, t.last_error, t.lease_owner, t.lease_expires_at, t.created_at, t.updated_at,
			i.task_id, i.url, i.canonical_url, i.source_archive_name, i.source_archive_path, i.source_archive_size, i.metadata::text,
			p.phase, p.current, p.total, p.unit, p.message,
			r.task_id, r.artifact_path, r.artifact_name, r.artifact_size, r.artifact_kind, r.komga_target_path
		FROM task_core_tasks t
		JOIN task_core_inputs i ON i.task_id = t.id
		LEFT JOIN task_core_progress p ON p.task_id = t.id
		LEFT JOIN task_core_results r ON r.task_id = t.id`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTaskView(row rowScanner) (app.TaskView, error) {
	var view app.TaskView
	var taskScan dbTaskScan
	var inputScan dbInputScan
	var progressScan dbProgressScan
	var resultScan dbResultScan
	if err := row.Scan(
		&view.Task.ID,
		&taskScan.kind,
		&taskScan.status,
		&view.Task.Attempt,
		&taskScan.lastError,
		&taskScan.leaseOwner,
		&taskScan.leaseExpiresAt,
		&view.Task.CreatedAt,
		&view.Task.UpdatedAt,
		&view.Input.TaskID,
		&inputScan.url,
		&inputScan.canonicalURL,
		&inputScan.sourceArchiveName,
		&inputScan.sourceArchivePath,
		&inputScan.sourceArchiveSize,
		&inputScan.metadata,
		&progressScan.phase,
		&progressScan.current,
		&progressScan.total,
		&progressScan.unit,
		&progressScan.message,
		&resultScan.taskID,
		&resultScan.artifactPath,
		&resultScan.artifactName,
		&resultScan.artifactSize,
		&resultScan.artifactKind,
		&resultScan.komgaTargetPath,
	); err != nil {
		return app.TaskView{}, err
	}
	taskScan.apply(&view.Task)
	if err := inputScan.apply(&view.Input); err != nil {
		return app.TaskView{}, err
	}
	progressScan.apply(&view.Progress)
	result := resultScan.result()
	view.Result = result
	return view, nil
}

type dbTaskScan struct {
	kind           string
	status         string
	lastError      sql.NullString
	leaseOwner     sql.NullString
	leaseExpiresAt sql.NullTime
}

func (s *dbTaskScan) apply(task *app.Task) {
	task.Kind = domain.Kind(s.kind)
	task.Status = domain.Status(s.status)
	if s.lastError.Valid {
		task.LastError = s.lastError.String
	}
	if s.leaseOwner.Valid {
		task.LeaseOwner = s.leaseOwner.String
	}
	if s.leaseExpiresAt.Valid {
		expires := s.leaseExpiresAt.Time
		task.LeaseExpiresAt = &expires
	}
}

func scanTask(row rowScanner) (app.Task, error) {
	var task app.Task
	var scan dbTaskScan
	if err := row.Scan(
		&task.ID,
		&scan.kind,
		&scan.status,
		&task.Attempt,
		&scan.lastError,
		&scan.leaseOwner,
		&scan.leaseExpiresAt,
		&task.CreatedAt,
		&task.UpdatedAt,
	); err != nil {
		return app.Task{}, err
	}
	scan.apply(&task)
	return task, nil
}

type dbInputScan struct {
	url               sql.NullString
	canonicalURL      sql.NullString
	sourceArchiveName sql.NullString
	sourceArchivePath sql.NullString
	sourceArchiveSize sql.NullInt64
	metadata          string
}

func (s *dbInputScan) apply(input *app.Input) error {
	if s.url.Valid {
		input.URL = s.url.String
	}
	if s.canonicalURL.Valid {
		input.CanonicalURL = s.canonicalURL.String
	}
	if s.sourceArchiveName.Valid {
		input.SourceArchiveName = s.sourceArchiveName.String
	}
	if s.sourceArchivePath.Valid {
		input.SourceArchivePath = s.sourceArchivePath.String
	}
	if s.sourceArchiveSize.Valid {
		input.SourceArchiveSize = s.sourceArchiveSize.Int64
	}
	input.Metadata = map[string]string{}
	if strings.TrimSpace(s.metadata) == "" {
		return nil
	}
	return json.Unmarshal([]byte(s.metadata), &input.Metadata)
}

type dbProgressScan struct {
	phase   sql.NullString
	current sql.NullInt64
	total   sql.NullInt64
	unit    sql.NullString
	message sql.NullString
}

func (s *dbProgressScan) apply(progress *domain.Progress) {
	progress.Phase = domain.PhasePreparing
	progress.Unit = domain.UnitNone
	if s.phase.Valid && s.phase.String != "" {
		progress.Phase = domain.Phase(s.phase.String)
	}
	if s.current.Valid {
		progress.Current = s.current.Int64
	}
	if s.total.Valid {
		progress.Total = s.total.Int64
	}
	if s.unit.Valid && s.unit.String != "" {
		progress.Unit = domain.Unit(s.unit.String)
	}
	if s.message.Valid {
		progress.Message = s.message.String
	}
}

type dbResultScan struct {
	taskID          sql.NullString
	artifactPath    sql.NullString
	artifactName    sql.NullString
	artifactSize    sql.NullInt64
	artifactKind    sql.NullString
	komgaTargetPath sql.NullString
}

func (s *dbResultScan) result() *app.Result {
	if !s.taskID.Valid {
		return nil
	}
	result := &app.Result{TaskID: s.taskID.String}
	if s.artifactPath.Valid {
		result.ArtifactPath = s.artifactPath.String
	}
	if s.artifactName.Valid {
		result.ArtifactName = s.artifactName.String
	}
	if s.artifactSize.Valid {
		result.ArtifactSize = s.artifactSize.Int64
	}
	if s.artifactKind.Valid {
		result.ArtifactKind = s.artifactKind.String
	}
	if s.komgaTargetPath.Valid {
		result.KomgaTargetPath = s.komgaTargetPath.String
	}
	return result
}

func encodeMetadata(metadata map[string]string) ([]byte, error) {
	if metadata == nil {
		metadata = map[string]string{}
	}
	return json.Marshal(metadata)
}

func nullablePositiveSize(size int64) any {
	if size == 0 {
		return nil
	}
	return size
}

func statusString(status *domain.Status) any {
	if status == nil {
		return nil
	}
	return string(*status)
}

func intervalString(d time.Duration) string {
	if d <= 0 {
		d = 30 * time.Second
	}
	return fmt.Sprintf("%f seconds", d.Seconds())
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func mapPgError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return app.ErrConflict
	}
	return err
}

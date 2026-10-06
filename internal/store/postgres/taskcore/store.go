package taskcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ryancheng/telegram-downloader/internal/app/telegram"
	metadataDomain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/config"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

var _ app.Repository = (*Store)(nil)

// Keep canonical lookups indexable; only URL-only legacy inputs need the fallback.
const canonicalURLPredicate = `(i.canonical_url = $1 OR ((i.canonical_url IS NULL OR i.canonical_url = '') AND btrim(i.url) = $1))`

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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return app.Task{}, err
	}
	defer rollback(ctx, tx)
	if replay, err := lockSubmission(ctx, tx, input.Submission); err != nil || replay != nil {
		if replay != nil {
			return replay.Task, tx.Commit(ctx)
		}
		return app.Task{}, err
	}
	created, err := s.insertTask(ctx, tx, task, input, progress)
	if err != nil {
		return app.Task{}, err
	}
	if err := saveSubmission(ctx, tx, input.Submission, app.CreateURLResult{Task: created}); err != nil {
		return app.Task{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.Task{}, err
	}
	return created, nil
}

func (s *Store) CreateURLTask(ctx context.Context, task app.Task, input app.Input, progress domain.Progress, force bool) (app.CreateURLResult, error) {
	canonicalURL := strings.TrimSpace(input.CanonicalURL)
	if canonicalURL == "" {
		canonicalURL = strings.TrimSpace(input.URL)
	}
	var telegramSource any
	lockKey := canonicalURL
	if task.Kind == domain.KindTelegram {
		if input.Telegram == nil || telegram.ValidateInput(*input.Telegram) != nil {
			return app.CreateURLResult{}, app.ErrInvalidInput
		}
		encoded, err := json.Marshal(input.Telegram)
		if err != nil {
			return app.CreateURLResult{}, err
		}
		telegramSource = string(encoded)
		lockKey = telegramLockKey(*input.Telegram)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return app.CreateURLResult{}, err
	}
	defer rollback(ctx, tx)
	if replay, err := lockSubmission(ctx, tx, input.Submission); err != nil || replay != nil {
		if replay != nil {
			return *replay, tx.Commit(ctx)
		}
		return app.CreateURLResult{}, err
	}
	finish := func(result app.CreateURLResult) (app.CreateURLResult, error) {
		if err := saveSubmission(ctx, tx, input.Submission, result); err != nil {
			return app.CreateURLResult{}, err
		}
		return result, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return app.CreateURLResult{}, err
	}
	view, err := scanTaskView(tx.QueryRow(ctx, taskViewQuery()+`
 WHERE t.kind = $2 AND (i.telegram_source IS NOT DISTINCT FROM $3::jsonb) AND `+canonicalURLPredicate+` AND t.status IN ('READY', 'RUNNING', 'CANCELING')
 ORDER BY t.created_at DESC, t.id DESC LIMIT 1`, canonicalURL, string(task.Kind), telegramSource))
	if err == nil {
		if input.Submission != nil && !sameReviewedMetadata(input, view.Input) {
			return app.CreateURLResult{}, app.ErrSubmissionSourceConflict
		}
		return finish(app.CreateURLResult{Task: view.Task, Reused: true, Result: view.Result})
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return app.CreateURLResult{}, err
	}
	if !force {
		view, err = scanTaskView(tx.QueryRow(ctx, taskViewQuery()+`
 WHERE t.kind = $2 AND (i.telegram_source IS NOT DISTINCT FROM $3::jsonb) AND `+canonicalURLPredicate+` AND t.status = 'SUCCEEDED' AND r.task_id IS NOT NULL
 ORDER BY t.created_at DESC, t.id DESC LIMIT 1`, canonicalURL, string(task.Kind), telegramSource))
		if err == nil && (input.CanReuseResult == nil || input.CanReuseResult(view.Result)) {
			if input.Submission != nil && !sameReviewedMetadata(input, view.Input) {
				return app.CreateURLResult{}, app.ErrSubmissionSourceConflict
			}
			return finish(app.CreateURLResult{Task: view.Task, Reused: true, NeedsConfirmation: true, Result: view.Result})
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return app.CreateURLResult{}, err
		}
	}
	created, err := s.insertTask(ctx, tx, task, input, progress)
	if err != nil {
		return app.CreateURLResult{}, err
	}
	return finish(app.CreateURLResult{Task: created})
}

func (s *Store) insertTask(ctx context.Context, tx pgx.Tx, task app.Task, input app.Input, progress domain.Progress) (app.Task, error) {
	now := s.clock()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	input.TaskID = task.ID
	if input.MetadataDocument == nil {
		doc, err := metadataDomain.FromLegacy(app.LegacyMetadata(input.Metadata))
		if err != nil {
			return app.Task{}, err
		}
		input.MetadataDocument = &doc
	}
	input.Metadata = app.MetadataProjection(metadataDomain.ToLegacy(*input.MetadataDocument))
	document, err := json.Marshal(input.MetadataDocument)
	if err != nil {
		return app.Task{}, err
	}
	metadata, err := encodeMetadata(input.Metadata)
	if err != nil {
		return app.Task{}, err
	}
	var telegramSource any
	if input.Telegram != nil {
		encoded, err := json.Marshal(input.Telegram)
		if err != nil {
			return app.Task{}, err
		}
		telegramSource = string(encoded)
	}
	var runtimeSettings any
	if input.RuntimeSettings != nil {
		encoded, err := json.Marshal(input.RuntimeSettings)
		if err != nil {
			return app.Task{}, err
		}
		runtimeSettings = string(encoded)
	}
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
			task_id, url, canonical_url, source_archive_name, source_archive_path, source_archive_size, metadata, runtime_settings, metadata_document, telegram_source, source_sha256
		) VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $7::jsonb, $8::jsonb, $9::jsonb, $10::jsonb, NULLIF($11, ''))
	`, input.TaskID, input.URL, input.CanonicalURL, input.SourceArchiveName, input.SourceArchivePath, nullablePositiveSize(input.SourceArchiveSize), string(metadata), runtimeSettings, string(document), telegramSource, input.SourceSHA256)
	if err != nil {
		return app.Task{}, mapPgError(err)
	}

	if len(input.MetadataDocument.Fields) > 0 {
		_, err = tx.Exec(ctx, `INSERT INTO metadata_history (task_id, task_type, url, author, series_name, series_number, comic_name, summary, tags, genres, metadata_document, created_at)
         VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),$11::jsonb,$12)`,
			task.ID, string(task.Kind), input.URL, input.Metadata["author"], input.Metadata["series_name"], input.Metadata["series_number"], input.Metadata["comic_name"], input.Metadata["summary"], input.Metadata["tags"], input.Metadata["genres"], string(document), now)
		if err != nil {
			return app.Task{}, err
		}
	}
	if err := upsertProgress(ctx, tx, task.ID, progress, now); err != nil {
		return app.Task{}, err
	}
	if err := insertEvent(ctx, tx, task.ID, "TASK_CREATED", nil, &task.Status, domain.ActorAPI, "task created", nil, now); err != nil {
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
		RETURNING id, kind, status, attempt, generation, last_error, lease_owner, lease_expires_at, created_at, updated_at
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
		RETURNING id, kind, status, attempt, generation, last_error, lease_owner, lease_expires_at, created_at, updated_at
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

	// URL inputs are immutable. Read their identity first so every URL writer
	// acquires the canonical advisory lock before locking its own task row.
	var kind, canonicalURL string
	var telegramSource sql.NullString
	err = tx.QueryRow(ctx, `SELECT t.kind, COALESCE(NULLIF(i.canonical_url, ''), btrim(i.url), ''), i.telegram_source::text
 FROM task_core_tasks t JOIN task_core_inputs i ON i.task_id = t.id WHERE t.id = $1`, taskID).Scan(&kind, &canonicalURL, &telegramSource)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Task{}, app.ErrNotFound
	}
	if err != nil {
		return app.Task{}, err
	}
	lockKey := canonicalURL
	var telegramJSON any
	if kind == string(domain.KindTelegram) {
		var source telegram.Input
		if !telegramSource.Valid || metadataDomain.DecodeJSON([]byte(telegramSource.String), &source) != nil || telegram.ValidateInput(source) != nil {
			return app.Task{}, app.ErrConflict
		}
		lockKey = telegramLockKey(source)
		telegramJSON = telegramSource.String
	}
	if kind == string(domain.KindURL) || kind == string(domain.KindTelegram) {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
			return app.Task{}, err
		}
	}

	view, err := scanTaskView(tx.QueryRow(ctx, taskViewQuery()+` WHERE t.id = $1 FOR UPDATE OF t`, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Task{}, app.ErrNotFound
	}
	if err != nil {
		return app.Task{}, err
	}
	if !domain.CanRetry(view.Task.Status, view.Input.HasSource(view.Task.Kind)) {
		return app.Task{}, app.ErrConflict
	}
	if view.Task.Kind == domain.KindURL || view.Task.Kind == domain.KindTelegram {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM task_core_tasks t JOIN task_core_inputs i ON i.task_id = t.id
 WHERE t.kind = $3 AND (i.telegram_source IS NOT DISTINCT FROM $4::jsonb) AND `+canonicalURLPredicate+` AND t.id <> $2
 AND t.status IN ('READY', 'RUNNING', 'CANCELING'))`, canonicalURL, taskID, kind, telegramJSON).Scan(&active); err != nil {
			return app.Task{}, err
		}
		if active {
			return app.Task{}, app.ErrConflict
		}
	}
	current := view.Task.Status
	to := domain.StatusReady

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
		RETURNING id, kind, status, attempt, generation, last_error, lease_owner, lease_expires_at, created_at, updated_at
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
 generation = t.generation + 1,
		    started_at = $3::timestamptz,
		    updated_at = $3::timestamptz,
		    lease_owner = $1,
		    lease_expires_at = $3::timestamptz + $2::interval,
		    heartbeat_at = $3::timestamptz
		FROM candidate
		WHERE t.id = candidate.id
		RETURNING t.id, t.kind, t.status, t.attempt, t.generation, t.last_error, t.lease_owner, t.lease_expires_at, t.created_at, t.updated_at
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

func (s *Store) Heartbeat(ctx context.Context, taskID string, workerID string, generation int64, leaseTTL time.Duration) (app.HeartbeatResult, error) {
	now := s.clock()
	var status string
	err := s.pool.QueryRow(ctx, `
		UPDATE task_core_tasks
		SET lease_expires_at = $4::timestamptz + $3::interval,
		    heartbeat_at = $4::timestamptz,
		    updated_at = $4::timestamptz
		WHERE id = $1 AND lease_owner = $2 AND generation = $5 AND status IN ('RUNNING', 'CANCELING')
		RETURNING status
	`, taskID, workerID, intervalString(leaseTTL), now, generation).Scan(&status)
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

func (s *Store) UpdateProgress(ctx context.Context, taskID string, workerID string, generation int64, progress domain.Progress) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM task_core_tasks WHERE id = $1 AND lease_owner = $2 AND generation = $3 AND status = 'RUNNING' FOR UPDATE`, taskID, workerID, generation).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return conditionalUpdateError(ctx, tx, taskID)
	}
	if err != nil {
		return err
	}
	if err := upsertProgress(ctx, tx, taskID, progress, s.clock()); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	if err := updateWorkerTerminal(ctx, tx, in.TaskID, in.WorkerID, in.Attempt, in.Generation, to, "", now); err != nil {
		return err
	}
	warnings := in.MetadataWarnings
	if warnings == nil {
		warnings = []metadataDomain.Warning{}
	}
	warningData, err := json.Marshal(warnings)
	if err != nil {
		return err
	}
	var retentionManifest any
	if in.RetentionManifest != nil {
		if err := domain.ValidateRetentionManifest(*in.RetentionManifest); err != nil {
			return err
		}
		encoded, err := json.Marshal(in.RetentionManifest)
		if err != nil {
			return err
		}
		retentionManifest = string(encoded)
		var registered bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM task_core_retention WHERE task_id=$1 AND generation=$2 AND manifest=$3::jsonb)`, in.TaskID, in.Generation, retentionManifest).Scan(&registered); err != nil {
			return err
		}
		if !registered {
			return app.ErrConflict
		}
	}
	var effectiveDocument any
	if in.EffectiveMetadataDocument != nil {
		encoded, err := json.Marshal(in.EffectiveMetadataDocument)
		if err != nil {
			return err
		}
		effectiveDocument = string(encoded)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO task_core_results (task_id, artifact_path, artifact_name, artifact_size, artifact_kind, created_at, effective_metadata_document, generation, retention_manifest,metadata_warnings,metadata_profile)
		VALUES ($1, $2, $3, $4, 'cbz', $5, $6::jsonb, $7, $8::jsonb,$9::jsonb,$10)
		ON CONFLICT (task_id) DO UPDATE SET
		    artifact_path = EXCLUDED.artifact_path,
		    artifact_name = EXCLUDED.artifact_name,
		    artifact_size = EXCLUDED.artifact_size,
		    artifact_kind = EXCLUDED.artifact_kind,
		    created_at = EXCLUDED.created_at,
		    effective_metadata_document = EXCLUDED.effective_metadata_document,
		    generation = EXCLUDED.generation,
 retention_manifest = EXCLUDED.retention_manifest,metadata_warnings=EXCLUDED.metadata_warnings,metadata_profile=EXCLUDED.metadata_profile
	`, in.TaskID, in.ArtifactPath, in.ArtifactName, in.ArtifactSize, now, effectiveDocument, in.Generation, retentionManifest, string(warningData), in.MetadataProfile)
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
	if err := updateWorkerTerminal(ctx, tx, in.TaskID, in.WorkerID, in.Attempt, in.Generation, to, message, now); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, in.TaskID, "TASK_FAILED", &from, &to, domain.ActorWorker, message, map[string]any{"worker_id": in.WorkerID, "attempt": in.Attempt}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int, generation int64) error {
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
		WHERE id = $1 AND lease_owner = $2 AND attempt = $3 AND generation = $5 AND status = 'CANCELING'
	`, taskID, workerID, attempt, now, generation)
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

		if err := domain.ValidateTransition(domain.StatusRunning, domain.StatusFailed, domain.ActorRecovery); err != nil {
			return app.RecoveryResult{}, err
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
		FOR UPDATE SKIP LOCKED
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
		WHERE status = 'CANCELING' AND (lease_expires_at IS NULL OR lease_expires_at < $2) AND cancel_requested_at < $1
		FOR UPDATE SKIP LOCKED
	`, cutoff, cutoff.Add(2*time.Minute))
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

func updateWorkerTerminal(ctx context.Context, tx pgx.Tx, taskID, workerID string, attempt int, generation int64, to domain.Status, lastError string, now time.Time) error {
	command, err := tx.Exec(ctx, `
		UPDATE task_core_tasks
		SET status = $4,
		    last_error = CASE WHEN $4 = 'FAILED' THEN NULLIF($5, '') ELSE last_error END,
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    heartbeat_at = NULL,
		    finished_at = $6,
		    updated_at = $6
		WHERE id = $1 AND lease_owner = $2 AND attempt = $3 AND generation = $7 AND status = 'RUNNING'
	`, taskID, workerID, attempt, string(to), lastError, now, generation)
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
			t.id, t.kind, t.status, t.attempt, t.generation, t.last_error, t.lease_owner, t.lease_expires_at, t.created_at, t.updated_at,
			i.task_id, i.url, i.canonical_url, i.source_archive_name, i.source_archive_path, i.source_archive_size, i.metadata::text, i.runtime_settings::text, i.metadata_document::text, i.telegram_source::text, COALESCE(i.source_sha256, ''),
			p.phase, p.current, p.total, p.unit, p.message,
			r.task_id, r.artifact_path, r.artifact_name, r.artifact_size, r.artifact_kind, r.komga_target_path, r.effective_metadata_document::text, r.generation, r.retention_manifest::text,r.metadata_warnings::text,r.metadata_profile
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
		&view.Task.Generation,
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
		&inputScan.runtimeSettings,
		&inputScan.metadataDocument,
		&inputScan.telegramSource,
		&view.Input.SourceSHA256,
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
		&resultScan.effectiveDocument,
		&resultScan.generation,
		&resultScan.retentionManifest,
		&resultScan.metadataWarnings, &resultScan.metadataProfile,
	); err != nil {
		return app.TaskView{}, err
	}
	taskScan.apply(&view.Task)
	if err := inputScan.apply(&view.Input); err != nil {
		return app.TaskView{}, err
	}
	progressScan.apply(&view.Progress)
	result := resultScan.result()
	if result != nil {
		result.Generation = resultScan.generation.Int64
		result.MetadataProfile = resultScan.metadataProfile.String
		if resultScan.metadataWarnings.Valid {
			if err := metadataDomain.DecodeJSON([]byte(resultScan.metadataWarnings.String), &result.MetadataWarnings); err != nil {
				return app.TaskView{}, err
			}
		}
		if resultScan.retentionManifest.Valid {
			result.RetentionManifest = &domain.RetentionManifest{}
			if err := metadataDomain.DecodeJSON([]byte(resultScan.retentionManifest.String), result.RetentionManifest); err != nil {
				return app.TaskView{}, err
			}
			if err := domain.ValidateRetentionManifest(*result.RetentionManifest); err != nil {
				return app.TaskView{}, err
			}
		}
		if resultScan.effectiveDocument.Valid {
			doc, err := metadataDomain.DecodeStored([]byte(resultScan.effectiveDocument.String))
			if err != nil {
				return app.TaskView{}, err
			}
			result.EffectiveMetadataDocument = &doc
		}
	}
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
		&task.Generation,
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
	runtimeSettings   sql.NullString
	metadataDocument  sql.NullString
	telegramSource    sql.NullString
}

func (s *dbInputScan) apply(input *app.Input) error {
	if s.telegramSource.Valid {
		input.Telegram = &telegram.Input{}
		if err := metadataDomain.DecodeJSON([]byte(s.telegramSource.String), input.Telegram); err != nil {
			return err
		}
		if err := telegram.ValidateInput(*input.Telegram); err != nil {
			return err
		}
	}
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
	if s.runtimeSettings.Valid {
		input.RuntimeSettings = &config.SettingsSnapshot{}
		if err := json.Unmarshal([]byte(s.runtimeSettings.String), input.RuntimeSettings); err != nil {
			return err
		}
	}
	input.Metadata = map[string]string{}
	if s.metadataDocument.Valid {
		doc, err := metadataDomain.DecodeStored([]byte(s.metadataDocument.String))
		if err != nil {
			return err
		}
		input.MetadataDocument = &doc
		input.Metadata = app.MetadataProjection(metadataDomain.ToLegacy(*input.MetadataDocument))
		return nil
	}
	if strings.TrimSpace(s.metadata) != "" {
		if err := json.Unmarshal([]byte(s.metadata), &input.Metadata); err != nil {
			return err
		}
	}
	doc, err := metadataDomain.FromLegacy(app.LegacyMetadata(input.Metadata))
	if err != nil {
		return err
	}
	input.MetadataDocument = &doc
	input.Metadata = app.MetadataProjection(metadataDomain.ToLegacy(doc))
	return nil
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
	taskID            sql.NullString
	artifactPath      sql.NullString
	artifactName      sql.NullString
	artifactSize      sql.NullInt64
	artifactKind      sql.NullString
	komgaTargetPath   sql.NullString
	effectiveDocument sql.NullString
	retentionManifest sql.NullString
	metadataWarnings  sql.NullString
	metadataProfile   sql.NullString
	generation        sql.NullInt64
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

func (s *Store) QueryTasks(ctx context.Context, query app.TaskQuery) (app.TaskPage, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return app.TaskPage{}, err
	}
	defer rollback(ctx, tx)
	const filter = ` WHERE ($1 = '' OR t.status = $1) AND ($2 = '' OR strpos(lower(concat_ws(' ',
 t.id::text, t.kind, t.status, t.last_error, i.url, i.canonical_url, i.source_archive_name, i.source_archive_path,
 i.metadata::text, r.artifact_name, r.artifact_path, r.komga_target_path)), lower($2)) > 0)`
	page := app.TaskPage{Tasks: make([]app.TaskView, 0)}
	err = tx.QueryRow(ctx, `SELECT count(*) FROM task_core_tasks t
 JOIN task_core_inputs i ON i.task_id = t.id LEFT JOIN task_core_results r ON r.task_id = t.id`+filter,
		string(query.Status), query.Keyword).Scan(&page.Total)
	if err != nil {
		return app.TaskPage{}, err
	}
	rows, err := tx.Query(ctx, taskViewQuery()+filter+` ORDER BY t.created_at DESC, t.id DESC LIMIT $3 OFFSET $4`,
		string(query.Status), query.Keyword, query.Limit, query.Offset)
	if err != nil {
		return app.TaskPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		view, err := scanTaskView(rows)
		if err != nil {
			return app.TaskPage{}, err
		}
		page.Tasks = append(page.Tasks, view)
	}
	if err := rows.Err(); err != nil {
		return app.TaskPage{}, err
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return app.TaskPage{}, err
	}
	return page, nil
}

func (s *Store) StatusCounts(ctx context.Context) (map[domain.Status]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT status, count(*) FROM task_core_tasks GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[domain.Status]int)
	for rows.Next() {
		var status domain.Status
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

func telegramLockKey(source telegram.Input) string {
	return fmt.Sprintf("telegram:%s:%d:%s", source.AccountIdentity, source.AccountRevision, source.MessageURL)
}

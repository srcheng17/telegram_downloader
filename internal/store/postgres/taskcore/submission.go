package taskcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
)

func (s *Store) FindSubmission(ctx context.Context, key string) (*app.TaskView, string, bool, error) {
	return findSubmission(ctx, s.pool, key)
}

func findSubmission(ctx context.Context, query executor, key string) (*app.TaskView, string, bool, error) {
	var taskID, requestHash string
	var confirmation bool
	err := query.QueryRow(ctx, `SELECT task_id, request_hash, needs_confirmation FROM task_core_submissions WHERE idempotency_key=$1`, key).Scan(&taskID, &requestHash, &confirmation)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", false, app.ErrNotFound
	}
	if err != nil {
		return nil, "", false, err
	}
	view, err := scanTaskView(query.QueryRow(ctx, taskViewQuery()+` WHERE t.id=$1`, taskID))
	if err != nil {
		return nil, "", false, err
	}
	return &view, requestHash, confirmation, nil
}

// Lock receipt identity before URL identity and re-read after acquiring the
// lock. Task creation and the receipt then commit atomically across instances.
func lockSubmission(ctx context.Context, tx pgx.Tx, submission *app.Submission) (*app.CreateURLResult, error) {
	if submission == nil {
		return nil, nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 1))`, submission.Key); err != nil {
		return nil, err
	}
	view, hash, confirmation, err := findSubmission(ctx, tx, submission.Key)
	if errors.Is(err, app.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if hash != submission.RequestHash {
		return nil, app.ErrIdempotencyConflict
	}
	return &app.CreateURLResult{Task: view.Task, Result: view.Result, Reused: true, NeedsConfirmation: confirmation}, nil
}

func saveSubmission(ctx context.Context, tx pgx.Tx, submission *app.Submission, result app.CreateURLResult) error {
	if submission == nil {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO task_core_submissions (idempotency_key,request_hash,task_id,needs_confirmation) VALUES($1,$2,$3,$4)`, submission.Key, submission.RequestHash, result.ID, result.NeedsConfirmation)
	return err
}

// A canonical-source reuse may predate this client key. Keyed callers must not
// silently receive an artifact built from a different reviewed document.
func sameReviewedMetadata(a, b app.Input) bool {
	left, err := json.Marshal(a.MetadataDocument)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b.MetadataDocument)
	return err == nil && bytes.Equal(left, right)
}

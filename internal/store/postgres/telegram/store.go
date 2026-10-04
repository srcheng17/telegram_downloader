package telegram

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/telegram"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }
func (s *Store) Account(ctx context.Context) (app.AccountRecord, error) {
	var r app.AccountRecord
	var verified *time.Time
	err := s.pool.QueryRow(ctx, `SELECT revision,namespace,identity,verified_at FROM telegram_account WHERE singleton`).Scan(&r.Revision, &r.Namespace, &r.Identity, &verified)
	if verified != nil {
		r.VerifiedAt = *verified
	}
	return r, err
}
func scanAttempt(row pgx.Row) (app.AttemptRecord, error) {
	var r app.AttemptRecord
	err := row.Scan(&r.ID, &r.Owner, &r.Namespace, &r.ExpectedRevision, &r.Seq, &r.Revision, &r.State, &r.Code, &r.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, app.ErrNotFound
	}
	return r, err
}

const attemptColumns = `id,owner,namespace,expected_revision,seq,revision,state,code,expires_at`

func (s *Store) Attempt(ctx context.Context, id string) (app.AttemptRecord, error) {
	return scanAttempt(s.pool.QueryRow(ctx, `SELECT `+attemptColumns+` FROM telegram_login_attempts WHERE id=$1`, id))
}
func (s *Store) LatestAttempt(ctx context.Context) (app.AttemptRecord, error) {
	return scanAttempt(s.pool.QueryRow(ctx, `SELECT `+attemptColumns+` FROM telegram_login_attempts ORDER BY updated_at DESC LIMIT 1`))
}
func (s *Store) CreateAttempt(ctx context.Context, a app.AttemptRecord) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Caller holds both process-lifetime locks: any prior open attempt was orphaned.
	if _, err = tx.Exec(ctx, `UPDATE telegram_login_attempts SET state='failed',code='interrupted',seq=seq+1,revision=revision+1,updated_at=now() WHERE state IN ('waiting_qr','password_required','verifying')`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO telegram_login_attempts(`+attemptColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.ID, a.Owner, a.Namespace, a.ExpectedRevision, a.Seq, a.Revision, a.State, a.Code, a.ExpiresAt); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM telegram_login_attempts WHERE updated_at < now()-interval '24 hours' AND state IN ('connected','cancelled','expired','failed')`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) UpdateAttempt(ctx context.Context, a app.AttemptRecord) error {
	result, err := s.pool.Exec(ctx, `UPDATE telegram_login_attempts SET seq=$3,revision=$4,state=$5,code=$6,updated_at=now() WHERE id=$1 AND owner=$2 AND seq<$3 AND state NOT IN ('connected','cancelled','expired','failed')`, a.ID, a.Owner, a.Seq, a.Revision, a.State, a.Code)
	if err == nil && result.RowsAffected() != 1 {
		return app.ErrConflict
	}
	return err
}
func (s *Store) Promote(ctx context.Context, a app.AttemptRecord, identity string, verified time.Time) (app.AccountRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return app.AccountRecord{}, err
	}
	defer tx.Rollback(ctx)
	var valid bool
	err = tx.QueryRow(ctx, `SELECT state='verifying' AND owner=$2 AND expires_at>now() FROM telegram_login_attempts WHERE id=$1 FOR UPDATE`, a.ID, a.Owner).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !valid {
		return app.AccountRecord{}, app.ErrConflict
	}
	if err != nil {
		return app.AccountRecord{}, err
	}
	r := app.AccountRecord{Namespace: a.Namespace, Identity: identity, VerifiedAt: verified}
	err = tx.QueryRow(ctx, `UPDATE telegram_account SET revision=revision+1,namespace=$2,identity=$3,verified_at=$4 WHERE singleton AND revision=$1 RETURNING revision`, a.ExpectedRevision, a.Namespace, identity, verified).Scan(&r.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, app.ErrConflict
	}
	if err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, `UPDATE telegram_login_attempts SET state='connected',code='',seq=seq+1,revision=revision+1,updated_at=now() WHERE id=$1`, a.ID); err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}

package adminauth

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/adminauth"
	"time"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }
func (s *Store) Account(ctx context.Context) (app.Account, error) {
	var a app.Account
	err := s.pool.QueryRow(ctx, `SELECT password_hash,credential_version FROM admin_account WHERE id=1`).Scan(&a.PasswordHash, &a.CredentialVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		err = app.ErrNotFound
	}
	return a, err
}
func (s *Store) Bootstrap(ctx context.Context, hash string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO admin_account(id,password_hash) VALUES(1,$1) ON CONFLICT(id) DO NOTHING`, hash)
	return err
}
func insert(ctx context.Context, tx pgx.Tx, v app.Session) error {
	_, err := tx.Exec(ctx, `INSERT INTO admin_sessions(token_hash,csrf_token,credential_version,preauth,created_at,last_seen,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, v.TokenHash, v.CSRFToken, v.CredentialVersion, v.Preauth, v.CreatedAt, v.LastSeen, v.ExpiresAt)
	return err
}
func (s *Store) CreateSession(ctx context.Context, v app.Session) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM admin_sessions WHERE expires_at<=$1 OR last_seen<=$2`, v.CreatedAt, v.CreatedAt.Add(-app.IdleTTL)); err != nil {
		return err
	}
	if err = insert(ctx, tx, v); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) Session(ctx context.Context, hash string, now time.Time, touchActivity bool) (app.Session, error) {
	var v app.Session
	const valid = ` WHERE token_hash=$1 AND expires_at>$2 AND last_seen>$2-interval '30 minutes' AND (preauth OR credential_version=(SELECT credential_version FROM admin_account WHERE id=1))`
	const columns = `token_hash,csrf_token,credential_version,preauth,created_at,last_seen,expires_at`
	scan := func(row pgx.Row) error {
		return row.Scan(&v.TokenHash, &v.CSRFToken, &v.CredentialVersion, &v.Preauth, &v.CreatedAt, &v.LastSeen, &v.ExpiresAt)
	}
	err := scan(s.pool.QueryRow(ctx, `SELECT `+columns+` FROM admin_sessions`+valid, hash, now))
	// Most authenticated requests are reads; persist activity at most once a minute.
	if err == nil && touchActivity && now.Sub(v.LastSeen) >= time.Minute {
		err = scan(s.pool.QueryRow(ctx, `UPDATE admin_sessions SET last_seen=$2`+valid+` RETURNING `+columns, hash, now))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = app.ErrUnauthenticated
	}
	return v, err
}

func (s *Store) RotateSession(ctx context.Context, old string, v app.Session) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var version int64
	if err = tx.QueryRow(ctx, `SELECT credential_version FROM admin_account WHERE id=1 FOR UPDATE`).Scan(&version); err != nil {
		return err
	}
	if version != v.CredentialVersion {
		return app.ErrUnauthenticated
	}
	tag, err := tx.Exec(ctx, `DELETE FROM admin_sessions WHERE token_hash=$1 AND preauth AND expires_at>$2`, old, v.CreatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return app.ErrUnauthenticated
	}
	if err = insert(ctx, tx, v); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM admin_sessions WHERE token_hash=$1`, hash)
	return err
}
func (s *Store) ChangePassword(ctx context.Context, version int64, hash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE admin_account SET password_hash=$1,credential_version=credential_version+1,updated_at=now() WHERE id=1 AND credential_version=$2`, hash, version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return app.ErrUnauthenticated
	}
	if _, err = tx.Exec(ctx, `DELETE FROM admin_sessions`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

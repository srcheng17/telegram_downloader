package sourcesettings

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }
func (s *Store) Get(ctx context.Context, id string) (app.Record, error) {
	var r app.Record
	err := s.pool.QueryRow(ctx, `SELECT provider_id,enabled,priority,config,auth_mode,COALESCE(ciphertext,''::bytea),COALESCE(nonce,''::bytea),COALESCE(key_id,''),config_version,credential_version FROM source_settings WHERE provider_id=$1`, id).Scan(&r.ProviderID, &r.Enabled, &r.Priority, &r.Config, &r.AuthMode, &r.Envelope.Ciphertext, &r.Envelope.Nonce, &r.Envelope.KeyID, &r.ConfigVersion, &r.CredentialVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		err = app.ErrNotFound
	}
	return r, err
}
func (s *Store) Save(ctx context.Context, r app.Record, expected int64) (app.Record, error) {
	if expected < 0 {
		return app.Record{}, app.ErrConflict
	}
	query := `UPDATE source_settings SET enabled=$2,priority=$3,config=$4,auth_mode=$5,ciphertext=$6,nonce=$7,key_id=$8,credential_version=$9,config_version=config_version+1,updated_at=now() WHERE provider_id=$1 AND config_version=$10 RETURNING config_version`
	if expected == 0 {
		query = `INSERT INTO source_settings(provider_id,enabled,priority,config,auth_mode,ciphertext,nonce,key_id,credential_version,config_version) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,1 WHERE $10::bigint=0 ON CONFLICT(provider_id) DO NOTHING RETURNING config_version`
	}
	err := s.pool.QueryRow(ctx, query, r.ProviderID, r.Enabled, r.Priority, r.Config, r.AuthMode, r.Envelope.Ciphertext, r.Envelope.Nonce, r.Envelope.KeyID, r.CredentialVersion, expected).Scan(&r.ConfigVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		err = app.ErrConflict
	}
	return r, err
}

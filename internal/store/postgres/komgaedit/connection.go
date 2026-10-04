package komgaedit

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	app "github.com/ryancheng/telegram-downloader/internal/app/komgaedit"
)

type ConnectionStore struct{ pool *pgxpool.Pool }

func NewConnectionStore(pool *pgxpool.Pool) *ConnectionStore { return &ConnectionStore{pool: pool} }

func (s *ConnectionStore) Get(ctx context.Context) (app.ConnectionRecord, error) {
	var record app.ConnectionRecord
	err := s.pool.QueryRow(ctx, `SELECT base_url, COALESCE(ciphertext,''::bytea), COALESCE(nonce,''::bytea), COALESCE(key_id,''), credential_version, config_version FROM komga_connection WHERE singleton=true`).Scan(
		&record.BaseURL, &record.Envelope.Ciphertext, &record.Envelope.Nonce, &record.Envelope.KeyID, &record.CredentialVersion, &record.ConfigVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ConnectionRecord{}, app.ErrNotFound
	}
	return record, err
}

func (s *ConnectionStore) Save(ctx context.Context, record app.ConnectionRecord, expected int64) (app.ConnectionRecord, error) {
	if expected < 0 {
		return app.ConnectionRecord{}, app.ErrConflict
	}
	query := `UPDATE komga_connection SET base_url=$1,ciphertext=$2,nonce=$3,key_id=$4,credential_version=$5,config_version=config_version+1,updated_at=now() WHERE singleton=true AND config_version=$6 RETURNING config_version`
	if expected == 0 {
		query = `INSERT INTO komga_connection(singleton,base_url,ciphertext,nonce,key_id,credential_version,config_version) SELECT true,$1,$2,$3,$4,$5,1 WHERE $6::bigint=0 ON CONFLICT(singleton) DO NOTHING RETURNING config_version`
	}
	err := s.pool.QueryRow(ctx, query, record.BaseURL, record.Envelope.Ciphertext, record.Envelope.Nonce, record.Envelope.KeyID, record.CredentialVersion, expected).Scan(&record.ConfigVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ConnectionRecord{}, app.ErrConflict
	}
	return record, err
}

package httpv2

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresSettingsStoreDB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type PostgresTaskStore struct {
	db postgresSettingsStoreDB
}

func NewPostgresTaskStore(pool *pgxpool.Pool) *PostgresTaskStore {
	if pool == nil {
		return &PostgresTaskStore{}
	}
	return &PostgresTaskStore{db: pool}
}

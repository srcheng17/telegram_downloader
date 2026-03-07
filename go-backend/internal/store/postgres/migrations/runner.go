package migrations

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed *.sql
var migrationFS embed.FS

type Executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func Run(ctx context.Context, executor Executor) error {
	if executor == nil {
		return errors.New("migrations executor is required")
	}

	if err := ensureVersionTable(ctx, executor); err != nil {
		return err
	}

	versions, err := listMigrationVersions()
	if err != nil {
		return err
	}
	for _, version := range versions {
		applied, err := migrationApplied(ctx, executor, version)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied {
			continue
		}

		sqlBytes, err := migrationFS.ReadFile(version)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		if _, err := executor.Exec(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if _, err := executor.Exec(
			ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`,
			version,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
	}
	return nil
}

func PendingCount(ctx context.Context, executor Executor) (int, error) {
	if executor == nil {
		return 0, errors.New("migrations executor is required")
	}

	if err := ensureVersionTable(ctx, executor); err != nil {
		return 0, err
	}

	versions, err := listMigrationVersions()
	if err != nil {
		return 0, err
	}

	pending := 0
	for _, version := range versions {
		applied, err := migrationApplied(ctx, executor, version)
		if err != nil {
			return 0, fmt.Errorf("check migration %s: %w", version, err)
		}
		if !applied {
			pending++
		}
	}
	return pending, nil
}

func ensureVersionTable(ctx context.Context, executor Executor) error {
	_, err := executor.Exec(
		ctx,
		`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
		`,
	)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}
	return nil
}

func listMigrationVersions() ([]string, error) {
	entries, err := fs.Glob(migrationFS, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migration files: %w", err)
	}

	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry)
		if name == "" {
			continue
		}
		versions = append(versions, name)
	}
	sort.Strings(versions)
	return versions, nil
}

func migrationApplied(ctx context.Context, executor Executor, version string) (bool, error) {
	var applied bool
	if err := executor.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`,
		version,
	).Scan(&applied); err != nil {
		return false, err
	}
	return applied, nil
}

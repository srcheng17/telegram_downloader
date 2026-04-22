package migrations

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRunMigrationsAppliesAllSQLFiles(t *testing.T) {
	versions, err := listMigrationVersions()
	if err != nil {
		t.Fatalf("list migration versions: %v", err)
	}
	if len(versions) == 0 {
		t.Fatalf("expected at least one migration file")
	}

	executor := newFakeMigrationExecutor()
	if err := Run(context.Background(), executor); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	if len(executor.appliedBodies) != len(versions) {
		t.Fatalf("expected %d migration bodies applied, got %d", len(versions), len(executor.appliedBodies))
	}
	if len(executor.appliedVersions) != len(versions) {
		t.Fatalf("expected %d recorded versions, got %d", len(versions), len(executor.appliedVersions))
	}
	for _, version := range versions {
		if _, ok := executor.appliedVersions[version]; !ok {
			t.Fatalf("expected version %q to be recorded", version)
		}
	}

	if err := Run(context.Background(), executor); err != nil {
		t.Fatalf("run migrations second time: %v", err)
	}
	if len(executor.appliedBodies) != len(versions) {
		t.Fatalf("expected idempotent run to keep applied body count %d, got %d", len(versions), len(executor.appliedBodies))
	}
}

func TestPendingCountReflectsUnappliedMigrations(t *testing.T) {
	versions, err := listMigrationVersions()
	if err != nil {
		t.Fatalf("list migration versions: %v", err)
	}
	executor := newFakeMigrationExecutor()
	if len(versions) > 0 {
		executor.appliedVersions[versions[0]] = struct{}{}
	}

	pending, err := PendingCount(context.Background(), executor)
	if err != nil {
		t.Fatalf("pending count: %v", err)
	}
	if pending != len(versions)-1 {
		t.Fatalf("expected pending=%d, got %d", len(versions)-1, pending)
	}
}

func TestListMigrationVersionsIncludesTaskSearchIndexesMigration(t *testing.T) {
	versions, err := listMigrationVersions()
	if err != nil {
		t.Fatalf("list migration versions: %v", err)
	}

	found := false
	for _, version := range versions {
		if version == "006_task_search_trgm_indexes.sql" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task search migration to be listed, got %#v", versions)
	}
}

func TestListMigrationVersionsIncludesTaskCoreSchema(t *testing.T) {
	versions, err := listMigrationVersions()
	if err != nil {
		t.Fatalf("list migration versions: %v", err)
	}

	found := false
	for _, version := range versions {
		if version == "011_task_core_schema.sql" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task core schema migration to be listed, got %#v", versions)
	}
}

func TestRunnerApplies007V2TasksStatusUpdatedIndex(t *testing.T) {
	const version007 = "007_v2_tasks_status_updated_index.sql"

	versions, err := listMigrationVersions()
	if err != nil {
		t.Fatalf("list migration versions: %v", err)
	}

	hasMigration := false
	for _, version := range versions {
		if version == version007 {
			hasMigration = true
			break
		}
	}
	if !hasMigration {
		t.Fatalf("expected 007 migration to be listed, got %#v", versions)
	}

	executor := newFakeMigrationExecutor()
	if err := Run(context.Background(), executor); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if _, ok := executor.appliedVersions[version007]; !ok {
		t.Fatalf("expected version %q to be recorded as applied, got %#v", version007, executor.appliedVersions)
	}

	const expectedSQL = "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_v2_tasks_status_updated_at ON v2_tasks (status, updated_at DESC);"
	applied := false
	for _, body := range executor.appliedBodies {
		normalizedBody := strings.Join(strings.Fields(body), " ")
		if normalizedBody == expectedSQL {
			applied = true
			break
		}
	}
	if !applied {
		t.Fatalf("expected 007 index migration SQL to be applied, bodies=%#v", executor.appliedBodies)
	}
}

type fakeMigrationExecutor struct {
	appliedVersions map[string]struct{}
	appliedBodies   []string
}

func newFakeMigrationExecutor() *fakeMigrationExecutor {
	return &fakeMigrationExecutor{
		appliedVersions: map[string]struct{}{},
		appliedBodies:   make([]string, 0),
	}
}

func (f *fakeMigrationExecutor) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(query, "SELECT pg_advisory_lock"):
		return pgconn.NewCommandTag("SELECT 1"), nil
	case strings.Contains(query, "SELECT pg_advisory_unlock"):
		return pgconn.NewCommandTag("SELECT 1"), nil
	case strings.Contains(query, "CREATE TABLE IF NOT EXISTS schema_migrations"):
		return pgconn.NewCommandTag("CREATE TABLE"), nil
	case strings.Contains(query, "INSERT INTO schema_migrations"):
		if len(args) != 1 {
			return pgconn.CommandTag{}, fmt.Errorf("expected one arg for migration insert, got %d", len(args))
		}
		version, ok := args[0].(string)
		if !ok {
			return pgconn.CommandTag{}, fmt.Errorf("expected migration version arg type string, got %T", args[0])
		}
		f.appliedVersions[strings.TrimSpace(version)] = struct{}{}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		f.appliedBodies = append(f.appliedBodies, strings.TrimSpace(query))
		return pgconn.NewCommandTag("APPLY 1"), nil
	}
}

func (f *fakeMigrationExecutor) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	if !strings.Contains(query, "SELECT EXISTS(SELECT 1 FROM schema_migrations") {
		return fakeMigrationRow{err: fmt.Errorf("unexpected query: %s", query)}
	}
	if len(args) != 1 {
		return fakeMigrationRow{err: fmt.Errorf("expected one arg for exists query, got %d", len(args))}
	}
	version, ok := args[0].(string)
	if !ok {
		return fakeMigrationRow{err: fmt.Errorf("expected version arg type string, got %T", args[0])}
	}
	_, exists := f.appliedVersions[strings.TrimSpace(version)]
	return fakeMigrationRow{exists: exists}
}

type fakeMigrationRow struct {
	exists bool
	err    error
}

func (f fakeMigrationRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	if len(dest) != 1 {
		return fmt.Errorf("expected one scan destination, got %d", len(dest))
	}
	boolPtr, ok := dest[0].(*bool)
	if !ok {
		return fmt.Errorf("expected destination type *bool, got %T", dest[0])
	}
	*boolPtr = f.exists
	return nil
}

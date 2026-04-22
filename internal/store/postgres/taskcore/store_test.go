package taskcore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

func TestClaimNextOnlyClaimsReadyTasks(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	created := insertTaskCoreReadyTask(t, ctx, store, "44444444-4444-4444-4444-444444444444")

	claimed, err := store.ClaimNext(ctx, "worker-a", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatalf("expected claimed task")
	}
	if claimed.ID != created.ID || claimed.Status != domain.StatusRunning || claimed.Attempt != 1 {
		t.Fatalf("claimed = %#v, want task %s RUNNING attempt 1", claimed, created.ID)
	}
}

func TestHeartbeatRequiresLeaseOwner(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	insertTaskCoreReadyTask(t, ctx, store, "55555555-5555-5555-5555-555555555555")
	claimed, err := store.ClaimNext(ctx, "worker-a", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	_, err = store.Heartbeat(ctx, claimed.ID, "worker-b", time.Minute)
	if !errors.Is(err, app.ErrConflict) {
		t.Fatalf("heartbeat by stale worker err = %v, want ErrConflict", err)
	}
}

func TestCompleteRequiresMatchingWorkerAndAttempt(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	insertTaskCoreReadyTask(t, ctx, store, "66666666-6666-6666-6666-666666666666")
	claimed, err := store.ClaimNext(ctx, "worker-a", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	staleWorker := app.CompleteInput{TaskID: claimed.ID, WorkerID: "worker-b", Attempt: claimed.Attempt, ArtifactPath: "/tmp/out.cbz", ArtifactName: "out.cbz", ArtifactSize: 1}
	if err := store.Complete(ctx, staleWorker); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("complete with stale worker err = %v, want ErrConflict", err)
	}

	staleAttempt := app.CompleteInput{TaskID: claimed.ID, WorkerID: "worker-a", Attempt: claimed.Attempt + 1, ArtifactPath: "/tmp/out.cbz", ArtifactName: "out.cbz", ArtifactSize: 1}
	if err := store.Complete(ctx, staleAttempt); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("complete with stale attempt err = %v, want ErrConflict", err)
	}
}

func TestRecoverExpiredRequeuesBelowMaxAttempts(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	insertExpiredRunningTask(t, ctx, store, "77777777-7777-7777-7777-777777777777", 1)

	result, err := store.RecoverExpired(ctx, 3)
	if err != nil {
		t.Fatalf("RecoverExpired: %v", err)
	}
	if result.Requeued != 1 || result.Failed != 0 {
		t.Fatalf("result = %#v, want 1 requeued and 0 failed", result)
	}
	view, err := store.GetTask(ctx, "77777777-7777-7777-7777-777777777777")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if view.Task.Status != domain.StatusReady || view.Task.LeaseOwner != "" || view.Task.LeaseExpiresAt != nil {
		t.Fatalf("recovered task = %#v, want READY with lease cleared", view.Task)
	}
}

func TestRecoverExpiredFailsAtMaxAttempts(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	insertExpiredRunningTask(t, ctx, store, "88888888-8888-8888-8888-888888888888", 3)

	result, err := store.RecoverExpired(ctx, 3)
	if err != nil {
		t.Fatalf("RecoverExpired: %v", err)
	}
	if result.Requeued != 0 || result.Failed != 1 {
		t.Fatalf("result = %#v, want 0 requeued and 1 failed", result)
	}
	view, err := store.GetTask(ctx, "88888888-8888-8888-8888-888888888888")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if view.Task.Status != domain.StatusFailed || view.Task.LastError == "" || view.Task.LeaseOwner != "" || view.Task.LeaseExpiresAt != nil {
		t.Fatalf("recovered task = %#v, want FAILED with error and lease cleared", view.Task)
	}
}

func TestRetryResetsAttemptAndClearsErrorLeaseAndResult(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	const taskID = "99999999-9999-9999-9999-999999999991"
	insertFailedTaskWithResult(t, ctx, store, taskID, 3)

	retried, err := store.Retry(ctx, taskID, domain.NewProgress(domain.PhasePreparing, 0, 0, domain.UnitNone, "等待重试"))
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if retried.Status != domain.StatusReady || retried.Attempt != 0 {
		t.Fatalf("retried task = %#v, want READY attempt 0", retried)
	}
	if retried.LastError != "" || retried.LeaseOwner != "" || retried.LeaseExpiresAt != nil {
		t.Fatalf("retried task retained error/lease fields: %#v", retried)
	}

	view, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if view.Task.Status != domain.StatusReady || view.Task.Attempt != 0 {
		t.Fatalf("persisted task = %#v, want READY attempt 0", view.Task)
	}
	if view.Task.LastError != "" || view.Task.LeaseOwner != "" || view.Task.LeaseExpiresAt != nil {
		t.Fatalf("persisted task retained error/lease fields: %#v", view.Task)
	}
	if view.Result != nil {
		t.Fatalf("result = %#v, want nil after retry", view.Result)
	}
}

func TestCompletePersistsResultForGetTask(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	const taskID = "99999999-9999-9999-9999-999999999992"
	insertTaskCoreReadyTask(t, ctx, store, taskID)
	claimed, err := store.ClaimNext(ctx, "worker-complete", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	err = store.Complete(ctx, app.CompleteInput{
		TaskID:       claimed.ID,
		WorkerID:     "worker-complete",
		Attempt:      claimed.Attempt,
		ArtifactPath: "/tmp/complete.cbz",
		ArtifactName: "complete.cbz",
		ArtifactSize: 42,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	view, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if view.Task.Status != domain.StatusSucceeded {
		t.Fatalf("status = %s, want SUCCEEDED", view.Task.Status)
	}
	if view.Result == nil {
		t.Fatalf("result = nil, want persisted result")
	}
	if view.Result.ArtifactPath != "/tmp/complete.cbz" || view.Result.ArtifactName != "complete.cbz" || view.Result.ArtifactSize != 42 || view.Result.ArtifactKind != "cbz" {
		t.Fatalf("result = %#v", view.Result)
	}
}

func openTaskCoreTestStore(t *testing.T) (context.Context, *Store) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("requires TEST_DATABASE_URL")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	migrationPath := filepath.Join("..", "migrations", "011_task_core_schema.sql")
	migration, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration %s: %v", migrationPath, err)
	}
	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("apply task core migration: %v", err)
	}

	store := NewStore(pool)
	cleanupTaskCoreRows(t, ctx, store,
		"44444444-4444-4444-4444-444444444444",
		"55555555-5555-5555-5555-555555555555",
		"66666666-6666-6666-6666-666666666666",
		"77777777-7777-7777-7777-777777777777",
		"88888888-8888-8888-8888-888888888888",
		"99999999-9999-9999-9999-999999999991",
		"99999999-9999-9999-9999-999999999992",
	)
	return ctx, store
}

func cleanupTaskCoreRows(t *testing.T, ctx context.Context, store *Store, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := store.pool.Exec(ctx, `DELETE FROM task_core_tasks WHERE id = $1`, id); err != nil {
			t.Fatalf("cleanup task %s: %v", id, err)
		}
	}
	t.Cleanup(func() {
		for _, id := range ids {
			if _, err := store.pool.Exec(context.Background(), `DELETE FROM task_core_tasks WHERE id = $1`, id); err != nil {
				t.Fatalf("cleanup task %s: %v", id, err)
			}
		}
	})
}

func insertTaskCoreReadyTask(t *testing.T, ctx context.Context, store *Store, id string) app.Task {
	t.Helper()

	task, err := store.CreateTask(ctx,
		app.Task{ID: id, Kind: domain.KindURL, Status: domain.StatusReady},
		app.Input{TaskID: id, URL: "https://telegra.ph/demo", CanonicalURL: "https://telegra.ph/demo", Metadata: map[string]string{"comic_name": "Demo"}},
		domain.NewProgress(domain.PhasePreparing, 0, 0, domain.UnitNone, "ready"),
	)
	if err != nil {
		t.Fatalf("CreateTask(%s): %v", id, err)
	}
	return task
}

func insertExpiredRunningTask(t *testing.T, ctx context.Context, store *Store, id string, attempt int) {
	t.Helper()

	insertTaskCoreReadyTask(t, ctx, store, id)
	expiresAt := time.Now().Add(-time.Minute)
	startedAt := expiresAt.Add(-time.Minute)
	_, err := store.pool.Exec(ctx, `
		UPDATE task_core_tasks
		SET status = 'RUNNING',
		    attempt = $2,
		    lease_owner = 'expired-worker',
		    lease_expires_at = $3,
		    heartbeat_at = $3,
		    started_at = $4,
		    updated_at = $4
		WHERE id = $1
	`, id, attempt, expiresAt, startedAt)
	if err != nil {
		t.Fatalf("make task %s expired running: %v", id, err)
	}
}

func insertFailedTaskWithResult(t *testing.T, ctx context.Context, store *Store, id string, attempt int) {
	t.Helper()

	insertTaskCoreReadyTask(t, ctx, store, id)
	expiresAt := time.Now().Add(time.Minute)
	_, err := store.pool.Exec(ctx, `
		UPDATE task_core_tasks
		SET status = 'FAILED',
		    attempt = $2,
		    last_error = 'previous failure',
		    lease_owner = 'stale-worker',
		    lease_expires_at = $3,
		    heartbeat_at = $3,
		    finished_at = $3,
		    updated_at = $3
		WHERE id = $1
	`, id, attempt, expiresAt)
	if err != nil {
		t.Fatalf("make task %s failed: %v", id, err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO task_core_results (task_id, artifact_path, artifact_name, artifact_size, artifact_kind, created_at)
		VALUES ($1, '/tmp/old.cbz', 'old.cbz', 9, 'cbz', $2)
	`, id, expiresAt)
	if err != nil {
		t.Fatalf("insert result for task %s: %v", id, err)
	}
}

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestTransitionPendingToInProgressRequiresEnqueueToken(t *testing.T) {
	taskID := "task-transition-1"
	enqueueToken := "token-ok"
	worker := "worker-a"

	db := newFakeTransitionDB()
	db.insertPending(taskID, enqueueToken)

	store := &Store{pool: db}

	transitioned, err := store.TransitionPendingToInProgress(context.Background(), taskID, "token-wrong", worker)
	if err != nil {
		t.Fatalf("transition with wrong token: %v", err)
	}
	if transitioned {
		t.Fatalf("expected wrong token transition to be rejected")
	}

	task := db.tasks[taskID]
	if task.status != "PENDING" {
		t.Fatalf("expected task to stay pending after wrong token, got %q", task.status)
	}

	transitioned, err = store.TransitionPendingToInProgress(context.Background(), taskID, enqueueToken, worker)
	if err != nil {
		t.Fatalf("transition with correct token: %v", err)
	}
	if !transitioned {
		t.Fatalf("expected transition with correct token to succeed")
	}

	task = db.tasks[taskID]
	if task.status != "IN_PROGRESS" {
		t.Fatalf("expected IN_PROGRESS status, got %q", task.status)
	}
	if task.claimedBy != worker {
		t.Fatalf("expected claimed_by %q, got %q", worker, task.claimedBy)
	}
	if task.claimedAt.IsZero() {
		t.Fatalf("expected claimed_at to be set")
	}
	if task.heartbeatAt.IsZero() {
		t.Fatalf("expected heartbeat_at to be set")
	}
}

type fakeTransitionDB struct {
	tasks map[string]*fakeTransitionTask
}

type fakeTransitionTask struct {
	status       string
	enqueueToken string
	claimedBy    string
	claimedAt    time.Time
	heartbeatAt  time.Time
}

func newFakeTransitionDB() *fakeTransitionDB {
	return &fakeTransitionDB{
		tasks: map[string]*fakeTransitionTask{},
	}
}

func (f *fakeTransitionDB) insertPending(taskID, enqueueToken string) {
	f.tasks[taskID] = &fakeTransitionTask{
		status:       "PENDING",
		enqueueToken: enqueueToken,
	}
}

func (f *fakeTransitionDB) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	taskID := args[0].(string)
	token := args[1].(string)
	worker := args[2].(string)

	task := f.tasks[taskID]
	if task == nil {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	if task.status != "PENDING" || task.enqueueToken != token {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}

	now := time.Now()
	task.status = "IN_PROGRESS"
	task.claimedBy = worker
	task.claimedAt = now
	task.heartbeatAt = now

	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *fakeTransitionDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}

func (f *fakeTransitionDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow call")
}

func (f *fakeTransitionDB) Begin(context.Context) (pgx.Tx, error) {
	panic("unexpected Begin call")
}

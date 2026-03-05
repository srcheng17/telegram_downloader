package postgres

import (
	"context"
	"fmt"
	"strings"
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
	if task.version != 1 {
		t.Fatalf("expected version increment to 1, got %d", task.version)
	}
}

func TestUpdateTaskHeartbeatHonorsClaimedWorker(t *testing.T) {
	taskID := "task-heartbeat-1"
	worker := "worker-a"
	wrongWorker := "worker-b"
	before := time.Unix(100, 0).UTC()

	db := newFakeTransitionDB()
	db.insertTask(taskID, fakeTransitionTask{
		status:      "IN_PROGRESS",
		claimedBy:   worker,
		heartbeatAt: before,
		version:     5,
	})

	store := &Store{pool: db}

	if err := store.UpdateTaskHeartbeat(context.Background(), taskID, worker); err != nil {
		t.Fatalf("heartbeat update: %v", err)
	}

	task := db.tasks[taskID]
	if !task.heartbeatAt.After(before) {
		t.Fatalf("expected heartbeat_at to advance, before=%v after=%v", before, task.heartbeatAt)
	}
	if task.version != 6 {
		t.Fatalf("expected version increment to 6, got %d", task.version)
	}

	afterSuccess := task.heartbeatAt
	if err := store.UpdateTaskHeartbeat(context.Background(), taskID, wrongWorker); err != nil {
		t.Fatalf("heartbeat update with wrong worker: %v", err)
	}

	task = db.tasks[taskID]
	if !task.heartbeatAt.Equal(afterSuccess) {
		t.Fatalf("expected wrong-worker heartbeat to no-op")
	}
	if task.version != 6 {
		t.Fatalf("expected wrong-worker heartbeat version to stay 6, got %d", task.version)
	}
}

func TestTransitionToTerminalBehaviors(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		taskID := "task-terminal-happy"
		worker := "worker-a"
		resultPath := "/tmp/file.cbz"
		errMsg := "download failed"

		db := newFakeTransitionDB()
		db.insertTask(taskID, fakeTransitionTask{
			status:    "IN_PROGRESS",
			claimedBy: worker,
			version:   2,
		})

		store := &Store{pool: db}
		err := store.TransitionToTerminal(context.Background(), TransitionTerminalInput{
			TaskID:        taskID,
			Worker:        worker,
			Status:        "FAILED",
			Error:         stringPtr(errMsg),
			ResultZipPath: stringPtr(resultPath),
		})
		if err != nil {
			t.Fatalf("transition to terminal: %v", err)
		}

		task := db.tasks[taskID]
		if task.status != "FAILED" {
			t.Fatalf("expected FAILED, got %q", task.status)
		}
		if task.error == nil || *task.error != errMsg {
			t.Fatalf("expected error message %q, got %#v", errMsg, task.error)
		}
		if task.resultZipPath == nil || *task.resultZipPath != resultPath {
			t.Fatalf("expected result path %q, got %#v", resultPath, task.resultZipPath)
		}
		if task.endTime <= 0 {
			t.Fatalf("expected end_time to be set, got %f", task.endTime)
		}
		if task.heartbeatAt.IsZero() {
			t.Fatalf("expected heartbeat_at to be set")
		}
		if task.version != 3 {
			t.Fatalf("expected version increment to 3, got %d", task.version)
		}
	})

	t.Run("invalid status rejected before write", func(t *testing.T) {
		taskID := "task-terminal-invalid"
		worker := "worker-a"

		db := newFakeTransitionDB()
		db.insertTask(taskID, fakeTransitionTask{
			status:    "IN_PROGRESS",
			claimedBy: worker,
		})

		store := &Store{pool: db}
		err := store.TransitionToTerminal(context.Background(), TransitionTerminalInput{
			TaskID: taskID,
			Worker: worker,
			Status: "IN_PROGRESS",
		})
		if err == nil {
			t.Fatalf("expected invalid status error")
		}
		if db.execCalls != 0 {
			t.Fatalf("expected invalid status to skip db write, execCalls=%d", db.execCalls)
		}
		if db.tasks[taskID].status != "IN_PROGRESS" {
			t.Fatalf("expected task status to remain unchanged")
		}
	})

	t.Run("already terminal is idempotent no-op", func(t *testing.T) {
		taskID := "task-terminal-noop-terminal"
		worker := "worker-a"

		db := newFakeTransitionDB()
		db.insertTask(taskID, fakeTransitionTask{
			status:    "SUCCESS",
			claimedBy: worker,
			version:   7,
		})

		store := &Store{pool: db}
		err := store.TransitionToTerminal(context.Background(), TransitionTerminalInput{
			TaskID: taskID,
			Worker: worker,
			Status: "FAILED",
		})
		if err != nil {
			t.Fatalf("already terminal transition should be no-op, got %v", err)
		}
		task := db.tasks[taskID]
		if task.status != "SUCCESS" {
			t.Fatalf("expected status to remain SUCCESS, got %q", task.status)
		}
		if task.version != 7 {
			t.Fatalf("expected version unchanged at 7, got %d", task.version)
		}
	})

	t.Run("worker mismatch is idempotent no-op", func(t *testing.T) {
		taskID := "task-terminal-noop-worker"
		worker := "worker-a"

		db := newFakeTransitionDB()
		db.insertTask(taskID, fakeTransitionTask{
			status:    "IN_PROGRESS",
			claimedBy: worker,
			version:   9,
		})

		store := &Store{pool: db}
		err := store.TransitionToTerminal(context.Background(), TransitionTerminalInput{
			TaskID: taskID,
			Worker: "worker-b",
			Status: "FAILED",
		})
		if err != nil {
			t.Fatalf("worker mismatch transition should be no-op, got %v", err)
		}
		task := db.tasks[taskID]
		if task.status != "IN_PROGRESS" {
			t.Fatalf("expected status to remain IN_PROGRESS, got %q", task.status)
		}
		if task.version != 9 {
			t.Fatalf("expected version unchanged at 9, got %d", task.version)
		}
	})
}

func TestRequestTaskCancelBehaviors(t *testing.T) {
	t.Run("active task transitions to cancel requested", func(t *testing.T) {
		taskID := "task-cancel-active"
		path := "/tmp/existing.cbz"
		cancelMessage := "cancel requested"

		db := newFakeTransitionDB()
		db.insertTask(taskID, fakeTransitionTask{
			status:        "IN_PROGRESS",
			resultZipPath: stringPtr(path),
			version:       4,
		})

		store := &Store{pool: db}
		if err := store.RequestTaskCancel(context.Background(), taskID, cancelMessage); err != nil {
			t.Fatalf("request cancel: %v", err)
		}

		task := db.tasks[taskID]
		if task.status != "CANCEL_REQUESTED" {
			t.Fatalf("expected CANCEL_REQUESTED, got %q", task.status)
		}
		if task.cancelRequestedAt.IsZero() {
			t.Fatalf("expected cancel_requested_at to be set")
		}
		if task.resultZipPath != nil {
			t.Fatalf("expected result_zip_path to be cleared, got %#v", task.resultZipPath)
		}
		if task.error == nil || *task.error != cancelMessage {
			t.Fatalf("expected cancel error %q, got %#v", cancelMessage, task.error)
		}
	})

	t.Run("terminal task is not mutated", func(t *testing.T) {
		taskID := "task-cancel-terminal"
		path := "/tmp/keep.cbz"
		beforeCancelAt := time.Unix(123, 0).UTC()

		db := newFakeTransitionDB()
		db.insertTask(taskID, fakeTransitionTask{
			status:            "SUCCESS",
			resultZipPath:     stringPtr(path),
			cancelRequestedAt: beforeCancelAt,
		})

		store := &Store{pool: db}
		if err := store.RequestTaskCancel(context.Background(), taskID, "cancel requested"); err != nil {
			t.Fatalf("request cancel terminal: %v", err)
		}

		task := db.tasks[taskID]
		if task.status != "SUCCESS" {
			t.Fatalf("expected terminal status to remain SUCCESS, got %q", task.status)
		}
		if task.resultZipPath == nil || *task.resultZipPath != path {
			t.Fatalf("expected result_zip_path preserved as %q, got %#v", path, task.resultZipPath)
		}
		if !task.cancelRequestedAt.Equal(beforeCancelAt) {
			t.Fatalf("expected cancel_requested_at unchanged")
		}
	})
}

type fakeTransitionDB struct {
	execCalls int
	tasks     map[string]*fakeTransitionTask
}

type fakeTransitionTask struct {
	status            string
	enqueueToken      string
	claimedBy         string
	claimedAt         time.Time
	heartbeatAt       time.Time
	cancelRequestedAt time.Time
	error             *string
	resultZipPath     *string
	endTime           float64
	version           int64
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

func (f *fakeTransitionDB) insertTask(taskID string, task fakeTransitionTask) {
	copied := task
	f.tasks[taskID] = &copied
}

func (f *fakeTransitionDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	f.execCalls++

	switch {
	case strings.Contains(query, "AND enqueue_token = $2"):
		return f.execTransitionPending(args...)
	case strings.Contains(query, "$3 = 'CANCELED'") && strings.Contains(query, "$3 IN ('SUCCESS', 'FAILED')"):
		return f.execTransitionTerminal(args...)
	case strings.Contains(query, "AND claimed_by = $2") && strings.Contains(query, "status = 'IN_PROGRESS'"):
		return f.execHeartbeat(args...)
	case strings.Contains(query, "cancel_requested_at = NOW()"):
		return f.execRequestCancel(query, args...)
	default:
		panic(fmt.Sprintf("unexpected Exec query: %s", query))
	}
}

func (f *fakeTransitionDB) execTransitionPending(args ...any) (pgconn.CommandTag, error) {
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
	task.version++

	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *fakeTransitionDB) execHeartbeat(args ...any) (pgconn.CommandTag, error) {
	taskID := args[0].(string)
	worker := args[1].(string)

	task := f.tasks[taskID]
	if task == nil {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	if task.status != "IN_PROGRESS" || task.claimedBy != worker {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}

	task.heartbeatAt = time.Now()
	task.version++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *fakeTransitionDB) execTransitionTerminal(args ...any) (pgconn.CommandTag, error) {
	taskID := args[0].(string)
	worker := args[1].(string)
	status := args[2].(string)
	errValue, _ := args[3].(*string)
	pathValue, _ := args[4].(*string)

	task := f.tasks[taskID]
	if task == nil {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	if task.claimedBy != worker {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}

	switch status {
	case "CANCELED":
		if task.status != "IN_PROGRESS" && task.status != "CANCEL_REQUESTED" {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
	case "SUCCESS", "FAILED":
		if task.status != "IN_PROGRESS" {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
	default:
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}

	task.status = status
	task.error = errValue
	task.resultZipPath = pathValue
	task.endTime = float64(time.Now().Unix())
	task.heartbeatAt = time.Now()
	task.version++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *fakeTransitionDB) execRequestCancel(query string, args ...any) (pgconn.CommandTag, error) {
	taskID := args[0].(string)
	cancelError := args[1].(string)

	task := f.tasks[taskID]
	if task == nil {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}

	hasActiveStatusGuard := strings.Contains(query, "status IN ('PENDING', 'IN_PROGRESS', 'CANCEL_REQUESTED')")
	if hasActiveStatusGuard {
		if task.status != "PENDING" && task.status != "IN_PROGRESS" && task.status != "CANCEL_REQUESTED" {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
	}

	task.status = "CANCEL_REQUESTED"
	task.cancelRequestedAt = time.Now()
	task.error = stringPtr(cancelError)
	task.resultZipPath = nil
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

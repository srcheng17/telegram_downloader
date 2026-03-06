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

func TestCreateTaskPersistsQueuedTask(t *testing.T) {
	db := newFakeV2RepoDB()
	repo := &PostgresV2TaskRepo{db: db}

	canonicalURL := "https://telegra.ph/demo"
	task, err := repo.CreateTask(context.Background(), CreateTaskInput{
		ID:           "task-v2-1",
		URL:          canonicalURL,
		CanonicalURL: stringPtr(canonicalURL),
		EnqueueToken: "enqueue-token-v2",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.Status != "QUEUED" {
		t.Fatalf("expected QUEUED status, got %q", task.Status)
	}

	persisted, ok := db.tasks[task.ID]
	if !ok {
		t.Fatalf("expected task %q to be persisted", task.ID)
	}
	if persisted.status != "QUEUED" {
		t.Fatalf("expected persisted status QUEUED, got %q", persisted.status)
	}
	if persisted.enqueueToken != "enqueue-token-v2" {
		t.Fatalf("expected enqueue token to be persisted, got %q", persisted.enqueueToken)
	}
}

func TestAppendEventPersistsTransitionAudit(t *testing.T) {
	db := newFakeV2RepoDB()
	repo := &PostgresV2TaskRepo{db: db}

	fromStatus := "QUEUED"
	toStatus := "RUNNING"
	err := repo.AppendTaskEvent(context.Background(), TaskEvent{
		TaskID:      "task-v2-1",
		EventType:   "STATUS_TRANSITION",
		FromStatus:  &fromStatus,
		ToStatus:    &toStatus,
		PayloadJSON: `{"worker":"worker-a"}`,
	})
	if err != nil {
		t.Fatalf("append task event: %v", err)
	}
	if len(db.events) != 1 {
		t.Fatalf("expected one event persisted, got %d", len(db.events))
	}

	persisted := db.events[0]
	if persisted.taskID != "task-v2-1" {
		t.Fatalf("expected task_id task-v2-1, got %q", persisted.taskID)
	}
	if persisted.eventType != "STATUS_TRANSITION" {
		t.Fatalf("expected event_type STATUS_TRANSITION, got %q", persisted.eventType)
	}
	if persisted.fromStatus == nil || *persisted.fromStatus != fromStatus {
		t.Fatalf("expected from_status %q, got %#v", fromStatus, persisted.fromStatus)
	}
	if persisted.toStatus == nil || *persisted.toStatus != toStatus {
		t.Fatalf("expected to_status %q, got %#v", toStatus, persisted.toStatus)
	}
	if persisted.payloadJSON != `{"worker":"worker-a"}` {
		t.Fatalf("expected payload to be persisted, got %q", persisted.payloadJSON)
	}
}

type fakeV2RepoDB struct {
	tasks  map[string]fakeV2Task
	events []fakeV2Event
}

type fakeV2Task struct {
	id           string
	url          string
	canonicalURL string
	status       string
	enqueueToken string
	createdAt    time.Time
	updatedAt    time.Time
}

type fakeV2Event struct {
	taskID      string
	eventType   string
	fromStatus  *string
	toStatus    *string
	payloadJSON string
}

func newFakeV2RepoDB() *fakeV2RepoDB {
	return &fakeV2RepoDB{
		tasks:  map[string]fakeV2Task{},
		events: make([]fakeV2Event, 0),
	}
}

func (f *fakeV2RepoDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(query, "INSERT INTO v2_tasks"):
		if len(args) != 7 {
			return pgconn.CommandTag{}, fmt.Errorf("expected 7 create task args, got %d", len(args))
		}
		f.tasks[args[0].(string)] = fakeV2Task{
			id:           args[0].(string),
			url:          args[1].(string),
			canonicalURL: args[2].(string),
			status:       args[3].(string),
			enqueueToken: args[4].(string),
			createdAt:    args[5].(time.Time),
			updatedAt:    args[6].(time.Time),
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.Contains(query, "INSERT INTO v2_task_events"):
		if len(args) != 5 {
			return pgconn.CommandTag{}, fmt.Errorf("expected 5 task event args, got %d", len(args))
		}
		f.events = append(f.events, fakeV2Event{
			taskID:      args[0].(string),
			eventType:   args[1].(string),
			fromStatus:  cloneV2String(args[2].(*string)),
			toStatus:    cloneV2String(args[3].(*string)),
			payloadJSON: args[4].(string),
		})
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (f *fakeV2RepoDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}

func (f *fakeV2RepoDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow call")
}

func (f *fakeV2RepoDB) Begin(context.Context) (pgx.Tx, error) {
	panic("unexpected Begin call")
}

func cloneV2String(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

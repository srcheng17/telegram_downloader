package postgres

import (
	"context"
	"errors"
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
		Author:       stringPtr("作者A"),
		SeriesName:   stringPtr("系列B"),
		ComicName:    stringPtr("漫画C"),
		Summary:      stringPtr("简介D"),
		TagsRaw:      stringPtr("标签1，标签2"),
		TagsNormalized: stringPtr(
			"标签1,标签2",
		),
		GenresRaw:        stringPtr("类型1，类型2"),
		GenresNormalized: stringPtr("类型1,类型2"),
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
	if persisted.author == nil || *persisted.author != "作者A" {
		t.Fatalf("expected author persisted, got %#v", persisted.author)
	}
	if persisted.seriesName == nil || *persisted.seriesName != "系列B" {
		t.Fatalf("expected series_name persisted, got %#v", persisted.seriesName)
	}
	if persisted.comicName == nil || *persisted.comicName != "漫画C" {
		t.Fatalf("expected comic_name persisted, got %#v", persisted.comicName)
	}
	if persisted.summary == nil || *persisted.summary != "简介D" {
		t.Fatalf("expected summary persisted, got %#v", persisted.summary)
	}
	if persisted.tagsRaw == nil || *persisted.tagsRaw != "标签1，标签2" {
		t.Fatalf("expected tags_raw persisted, got %#v", persisted.tagsRaw)
	}
	if persisted.tagsNormalized == nil || *persisted.tagsNormalized != "标签1,标签2" {
		t.Fatalf("expected tags_normalized persisted, got %#v", persisted.tagsNormalized)
	}
	if persisted.genresRaw == nil || *persisted.genresRaw != "类型1，类型2" {
		t.Fatalf("expected genres_raw persisted, got %#v", persisted.genresRaw)
	}
	if persisted.genresNormalized == nil || *persisted.genresNormalized != "类型1,类型2" {
		t.Fatalf("expected genres_normalized persisted, got %#v", persisted.genresNormalized)
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

func TestUpdateTaskStatusUpdatesWhenFromMatches(t *testing.T) {
	db := newFakeV2RepoDB()
	db.tasks["task-v2-update"] = fakeV2Task{
		id:        "task-v2-update",
		url:       "https://telegra.ph/demo",
		status:    "QUEUED",
		createdAt: time.Now().UTC(),
		updatedAt: time.Now().UTC(),
	}
	repo := &PostgresV2TaskRepo{db: db}

	errText := "queued by api"
	claimedBy := "worker-a"
	if err := repo.UpdateTaskStatus(context.Background(), "task-v2-update", "QUEUED", "RUNNING", StatusPatch{
		Error:     &errText,
		ClaimedBy: &claimedBy,
	}); err != nil {
		t.Fatalf("update task status: %v", err)
	}

	persisted := db.tasks["task-v2-update"]
	if persisted.status != "RUNNING" {
		t.Fatalf("expected updated status RUNNING, got %q", persisted.status)
	}
	if persisted.error == nil || *persisted.error != errText {
		t.Fatalf("expected error %q, got %#v", errText, persisted.error)
	}
	if persisted.claimedBy == nil || *persisted.claimedBy != claimedBy {
		t.Fatalf("expected claimed_by %q, got %#v", claimedBy, persisted.claimedBy)
	}
}

func TestUpdateTaskStatusReturnsErrorWhenFromMismatched(t *testing.T) {
	db := newFakeV2RepoDB()
	db.tasks["task-v2-update"] = fakeV2Task{
		id:        "task-v2-update",
		url:       "https://telegra.ph/demo",
		status:    "RUNNING",
		createdAt: time.Now().UTC(),
		updatedAt: time.Now().UTC(),
	}
	repo := &PostgresV2TaskRepo{db: db}

	err := repo.UpdateTaskStatus(context.Background(), "task-v2-update", "QUEUED", "SUCCESS", StatusPatch{})
	if err == nil {
		t.Fatalf("expected status mismatch update to fail")
	}
	if !strings.Contains(err.Error(), "status mismatch") {
		t.Fatalf("expected mismatch semantics, got %v", err)
	}
	if !errors.Is(err, ErrV2TaskStatusMismatchOrNotFound) {
		t.Fatalf("expected mismatch sentinel error, got %v", err)
	}
}

func TestUpdateTaskStatusSetsCancelRequestedTimestamp(t *testing.T) {
	db := newFakeV2RepoDB()
	db.tasks["task-v2-cancel-request"] = fakeV2Task{
		id:        "task-v2-cancel-request",
		url:       "https://telegra.ph/demo",
		status:    "RUNNING",
		createdAt: time.Now().UTC(),
		updatedAt: time.Now().UTC(),
	}
	repo := &PostgresV2TaskRepo{db: db}

	reason := "Cancellation requested by user."
	if err := repo.UpdateTaskStatus(
		context.Background(),
		"task-v2-cancel-request",
		"RUNNING",
		"CANCEL_REQUESTED",
		StatusPatch{Error: &reason},
	); err != nil {
		t.Fatalf("update task status: %v", err)
	}

	persisted := db.tasks["task-v2-cancel-request"]
	if persisted.status != "CANCEL_REQUESTED" {
		t.Fatalf("expected status CANCEL_REQUESTED, got %q", persisted.status)
	}
	if persisted.cancelRequestedAt == nil || persisted.cancelRequestedAt.IsZero() {
		t.Fatalf("expected cancel_requested_at to be set")
	}
}

func TestUpdateTaskHeartbeatTouchesRunningTask(t *testing.T) {
	db := newFakeV2RepoDB()
	db.tasks["task-v2-heartbeat"] = fakeV2Task{
		id:        "task-v2-heartbeat",
		url:       "https://telegra.ph/demo",
		status:    "RUNNING",
		claimedBy: stringPtr("worker-v2-a"),
		createdAt: time.Now().UTC(),
		updatedAt: time.Now().UTC(),
	}
	repo := &PostgresV2TaskRepo{db: db}

	if err := repo.UpdateTaskHeartbeat(context.Background(), "task-v2-heartbeat", "worker-v2-a"); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}

	persisted := db.tasks["task-v2-heartbeat"]
	if persisted.heartbeatAt == nil || persisted.heartbeatAt.IsZero() {
		t.Fatalf("expected heartbeat_at to be set")
	}
}

func TestUpdateTaskStatusIncrementsRetryCountOnRunningFailure(t *testing.T) {
	db := newFakeV2RepoDB()
	db.tasks["task-v2-retry"] = fakeV2Task{
		id:         "task-v2-retry",
		url:        "https://telegra.ph/demo",
		status:     "RUNNING",
		retryCount: 2,
		createdAt:  time.Now().UTC(),
		updatedAt:  time.Now().UTC(),
	}
	repo := &PostgresV2TaskRepo{db: db}

	reason := "download failed"
	if err := repo.UpdateTaskStatus(
		context.Background(),
		"task-v2-retry",
		"RUNNING",
		"FAILED",
		StatusPatch{Error: &reason},
	); err != nil {
		t.Fatalf("update task status: %v", err)
	}

	persisted := db.tasks["task-v2-retry"]
	if persisted.status != "FAILED" {
		t.Fatalf("expected status FAILED, got %q", persisted.status)
	}
	if persisted.retryCount != 3 {
		t.Fatalf("expected retry_count=3, got %d", persisted.retryCount)
	}
}

type fakeV2RepoDB struct {
	tasks  map[string]fakeV2Task
	events []fakeV2Event
}

type fakeV2Task struct {
	id                string
	url               string
	canonicalURL      string
	status            string
	enqueueToken      string
	author            *string
	seriesName        *string
	comicName         *string
	summary           *string
	tagsRaw           *string
	tagsNormalized    *string
	genresRaw         *string
	genresNormalized  *string
	error             *string
	resultZipPath     *string
	claimedBy         *string
	heartbeatAt       *time.Time
	cancelRequestedAt *time.Time
	retryCount        int
	createdAt         time.Time
	updatedAt         time.Time
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
		if len(args) != 15 {
			return pgconn.CommandTag{}, fmt.Errorf("expected 15 create task args, got %d", len(args))
		}
		f.tasks[args[0].(string)] = fakeV2Task{
			id:               args[0].(string),
			url:              args[1].(string),
			canonicalURL:     args[2].(string),
			status:           args[3].(string),
			enqueueToken:     args[4].(string),
			author:           cloneV2String(args[5].(*string)),
			seriesName:       cloneV2String(args[6].(*string)),
			comicName:        cloneV2String(args[7].(*string)),
			summary:          cloneV2String(args[8].(*string)),
			tagsRaw:          cloneV2String(args[9].(*string)),
			tagsNormalized:   cloneV2String(args[10].(*string)),
			genresRaw:        cloneV2String(args[11].(*string)),
			genresNormalized: cloneV2String(args[12].(*string)),
			createdAt:        args[13].(time.Time),
			updatedAt:        args[14].(time.Time),
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
	case strings.Contains(query, "UPDATE v2_tasks"):
		if strings.Contains(query, "status IN ('RUNNING', 'CANCEL_REQUESTED')") {
			if len(args) != 2 {
				return pgconn.CommandTag{}, fmt.Errorf("expected 2 heartbeat args, got %d", len(args))
			}
			taskID := args[0].(string)
			worker := args[1].(string)
			task, ok := f.tasks[taskID]
			if !ok || task.claimedBy == nil || *task.claimedBy != worker {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			}
			if task.status != "RUNNING" && task.status != "CANCEL_REQUESTED" {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			}
			now := time.Now().UTC()
			task.heartbeatAt = &now
			task.updatedAt = now
			f.tasks[taskID] = task
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}

		if len(args) != 6 {
			return pgconn.CommandTag{}, fmt.Errorf("expected 6 update args, got %d", len(args))
		}
		taskID := args[0].(string)
		fromStatus := args[1].(string)
		toStatus := args[2].(string)

		task, ok := f.tasks[taskID]
		if !ok || task.status != fromStatus {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}

		task.status = toStatus
		task.error = cloneV2String(args[3].(*string))
		task.resultZipPath = cloneV2String(args[4].(*string))
		if args[5].(*string) != nil {
			task.claimedBy = cloneV2String(args[5].(*string))
		}
		now := time.Now().UTC()
		task.updatedAt = now
		if toStatus == "RUNNING" || toStatus == "SUCCESS" || toStatus == "FAILED" || toStatus == "CANCELED" {
			task.heartbeatAt = &now
		}
		if toStatus == "CANCEL_REQUESTED" {
			task.cancelRequestedAt = &now
		}
		if fromStatus == "RUNNING" && toStatus == "FAILED" {
			task.retryCount++
		}
		f.tasks[taskID] = task

		return pgconn.NewCommandTag("UPDATE 1"), nil
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

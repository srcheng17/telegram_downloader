package httpv2

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestClaimTaskForLegacyReusesSuccessTaskWhenAvailable(t *testing.T) {
	db := newFakeLegacyClaimDB()
	db.tx.successTask = &Task{
		ID:           "task-success-1",
		URL:          "https://telegra.ph/success",
		CanonicalURL: stringPtr("https://telegra.ph/success"),
		Status:       TaskStatusSuccess,
		ResultZipPath: stringPtr(
			"/tmp/task-success-1.zip",
		),
		CreatedAt: time.Unix(100, 0).UTC(),
		UpdatedAt: time.Unix(200, 0).UTC(),
	}

	store := &PostgresTaskStore{db: db}
	result, err := store.ClaimTaskForLegacy(context.Background(), LegacyClaimTaskInput{
		ID:           "task-new-1",
		URL:          " https://telegra.ph/success ",
		EnqueueToken: "enqueue-token-1",
		ReuseSuccess: true,
	})
	if err != nil {
		t.Fatalf("claim task for legacy: %v", err)
	}

	if result.Decision != LegacyClaimTaskDecisionReuseSuccess {
		t.Fatalf("expected decision %q, got %q", LegacyClaimTaskDecisionReuseSuccess, result.Decision)
	}
	if result.Task.ID != "task-success-1" {
		t.Fatalf("expected reused task id task-success-1, got %q", result.Task.ID)
	}
	if db.beginCalls != 1 {
		t.Fatalf("expected one begin call, got %d", db.beginCalls)
	}
	if db.tx.commitCalls != 1 {
		t.Fatalf("expected one commit call, got %d", db.tx.commitCalls)
	}
	if db.tx.rollbackCalls != 0 {
		t.Fatalf("expected no rollback call, got %d", db.tx.rollbackCalls)
	}
	if len(db.tx.insertedRows) != 0 {
		t.Fatalf("expected no insert calls when success task reused")
	}
	if len(db.tx.queryKinds) != 1 || db.tx.queryKinds[0] != "success" {
		t.Fatalf("expected only success query, got %#v", db.tx.queryKinds)
	}
	if db.tx.lockCanonicalURL != "https://telegra.ph/success" {
		t.Fatalf("expected lock canonical url https://telegra.ph/success, got %q", db.tx.lockCanonicalURL)
	}
}

func TestClaimTaskForLegacyReusesActiveTaskWhenNoSuccess(t *testing.T) {
	db := newFakeLegacyClaimDB()
	db.tx.activeTask = &Task{
		ID:           "task-running-1",
		URL:          "https://telegra.ph/running",
		CanonicalURL: stringPtr("https://telegra.ph/running"),
		Status:       TaskStatusRunning,
		CreatedAt:    time.Unix(300, 0).UTC(),
		UpdatedAt:    time.Unix(400, 0).UTC(),
	}

	store := &PostgresTaskStore{db: db}
	result, err := store.ClaimTaskForLegacy(context.Background(), LegacyClaimTaskInput{
		ID:           "task-new-2",
		URL:          "https://telegra.ph/raw",
		CanonicalURL: stringPtr(" https://telegra.ph/running "),
		EnqueueToken: "enqueue-token-2",
		ReuseSuccess: true,
	})
	if err != nil {
		t.Fatalf("claim task for legacy: %v", err)
	}

	if result.Decision != LegacyClaimTaskDecisionReuseActive {
		t.Fatalf("expected decision %q, got %q", LegacyClaimTaskDecisionReuseActive, result.Decision)
	}
	if result.Task.ID != "task-running-1" {
		t.Fatalf("expected reused task id task-running-1, got %q", result.Task.ID)
	}
	if db.beginCalls != 1 {
		t.Fatalf("expected one begin call, got %d", db.beginCalls)
	}
	if db.tx.commitCalls != 1 {
		t.Fatalf("expected one commit call, got %d", db.tx.commitCalls)
	}
	if db.tx.rollbackCalls != 0 {
		t.Fatalf("expected no rollback call, got %d", db.tx.rollbackCalls)
	}
	if len(db.tx.insertedRows) != 0 {
		t.Fatalf("expected no insert calls when active task reused")
	}
	if len(db.tx.queryKinds) != 2 || db.tx.queryKinds[0] != "success" || db.tx.queryKinds[1] != "active" {
		t.Fatalf("expected success then active query, got %#v", db.tx.queryKinds)
	}
	if db.tx.lockCanonicalURL != "https://telegra.ph/running" {
		t.Fatalf("expected lock canonical url https://telegra.ph/running, got %q", db.tx.lockCanonicalURL)
	}
}

func TestClaimTaskForLegacyCreatesTaskWhenNoReusableTask(t *testing.T) {
	db := newFakeLegacyClaimDB()
	store := &PostgresTaskStore{db: db}

	result, err := store.ClaimTaskForLegacy(context.Background(), LegacyClaimTaskInput{
		ID:           "task-created-1",
		URL:          " https://telegra.ph/created ",
		EnqueueToken: "enqueue-token-3",
		ReuseSuccess: false,
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
		t.Fatalf("claim task for legacy: %v", err)
	}

	if result.Decision != LegacyClaimTaskDecisionCreated {
		t.Fatalf("expected decision %q, got %q", LegacyClaimTaskDecisionCreated, result.Decision)
	}
	if result.Task.ID != "task-created-1" {
		t.Fatalf("expected created task id task-created-1, got %q", result.Task.ID)
	}
	if result.Task.Status != TaskStatusQueued {
		t.Fatalf("expected created task status QUEUED, got %q", result.Task.Status)
	}
	if result.EnqueueToken != "enqueue-token-3" {
		t.Fatalf("expected enqueue token enqueue-token-3, got %q", result.EnqueueToken)
	}
	if result.Task.CanonicalURL == nil || *result.Task.CanonicalURL != "https://telegra.ph/created" {
		t.Fatalf("expected canonical url https://telegra.ph/created, got %#v", result.Task.CanonicalURL)
	}
	if db.beginCalls != 1 {
		t.Fatalf("expected one begin call, got %d", db.beginCalls)
	}
	if db.tx.commitCalls != 1 {
		t.Fatalf("expected one commit call, got %d", db.tx.commitCalls)
	}
	if db.tx.rollbackCalls != 0 {
		t.Fatalf("expected no rollback call, got %d", db.tx.rollbackCalls)
	}
	if len(db.tx.queryKinds) != 1 || db.tx.queryKinds[0] != "active" {
		t.Fatalf("expected active-only query when reuse_success disabled, got %#v", db.tx.queryKinds)
	}
	if len(db.tx.insertedRows) != 1 {
		t.Fatalf("expected one inserted row, got %d", len(db.tx.insertedRows))
	}
	inserted := db.tx.insertedRows[0]
	if inserted.taskID != "task-created-1" {
		t.Fatalf("expected inserted task id task-created-1, got %q", inserted.taskID)
	}
	if inserted.url != "https://telegra.ph/created" {
		t.Fatalf("expected inserted url https://telegra.ph/created, got %q", inserted.url)
	}
	if inserted.canonicalURL != "https://telegra.ph/created" {
		t.Fatalf("expected inserted canonical url https://telegra.ph/created, got %q", inserted.canonicalURL)
	}
	if inserted.status != TaskStatusQueued {
		t.Fatalf("expected inserted status QUEUED, got %q", inserted.status)
	}
	if inserted.enqueueToken != "enqueue-token-3" {
		t.Fatalf("expected inserted enqueue token enqueue-token-3, got %q", inserted.enqueueToken)
	}
	if inserted.author == nil || *inserted.author != "作者A" {
		t.Fatalf("expected inserted author 作者A, got %#v", inserted.author)
	}
	if inserted.seriesName == nil || *inserted.seriesName != "系列B" {
		t.Fatalf("expected inserted series_name 系列B, got %#v", inserted.seriesName)
	}
	if inserted.comicName == nil || *inserted.comicName != "漫画C" {
		t.Fatalf("expected inserted comic_name 漫画C, got %#v", inserted.comicName)
	}
	if inserted.summary == nil || *inserted.summary != "简介D" {
		t.Fatalf("expected inserted summary 简介D, got %#v", inserted.summary)
	}
	if inserted.tagsRaw == nil || *inserted.tagsRaw != "标签1，标签2" {
		t.Fatalf("expected inserted tags_raw 标签1，标签2, got %#v", inserted.tagsRaw)
	}
	if inserted.tagsNormalized == nil || *inserted.tagsNormalized != "标签1,标签2" {
		t.Fatalf("expected inserted tags_normalized 标签1,标签2, got %#v", inserted.tagsNormalized)
	}
	if inserted.genresRaw == nil || *inserted.genresRaw != "类型1，类型2" {
		t.Fatalf("expected inserted genres_raw 类型1，类型2, got %#v", inserted.genresRaw)
	}
	if inserted.genresNormalized == nil || *inserted.genresNormalized != "类型1,类型2" {
		t.Fatalf("expected inserted genres_normalized 类型1,类型2, got %#v", inserted.genresNormalized)
	}
	if inserted.createdAt.IsZero() || inserted.updatedAt.IsZero() {
		t.Fatalf("expected non-zero inserted timestamps")
	}
	if !inserted.createdAt.Equal(inserted.updatedAt) {
		t.Fatalf("expected inserted timestamps to be equal, got created=%s updated=%s", inserted.createdAt, inserted.updatedAt)
	}
}

func TestClaimTaskForLegacyRejectsBlankEnqueueTokenBeforeBegin(t *testing.T) {
	db := newFakeLegacyClaimDB()
	store := &PostgresTaskStore{db: db}

	_, err := store.ClaimTaskForLegacy(context.Background(), LegacyClaimTaskInput{
		ID:           "task-created-2",
		URL:          "https://telegra.ph/demo",
		EnqueueToken: "   ",
	})
	if err == nil {
		t.Fatalf("expected blank enqueue token to fail")
	}
	if !strings.Contains(err.Error(), "enqueue token is required") {
		t.Fatalf("expected enqueue token validation error, got %v", err)
	}
	if db.beginCalls != 0 {
		t.Fatalf("expected no begin call on validation failure, got %d", db.beginCalls)
	}
}

type fakeLegacyClaimDB struct {
	tx         *fakeLegacyClaimTx
	beginCalls int
	beginErr   error
}

func newFakeLegacyClaimDB() *fakeLegacyClaimDB {
	return &fakeLegacyClaimDB{
		tx: &fakeLegacyClaimTx{},
	}
}

func (f *fakeLegacyClaimDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}

func (f *fakeLegacyClaimDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow call")
}

func (f *fakeLegacyClaimDB) Begin(context.Context) (pgx.Tx, error) {
	f.beginCalls++
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f.tx, nil
}

type fakeLegacyClaimInsertRow struct {
	taskID           string
	url              string
	canonicalURL     string
	status           string
	enqueueToken     string
	author           *string
	seriesName       *string
	comicName        *string
	summary          *string
	tagsRaw          *string
	tagsNormalized   *string
	genresRaw        *string
	genresNormalized *string
	createdAt        time.Time
	updatedAt        time.Time
}

type fakeLegacyClaimTx struct {
	successTask      *Task
	activeTask       *Task
	lockCanonicalURL string
	queryKinds       []string
	insertedRows     []fakeLegacyClaimInsertRow
	commitCalls      int
	rollbackCalls    int
	closed           bool
}

func (f *fakeLegacyClaimTx) Begin(context.Context) (pgx.Tx, error) {
	panic("unexpected Begin call")
}

func (f *fakeLegacyClaimTx) Commit(context.Context) error {
	if f.closed {
		return pgx.ErrTxClosed
	}
	f.closed = true
	f.commitCalls++
	return nil
}

func (f *fakeLegacyClaimTx) Rollback(context.Context) error {
	if f.closed {
		return pgx.ErrTxClosed
	}
	f.closed = true
	f.rollbackCalls++
	return nil
}

func (f *fakeLegacyClaimTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	panic("unexpected CopyFrom call")
}

func (f *fakeLegacyClaimTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	panic("unexpected SendBatch call")
}

func (f *fakeLegacyClaimTx) LargeObjects() pgx.LargeObjects {
	panic("unexpected LargeObjects call")
}

func (f *fakeLegacyClaimTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	panic("unexpected Prepare call")
}

func (f *fakeLegacyClaimTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(query, "SELECT pg_advisory_xact_lock"):
		if len(args) != 1 {
			return pgconn.CommandTag{}, fmt.Errorf("expected one advisory lock arg, got %d", len(args))
		}
		canonicalURL, ok := args[0].(string)
		if !ok {
			return pgconn.CommandTag{}, fmt.Errorf("expected advisory lock arg type string, got %T", args[0])
		}
		f.lockCanonicalURL = canonicalURL
		return pgconn.NewCommandTag("SELECT 1"), nil
	case strings.Contains(query, "INSERT INTO v2_tasks"):
		if len(args) != 15 {
			return pgconn.CommandTag{}, fmt.Errorf("expected fifteen insert args, got %d", len(args))
		}

		createdAt, ok := args[13].(time.Time)
		if !ok {
			return pgconn.CommandTag{}, fmt.Errorf("expected created_at type time.Time, got %T", args[13])
		}
		updatedAt, ok := args[14].(time.Time)
		if !ok {
			return pgconn.CommandTag{}, fmt.Errorf("expected updated_at type time.Time, got %T", args[14])
		}

		f.insertedRows = append(f.insertedRows, fakeLegacyClaimInsertRow{
			taskID:           args[0].(string),
			url:              args[1].(string),
			canonicalURL:     args[2].(string),
			status:           args[3].(string),
			enqueueToken:     args[4].(string),
			author:           cloneLegacyClaimString(args[5].(*string)),
			seriesName:       cloneLegacyClaimString(args[6].(*string)),
			comicName:        cloneLegacyClaimString(args[7].(*string)),
			summary:          cloneLegacyClaimString(args[8].(*string)),
			tagsRaw:          cloneLegacyClaimString(args[9].(*string)),
			tagsNormalized:   cloneLegacyClaimString(args[10].(*string)),
			genresRaw:        cloneLegacyClaimString(args[11].(*string)),
			genresNormalized: cloneLegacyClaimString(args[12].(*string)),
			createdAt:        createdAt,
			updatedAt:        updatedAt,
		})
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected Exec query: %s", query)
	}
}

func (f *fakeLegacyClaimTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	return nil, fmt.Errorf("unexpected Query call: %s", query)
}

func (f *fakeLegacyClaimTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	switch {
	case strings.Contains(query, "result_zip_path IS NOT NULL"):
		f.queryKinds = append(f.queryKinds, "success")
		if f.successTask == nil {
			return fakeLegacyClaimRow{err: pgx.ErrNoRows}
		}
		return fakeLegacyClaimRow{task: f.successTask}
	case strings.Contains(query, "status = ANY($1)"):
		f.queryKinds = append(f.queryKinds, "active")
		if f.activeTask == nil {
			return fakeLegacyClaimRow{err: pgx.ErrNoRows}
		}
		return fakeLegacyClaimRow{task: f.activeTask}
	default:
		return fakeLegacyClaimRow{err: fmt.Errorf("unexpected QueryRow query: %s", query)}
	}
}

func (f *fakeLegacyClaimTx) Conn() *pgx.Conn {
	return nil
}

type fakeLegacyClaimRow struct {
	task *Task
	err  error
}

func (f fakeLegacyClaimRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	if f.task == nil {
		return pgx.ErrNoRows
	}
	if len(dest) != 8 {
		return fmt.Errorf("expected eight scan destinations, got %d", len(dest))
	}

	*dest[0].(*string) = f.task.ID
	*dest[1].(*string) = f.task.URL
	*dest[2].(**string) = cloneLegacyClaimString(f.task.CanonicalURL)
	*dest[3].(*string) = f.task.Status
	*dest[4].(**string) = cloneLegacyClaimString(f.task.Error)
	*dest[5].(**string) = cloneLegacyClaimString(f.task.ResultZipPath)
	*dest[6].(*time.Time) = f.task.CreatedAt
	*dest[7].(*time.Time) = f.task.UpdatedAt
	return nil
}

func cloneLegacyClaimString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

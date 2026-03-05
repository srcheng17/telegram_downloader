package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
)

func TestClaimDownloadTaskPersistsEnqueueToken(t *testing.T) {
	db := newFakeClaimDB()
	store := &Store{pool: db}

	enqueueToken := "enqueue-token-1"
	canonicalURL := "https://telegra.ph/demo"
	taskID := "task-claim-1"

	claim, err := store.ClaimDownloadTask(context.Background(), domain.ClaimDownloadTaskInput{
		Task: domain.TaskLog{
			ID:               taskID,
			URL:              canonicalURL,
			CanonicalURL:     stringPtr(canonicalURL),
			Status:           domain.StatusPending,
			StartTime:        123,
			Progress:         0,
			TotalImages:      0,
			ImageConcurrency: 2,
		},
		EnqueueToken:   enqueueToken,
		ActiveStatuses: domain.ActiveTaskStatuses,
		ReuseSuccess:   true,
	})
	if err != nil {
		t.Fatalf("claim download task: %v", err)
	}
	if claim.Decision != domain.ClaimDecisionCreated {
		t.Fatalf("expected created claim decision, got %q", claim.Decision)
	}

	persisted, ok := db.tasks[taskID]
	if !ok {
		t.Fatalf("expected task %q to be inserted", taskID)
	}
	if persisted.enqueueToken != enqueueToken {
		t.Fatalf("expected enqueue token %q, got %q", enqueueToken, persisted.enqueueToken)
	}
}

func TestClaimDownloadTaskRejectsEmptyEnqueueToken(t *testing.T) {
	db := newFakeClaimDB()
	store := &Store{pool: db}

	_, err := store.ClaimDownloadTask(context.Background(), domain.ClaimDownloadTaskInput{
		Task: domain.TaskLog{
			ID:               "task-claim-empty-token",
			URL:              "https://telegra.ph/demo",
			Status:           domain.StatusPending,
			StartTime:        123,
			Progress:         0,
			TotalImages:      0,
			ImageConcurrency: 2,
		},
		EnqueueToken: "   ",
	})
	if err == nil {
		t.Fatalf("expected empty enqueue token to be rejected")
	}
	if !strings.Contains(err.Error(), "enqueue token is required") {
		t.Fatalf("expected empty token error, got %v", err)
	}
	if db.beginCalls != 0 {
		t.Fatalf("expected empty token to fail before transaction, begin calls=%d", db.beginCalls)
	}
}

type fakeClaimDB struct {
	beginCalls int
	tasks      map[string]fakeClaimTask
}

type fakeClaimTask struct {
	task         domain.TaskLog
	enqueueToken string
}

func newFakeClaimDB() *fakeClaimDB {
	return &fakeClaimDB{
		tasks: map[string]fakeClaimTask{},
	}
}

func (f *fakeClaimDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec call")
}

func (f *fakeClaimDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}

func (f *fakeClaimDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow call")
}

func (f *fakeClaimDB) Begin(context.Context) (pgx.Tx, error) {
	f.beginCalls++
	return &fakeClaimTx{db: f}, nil
}

type fakeClaimTx struct {
	db     *fakeClaimDB
	closed bool
}

func (f *fakeClaimTx) Begin(context.Context) (pgx.Tx, error) {
	panic("unexpected Begin call")
}

func (f *fakeClaimTx) Commit(context.Context) error {
	if f.closed {
		return pgx.ErrTxClosed
	}
	f.closed = true
	return nil
}

func (f *fakeClaimTx) Rollback(context.Context) error {
	if f.closed {
		return pgx.ErrTxClosed
	}
	f.closed = true
	return nil
}

func (f *fakeClaimTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	panic("unexpected CopyFrom call")
}

func (f *fakeClaimTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	panic("unexpected SendBatch call")
}

func (f *fakeClaimTx) LargeObjects() pgx.LargeObjects {
	panic("unexpected LargeObjects call")
}

func (f *fakeClaimTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	panic("unexpected Prepare call")
}

func (f *fakeClaimTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(query, "SELECT pg_advisory_xact_lock"):
		return pgconn.NewCommandTag("SELECT 1"), nil
	case strings.Contains(query, "INSERT INTO tasks"):
		if len(args) != 19 {
			return pgconn.CommandTag{}, fmt.Errorf("expected 19 insert args, got %d", len(args))
		}
		inserted := domain.TaskLog{
			ID:               args[0].(string),
			URL:              args[1].(string),
			CanonicalURL:     args[2].(*string),
			Status:           args[3].(string),
			StartTime:        args[4].(float64),
			Error:            args[6].(*string),
			Progress:         args[7].(int),
			TotalImages:      args[8].(int),
			ImageConcurrency: args[9].(int),
			ResultZipPath:    args[10].(*string),
			Author:           args[11].(*string),
			SeriesName:       args[12].(*string),
			ComicName:        args[13].(*string),
			Summary:          args[14].(*string),
			TagsRaw:          args[15].(*string),
			TagsNormalized:   args[16].(*string),
			GenresRaw:        args[17].(*string),
			GenresNormalized: args[18].(*string),
		}
		f.db.tasks[inserted.ID] = fakeClaimTask{
			task:         inserted,
			enqueueToken: args[5].(string),
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected Exec query: %s", query)
	}
}

func (f *fakeClaimTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}

func (f *fakeClaimTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	switch {
	case strings.Contains(query, "status = 'SUCCESS'"):
		return fakeClaimRow{err: pgx.ErrNoRows}
	case strings.Contains(query, "status = ANY($1)"):
		return fakeClaimRow{err: pgx.ErrNoRows}
	case strings.Contains(query, "FROM tasks WHERE id = $1"):
		taskID := args[0].(string)
		persisted, ok := f.db.tasks[taskID]
		if !ok {
			return fakeClaimRow{err: pgx.ErrNoRows}
		}
		return fakeClaimRow{task: persisted.task}
	default:
		return fakeClaimRow{err: fmt.Errorf("unexpected QueryRow query: %s", query)}
	}
}

func (f *fakeClaimTx) Conn() *pgx.Conn {
	return nil
}

type fakeClaimRow struct {
	task domain.TaskLog
	err  error
}

func (f fakeClaimRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	if len(dest) != 18 {
		return fmt.Errorf("expected 18 scan destinations, got %d", len(dest))
	}
	*dest[0].(*string) = f.task.ID
	*dest[1].(*string) = f.task.URL
	*dest[2].(**string) = cloneString(f.task.CanonicalURL)
	*dest[3].(*string) = f.task.Status
	*dest[4].(*float64) = f.task.StartTime
	*dest[5].(**string) = cloneString(f.task.Error)
	*dest[6].(*int) = f.task.Progress
	*dest[7].(*int) = f.task.TotalImages
	*dest[8].(*int) = f.task.ImageConcurrency
	*dest[9].(**string) = cloneString(f.task.ResultZipPath)
	*dest[10].(**string) = cloneString(f.task.Author)
	*dest[11].(**string) = cloneString(f.task.SeriesName)
	*dest[12].(**string) = cloneString(f.task.ComicName)
	*dest[13].(**string) = cloneString(f.task.Summary)
	*dest[14].(**string) = cloneString(f.task.TagsRaw)
	*dest[15].(**string) = cloneString(f.task.TagsNormalized)
	*dest[16].(**string) = cloneString(f.task.GenresRaw)
	*dest[17].(**string) = cloneString(f.task.GenresNormalized)
	return nil
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

package komgaedit_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/komgaedit"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/komgaedit"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/migrations"
)

func randomID(t *testing.T) string {
	t.Helper()
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(raw[:])
}

func TestOperationPersistenceAndCrossProcessLock(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := migrations.Run(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := komgaedit.NewOperationStore(pool)
	firstID, secondID := randomID(t), randomID(t)
	defer pool.Exec(ctx, `DELETE FROM komga_edit_operations WHERE id=ANY($1)`, []string{firstID, secondID})
	key := "test-" + firstID
	first := app.Operation{ID: firstID, IdempotencyKey: key, RequestDigest: randomID(t), BookID: "book-" + firstID,
		LibraryID: "lib-" + firstID, RelativePath: "folder/book.cbz", SourceSHA256: randomID(t),
		TargetSHA256: randomID(t), State: app.StatePreparing, DesiredFields: []app.FieldChange{}}
	first, err = store.Create(ctx, first)
	if err != nil || first.Version != 1 {
		t.Fatalf("create: %#v, %v", first, err)
	}
	read, err := store.GetByKey(ctx, key)
	if err != nil || read.ID != firstID || read.State != app.StatePreparing {
		t.Fatalf("read: %#v, %v", read, err)
	}
	other := first
	other.ID = secondID
	other.IdempotencyKey = "test-" + secondID
	if _, err := store.Create(ctx, other); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("active path accepted: %v", err)
	}
	first.State = app.StateCurrentValueConsistent
	first.FileNoChange = true
	first.ProjectionConsistent = true
	first, err = store.Update(ctx, first)
	if err != nil || first.Version != 2 {
		t.Fatalf("update: %#v, %v", first, err)
	}
	if _, err := store.Update(ctx, read); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("stale update accepted: %v", err)
	}
	if _, err := store.Create(ctx, other); err != nil {
		t.Fatalf("completed operation still blocks path: %v", err)
	}
	var active, overlap atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := store.WithBookLock(ctx, first.LibraryID+"/"+first.RelativePath, func(context.Context) error {
				if active.Add(1) != 1 {
					overlap.Add(1)
				}
				time.Sleep(25 * time.Millisecond)
				active.Add(-1)
				return nil
			}); err != nil {
				t.Errorf("book lock: %v", err)
			}
		}()
	}
	group.Wait()
	if overlap.Load() != 0 {
		t.Fatal("two writers held the same book lock")
	}
	// A canceled waiter must not leave a pooled session holding this path's
	// advisory lock, including when cancellation races with a grant.
	locked := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- store.WithBookLock(ctx, first.LibraryID+"/"+first.RelativePath, func(context.Context) error {
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	waitCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	if err := store.WithBookLock(waitCtx, first.LibraryID+"/"+first.RelativePath, func(context.Context) error {
		t.Error("canceled waiter entered the critical section")
		return nil
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("canceled lock waiter: %v", err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	checkCtx, checkCancel := context.WithTimeout(ctx, time.Second)
	defer checkCancel()
	if err := store.WithBookLock(checkCtx, first.LibraryID+"/"+first.RelativePath, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("lock unusable after canceled waiter: %v", err)
	}
}

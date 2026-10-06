package taskcore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/config"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

func TestSubmissionConcurrentUploadAndRestartRecovery(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	const callers = 8
	ids := make([]string, callers)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	cleanupTaskCoreRows(t, ctx, store, ids...)
	key := uuid.NewString()
	var wg sync.WaitGroup
	results := make(chan app.Task, callers)
	failures := make(chan error, callers)
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			service := app.NewService(NewStore(store.pool), app.Config{})
			task, err := service.InitUploadTask(ctx, app.InitUploadInput{ID: id, IdempotencyKey: key, DeliveryTarget: "komga", FileName: "same.zip", FileSize: 3, FileSHA256: strings.Repeat("a", 64), Metadata: map[string]string{"comic_name": "Reviewed title"}})
			if err != nil {
				failures <- err
				return
			}
			results <- task
		}(id)
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var taskID string
	for task := range results {
		if taskID == "" {
			taskID = task.ID
		}
		if task.ID != taskID {
			t.Fatal("same submission created multiple tasks")
		}
	}
	if taskID == "" {
		t.Fatal("no task")
	}
	var events, histories int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM task_core_events WHERE task_id=$1 AND event_type='TASK_CREATED'`, taskID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM metadata_history WHERE task_id=$1`, taskID).Scan(&histories); err != nil {
		t.Fatal(err)
	}
	if events != 1 || histories != 1 {
		t.Fatalf("creation repeated: events=%d history=%d", events, histories)
	}
	// A fresh API service can recover with only the client key after response loss.
	service := app.NewService(NewStore(store.pool), app.Config{})
	view, err := service.GetSubmission(ctx, key)
	if err != nil || view.Task.ID != taskID || view.Task.Status != domain.StatusCreated || view.Input.SourceArchiveName != "same.zip" || view.Input.SourceSHA256 != strings.Repeat("a", 64) {
		t.Fatalf("recovery=%+v err=%v", view, err)
	}
	_, err = service.InitUploadTask(ctx, app.InitUploadInput{ID: uuid.NewString(), IdempotencyKey: key, DeliveryTarget: "komga", FileName: "same.zip", FileSize: 3, FileSHA256: strings.Repeat("b", 64), Metadata: map[string]string{"comic_name": "Reviewed title"}})
	if !errors.Is(err, app.ErrIdempotencyConflict) {
		t.Fatalf("changed file reused key: %v", err)
	}
	// A server setting change cannot create another task for the same reviewed input.
	replay, err := service.InitUploadTask(ctx, app.InitUploadInput{ID: uuid.NewString(), IdempotencyKey: key, DeliveryTarget: "komga", FileName: "same.zip", FileSize: 3, FileSHA256: strings.Repeat("a", 64), Metadata: map[string]string{"comic_name": "Reviewed title"}, RuntimeSettings: &config.SettingsSnapshot{Timeout: 17}})
	if err != nil || replay.ID != taskID {
		t.Fatalf("settings changed replay: %v", err)
	}
}

func TestSubmissionURLKeepsCanonicalDedupAndRejectsChangedSnapshot(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	cleanupTaskCoreRows(t, ctx, store, ids...)
	service := app.NewService(store, app.Config{})
	url := "https://telegra.ph/submission-" + uuid.NewString()
	first := app.CreateURLInput{ID: ids[0], URL: url, IdempotencyKey: uuid.NewString(), DeliveryTarget: "download", Metadata: map[string]string{"comic_name": "First"}}
	a, err := service.CreateURLTask(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID, second.IdempotencyKey, second.Force = ids[1], uuid.NewString(), true
	second.DeliveryTarget = "komga"
	b, err := service.CreateURLTask(ctx, second)
	if err != nil || b.ID != a.ID || !b.Reused {
		t.Fatalf("active force bypassed canonical identity: %+v %v", b, err)
	}
	lookup, err := service.GetSubmission(ctx, second.IdempotencyKey)
	if err != nil || lookup.Task.ID != a.ID {
		t.Fatalf("dedup receipt lost: %v", err)
	}
	first.ID, first.DeliveryTarget = ids[2], "komga"
	_, err = service.CreateURLTask(context.Background(), first)
	if !errors.Is(err, app.ErrIdempotencyConflict) {
		t.Fatalf("changed target reused key: %v", err)
	}
}

func TestSubmissionCannotSilentlyReuseAnotherReviewedDocument(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	firstID, secondID := uuid.NewString(), uuid.NewString()
	cleanupTaskCoreRows(t, ctx, store, firstID, secondID)
	service := app.NewService(store, app.Config{})
	url := "https://telegra.ph/metadata-" + uuid.NewString()
	_, err := service.CreateURLTask(ctx, app.CreateURLInput{ID: firstID, URL: url, Metadata: map[string]string{"comic_name": "Original"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateURLTask(ctx, app.CreateURLInput{ID: secondID, URL: url, IdempotencyKey: uuid.NewString(), Force: true, Metadata: map[string]string{"comic_name": "Reviewed replacement"}})
	if !errors.Is(err, app.ErrSubmissionSourceConflict) {
		t.Fatalf("reviewed metadata discarded: %v", err)
	}
	view, err := service.GetTask(ctx, firstID)
	if err != nil || view.Input.Metadata["comic_name"] != "Original" {
		t.Fatal("changed active task")
	}
	if _, err := service.GetTask(ctx, secondID); !errors.Is(err, app.ErrNotFound) {
		t.Fatal("created duplicate active URL")
	}
}

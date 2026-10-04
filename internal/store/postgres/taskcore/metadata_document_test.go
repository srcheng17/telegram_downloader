package taskcore

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	appmetadata "github.com/ryancheng/telegram-downloader/internal/app/metadata"
	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	metadata "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/metadatadoc"
)

func TestDocumentSnapshotHistoryAndRetryUseOneTransaction(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	ms := appmetadata.NewService(metadatadoc.NewStore(store.pool))
	registry, err := ms.Schema(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// More than the old seven fields, explicit clearing and an array with spaces.
	operations := []metadata.Operation{}
	for key, value := range map[string]any{"title": "集成样例", "series": "示例系列", "number": "特别篇", "summary": "保留段落", "creators.writer": []string{"作者甲"}, "tags": []string{"slice of life"}, "genres": []string{"日常"}, "publisher": "示例出版", "aliases": []string{"别名"}, "language": "zh"} {
		encoded, _ := json.Marshal(value)
		operations = append(operations, metadata.Operation{Op: "set", Key: key, Value: encoded})
	}
	operations = append(operations, metadata.Operation{Op: "clear", Key: "imprint"})
	doc, _, err := metadata.ApplyPatch(metadata.EmptyDocument(registry), registry, 0, operations, metadata.MergeContext{Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	svc := app.NewService(store, app.Config{MetadataNormalizer: ms})
	id := uuid.NewString()
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, "DELETE FROM task_core_tasks WHERE id=$1", id)
		_, _ = store.pool.Exec(ctx, "DELETE FROM metadata_history WHERE task_id=$1", id)
	})
	created, err := svc.CreateURLTask(ctx, app.CreateURLInput{ID: id, URL: "https://telegra.ph/" + id, MetadataDocument: &doc})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*got.Input.MetadataDocument, doc) {
		t.Fatal("document lost data during task roundtrip")
	}
	history, err := postgres.NewUploadTaskStore(store.pool).ListMetadataHistory(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range history {
		if entry.TaskID != nil && *entry.TaskID == id {
			found = true
			if !reflect.DeepEqual(*entry.MetadataDocument, doc) {
				t.Fatal("history differs from task snapshot")
			}
		}
	}
	if !found {
		t.Fatal("transaction did not write history")
	}
	claimed, err := svc.ClaimNext(ctx, "metadata-test")
	if err != nil || claimed.Task == nil {
		t.Fatalf("claim %v", err)
	}
	// The unique task must be the one claimed in this isolated test database.
	if claimed.Task.ID != created.ID {
		t.Fatalf("unexpected competing task %s", claimed.Task.ID)
	}
	if err := svc.Fail(ctx, app.FailInput{TaskID: id, WorkerID: "metadata-test", Attempt: claimed.Task.Attempt, Generation: claimed.Task.Generation, Message: "fixture"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Retry(ctx, id); err != nil {
		t.Fatal(err)
	}
	again, err := store.GetTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*again.Input.MetadataDocument, doc) {
		t.Fatal("retry changed document snapshot")
	}
	mismatched := "different title"
	_, err = svc.CreateURLTask(ctx, app.CreateURLInput{ID: uuid.NewString(), URL: "https://telegra.ph/conflict", MetadataDocument: &doc, Metadata: map[string]string{"comic_name": mismatched}})
	if !errors.Is(err, app.ErrInvalidInput) {
		t.Fatalf("mixed protocol conflict accepted: %v", err)
	}
}

func TestTaskHistoryFailureRollsBackTask(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	// A rollback-only transaction checks the same insert helper used by creation.
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `ALTER TABLE metadata_history ADD CONSTRAINT reject_test_history CHECK (task_type <> 'url') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	title := "Rollback fixture"
	doc, err := metadata.FromLegacy(metadata.Legacy{ComicName: &title})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	_, err = store.insertTask(ctx, tx, app.Task{ID: id, Kind: "url", Status: "READY"}, app.Input{URL: "https://telegra.ph/rollback", MetadataDocument: &doc}, domain.NewProgress(domain.PhasePreparing, 0, 0, domain.UnitNone, ""))
	if err == nil {
		t.Fatal("expected history write failure")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := store.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM task_core_tasks WHERE id=$1)", id).Scan(&exists); err != nil || exists {
		t.Fatalf("partial task survived failed history: %v", err)
	}
}

func TestHistoryKeepsDistinctTasksWithIdenticalLegacyMetadata(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	ms := appmetadata.NewService(metadatadoc.NewStore(store.pool))
	registry, err := ms.Schema(ctx)
	if err != nil {
		t.Fatal(err)
	}
	svc := app.NewService(store, app.Config{MetadataNormalizer: ms})
	ids := []string{uuid.NewString(), uuid.NewString()}
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = store.pool.Exec(ctx, "DELETE FROM task_core_tasks WHERE id=$1", id)
			_, _ = store.pool.Exec(ctx, "DELETE FROM metadata_history WHERE task_id=$1", id)
		}
	})
	for i, id := range ids {
		doc, _, err := metadata.ApplyPatch(metadata.EmptyDocument(registry), registry, 0, []metadata.Operation{
			{Op: "set", Key: "title", Value: json.RawMessage(`"Same title"`)},
			{Op: "set", Key: "publisher", Value: json.RawMessage([]string{`"Publisher A"`, `"Publisher B"`}[i])},
		}, metadata.MergeContext{Now: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.InitUploadTask(ctx, app.InitUploadInput{ID: id, MetadataDocument: &doc})
		if err != nil {
			t.Fatalf("task %d must retain its own snapshot: %v", i, err)
		}
	}
	var count int
	if err := store.pool.QueryRow(ctx, "SELECT count(*) FROM metadata_history WHERE task_id = ANY($1)", ids).Scan(&count); err != nil || count != 2 {
		t.Fatalf("want two independent history snapshots, got %d: %v", count, err)
	}
}

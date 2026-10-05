package taskcore

import (
	"errors"
	"github.com/google/uuid"
	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
	"testing"
	"time"
)

func TestRetentionAndPublicationAreGenerationFenced(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	id := uuid.NewString()
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM task_core_retention WHERE task_id=$1`, id)
		_, _ = store.pool.Exec(ctx, `DELETE FROM metadata_history WHERE task_id=$1`, id)
		_, _ = store.pool.Exec(ctx, `DELETE FROM task_core_tasks WHERE id=$1`, id)
	})
	insertTaskCoreReadyTask(t, ctx, store, id)
	claimed, err := store.ClaimNext(ctx, "retention-worker", time.Minute)
	if err != nil || claimed == nil || claimed.ID != id {
		t.Fatal(claimed, err)
	}
	manifest := domain.RetentionManifest{Version: 1, Files: []domain.RetainedFile{}}
	stale := *claimed
	stale.Generation++
	if err = store.RecordRetention(ctx, stale, manifest); !errors.Is(err, app.ErrConflict) {
		t.Fatal("stale retention accepted", err)
	}
	complete := app.CompleteInput{TaskID: id, WorkerID: claimed.LeaseOwner, Attempt: claimed.Attempt, Generation: claimed.Generation, ArtifactPath: "/synthetic/output.cbz", ArtifactName: "output.cbz", ArtifactSize: 1, RetentionManifest: &manifest}
	if err = store.Complete(ctx, complete); !errors.Is(err, app.ErrConflict) {
		t.Fatal("unregistered retention published", err)
	}
	if err = store.RecordRetention(ctx, *claimed, manifest); err != nil {
		t.Fatal(err)
	}
	if err = store.Complete(ctx, complete); err != nil {
		t.Fatal(err)
	}
	view, err := store.GetTask(ctx, id)
	if err != nil || view.Result == nil || view.Result.RetentionManifest == nil {
		t.Fatal("manifest missing from result", err)
	}
	if err = store.RecordRetention(ctx, *claimed, manifest); !errors.Is(err, app.ErrConflict) {
		t.Fatal("terminal retention changed", err)
	}
}

package taskcore

import (
	"errors"
	"github.com/google/uuid"
	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/app/telegram"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
	"strings"
	"testing"
)

func TestTelegramSnapshotDedupAndRetry(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	svc := app.NewService(store, app.Config{})
	source := telegram.Input{MessageURL: "https://t.me/example_channel/123", AccountRevision: 1, AccountIdentity: strings.Repeat("a", 64)}
	create := func(input telegram.Input) (app.CreateURLResult, error) {
		id := uuid.NewString()
		t.Cleanup(func() { _, _ = store.pool.Exec(ctx, `DELETE FROM task_core_tasks WHERE id=$1`, id) })
		return svc.CreateTelegramTask(ctx, app.CreateTelegramInput{ID: id, Source: input})
	}
	first, err := create(source)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := create(source)
	if err != nil || !duplicate.Reused || duplicate.ID != first.ID {
		t.Fatal("active telegram not reused", duplicate, err)
	}
	view, err := store.GetTask(ctx, first.ID)
	if err != nil || view.Input.Telegram == nil || *view.Input.Telegram != source || !view.Input.HasSource(domain.KindTelegram) {
		t.Fatal("lost source snapshot", err)
	}
	source.AccountRevision = 2
	next, err := create(source)
	if err != nil || next.Reused {
		t.Fatal("different account revision wrongly reused", err)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE task_core_tasks SET status='FAILED' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Retry(ctx, first.ID); err != nil {
		t.Fatal("new account should not conflict with old immutable source", err)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE task_core_tasks SET status='FAILED' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	source.AccountRevision = 1
	if _, err = create(source); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Retry(ctx, first.ID); !errors.Is(err, app.ErrConflict) {
		t.Fatal("retry failed to deduplicate active telegram input", err)
	}
}

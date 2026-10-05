package extractionrules_test

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/extractionrules"
	metadata "github.com/ryancheng/telegram-downloader/internal/app/metadata"
	store "github.com/ryancheng/telegram-downloader/internal/store/postgres/extractionrules"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/metadatadoc"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/migrations"
	"os"
	"sync"
	"testing"
)

func TestVersionedRulesPostgres(t *testing.T) {
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
	if err = migrations.Run(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM extraction_rules`); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM extraction_rules`)
	meta := metadata.NewService(metadatadoc.NewStore(pool))
	schema, err := meta.Schema(ctx)
	if err != nil {
		t.Fatal(err)
	}
	svc := app.NewService(store.New(pool), meta)
	initial, err := svc.Get(ctx)
	if err != nil || initial.Rules == nil || initial.RulesVersion != 0 {
		t.Fatal(initial, err)
	}
	zero := uint64(0)
	input := app.Update{ExpectedVersion: &zero, DefinitionsVersion: schema.DefinitionsVersion, Rules: []app.Rule{{ID: "title", TargetKey: "title", Labels: []string{"标题"}, Mode: "label_value"}}}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := svc.Save(ctx, input); results <- e }()
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for e := range results {
		if e == nil {
			wins++
		} else if errors.Is(e, app.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("rule CAS did not serialize writers")
	}
	read, err := svc.Get(ctx)
	if err != nil || read.RulesVersion != 1 || read.Rules[0].Labels[0] != "标题" {
		t.Fatal(read, err)
	}
	input.DefinitionsVersion = "stale"
	if _, err = svc.Save(ctx, input); !errors.Is(err, app.ErrConflict) {
		t.Fatal("stale schema accepted")
	}
}

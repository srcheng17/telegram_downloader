package metadatadoc

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/metadata"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/migrations"
)

func TestImmutableRegistryPersistenceAndCAS(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required")
		}
		t.Skip("TEST_DATABASE_URL not configured")
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
	if _, err := pool.Exec(ctx, "TRUNCATE metadata_definition_head, metadata_definition_versions"); err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	base, err := store.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	custom := domain.FieldDefinition{Key: "custom.user.note", Label: "测试备注", Type: "string", MaxBytes: 100, Editable: true, Enabled: true, ExportStatus: "internal_only", Extractable: []string{"rule"}}
	a, err := domain.NewRegistry(base, []domain.FieldDefinition{custom})
	if err != nil {
		t.Fatal(err)
	}
	custom.Label = "并发备注"
	b, err := domain.NewRegistry(base, []domain.FieldDefinition{custom})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, next := range []domain.Registry{a, b} {
		wg.Add(1)
		go func(r domain.Registry) { defer wg.Done(); results <- store.Save(ctx, base.DefinitionsVersion, r) }(next)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, domain.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	// New repository/service instances must read the persisted current and old versions.
	restarted := NewStore(pool)
	current, err := restarted.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current.DefinitionsVersion == base.DefinitionsVersion {
		t.Fatal("version not persisted")
	}
	old, err := restarted.Get(ctx, base.DefinitionsVersion)
	if err != nil {
		t.Fatal(err)
	}
	if len(old.CustomDefinitions()) != 0 {
		t.Fatal("old version mutated")
	}
	service := app.NewService(restarted)
	doc := domain.EmptyDocument(current)
	result, err := service.Patch(ctx, app.PatchInput{Document: doc, Operations: []domain.Operation{
		{Op: "set", Key: custom.Key, Value: []byte(`"持久保存"`)},
		{Op: "set", Key: "aliases", Value: []byte(`[]`)},
		{Op: "set", Key: "identifiers", Value: []byte(`[]`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := service.Encode(ctx, result.Document)
	if err != nil {
		t.Fatal(err)
	}
	var jsonb []byte
	if err := pool.QueryRow(ctx, `SELECT $1::jsonb`, encoded).Scan(&jsonb); err != nil {
		t.Fatal(err)
	}
	restored, err := service.Decode(ctx, jsonb)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Fields[custom.Key].ManualLocked || restored.DefinitionsVersion != current.DefinitionsVersion {
		t.Fatal("snapshot lost")
	}
	for _, key := range []string{"aliases", "identifiers"} {
		field := restored.Fields[key]
		if field.State != "value" || string(field.Value) != "[]" || !field.ManualLocked {
			t.Fatalf("empty %s list lost during JSONB roundtrip", key)
		}
	}
	defs := current.CustomDefinitions()
	defs[0].Enabled = false
	disabled, err := service.UpdateFields(ctx, app.UpdateFieldsInput{ExpectedDefinitionsVersion: current.DefinitionsVersion, Definitions: defs})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.DefinitionsVersion == current.DefinitionsVersion {
		t.Fatal("disable did not version")
	}
	if _, err := service.Decode(ctx, encoded); err != nil {
		t.Fatal("old document reinterpreted", err)
	}
	forged := current
	forged.DefinitionsVersion = "forged"
	if err := store.Save(ctx, disabled.DefinitionsVersion, forged); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("accepted forged registry %v", err)
	}
}

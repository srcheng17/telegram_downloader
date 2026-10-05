package metadata

import (
	"context"
	"errors"
	"testing"

	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type fakeRepo struct {
	current  domain.Registry
	versions map[string]domain.Registry
}

func newFakeRepo() *fakeRepo {
	r := domain.StandardRegistry()
	return &fakeRepo{r, map[string]domain.Registry{r.DefinitionsVersion: r}}
}
func (f *fakeRepo) Current(context.Context) (domain.Registry, error) { return f.current, nil }
func (f *fakeRepo) Get(_ context.Context, v string) (domain.Registry, error) {
	r, ok := f.versions[v]
	if !ok {
		return r, domain.ErrUnsupportedVersion
	}
	return r, nil
}
func (f *fakeRepo) Save(_ context.Context, expected string, r domain.Registry) error {
	if expected != f.current.DefinitionsVersion {
		return domain.ErrConflict
	}
	f.current = r
	f.versions[r.DefinitionsVersion] = r
	return nil
}

func TestImmutableDefinitionsAndOldDocument(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepo()
	service := NewService(repo)
	old := domain.EmptyDocument(repo.current)
	def := domain.FieldDefinition{Key: "custom.user.note", Label: "自定备注", Type: "string", MaxBytes: 200, Editable: true, Enabled: true, ExportStatus: "internal_only", Extractable: []string{"rule"}}
	first, err := service.UpdateFields(ctx, UpdateFieldsInput{ExpectedDefinitionsVersion: old.DefinitionsVersion, Definitions: []domain.FieldDefinition{def}})
	if err != nil {
		t.Fatal(err)
	}
	def.Enabled = false
	if _, err = service.UpdateFields(ctx, UpdateFieldsInput{ExpectedDefinitionsVersion: first.DefinitionsVersion, Definitions: []domain.FieldDefinition{def}}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Validate(ctx, old); err != nil {
		t.Fatal("old version invalidated", err)
	}
	if _, err = service.UpdateFields(ctx, UpdateFieldsInput{ExpectedDefinitionsVersion: old.DefinitionsVersion, Definitions: []domain.FieldDefinition{def}}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want conflict: %v", err)
	}
	if oldReg, err := repo.Get(ctx, first.DefinitionsVersion); err != nil || !oldReg.Definitions[def.Key].Enabled {
		t.Fatal("old definitions mutated")
	}
}

func TestNormalizeMixedInput(t *testing.T) {
	service := NewService(newFakeRepo())
	title := "原始标题"
	doc, _, err := service.NormalizeInput(context.Background(), nil, domain.Legacy{ComicName: &title})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.NormalizeInput(context.Background(), &doc, domain.Legacy{ComicName: &title}); err != nil {
		t.Fatal(err)
	}
	title = "不同标题"
	if _, _, err = service.NormalizeInput(context.Background(), &doc, domain.Legacy{ComicName: &title}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("accepted conflict: %v", err)
	}
}

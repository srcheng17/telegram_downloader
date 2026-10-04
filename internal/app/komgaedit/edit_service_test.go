package komgaedit

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/archive/cbzedit"
	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"github.com/ryancheng/telegram-downloader/internal/infra/komga"
)

type staticRegistry struct{ registry metadata.Registry }

func (r staticRegistry) Schema(context.Context) (metadata.Registry, error) { return r.registry, nil }

type editFakeGateway struct {
	mu          sync.Mutex
	book        komga.Book
	library     komga.Library
	analyzeErr  error
	analyzeCall int
	clearCalls  [][]komga.ClearField
	onAnalyze   func(*komga.Book)
}

func (f *editFakeGateway) Libraries(context.Context) ([]komga.Library, error) {
	return []komga.Library{f.library}, nil
}
func (f *editFakeGateway) Library(_ context.Context, id string) (komga.Library, error) {
	if id != f.library.ID {
		return komga.Library{}, ErrNotFound
	}
	return f.library, nil
}
func (f *editFakeGateway) Books(context.Context, komga.ListBooksOptions) (komga.BookPage, error) {
	return komga.BookPage{Content: []komga.Book{f.book}, Size: 1}, nil
}
func (f *editFakeGateway) Book(_ context.Context, id string) (komga.Book, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != f.book.ID {
		return komga.Book{}, ErrNotFound
	}
	return f.book, nil
}
func (f *editFakeGateway) AnalyzeBook(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.analyzeCall++
	if f.analyzeErr != nil {
		return f.analyzeErr
	}
	if f.onAnalyze != nil {
		f.onAnalyze(&f.book)
	}
	return nil
}
func (f *editFakeGateway) ClearBookMetadata(_ context.Context, _ string, fields ...komga.ClearField) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearCalls = append(f.clearCalls, append([]komga.ClearField(nil), fields...))
	for _, field := range fields {
		switch field {
		case komga.ClearSummary:
			f.book.Metadata.Summary = ""
		case komga.ClearReleaseDate:
			f.book.Metadata.ReleaseDate = nil
		case komga.ClearAuthors:
			f.book.Metadata.Authors = nil
		case komga.ClearTags:
			f.book.Metadata.Tags = nil
		case komga.ClearISBN:
			f.book.Metadata.ISBN = ""
		case komga.ClearLinks:
			f.book.Metadata.Links = nil
		}
	}
	return nil
}

type memoryOperationRepo struct {
	mu     sync.Mutex
	lockMu sync.Mutex
	byID   map[string]Operation
	byKey  map[string]string
}

func newMemoryOperationRepo() *memoryOperationRepo {
	return &memoryOperationRepo{byID: map[string]Operation{}, byKey: map[string]string{}}
}
func (r *memoryOperationRepo) Create(_ context.Context, op Operation) (Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byKey[op.IdempotencyKey] != "" {
		return Operation{}, ErrConflict
	}
	for _, existing := range r.byID {
		if existing.LibraryID == op.LibraryID && existing.RelativePath == op.RelativePath && existing.State != StateCurrentValueConsistent && existing.State != StateRestored && existing.State != StateAborted {
			return Operation{}, ErrConflict
		}
	}
	op.Version = 1
	r.byID[op.ID] = op
	r.byKey[op.IdempotencyKey] = op.ID
	return op, nil
}
func (r *memoryOperationRepo) Get(_ context.Context, id string) (Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.byID[id]
	if !ok {
		return Operation{}, ErrNotFound
	}
	return op, nil
}
func (r *memoryOperationRepo) GetByKey(_ context.Context, key string) (Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.byKey[key]
	if id == "" {
		return Operation{}, ErrNotFound
	}
	return r.byID[id], nil
}
func (r *memoryOperationRepo) Update(_ context.Context, op Operation) (Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byID[op.ID].Version != op.Version {
		return Operation{}, ErrConflict
	}
	op.Version++
	r.byID[op.ID] = op
	return op, nil
}
func (r *memoryOperationRepo) ListRecoverable(context.Context, int) ([]Operation, error) {
	return nil, nil
}
func (r *memoryOperationRepo) WithBookLock(ctx context.Context, _ string, fn func(context.Context) error) error {
	r.lockMu.Lock()
	defer r.lockMu.Unlock()
	return fn(ctx)
}

func makeEditFixture(t *testing.T, xml string) (*EditService, *editFakeGateway, string) {
	t.Helper()
	libraryRoot := t.TempDir()
	privateParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backupRoot := filepath.Join(privateParent, "private")
	if err := os.Mkdir(backupRoot, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(libraryRoot, "book.cbz")
	handle, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(handle)
	for _, member := range []struct{ name, contents string }{{"001.jpg", "page 1"}, {"ComicInfo.xml", xml}} {
		entry, err := writer.Create(member.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(member.contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	fake := &editFakeGateway{library: komga.Library{ID: "lib", Root: "/books", ImportComicInfoBook: true},
		book: komga.Book{ID: "book", LibraryID: "lib", URL: "file:/books/book.cbz", Media: komga.Media{Status: "READY"},
			Metadata: komga.BookMetadata{Title: "Old", Summary: "Old summary", LastModified: "v1"}}}
	vault, err := credentials.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	connection := NewConnectionService(&memoryConnectionRepo{}, vault, true)
	zero := int64(0)
	base := "http://localhost:25600"
	if _, err := connection.Save(context.Background(), ConnectionUpdate{ExpectedVersion: &zero, BaseURL: &base, Credential: CredentialChange{Action: "replace", Value: "test-key"}}); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalogService(connection, []config.KomgaLibraryMapping{{LibraryID: "lib", KomgaRoot: "/books", LocalRoot: libraryRoot}}, nil)
	catalog.factory = func(string, credentials.Secret) (catalogGateway, error) { return fake, nil }
	service, err := NewEditService(catalog, newMemoryOperationRepo(), staticRegistry{metadata.StandardRegistry()}, backupRoot)
	if err != nil {
		t.Fatal(err)
	}
	service.editFactory = func(string, credentials.Secret) (editGateway, error) { return fake, nil }
	return service, fake, file
}

func TestEditSaveWritesFileThenVerifiesObservedKomgaValue(t *testing.T) {
	service, fake, file := makeEditFixture(t, `<ComicInfo><Title>Old</Title><Summary>Old summary</Summary></ComicInfo>`)
	fake.onAnalyze = func(book *komga.Book) { book.Metadata.Title = "New"; book.Metadata.LastModified = "v2" }
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil || !detail.CanSave || detail.SourceVersion == "" || detail.Document.Fields["title"].State != "value" {
		t.Fatalf("detail: %#v, %v", detail, err)
	}
	value, _ := json.Marshal("New")
	request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion,
		Changes: []FieldChange{{Key: "title", State: "value", Value: value}}}
	preview, err := service.Preview(ctx, "book", request)
	if err != nil || !preview.CanSave || !preview.FileChanged || len(preview.Diffs) != 1 {
		t.Fatalf("preview: %#v, %v", preview, err)
	}
	saved, err := service.Save(ctx, "book", SaveRequest{PreviewRequest: request, PreviewToken: preview.PreviewToken, IdempotencyKey: "edit-unique-key-0001"})
	if err != nil || !saved.FileCommitted || !saved.ProjectionConsistent || !saved.AnalyzeVerified || saved.State != StateCurrentValueConsistent {
		t.Fatalf("save: %#v, %v", saved, err)
	}
	original, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	inspected, err := cbzedit.Inspect(ctx, original, filepath.Base(file), cbzedit.Limits{})
	if err != nil || string(inspected.ComicInfo) == "" || inspected.SHA256 == detail.SourceVersion {
		t.Fatalf("CBZ not rewritten: %#v, %v", inspected, err)
	}
	duplicate, err := service.Save(ctx, "book", SaveRequest{PreviewRequest: request, IdempotencyKey: "edit-unique-key-0001"})
	if err != nil || duplicate.ID != saved.ID || fake.analyzeCall != 1 {
		t.Fatalf("idempotency failed: %#v, %v, analyze=%d", duplicate, err, fake.analyzeCall)
	}
}

func TestClearRequiresFileCommitAndPatchNeverProvesAnalyze(t *testing.T) {
	service, fake, _ := makeEditFixture(t, `<ComicInfo><Title>Old</Title><Summary>Old summary</Summary></ComicInfo>`)
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil {
		t.Fatal(err)
	}
	request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion,
		Changes: []FieldChange{{Key: "summary", State: "cleared"}}}
	preview, err := service.Preview(ctx, "book", request)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.Save(ctx, "book", SaveRequest{PreviewRequest: request, PreviewToken: preview.PreviewToken, IdempotencyKey: "edit-unique-key-0002"})
	if err != nil || !saved.FileCommitted || !saved.ProjectionConsistent || saved.AnalyzeVerified || len(fake.clearCalls) != 1 || fake.clearCalls[0][0] != komga.ClearSummary {
		t.Fatalf("clear result: %#v, %v, PATCH=%#v", saved, err, fake.clearCalls)
	}
}

func TestSyncFailureRetainsFileAndRetriesWithoutRewrite(t *testing.T) {
	service, fake, file := makeEditFixture(t, `<ComicInfo><Title>Old</Title></ComicInfo>`)
	fake.analyzeErr = errors.New("offline")
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil {
		t.Fatal(err)
	}
	value, _ := json.Marshal("New")
	request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion,
		Changes: []FieldChange{{Key: "title", State: "value", Value: value}}}
	preview, err := service.Preview(ctx, "book", request)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.Save(ctx, "book", SaveRequest{PreviewRequest: request, PreviewToken: preview.PreviewToken, IdempotencyKey: "edit-unique-key-0003"})
	if err != nil || !saved.FileCommitted || saved.ProjectionConsistent || saved.State != StateSyncFailed {
		t.Fatalf("partial save: %#v, %v", saved, err)
	}
	contentBefore, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	fake.analyzeErr = nil
	fake.onAnalyze = func(book *komga.Book) { book.Metadata.Title = "New"; book.Metadata.LastModified = "v2" }
	retried, err := service.RetrySync(ctx, saved.ID)
	if err != nil || !retried.ProjectionConsistent || !retried.FileCommitted {
		t.Fatalf("retry: %#v, %v", retried, err)
	}
	contentAfter, err := os.ReadFile(file)
	if err != nil || string(contentBefore) != string(contentAfter) {
		t.Fatal("retry rewrote CBZ")
	}
}

func TestPreviewRejectsCompetingKomgaMetadataBeforeFileWrite(t *testing.T) {
	service, fake, file := makeEditFixture(t, `<ComicInfo><Title>Old</Title></ComicInfo>`)
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil {
		t.Fatal(err)
	}
	request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion,
		Changes: []FieldChange{{Key: "title", State: "value", Value: json.RawMessage(`"New"`)}}}
	preview, err := service.Preview(ctx, "book", request)
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.book.Metadata.Summary = "changed in Komga"
	fake.book.Metadata.LastModified = "v2"
	fake.mu.Unlock()
	if _, err := service.Save(ctx, "book", SaveRequest{PreviewRequest: request, PreviewToken: preview.PreviewToken, IdempotencyKey: "edit-unique-key-0004"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("competing metadata accepted: %v", err)
	}
	root, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	current, err := cbzedit.Inspect(ctx, root, filepath.Base(file), cbzedit.Limits{})
	if err != nil || current.SHA256 != detail.SourceVersion {
		t.Fatalf("conflicted preview changed file: %#v, %v", current, err)
	}
}

func TestRenameBeforeDatabaseUpdateIsReconciledWithoutSecondWrite(t *testing.T) {
	service, fake, file := makeEditFixture(t, `<ComicInfo><Title>Old</Title></ComicInfo>`)
	fake.onAnalyze = func(book *komga.Book) { book.Metadata.Title = "New"; book.Metadata.LastModified = "v2" }
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil {
		t.Fatal(err)
	}
	request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion,
		Changes: []FieldChange{{Key: "title", State: "value", Value: json.RawMessage(`"New"`)}}}
	preview, err := service.Preview(ctx, "book", request)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.Save(ctx, "book", SaveRequest{PreviewRequest: request, PreviewToken: preview.PreviewToken, IdempotencyKey: "edit-unique-key-0005"})
	if err != nil {
		t.Fatal(err)
	}
	contentBefore, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	repo := service.operations.(*memoryOperationRepo)
	op, err := repo.Get(ctx, saved.ID)
	if err != nil || op.Prepared == nil {
		t.Fatalf("missing persisted preparation: %#v, %v", op, err)
	}
	op.State = StatePrepared
	op.FileCommitted = false
	op.ProjectionConsistent = false
	op.AnalyzeVerified = false
	if _, err := repo.Update(ctx, op); err != nil {
		t.Fatal(err)
	}
	reconciled, err := service.Operation(ctx, saved.ID)
	if err != nil || !reconciled.FileCommitted || reconciled.State != StateFileCommitted {
		t.Fatalf("rename recovery: %#v, %v", reconciled, err)
	}
	contentAfter, err := os.ReadFile(file)
	if err != nil || string(contentBefore) != string(contentAfter) || fake.analyzeCall != 1 {
		t.Fatal("reconcile rewrote the book or repeated analyze")
	}
}

func TestBarcodeISBNAndUnknownFieldCannotEnterSave(t *testing.T) {
	service, fake, _ := makeEditFixture(t, `<ComicInfo><Title>Old</Title><GTIN>9780306406157</GTIN></ComicInfo>`)
	fake.library.ImportBarcodeIsbn = true
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range detail.Fields {
		if field.Key == "identifiers" && field.CanClear {
			t.Fatal("barcode import library offers ISBN clear")
		}
	}
	request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion,
		Changes: []FieldChange{{Key: "identifiers", State: "cleared"}}}
	if _, err := service.Preview(ctx, "book", request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ISBN clear accepted: %v", err)
	}
	request.Changes = []FieldChange{{Key: "custom.user.secret", State: "value", Value: json.RawMessage(`"hidden"`)}}
	if _, err := service.Preview(ctx, "book", request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unmapped field accepted: %v", err)
	}
}

func TestSeriesScopeFieldsAreReadOnlyAndForgedPreviewIsRejected(t *testing.T) {
	service, fake, _ := makeEditFixture(t, `<ComicInfo><Title>Old</Title><Series>Original series</Series></ComicInfo>`)
	fake.library.ImportComicInfoSeries = true
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil || !detail.CanSave {
		t.Fatalf("detail: %#v, %v", detail, err)
	}
	for key, value := range map[string]json.RawMessage{
		"series": json.RawMessage(`"Forged"`), "publisher": json.RawMessage(`"Forged"`),
		"genres": json.RawMessage(`["Action"]`), "manga": json.RawMessage(`"yes"`),
		"reading_direction": json.RawMessage(`"rtl"`),
	} {
		found := false
		for _, field := range detail.Fields {
			if field.Key != key {
				continue
			}
			found = true
			if field.CanSet || field.CanClear || field.Reason != "series_scope" {
				t.Fatalf("series-scope field %q became editable: %#v", key, field)
			}
		}
		if !found {
			t.Fatalf("missing standard field %q", key)
		}
		request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion,
			Changes: []FieldChange{{Key: key, State: "value", Value: value}}}
		if _, err := service.Preview(ctx, "book", request); !errors.Is(err, ErrInvalid) {
			t.Fatalf("forged series-scope field %q accepted: %v", key, err)
		}
	}
}

func TestExplicitRestorePreservesOperationAndRequiresCurrentTarget(t *testing.T) {
	service, fake, file := makeEditFixture(t, `<ComicInfo><Title>Old</Title></ComicInfo>`)
	fake.onAnalyze = func(book *komga.Book) { book.Metadata.Title = "New"; book.Metadata.LastModified = "v2" }
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil {
		t.Fatal(err)
	}
	request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion,
		Changes: []FieldChange{{Key: "title", State: "value", Value: json.RawMessage(`"New"`)}}}
	preview, err := service.Preview(ctx, "book", request)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.Save(ctx, "book", SaveRequest{PreviewRequest: request, PreviewToken: preview.PreviewToken, IdempotencyKey: "edit-unique-key-0006"})
	if err != nil || !saved.FileCommitted {
		t.Fatalf("save: %#v, %v", saved, err)
	}
	if !slices.Contains(saved.AvailableActions, "restore") {
		t.Fatal("verified committed edit did not offer explicit restore")
	}
	restored, err := service.Restore(ctx, saved.ID)
	if err != nil || restored.State != StateRestored || !restored.FileRestored || restored.FileCommitted || restored.ProjectionConsistent || restored.AnalyzeVerified {
		t.Fatalf("restore: %#v, %v", restored, err)
	}
	root, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	current, err := cbzedit.Inspect(ctx, root, filepath.Base(file), cbzedit.Limits{})
	if err != nil || current.SHA256 != detail.SourceVersion {
		t.Fatalf("original not restored: %#v, %v", current, err)
	}
	second, err := service.Restore(ctx, saved.ID)
	if err != nil || second.State != StateRestored || fake.analyzeCall != 2 {
		t.Fatalf("duplicate restore: %#v, %v, analyzes=%d", second, err, fake.analyzeCall)
	}
}

func TestPreparingRecordRecoversDurablePreparedDescriptor(t *testing.T) {
	service, fake, file := makeEditFixture(t, `<ComicInfo><Title>Old</Title></ComicInfo>`)
	fake.onAnalyze = func(book *komga.Book) { book.Metadata.Title = "New"; book.Metadata.LastModified = "v2" }
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil {
		t.Fatal(err)
	}
	request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion,
		Changes: []FieldChange{{Key: "title", State: "value", Value: json.RawMessage(`"New"`)}}}
	preview, err := service.Preview(ctx, "book", request)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.Save(ctx, "book", SaveRequest{PreviewRequest: request, PreviewToken: preview.PreviewToken, IdempotencyKey: "edit-unique-key-0007"})
	if err != nil {
		t.Fatal(err)
	}
	contentBefore, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	repo := service.operations.(*memoryOperationRepo)
	op, err := repo.Get(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	op.State = StatePreparing
	op.Prepared = nil
	op.TargetSHA256 = op.SourceSHA256
	op.FileCommitted = false
	op.ProjectionConsistent = false
	op.AnalyzeVerified = false
	if _, err := repo.Update(ctx, op); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.Operation(ctx, saved.ID)
	if err != nil || !recovered.FileCommitted || recovered.State != StateFileCommitted {
		t.Fatalf("manifest recovery: %#v, %v", recovered, err)
	}
	contentAfter, err := os.ReadFile(file)
	if err != nil || string(contentBefore) != string(contentAfter) || fake.analyzeCall != 1 {
		t.Fatal("recovery rewrote CBZ or repeated analyze")
	}
}

func TestUnchangedInvalidWebIsPreservedWhileEditingAnotherField(t *testing.T) {
	service, fake, file := makeEditFixture(t, `<ComicInfo><Title>Old</Title><Web>not-a-public-url</Web></ComicInfo>`)
	fake.onAnalyze = func(book *komga.Book) { book.Metadata.Title = "New"; book.Metadata.LastModified = "v2" }
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil || !detail.CanSave {
		t.Fatalf("valid but non-importable source became read-only: %#v, %v", detail, err)
	}
	request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion,
		Changes: []FieldChange{{Key: "title", State: "value", Value: json.RawMessage(`"New"`)}}}
	preview, err := service.Preview(ctx, "book", request)
	if err != nil || !preview.CanSave || len(preview.Warnings) == 0 {
		t.Fatalf("preview omitted source warning: %#v, %v", preview, err)
	}
	saved, err := service.Save(ctx, "book", SaveRequest{PreviewRequest: request, PreviewToken: preview.PreviewToken, IdempotencyKey: "edit-unique-key-0008"})
	if err != nil || !saved.FileCommitted {
		t.Fatalf("safe edit rejected: %#v, %v", saved, err)
	}
	root, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	current, err := cbzedit.Inspect(ctx, root, filepath.Base(file), cbzedit.Limits{})
	if err != nil || !bytes.Contains(current.ComicInfo, []byte(`<Web>not-a-public-url</Web>`)) {
		t.Fatalf("unchanged source Web lost: %s, %v", current.ComicInfo, err)
	}
}

func TestClearRefusesCompetingKomgaValueAfterAnalyze(t *testing.T) {
	service, fake, _ := makeEditFixture(t, `<ComicInfo><Title>Old</Title><Summary>Old summary</Summary></ComicInfo>`)
	fake.onAnalyze = func(book *komga.Book) { book.Metadata.Summary = "external edit"; book.Metadata.LastModified = "v2" }
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil {
		t.Fatal(err)
	}
	request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion,
		Changes: []FieldChange{{Key: "summary", State: "cleared"}}}
	preview, err := service.Preview(ctx, "book", request)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.Save(ctx, "book", SaveRequest{PreviewRequest: request, PreviewToken: preview.PreviewToken, IdempotencyKey: "edit-unique-key-0009"})
	if err != nil || !saved.FileCommitted || saved.State != StateSyncFailed || saved.ProjectionConsistent || len(fake.clearCalls) != 0 || fake.book.Metadata.Summary != "external edit" {
		t.Fatalf("concurrent Komga value overwritten: %#v, %v, PATCH=%#v", saved, err, fake.clearCalls)
	}
}

func TestCompletedEditDoesNotBlockLaterExternalRevision(t *testing.T) {
	service, fake, file := makeEditFixture(t, `<ComicInfo><Title>Old</Title></ComicInfo>`)
	fake.onAnalyze = func(book *komga.Book) { book.Metadata.Title = "New"; book.Metadata.LastModified = "v2" }
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil {
		t.Fatal(err)
	}
	request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion,
		Changes: []FieldChange{{Key: "title", State: "value", Value: json.RawMessage(`"New"`)}}}
	preview, err := service.Preview(ctx, "book", request)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.Save(ctx, "book", SaveRequest{PreviewRequest: request, PreviewToken: preview.PreviewToken, IdempotencyKey: "edit-unique-key-0010"})
	if err != nil || saved.State != StateCurrentValueConsistent {
		t.Fatalf("initial save: %#v, %v", saved, err)
	}
	other, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(other)
	for _, part := range []struct{ name, text string }{{"001.jpg", "page 1"}, {"ComicInfo.xml", `<ComicInfo><Title>Third</Title></ComicInfo>`}} {
		entry, err := writer.Create(part.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(part.text)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	view, err := service.Operation(ctx, saved.ID)
	if err != nil || view.State != StateCurrentValueConsistent {
		t.Fatalf("historical operation became active conflict: %#v, %v", view, err)
	}
	if _, err := service.Restore(ctx, saved.ID); !errors.Is(err, cbzedit.ErrConflict) {
		t.Fatalf("restore overwrote later revision: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil || !bytes.Contains(data, []byte("Third")) {
		t.Fatal("later revision was lost")
	}
}

func TestCompletedEditDoesNotReactivateAfterBookMoves(t *testing.T) {
	service, fake, file := makeEditFixture(t, `<ComicInfo><Title>Old</Title></ComicInfo>`)
	detail, err := service.Detail(context.Background(), "book")
	if err != nil {
		t.Fatal(err)
	}
	repo := service.operations.(*memoryOperationRepo)
	op, err := repo.Create(context.Background(), Operation{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		IdempotencyKey: "historical-moved-book", BookID: "book", LibraryID: "lib",
		RelativePath: "book.cbz", SourceSHA256: detail.SourceVersion,
		TargetSHA256: detail.SourceVersion, State: StateCurrentValueConsistent,
		FileNoChange: true, ProjectionConsistent: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file, filepath.Join(filepath.Dir(file), "moved.cbz")); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.book.URL = "file:/books/moved.cbz"
	fake.mu.Unlock()
	view, err := service.Operation(context.Background(), op.ID)
	if err != nil || view.State != StateCurrentValueConsistent {
		t.Fatalf("moved historical edit became active: %#v, %v", view, err)
	}
	stored, err := repo.Get(context.Background(), op.ID)
	if err != nil || stored.State != StateCurrentValueConsistent {
		t.Fatalf("moved historical edit changed in storage: %#v, %v", stored, err)
	}
}

func TestPageCountCorrectionNeedsSeparateConfirmation(t *testing.T) {
	service, _, file := makeEditFixture(t, `<ComicInfo><Title>Old</Title><PageCount>9</PageCount></ComicInfo>`)
	ctx := context.Background()
	detail, err := service.Detail(ctx, "book")
	if err != nil || !detail.PageCountCorrectionNeeded || detail.PageCount != 1 {
		t.Fatalf("missing page-count warning: %#v, %v", detail, err)
	}
	request := PreviewRequest{SourceVersion: detail.SourceVersion, DefinitionsVersion: detail.Schema.DefinitionsVersion, Changes: []FieldChange{}}
	if _, err := service.Preview(ctx, "book", request); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("uncorrected page count accepted: %v", err)
	}
	request.CorrectPageCount = true
	preview, err := service.Preview(ctx, "book", request)
	if err != nil || !preview.FileChanged || len(preview.Diffs) != 1 || preview.Diffs[0].Key != "page_count" || preview.Diffs[0].Action != "corrected" {
		t.Fatalf("correction preview: %#v, %v", preview, err)
	}
	saved, err := service.Save(ctx, "book", SaveRequest{PreviewRequest: request, PreviewToken: preview.PreviewToken, IdempotencyKey: "edit-unique-key-0011"})
	if err != nil || !saved.FileCommitted || saved.AnalyzeVerified {
		t.Fatalf("correction save: %#v, %v", saved, err)
	}
	root, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	current, err := cbzedit.Inspect(ctx, root, filepath.Base(file), cbzedit.Limits{})
	if err != nil || !bytes.Contains(current.ComicInfo, []byte(`<PageCount>1</PageCount>`)) {
		t.Fatalf("page count not corrected: %s, %v", current.ComicInfo, err)
	}
}

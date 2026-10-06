package komgaedit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/app/tasks"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/infra/komga"
)

type deliveryFake struct {
	*editFakeGateway
	scanned bool
	scans   int
	scanErr error
}

func (f *deliveryFake) Books(_ context.Context, o komga.ListBooksOptions) (komga.BookPage, error) {
	if !f.scanned {
		return komga.BookPage{Content: []komga.Book{}, Size: o.Size}, nil
	}
	return komga.BookPage{Content: []komga.Book{f.book}, Size: o.Size, TotalPages: 1}, nil
}
func (f *deliveryFake) ScanLibrary(_ context.Context, id string) error {
	if id != f.library.ID {
		return ErrInvalid
	}
	f.scans++
	if f.scanErr != nil {
		return f.scanErr
	}
	f.scanned = true
	return nil
}
func deliveryFixture(t *testing.T) (*DeliveryService, *deliveryFake, string, string) {
	t.Helper()
	editor, gateway, source := makeEditFixture(t, `<ComicInfo><Title>Delivered title</Title><Summary>Reviewed summary</Summary><Writer>Sample Writer</Writer><PageCount>1</PageCount></ComicInfo>`)
	fake := &deliveryFake{editFakeGateway: gateway}
	fake.book.URL = "/books/tankobon/delivered.cbz"
	fake.book.Metadata = komga.BookMetadata{Title: "Delivered title", Summary: "Reviewed summary", Authors: []komga.Author{{Name: "Sample Writer", Role: "writer"}}}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	fake.book.SizeBytes, fake.book.Media.PagesCount = info.Size(), 1
	editor.catalog.factory = func(string, credentials.Secret) (catalogGateway, error) { return fake, nil }
	root := filepath.Dir(source)
	return NewDeliveryService(editor.catalog, root), fake, source, filepath.Join(root, "tankobon", "delivered.cbz")
}
func TestDeliveryScansAndReadsExactBookAndMetadataThenResumes(t *testing.T) {
	service, fake, source, target := deliveryFixture(t)
	result, err := service.Deliver(context.Background(), source, "delivered.cbz", "")
	if err != nil || result.Status != "indexed" || result.Indexed != "verified" || result.BookID != "book" || result.LibraryID != "lib" || !result.Copied || fake.scans != 1 {
		t.Fatalf("delivery=%+v err=%v scans=%d", result, err, fake.scans)
	}
	before, _ := os.Stat(target)
	// Simulate another API instance resuming after its predecessor's lost reply.
	fresh := NewDeliveryService(service.catalog, service.root)
	result, err = fresh.Deliver(context.Background(), source, "delivered.cbz", "")
	after, _ := os.Stat(target)
	if err != nil || result.Indexed != "verified" || !os.SameFile(before, after) || fake.scans != 1 {
		t.Fatalf("resume rewrote or rescanned: %+v %v", result, err)
	}
}
func TestDeliveryAcceptedScanAndReadyDoNotProveMetadataImport(t *testing.T) {
	service, fake, source, target := deliveryFixture(t)
	fake.book.Metadata.Summary = "stale summary"
	result, err := service.Deliver(context.Background(), source, "delivered.cbz", "")
	if err != nil || !result.Copied || result.Indexed != "pending" || result.BookID != "" {
		t.Fatalf("false indexed: %+v %v", result, err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("pending copy lost")
	}
	fake.book.Metadata.Summary = "Reviewed summary"
	result, err = service.Deliver(context.Background(), source, "delivered.cbz", "")
	if err != nil || result.Indexed != "verified" {
		t.Fatalf("cannot resume readback: %+v %v", result, err)
	}
}
func TestDeliveryWrongPathAndScanFailureStayPending(t *testing.T) {
	service, fake, source, _ := deliveryFixture(t)
	fake.scanErr = errors.New("unavailable")
	result, err := service.Deliver(context.Background(), source, "delivered.cbz", "")
	if err != nil || !result.Copied || result.Reason != "scan_failed" || result.Indexed != "pending" {
		t.Fatalf("scan failure lost copy phase: %+v %v", result, err)
	}
	fake.scanErr = nil
	fake.book.URL = "/books/other/delivered.cbz"
	result, err = service.Deliver(context.Background(), source, "delivered.cbz", "")
	if err != nil || result.Indexed != "pending" || result.BookID != "" {
		t.Fatalf("same name elsewhere accepted: %+v %v", result, err)
	}
}
func TestDeliveryRefusesUnmappedOrDifferentExistingFile(t *testing.T) {
	service, fake, source, target := deliveryFixture(t)
	fake.library.Root = "/unexpected"
	if _, err := service.Deliver(context.Background(), source, "delivered.cbz", ""); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("mapping mismatch: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrote unmapped library")
	}
	fake.library.Root = "/books"
	if err := os.Mkdir(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("unrelated book"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Deliver(context.Background(), source, "delivered.cbz", ""); !errors.Is(err, tasks.ErrKomgaTargetConflict) {
		t.Fatalf("existing file: %v", err)
	}
	bytes, _ := os.ReadFile(target)
	if string(bytes) != "unrelated book" || fake.scans != 0 {
		t.Fatal("conflict caused side effects")
	}
}

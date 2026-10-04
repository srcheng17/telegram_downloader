package komgaedit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/infra/komga"
)

func TestResolveBookConfinesFileToMappedLibrary(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "shelf"), 0700); err != nil {
		t.Fatal(err)
	}
	bookPath := filepath.Join(root, "shelf", "book.cbz")
	if err := os.WriteFile(bookPath, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	service := NewCatalogService(nil, []config.KomgaLibraryMapping{{LibraryID: "lib", KomgaRoot: "/books", LocalRoot: root}}, nil)
	library := komga.Library{ID: "lib", Root: "/books", ImportComicInfoBook: true}
	book := komga.Book{ID: "book", LibraryID: "lib", URL: "file:/books/shelf/book.cbz"}
	opened, rel, err := service.resolveFile(book, library)
	if err != nil || rel != filepath.Join("shelf", "book.cbz") {
		t.Fatalf("valid book failed: relative=%q err=%v", rel, err)
	}
	_ = opened.Close()
	book.URL = "/books/shelf/book.cbz"
	opened, rel, err = service.resolveFile(book, library)
	if err != nil || rel != filepath.Join("shelf", "book.cbz") {
		t.Fatalf("Komga 1.28.1 absolute path failed: relative=%q err=%v", rel, err)
	}
	_ = opened.Close()
	for _, raw := range []string{
		"/books2/shelf/book.cbz",
		"/books/../books2/book.cbz",
		"/books//shelf/book.cbz",
		"/books/shelf/book.cbr",
		"file:/books2/shelf/book.cbz",
		"file:/books/../books2/book.cbz",
		"file:/books/%2e%2e/books2/book.cbz",
		"file:/books/shelf/book.cbr",
		"https://example.invalid/books/shelf/book.cbz",
	} {
		book.URL = raw
		opened, _, err := service.resolveFile(book, library)
		if opened != nil {
			_ = opened.Close()
		}
		if err == nil {
			t.Fatalf("unsafe file accepted: %q", raw)
		}
	}
	if err := os.Symlink("shelf", filepath.Join(root, "shortcut")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	book.URL = "file:/books/shortcut/book.cbz"
	if opened, _, err := service.resolveFile(book, library); err == nil {
		_ = opened.Close()
		t.Fatal("symlinked path accepted")
	}
}

func TestProjectAcceptsKomgaAbsolutePathWithoutExposingIt(t *testing.T) {
	service := NewCatalogService(nil, []config.KomgaLibraryMapping{{LibraryID: "lib", KomgaRoot: "/books", LocalRoot: "/unused"}}, nil)
	view := service.project(komga.Book{ID: "book", LibraryID: "lib", URL: "/books/series/demo.cbz"}, &komga.Library{ID: "lib", Root: "/books", ImportComicInfoBook: true})
	if view.FileName != "demo.cbz" || view.FileType != ".cbz" || !view.Editable {
		t.Fatalf("absolute Komga URL not recognized: %#v", view)
	}
	if encoded, err := json.Marshal(view); err != nil || strings.Contains(string(encoded), "/books/") {
		t.Fatalf("private Komga path exposed: %q %v", encoded, err)
	}
}

func TestBookProjectionRedactsFileURLAndPrivateLink(t *testing.T) {
	service := NewCatalogService(nil, nil, []string{"lib"})
	book := komga.Book{ID: "b", LibraryID: "lib", URL: "file:/private/books/work.cbz", Metadata: komga.BookMetadata{Links: []komga.WebLink{{URL: "file:/private/secret"}, {URL: "https://example.invalid/work"}}}}
	view := service.project(book, nil)
	if view.FileName != "work.cbz" || view.Editable || len(view.Metadata.Links) != 1 || view.Metadata.Links[0].URL != "https://example.invalid/work" {
		t.Fatalf("unsafe book projection: %#v", view)
	}
}

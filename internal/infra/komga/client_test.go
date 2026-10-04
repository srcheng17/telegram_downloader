package komga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/credentials"
)

const testKey = "private-komga-api-key"

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, credentials.NewSecret(testKey))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func errorCode(t *testing.T, err error, want string) {
	t.Helper()
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != want {
		t.Fatalf("error = %v, want code %s", err, want)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatal("credential leaked in error")
	}
}

func TestBooksUsesOfficialPagedFilteredList(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/books/list" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("size") != "20" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		if r.Header.Get("X-API-Key") != testKey || r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing credential or content type")
		}
		var body struct {
			Condition struct {
				AllOf []map[string]map[string]any `json:"allOf"`
			} `json:"condition"`
			FullTextSearch string `json:"fullTextSearch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Condition.AllOf) != 2 || body.Condition.AllOf[0]["libraryId"]["operator"] != "is" || body.Condition.AllOf[0]["libraryId"]["value"] != "lib1" || body.Condition.AllOf[1]["deleted"]["operator"] != "isFalse" || body.FullTextSearch != "Title" {
			t.Errorf("unexpected search request: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"id":"book1","libraryId":"lib1","name":"Book","url":"file:/library/Book.cbz","media":{"status":"READY"},"metadata":{"title":"Title"}}],"number":2,"size":20,"totalElements":41,"totalPages":3}`)
	})
	page, err := client.Books(context.Background(), ListBooksOptions{LibraryID: "lib1", Query: " Title ", Page: 2, Size: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Content) != 1 || page.Content[0].Metadata.Title != "Title" || page.TotalElements != 41 {
		t.Fatalf("unexpected page: %+v", page)
	}
}

func TestClearBookMetadataOnlySendsWhitelistWithoutLocks(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/books/book1/metadata" || r.Header.Get("X-API-Key") != testKey {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(patch) != 6 || patch["summary"] != "" || patch["releaseDate"] != nil || patch["isbn"] != "" {
			t.Errorf("unexpected scalar clear payload: %+v", patch)
		}
		for _, name := range []string{"authors", "tags", "links"} {
			value, ok := patch[name].([]any)
			if !ok || len(value) != 0 {
				t.Errorf("%s = %#v, want empty array", name, patch[name])
			}
		}
		for key := range patch {
			if strings.HasSuffix(key, "Lock") || key == "title" || key == "number" || key == "numberSort" {
				t.Errorf("unsupported PATCH field: %s", key)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	err := client.ClearBookMetadata(context.Background(), "book1", ClearSummary, ClearReleaseDate, ClearAuthors, ClearTags, ClearISBN, ClearLinks)
	if err != nil {
		t.Fatal(err)
	}
}

func TestClearBookMetadataRejectsUnknownBeforeRequest(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	errorCode(t, client.ClearBookMetadata(context.Background(), "book1", ClearField("title")), "invalid_input")
	errorCode(t, client.ClearBookMetadata(context.Background(), "book1", ClearISBN, ClearISBN), "invalid_input")
	if calls.Load() != 0 {
		t.Fatal("invalid PATCH was sent")
	}
}

func TestAnalyzeRequiresAcceptedResponse(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/books/book1/analyze" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
	})
	if err := client.AnalyzeBook(context.Background(), "book1"); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryAndBookDetailsDecodeServerOnlyFields(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/libraries/lib1":
			_, _ = io.WriteString(w, `{"id":"lib1","name":"Library","root":"/komga/private","importComicInfoBook":true,"importBarcodeIsbn":true}`)
		case "/api/v1/books/book1":
			_, _ = io.WriteString(w, `{"id":"book1","libraryId":"lib1","name":"Book.cbz","url":"file:/komga/private/Book.cbz","media":{"status":"READY","mediaType":"application/vnd.comicbook+zip","pagesCount":5},"metadata":{"title":"Title","summaryLock":true,"releaseDate":null,"authors":[{"name":"A","role":"writer"}]}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	library, err := client.Library(context.Background(), "lib1")
	if err != nil {
		t.Fatal(err)
	}
	book, err := client.Book(context.Background(), "book1")
	if err != nil {
		t.Fatal(err)
	}
	if !library.ImportComicInfoBook || !library.ImportBarcodeIsbn || library.Root != "/komga/private" || book.URL != "file:/komga/private/Book.cbz" || book.Media.Status != "READY" || !book.Metadata.SummaryLock || book.Metadata.ReleaseDate != nil || len(book.Metadata.Authors) != 1 {
		t.Fatalf("unexpected detail decode: library=%+v book=%+v", library, book)
	}
}

func TestUpstreamBodyIsNotExposed(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"private location and `+testKey+`"}`)
	})
	_, err := client.Libraries(context.Background())
	errorCode(t, err, "unauthorized")
	if strings.Contains(err.Error(), "private location") {
		t.Fatal("upstream response leaked")
	}
}

func TestMissingListEndpointIsVersionErrorButMissingBookIsNotFound(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := client.Books(context.Background(), ListBooksOptions{LibraryID: "lib1", Size: 20})
	errorCode(t, err, "unsupported_version")
	_, err = client.Book(context.Background(), "book1")
	errorCode(t, err, "not_found")
}

func TestRedirectRefusesCredentialForwarding(t *testing.T) {
	var reached atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
	}))
	defer other.Close()
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/stolen", http.StatusTemporaryRedirect)
	})
	_, err := client.Libraries(context.Background())
	errorCode(t, err, "redirect_blocked")
	if reached.Load() {
		t.Fatal("redirect destination was requested")
	}
}

func TestResponseBoundAndSafeErrors(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, fmt.Sprintf(`{"secret":"%s","padding":"%s"}`, testKey, strings.Repeat("x", maxResponseBytes)))
	})
	_, err := client.Libraries(context.Background())
	errorCode(t, err, "response_too_large")
}

func TestBaseURLAndCredentialsValidation(t *testing.T) {
	for _, target := range []string{
		"http://example.com", "http://localhost@other.example", "https://user:pass@example.com",
		"https://example.com/?q=1", "https://example.com/#secret", "file:///tmp/komga",
		"https://example.com/a/../b", "https://example.com/%2e%2e/b",
	} {
		if _, err := NewClient(target, credentials.NewSecret(testKey)); err == nil {
			t.Errorf("accepted target %q", target)
		}
	}
	if _, err := NewClient("https://example.com", credentials.NewSecret("line\nbreak")); err == nil {
		t.Fatal("accepted malformed credential")
	}
}

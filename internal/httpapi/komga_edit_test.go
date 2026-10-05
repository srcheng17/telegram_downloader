package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ryancheng/telegram-downloader/internal/app/komgaedit"
)

type fakeKomgaEditor struct {
	saveCalls int
	saveErr   error
}

func (*fakeKomgaEditor) Detail(context.Context, string) (komgaedit.EditDetail, error) {
	return komgaedit.EditDetail{Book: komgaedit.BookView{ID: "book-a"}, CanSave: true, SourceVersion: strings.Repeat("a", 64)}, nil
}
func (*fakeKomgaEditor) Preview(context.Context, string, komgaedit.PreviewRequest) (komgaedit.PreviewResult, error) {
	return komgaedit.PreviewResult{PreviewToken: "token", CanSave: true}, nil
}
func (f *fakeKomgaEditor) Save(context.Context, string, komgaedit.SaveRequest) (komgaedit.OperationView, error) {
	f.saveCalls++
	if f.saveErr != nil {
		return komgaedit.OperationView{}, f.saveErr
	}
	return komgaedit.OperationView{ID: "op-a", BookID: "book-a", State: komgaedit.StateSyncPending, FileCommitted: true, AvailableActions: []string{"retry_sync"}}, nil
}
func (*fakeKomgaEditor) Operation(context.Context, string) (komgaedit.OperationView, error) {
	return komgaedit.OperationView{ID: "op-a", BookID: "book-a", State: komgaedit.StateSyncPending, FileCommitted: true}, nil
}
func (*fakeKomgaEditor) RetrySync(context.Context, string) (komgaedit.OperationView, error) {
	return komgaedit.OperationView{ID: "op-a", BookID: "book-a", State: komgaedit.StateCurrentValueConsistent, FileCommitted: true, ProjectionConsistent: true}, nil
}
func (*fakeKomgaEditor) Restore(context.Context, string) (komgaedit.OperationView, error) {
	return komgaedit.OperationView{}, komgaedit.ErrUnsafe
}

func TestKomgaEditRoutesKeepFileAndProjectionStatusSeparate(t *testing.T) {
	service := &fakeKomgaEditor{}
	r := chi.NewRouter()
	NewKomgaEditHandler(service).RegisterRoutes(r)
	detail := httptest.NewRecorder()
	r.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/komga/books/book-a/edit", nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"source_version"`) {
		t.Fatalf("detail=%d %s", detail.Code, detail.Body.String())
	}
	save := httptest.NewRecorder()
	r.ServeHTTP(save, httptest.NewRequest(http.MethodPost, "/api/komga/books/book-a/save", strings.NewReader(`{"source_version":"sha","definitions_version":"v1","changes":[],"preview_token":"token","idempotency_key":"idempotency-example"}`)))
	if save.Code != http.StatusOK {
		t.Fatalf("save=%d %s", save.Code, save.Body.String())
	}
	var response struct {
		Operation komgaedit.OperationView `json:"operation"`
	}
	if err := json.Unmarshal(save.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Operation.FileCommitted || response.Operation.ProjectionConsistent || response.Operation.AnalyzeVerified || response.Operation.State != komgaedit.StateSyncPending {
		t.Fatalf("partial operation collapsed to success: %#v", response.Operation)
	}
	if service.saveCalls != 1 {
		t.Fatalf("save calls=%d", service.saveCalls)
	}
	unknown := httptest.NewRecorder()
	r.ServeHTTP(unknown, httptest.NewRequest(http.MethodPost, "/api/komga/books/book-a/save", strings.NewReader(`{"unexpected":"value"}`)))
	if unknown.Code != http.StatusUnprocessableEntity || service.saveCalls != 1 {
		t.Fatalf("unsafe input dispatched: %d", unknown.Code)
	}
}

func TestKomgaEditConflictDoesNotReportSuccessfulWrite(t *testing.T) {
	service := &fakeKomgaEditor{saveErr: komgaedit.ErrConflict}
	r := chi.NewRouter()
	NewKomgaEditHandler(service).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/komga/books/book-a/save", strings.NewReader(`{"source_version":"sha","definitions_version":"v1","changes":[],"preview_token":"token","idempotency_key":"idempotency-example"}`)))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"edit_conflict"`) || strings.Contains(rec.Body.String(), `"operation"`) {
		t.Fatalf("conflict response=%d %s", rec.Code, rec.Body.String())
	}
}

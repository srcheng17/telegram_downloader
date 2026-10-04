package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	app "github.com/ryancheng/telegram-downloader/internal/app/metadataextract"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type extractionSchema struct{}

func (extractionSchema) Schema(context.Context) (domain.Registry, error) {
	return domain.StandardRegistry(), nil
}

type extractionModel struct{ calls int }

func (*extractionModel) Snapshot(context.Context, uint64) (app.ModelSnapshot, error) {
	return app.ModelSnapshot{ConfigRevision: 1, ModelID: "test", BudgetVerified: true}, nil
}
func (m *extractionModel) ExtractJSON(context.Context, app.ModelSnapshot, string, json.RawMessage, int) (json.RawMessage, error) {
	m.calls++
	return json.RawMessage(`{"fields":{"title":{"value":"Sample","evidence_quote":"Sample"}}}`), nil
}
func TestMetadataExtractStrictRequestBoundary(t *testing.T) {
	m := &extractionModel{}
	r := chi.NewRouter()
	RegisterMetadataExtract(r, app.NewService(extractionSchema{}, m))
	valid := `{"request_id":"test","text":"Title: Sample","field_keys":["title"],"schema_version":1,"definitions_version":"standard-v1","base_document_revision":0,"field_revisions":{"title":0},"input_revision":0,"config_revision":1}`
	for _, body := range []string{`{}`, strings.Replace(valid, `"input_revision":0,`, "", 1), strings.Replace(valid, `"input_revision":0`, `"input_revision":null`, 1), strings.Replace(valid, `"title":0`, `"title":null`, 1), strings.TrimSuffix(valid, "}") + `,"base_url":"https://example.test"}`, strings.TrimSuffix(valid, "}") + `,"text":"duplicate"}`, strings.Repeat(" ", 128*1024) + valid} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/metadata/extract", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("got %d %s", w.Code, w.Body.String())
		}
	}
	if m.calls != 0 {
		t.Fatal("invalid request reached model")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/metadata/extract", strings.NewReader(valid)))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"origin":"ai"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
func TestMetadataExtractErrorRedaction(t *testing.T) {
	for _, code := range []string{"disabled", "not_configured", "config_changed", "schema_unsupported", "context_exceeded", "refused", "unauthorized", "forbidden", "rate_limited", "timeout", "unreachable", "invalid_response", "cancelled"} {
		w := httptest.NewRecorder()
		writeExtractionError(w, app.Failure(code))
		if !strings.Contains(w.Body.String(), `"code":"`+code+`"`) {
			t.Fatalf("lost category %s", code)
		}
	}
	w := httptest.NewRecorder()
	writeExtractionError(w, app.Failure("secret-private-body"))
	if strings.Contains(w.Body.String(), "secret-private-body") {
		t.Fatal("unknown error leaked")
	}
}

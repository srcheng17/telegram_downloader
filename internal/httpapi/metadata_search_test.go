package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	search "github.com/ryancheng/telegram-downloader/internal/app/metadatasearch"
	"github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type searchHTTPFixture struct{ calls int }

func (*searchHTTPFixture) Sources(context.Context) ([]sourcesettings.SourceView, error) {
	return []sourcesettings.SourceView{{PublicSourceConfig: sourcesettings.PublicSourceConfig{ProviderID: "bangumi", Enabled: true, ConfigVersion: 1, CredentialConfigured: true}, Descriptor: sourcesettings.Descriptor{ID: "bangumi"}}}, nil
}
func (*searchHTTPFixture) PublicSourceConfig(context.Context, string) (sourcesettings.PublicSourceConfig, error) {
	return sourcesettings.PublicSourceConfig{ProviderID: "bangumi", Enabled: true, ConfigVersion: 1}, nil
}
func (*searchHTTPFixture) ResolveCredentials(context.Context, string) (credentials.Secret, error) {
	return credentials.NewSecret("synthetic-secret"), nil
}
func (*searchHTTPFixture) Schema(context.Context) (metadata.Registry, error) {
	return metadata.StandardRegistry(), nil
}
func (*searchHTTPFixture) Info(string) search.ProviderInfo { return search.ProviderInfo{} }
func (f *searchHTTPFixture) Search(context.Context, sourcesettings.PublicSourceConfig, credentials.Secret, string) ([]search.Record, error) {
	f.calls++
	return []search.Record{{ID: "123", Title: "Example"}}, nil
}
func (f *searchHTTPFixture) Resolve(context.Context, sourcesettings.PublicSourceConfig, credentials.Secret, string) (search.Record, error) {
	f.calls++
	return search.Record{ID: "123", Title: "Example", PublicURL: "https://bgm.tv/subject/123", Fields: map[string]search.Field{"title": {Value: json.RawMessage(`"Example"`), Path: "name"}}}, nil
}
func TestMetadataSearchHTTPStrictContractsAndNoSecret(t *testing.T) {
	f := &searchHTTPFixture{}
	router := chi.NewRouter()
	NewMetadataSearchHandler(search.NewService(f, f, f)).RegisterRoutes(router)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	w := request("GET", "/api/metadata/providers", "")
	if w.Code != 200 || f.calls != 0 || strings.Contains(w.Body.String(), "synthetic-secret") {
		t.Fatal("descriptor made request or leaked secret")
	}
	for _, body := range []string{`{"keyword":"Neutral","provider_ids":["bangumi"]}`, `{"keyword":"Neutral","provider_ids":["bangumi"],"query_revision":null}`, `{"keyword":"Neutral","keyword":"Duplicate","provider_ids":["bangumi"],"query_revision":1}`, `{"keyword":"Neutral","provider_ids":["bangumi"],"query_revision":1,"url":"https://evil.test"}`, `{"keyword":"Neutral","provider_ids":["bangumi"],"query_revision":1,"headers":{"Authorization":"secret"}}`} {
		if w = request("POST", "/api/metadata/search", body); w.Code != 422 {
			t.Fatalf("unsafe request accepted: %d", w.Code)
		}
	}
	w = request("POST", "/api/metadata/search", `{"keyword":"Neutral","provider_ids":["bangumi"],"query_revision":1}`)
	if w.Code != 200 || f.calls != 1 || strings.Contains(w.Body.String(), "synthetic-secret") {
		t.Fatal("search contract failed")
	}
	base := `"provider_id":"bangumi","record_id":"123","query_revision":1,"base_document_revision":0,"schema_version":1,"definitions_version":"standard-v1","config_version":1`
	w = request("POST", "/api/metadata/candidates/resolve", "{"+base+`,"field_revisions":{"title":null}}`)
	if w.Code != 422 {
		t.Fatal("null field revision accepted")
	}
	w = request("POST", "/api/metadata/candidates/resolve", "{"+base+`,"field_revisions":{}}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "synthetic-secret") {
		t.Fatalf("resolve contract failed %d %s", w.Code, w.Body.String())
	}
}

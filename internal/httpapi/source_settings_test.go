package httpapi

import (
	"bytes"
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"net/http/httptest"
	"strings"
	"testing"
)

type settingsTestRepo struct{ record sourcesettings.Record }

func (r *settingsTestRepo) Get(context.Context, string) (sourcesettings.Record, error) {
	if r.record.ConfigVersion == 0 {
		return sourcesettings.Record{}, sourcesettings.ErrNotFound
	}
	return r.record, nil
}
func (r *settingsTestRepo) Save(_ context.Context, v sourcesettings.Record, expected int64) (sourcesettings.Record, error) {
	if r.record.ConfigVersion != expected {
		return v, sourcesettings.ErrConflict
	}
	v.ConfigVersion++
	r.record = v
	return v, nil
}
func TestAISettingsHTTPRejectsMissingVersionAndRedactsSecret(t *testing.T) {
	repo := &settingsTestRepo{}
	vault, _ := credentials.NewVault(bytes.Repeat([]byte{1}, 32))
	service := sourcesettings.NewService(repo, vault, nil, nil, nil)
	r := chi.NewRouter()
	NewSourceSettings(service).RegisterRoutes(r)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		r.ServeHTTP(w, req)
		return w
	}
	base := `"enabled":false,"base_url":"http://localhost:8000/v1","model_id":"manual","credential":{"action":"replace","value":"synthetic-private-key"}`
	if w := request("PUT", "/api/settings/ai", "{"+base+"}"); w.Code != 422 {
		t.Fatalf("missing version accepted %d", w.Code)
	}
	saved := request("PUT", "/api/settings/ai", `{"expected_version":0,`+base+`}`)
	if saved.Code != 200 || strings.Contains(saved.Body.String(), "synthetic-private-key") {
		t.Fatal("save failed or secret exposed")
	}
	got := request("GET", "/api/settings/ai", "")
	if strings.Contains(got.Body.String(), "synthetic-private-key") || !strings.Contains(got.Body.String(), `"credential_configured":true`) {
		t.Fatal("public credential projection invalid")
	}
	for _, body := range []string{`{"config_version":1,"url":"http://evil.test"}`, `{"config_version":1,"text":"private OCR"}`, `{"config_version":1,"headers":{"Authorization":"evil"}}`} {
		if w := request("POST", "/api/settings/ai/models", body); w.Code != 422 {
			t.Fatal("uncontrolled model payload accepted")
		}
	}
	stale := request("PUT", "/api/settings/ai", `{"expected_version":0,`+base+`}`)
	if stale.Code != 409 {
		t.Fatal("CAS conflict missing")
	}
}

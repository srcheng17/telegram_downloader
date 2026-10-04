package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

func TestNormalizeTelegraphURLPreservesEscapedPath(t *testing.T) {
	for _, tc := range []struct {
		name, input, path string
	}{
		{"space", "https://telegra.ph/a%20b", "/a%20b"},
		{"unicode", "https://telegra.ph/中文", "/%E4%B8%AD%E6%96%87"},
		{"encoded unicode", "https://telegra.ph/%E4%B8%AD%E6%96%87", "/%E4%B8%AD%E6%96%87"},
		{"encoded separators", "https://telegra.ph//a%2Fb///%2f//", "/a%2Fb/%2f"},
		{"encoded question and fragment", "https://telegra.ph/a%3Fb%23c", "/a%3Fb%23c"},
		{"literal escape", "https://telegra.ph/%252F", "/%252F"},
		{"existing cleanup", " HTTP://WWW.TELEGRA.PH//demo///?source=test#top ", "/demo"},
		{"root", "https://telegra.ph///", "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			canonical := normalizeTelegraphURL(tc.input)
			if want := "https://telegra.ph" + tc.path; canonical != want {
				t.Fatalf("canonical = %q, want %q", canonical, want)
			}
			if again := normalizeTelegraphURL(canonical); again != canonical {
				t.Fatalf("normalization is not idempotent: %q -> %q", canonical, again)
			}
			req, err := http.NewRequest(http.MethodGet, canonical, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := req.URL.RequestURI(); got != tc.path {
				t.Fatalf("HTTP request path = %q, want %q", got, tc.path)
			}
		})
	}
}

func TestCreateURLTaskPreservesEncodedCanonicalURL(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{createURLStatus: domain.StatusReady}
	router := newTaskCoreTestRouter(t, svc)
	const raw = "https://telegra.ph//中文%20a%2Fb/"
	form := url.Values{"url": {raw}}
	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if svc.createdURL.URL != raw || svc.createdURL.CanonicalURL != "https://telegra.ph/%E4%B8%AD%E6%96%87%20a%2Fb" {
		t.Fatalf("download input = %+v", svc.createdURL)
	}
}

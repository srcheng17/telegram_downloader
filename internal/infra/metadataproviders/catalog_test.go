package metadataproviders

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	search "github.com/ryancheng/telegram-downloader/internal/app/metadatasearch"
	"github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func config(id string) sourcesettings.PublicSourceConfig {
	return sourcesettings.PublicSourceConfig{ProviderID: id, Enabled: true, ConfigVersion: 1, AuthMode: "none"}
}
func testCatalog(f transportFunc) *Catalog {
	c := newCatalog(f)
	zero := time.Duration(0)
	c.intervalOverride = &zero
	return c
}
func errorCode(t *testing.T, err error, want string) {
	t.Helper()
	var source *search.Error
	if !errors.As(err, &source) || source.Code != want {
		t.Fatalf("wanted %s, got %v", want, err)
	}
}

func TestFixedProtocolsAndFieldSemantics(t *testing.T) {
	for _, id := range []string{"mangabaka", "mangaupdates", "bangumi"} {
		t.Run(id, func(t *testing.T) {
			calls := 0
			c := testCatalog(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Scheme != "https" || r.Header.Get("User-Agent") != userAgent {
					t.Fatal("fixed HTTPS and UA required")
				}
				if r.Header.Get("Authorization") != "" {
					t.Fatal("anonymous source received token")
				}
				name := id
				switch {
				case strings.HasSuffix(r.URL.Path, "/persons"):
					name = "bangumi-persons"
				case strings.HasSuffix(r.URL.Path, "/subjects"):
					name = "bangumi-relations"
				}
				return response(200, fixture(t, name)), nil
			})
			record, err := c.Resolve(context.Background(), config(id), credentials.NewSecret("wrong-provider-secret"), "123")
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := record.Fields["number"]; ok {
				t.Fatal("series count became current number")
			}
			if _, ok := record.Fields["volume"]; ok {
				t.Fatal("series count became volume")
			}
			if string(record.Fields["creators.writer"].Value) != `["Original Writer"]` {
				t.Fatalf("writer role lost: %s", record.Fields["creators.writer"].Value)
			}
			if record.RetrievedAt == "" || record.PublicURL == "" || record.Fields["identifiers"].Path == "" {
				t.Fatal("record identity missing")
			}
			if id == "bangumi" {
				if calls != 3 || record.Relationship != "volume_in_series" || string(record.Fields["series"].Value) != `"Example Series"` || string(record.Extra["source.nsfw"].Value) != "false" {
					t.Fatalf("bangumi semantics: %+v", record)
				}
				if string(record.Fields["creators.translator"].Value) != `["Translator"]` {
					t.Fatal("translator role lost")
				}
			}
			if id == "mangaupdates" && string(record.Fields["format"].Value) != `"Doujinshi"` {
				t.Fatal("source type lost")
			}
			if id == "mangabaka" {
				if _, ok := record.Extra["source.status"]; !ok {
					t.Fatal("explicit custom source absent")
				}
				if strings.Contains(string(record.Fields["identifiers"].Value), "third-party") {
					t.Fatal("third-party source data relicensed")
				}
			}
		})
	}
}
func TestTokenIsolationRedirectAndErrorRedaction(t *testing.T) {
	for _, id := range []string{"mangabaka", "mangaupdates", "bangumi"} {
		t.Run(id, func(t *testing.T) {
			calls := 0
			c := testCatalog(func(r *http.Request) (*http.Response, error) {
				calls++
				want := ""
				if id == "bangumi" {
					want = "Bearer synthetic-bangumi-secret"
				}
				if r.Header.Get("Authorization") != want {
					t.Fatal("token sent to wrong source")
				}
				allowed := map[string]string{"mangabaka": "api.mangabaka.org", "mangaupdates": "api.mangaupdates.com", "bangumi": "api.bgm.tv"}
				if r.URL.Host != allowed[id] {
					t.Fatal("wrong destination")
				}
				res := response(302, "synthetic-bangumi-secret")
				res.Header.Set("Location", "https://example.invalid/steal")
				return res, nil
			})
			cfg := config(id)
			cfg.AuthMode = "bearer"
			_, err := c.Search(context.Background(), cfg, credentials.NewSecret("synthetic-bangumi-secret"), "neutral")
			errorCode(t, err, "invalid_response")
			if calls != 1 || strings.Contains(err.Error(), "secret") {
				t.Fatal("redirect followed or secret leaked")
			}
		})
	}
}
func TestBoundedBodiesMalformedPermissionAndRetryAfter(t *testing.T) {
	for _, test := range []struct {
		code       int
		body, want string
	}{{200, `{}`, "invalid_response"}, {200, `{"data":null}`, "invalid_response"}, {200, `{`, "invalid_response"}, {200, strings.Repeat(" ", maxBody+1), "invalid_response"}, {401, "secret", "auth_required"}, {403, "secret", "permission_denied"}, {429, "secret", "rate_limited"}, {500, "secret", "unavailable"}} {
		t.Run(test.want+string(rune(test.code)), func(t *testing.T) {
			calls := 0
			c := testCatalog(func(*http.Request) (*http.Response, error) {
				calls++
				r := response(test.code, test.body)
				r.Header.Set("Retry-After", "10")
				return r, nil
			})
			_, err := c.Search(context.Background(), config("mangabaka"), credentials.NewSecret(""), "neutral")
			errorCode(t, err, test.want)
			if test.code == 429 {
				_, err = c.Search(context.Background(), config("mangabaka"), credentials.NewSecret(""), "another")
				errorCode(t, err, "rate_limited")
				if calls != 1 {
					t.Fatal("Retry-After ignored")
				}
			}
		})
	}
}
func TestSearchCapsResultsCacheVersionAndNoResult(t *testing.T) {
	rows := make([]any, 25)
	for i := range rows {
		rows[i] = map[string]any{"record": map[string]any{"series_id": i + 1, "title": "Neutral", "type": "Manga"}, "hit_title": "Alias"}
	}
	raw, _ := json.Marshal(map[string]any{"results": rows})
	calls := 0
	c := testCatalog(func(r *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if len(body) != 2 || body["search"] != "neutral" {
			t.Fatal("unexpected data sent")
		}
		return response(200, string(raw)), nil
	})
	cfg := config("mangaupdates")
	for i := 0; i < 2; i++ {
		rows, err := c.Search(context.Background(), cfg, credentials.NewSecret(""), "neutral")
		if err != nil || len(rows) != 20 {
			t.Fatal("result cap failed")
		}
	}
	if calls != 1 {
		t.Fatal("cache missing")
	}
	cfg.ConfigVersion++
	c.Search(context.Background(), cfg, credentials.NewSecret(""), "neutral")
	if calls != 2 {
		t.Fatal("configuration cache isolation missing")
	}
	c = testCatalog(func(*http.Request) (*http.Response, error) { return response(200, `{"data":[]}`), nil })
	rowsEmpty, err := c.Search(context.Background(), config("bangumi"), credentials.NewSecret(""), "neutral")
	if err != nil || len(rowsEmpty) != 0 {
		t.Fatal("empty result confused with permission failure")
	}
}
func TestOneInFlightCancellationAndBoundedCache(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	c := testCatalog(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.Search(ctx, config("mangabaka"), credentials.NewSecret(""), "neutral")
		done <- err
	}()
	<-started
	_, err := c.Search(context.Background(), config("mangabaka"), credentials.NewSecret(""), "other")
	errorCode(t, err, "rate_limited")
	cancel()
	if !errors.Is(<-done, context.Canceled) || calls.Load() != 1 {
		t.Fatal("cancel or source concurrency failed")
	}
	c = testCatalog(func(*http.Request) (*http.Response, error) { return response(200, `{"data":[]}`), nil })
	for i := 0; i < 80; i++ {
		c.Search(context.Background(), config("bangumi"), credentials.NewSecret(""), string(rune(i+100)))
	}
	if len(c.cache) != 64 {
		t.Fatal("cache unbounded")
	}
	for key, value := range c.cache {
		value.expires = time.Now().Add(-time.Second)
		c.cache[key] = value
	}
	if _, err := c.Resolve(context.Background(), config("bangumi"), credentials.NewSecret(""), "https://evil.test"); err == nil {
		t.Fatal("arbitrary record URL accepted")
	}
}

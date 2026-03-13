package httpui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appconfig "github.com/ryancheng/telegram-downloader/go-backend/internal/config"
)

func TestUIRoutesRenderMainPages(t *testing.T) {
	router := NewRouter()

	cases := []struct {
		name    string
		path    string
		heading string
	}{
		{
			name:    "index",
			path:    "/",
			heading: "发起下载任务",
		},
		{
			name:    "logs",
			path:    "/logs",
			heading: "下载日志",
		},
		{
			name:    "settings",
			path:    "/settings",
			heading: "设置",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", recorder.Code)
			}
			body := recorder.Body.String()
			if !strings.Contains(body, tc.heading) {
				t.Fatalf("expected body to contain %q, got %q", tc.heading, body)
			}
		})
	}
}

func TestBaseTemplateLoadsDistBundles(t *testing.T) {
	router := NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	body := recorder.Body.String()
	assertContains(t, body, `/static/dist/app.bundle.js`)
	assertContains(t, body, `/static/dist/index.bundle.js`)
	assertContains(t, body, `/static/dist/logs.bundle.js`)
	assertContains(t, body, `/static/dist/settings.bundle.js`)
	assertNotContains(t, body, `/static/index.js`)
	assertNotContains(t, body, `/static/logs.js`)
	assertNotContains(t, body, `/static/app.js`)
}

func TestSettingsPageUsesDynamicSettingsValues(t *testing.T) {
	router := NewRouterWithConfig(Config{
		Settings: Settings{
			TaskConcurrency:   7,
			ImageConcurrency:  9,
			Timeout:           45,
			Retries:           12,
			LogRetentionDays:  15,
			FileRetentionDays: 21,
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	body := recorder.Body.String()
	assertContains(t, body, `name="task_concurrency" value="7"`)
	assertContains(t, body, `name="image_concurrency" value="9"`)
	assertContains(t, body, `name="timeout" value="45"`)
	assertContains(t, body, `name="retries" value="12"`)
	assertContains(t, body, `name="log_retention_days" value="15"`)
	assertContains(t, body, `name="file_retention_days" value="21"`)
}

func TestIndexPageUsesDynamicGuardrailsAndHTMXLinks(t *testing.T) {
	router := NewRouterWithConfig(Config{
		Guardrails: Guardrails{
			AllowedDomains: []string{"telegra.ph", "www.telegra.ph", "graph.org", "www.graph.org"},
			MaxImages:      456,
			MaxImageBytes:  33554432,
			MaxTotalBytes:  1073741824,
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	body := recorder.Body.String()
	assertContains(t, body, "telegra.ph, www.telegra.ph, graph.org, www.graph.org")
	assertContains(t, body, ">456<")
	assertContains(t, body, "33554432 字节")
	assertContains(t, body, "1073741824 字节")
	assertContains(t, body, `href="/" hx-get="/" hx-target="#content" hx-push-url="true"`)
	assertContains(t, body, `href="/logs" hx-get="/logs" hx-target="#content" hx-push-url="true"`)
	assertContains(t, body, `href="/settings" hx-get="/settings" hx-target="#content" hx-push-url="true"`)
	assertContains(t, body, `id="startup-recovery-link-home"`)
	assertContains(t, body, `hx-get="/logs"`)
	assertContains(t, body, `hx-target="#content"`)
	assertContains(t, body, `hx-push-url="true"`)
	assertContains(t, body, "支持英文逗号 (,) 与中文逗号（，）分隔多个作者。")
	assertContains(t, body, "支持英文逗号 (,)、中文逗号（，）、空格与 # 分隔多个标签，自动清理多余空格。")
	assertContains(t, body, "支持英文逗号 (,)、中文逗号（，）、空格与 # 分隔多个类型，自动清理多余空格。")
}

func TestHTMXRequestReturnsPageFragment(t *testing.T) {
	router := NewRouter()
	cases := []struct {
		name    string
		path    string
		heading string
		title   string
	}{
		{
			name:    "index",
			path:    "/",
			heading: "发起下载任务",
			title:   "首页",
		},
		{
			name:    "logs",
			path:    "/logs",
			heading: "下载日志",
			title:   "日志",
		},
		{
			name:    "settings",
			path:    "/settings",
			heading: "设置",
			title:   "设置",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("HX-Request", "true")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", recorder.Code)
			}
			if vary := recorder.Header().Get("Vary"); vary != "HX-Request" {
				t.Fatalf("expected Vary header HX-Request, got %q", vary)
			}

			body := recorder.Body.String()
			assertContains(t, body, tc.heading)
			assertContains(t, body, `<title hx-swap-oob="true">`+tc.title+`</title>`)
			assertNotContains(t, body, "<!DOCTYPE html>")
			assertNotContains(t, body, "<html")
			assertNotContains(t, body, `<nav class="main-nav">`)
		})
	}
}

func TestSettingsPageKeepsZeroRetriesValue(t *testing.T) {
	router := NewRouterWithConfig(Config{
		Settings: Settings{
			TaskConcurrency:   2,
			ImageConcurrency:  2,
			Timeout:           30,
			Retries:           0,
			LogRetentionDays:  7,
			FileRetentionDays: 7,
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	body := recorder.Body.String()
	assertContains(t, body, `name="retries" value="0"`)
}

func TestSettingsPagePostRedirectsBack(t *testing.T) {
	router := NewRouter()
	req := httptest.NewRequest(http.MethodPost, "/settings", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect location /settings, got %q", location)
	}
}

func TestSettingsPagePostUpdatesSettingsStore(t *testing.T) {
	store := &fakeUISettingsStore{}
	router := NewRouterWithConfig(Config{
		SettingsStore: store,
	})
	form := url.Values{
		"timeout":           {"75"},
		"retries":           {"6"},
		"image_concurrency": {"9"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect location /settings, got %q", location)
	}
	if store.updateCalls != 1 {
		t.Fatalf("expected update to be called once, got %d", store.updateCalls)
	}
	want := appconfig.SettingsSnapshot{
		Timeout:          75,
		Retries:          6,
		ImageConcurrency: 9,
	}
	if !reflect.DeepEqual(store.lastUpdated, want) {
		t.Fatalf("unexpected update payload:\n got: %#v\nwant: %#v", store.lastUpdated, want)
	}
}

func TestSettingsPageUsesSettingsStoreSnapshotValues(t *testing.T) {
	store := &fakeUISettingsStore{
		getSnapshot: appconfig.SettingsSnapshot{
			Timeout:          81,
			Retries:          4,
			ImageConcurrency: 13,
		},
	}
	router := NewRouterWithConfig(Config{
		Settings: Settings{
			TaskConcurrency:   2,
			ImageConcurrency:  3,
			Timeout:           30,
			Retries:           10,
			LogRetentionDays:  7,
			FileRetentionDays: 7,
		},
		SettingsStore: store,
	})
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if store.getCalls != 1 {
		t.Fatalf("expected get settings to be called once, got %d", store.getCalls)
	}
	body := recorder.Body.String()
	assertContains(t, body, `name="timeout" value="81"`)
	assertContains(t, body, `name="retries" value="4"`)
	assertContains(t, body, `name="image_concurrency" value="13"`)
}

func TestStaticRouteUsesConfiguredStaticDir(t *testing.T) {
	staticDir := t.TempDir()
	const fileName = "sentinel.txt"
	if err := os.WriteFile(filepath.Join(staticDir, fileName), []byte("configured-static-dir"), 0o644); err != nil {
		t.Fatalf("write static file: %v", err)
	}

	router := NewRouterWithConfig(Config{
		StaticDir: staticDir,
	})
	req := httptest.NewRequest(http.MethodGet, "/static/"+fileName, nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if body := recorder.Body.String(); body != "configured-static-dir" {
		t.Fatalf("expected body %q, got %q", "configured-static-dir", body)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected response body to contain %q", needle)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("expected response body not to contain %q", needle)
	}
}

type fakeUISettingsStore struct {
	getSnapshot appconfig.SettingsSnapshot
	getErr      error
	getCalls    int

	updateCalls int
	lastUpdated appconfig.SettingsSnapshot
	updateErr   error
}

func (f *fakeUISettingsStore) GetSettings(_ context.Context) (appconfig.SettingsSnapshot, error) {
	f.getCalls++
	if f.getErr != nil {
		return appconfig.SettingsSnapshot{}, f.getErr
	}
	return f.getSnapshot, nil
}

func (f *fakeUISettingsStore) UpdateSettings(_ context.Context, snapshot appconfig.SettingsSnapshot) (appconfig.SettingsSnapshot, error) {
	f.updateCalls++
	f.lastUpdated = snapshot
	if f.updateErr != nil {
		return appconfig.SettingsSnapshot{}, f.updateErr
	}
	return snapshot, nil
}

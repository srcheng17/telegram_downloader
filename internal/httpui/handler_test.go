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

	appconfig "github.com/ryancheng/telegram-downloader/internal/config"
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
			heading: "新建任务",
		},
		{
			name:    "logs",
			path:    "/logs",
			heading: "任务",
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

func TestTelegramBookmarksOpenConnectionSettings(t *testing.T) {
	for _, htmxHeader := range []string{"", "true", " TRUE "} {
		req := httptest.NewRequest(http.MethodGet, "/telegram", nil)
		if htmxHeader != "" {
			req.Header.Set("HX-Request", htmxHeader)
		}
		htmx := htmxHeader != ""
		w := httptest.NewRecorder()
		NewRouter().ServeHTTP(w, req)
		if w.Header().Get("HX-Redirect") != "/settings#connections" {
			t.Fatal("old account bookmark must point to the connection tab")
		}
		if htmx && w.Code != http.StatusOK {
			t.Fatalf("htmx requires a non-redirect response to consume HX-Redirect, got %d", w.Code)
		}
		if !htmx && (w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/settings#connections") {
			t.Fatal("normal navigation lost the connection tab fragment")
		}
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
	assertNotContains(t, body, `/static/v2/`)
}

func TestSettingsPageUsesDynamicSettingsValues(t *testing.T) {
	router := NewRouterWithConfig(Config{
		Settings: Settings{
			ImageConcurrency:   9,
			Timeout:            45,
			Retries:            12,
			DownloadActionMode: "komga_copy",
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	body := recorder.Body.String()
	assertNotContains(t, body, `name="task_concurrency"`)
	assertContains(t, body, `name="image_concurrency" value="9"`)
	assertContains(t, body, `name="timeout" value="45"`)
	assertContains(t, body, `name="retries" value="12"`)
	assertNotContains(t, body, `name="log_retention_days"`)
	assertNotContains(t, body, `name="file_retention_days"`)
	assertContains(t, body, `name="download_action_mode" value="komga_copy" checked`)
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
	assertContains(t, body, `href="/" hx-get="/" hx-target="#content" hx-push-url="true"`)
	assertContains(t, body, `href="/logs" hx-get="/logs" hx-target="#content" hx-push-url="true"`)
	assertContains(t, body, `href="/settings" hx-get="/settings" hx-target="#content" hx-push-url="true"`)
	assertContains(t, body, `id="startup-recovery-link-home"`)
	assertContains(t, body, `hx-get="/logs"`)
	assertContains(t, body, `hx-target="#content"`)
	assertContains(t, body, `hx-push-url="true"`)
	assertContains(t, body, "最近填写")
	assertContains(t, body, `id="metadata-history-collapsible" class="metadata-history-panel workspace-history"`)
	assertNotContains(t, body, `id="metadata-history-collapsible" class="metadata-history-panel workspace-history" open`)
	assertContains(t, body, "先核对提交信息或最终归档信息")
	assertContains(t, body, "Telegraph 链接")
	assertContains(t, body, "上传压缩包")
	assertContains(t, body, `id="archive_file"`)
	assertNotContains(t, body, "支持 ZIP、RAR、7Z。")
	assertNotContains(t, body, "支持英文逗号 (,)、中文逗号（，）、半角 # 与全角 ＃ 分隔多个作者")
	assertNotContains(t, body, "支持英文逗号 (,)、中文逗号（，）、空格、半角 # 与全角 ＃ 分隔多个标签")
	assertNotContains(t, body, "支持英文逗号 (,)、中文逗号（，）、空格、半角 # 与全角 ＃ 分隔多个类型")
	assertNotContains(t, body, "field-hint-toggle")
	assertContains(t, body, `id="metadata-editor"`)
	assertNotContains(t, body, `id="comic_name"`)
	assertContains(t, body, `hx-history="false"`)
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
			heading: "新建任务",
			title:   "新建任务",
		},
		{
			name:    "logs",
			path:    "/logs",
			heading: "任务",
			title:   "任务",
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
			ImageConcurrency:   2,
			Timeout:            30,
			Retries:            0,
			DownloadActionMode: "browser",
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
		"timeout":              {"75"},
		"retries":              {"6"},
		"image_concurrency":    {"9"},
		"download_action_mode": {"komga_copy"},
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
		Timeout:            75,
		Retries:            6,
		ImageConcurrency:   9,
		DownloadActionMode: "komga_copy",
	}
	if !reflect.DeepEqual(store.lastUpdated, want) {
		t.Fatalf("unexpected update payload:\n got: %#v\nwant: %#v", store.lastUpdated, want)
	}
}

func TestSettingsPageUsesSettingsStoreSnapshotValues(t *testing.T) {
	store := &fakeUISettingsStore{
		getSnapshot: appconfig.SettingsSnapshot{
			Timeout:            81,
			Retries:            4,
			ImageConcurrency:   13,
			DownloadActionMode: "komga_copy",
		},
	}
	router := NewRouterWithConfig(Config{
		Settings: Settings{
			ImageConcurrency:   3,
			Timeout:            30,
			Retries:            10,
			DownloadActionMode: "browser",
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
	assertContains(t, body, `name="download_action_mode" value="komga_copy" checked`)
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

func TestStaticDistRouteDisablesBrowserCacheForStableBundleNames(t *testing.T) {
	staticDir := t.TempDir()
	distDir := filepath.Join(staticDir, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatalf("create dist dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "logs.bundle.js"), []byte("console.log('fresh')"), 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	router := NewRouterWithConfig(Config{
		StaticDir: staticDir,
	})
	req := httptest.NewRequest(http.MethodGet, "/static/dist/logs.bundle.js", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
		t.Fatalf("expected Cache-Control to disable stale bundle reuse, got %q", got)
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

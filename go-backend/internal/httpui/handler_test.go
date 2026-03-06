package httpui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
			AllowedDomains: []string{"example.com", "demo.test"},
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
	assertContains(t, body, "example.com, demo.test")
	assertContains(t, body, ">456<")
	assertContains(t, body, "32 MB")
	assertContains(t, body, "1024 MB")
	assertContains(t, body, `href="/" hx-get="/" hx-target="#content" hx-push-url="true"`)
	assertContains(t, body, `href="/logs" hx-get="/logs" hx-target="#content" hx-push-url="true"`)
	assertContains(t, body, `href="/settings" hx-get="/settings" hx-target="#content" hx-push-url="true"`)
	assertContains(t, body, `id="startup-recovery-link-home"`)
	assertContains(t, body, `hx-get="/logs"`)
	assertContains(t, body, `hx-target="#content"`)
	assertContains(t, body, `hx-push-url="true"`)
}

func TestHTMXRequestReturnsPageFragment(t *testing.T) {
	router := NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	req.Header.Set("HX-Request", "true")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	body := recorder.Body.String()
	assertContains(t, body, "下载日志")
	assertNotContains(t, body, "<!DOCTYPE html>")
	assertNotContains(t, body, "<html")
	assertNotContains(t, body, `<nav class="main-nav">`)
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

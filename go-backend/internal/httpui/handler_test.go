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

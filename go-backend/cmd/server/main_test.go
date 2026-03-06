package main

import (
	"reflect"
	"testing"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/config"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/httpui"
)

func TestBuildUIConfigUsesRuntimeValues(t *testing.T) {
	t.Setenv("GO_UI_STATIC_DIR", "")
	t.Setenv("UI_STATIC_DIR", "")
	t.Setenv("STATIC_DIR", "")

	input := config.Config{
		DownloadTimeout:  88,
		DownloadRetries:  0,
		ImageConcurrency: 6,
	}

	got := buildUIConfig(input)
	want := httpui.Config{
		Settings: httpui.Settings{
			TaskConcurrency:   2,
			ImageConcurrency:  6,
			Timeout:           88,
			Retries:           0,
			LogRetentionDays:  7,
			FileRetentionDays: 7,
		},
		Guardrails: httpui.Guardrails{
			AllowedDomains: []string{"telegra.ph", "www.telegra.ph", "graph.org", "www.graph.org"},
			MaxImages:      300,
			MaxImageBytes:  25 * 1024 * 1024,
			MaxTotalBytes:  500 * 1024 * 1024,
		},
		StaticDir: "/app/static",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected ui config:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBuildUIConfigUsesStaticDirFromEnv(t *testing.T) {
	t.Setenv("GO_UI_STATIC_DIR", "/tmp/go-ui-static")
	t.Setenv("UI_STATIC_DIR", "/tmp/ui-static")
	t.Setenv("STATIC_DIR", "/tmp/fallback-static")

	got := buildUIConfig(config.Config{})
	if got.StaticDir != "/tmp/go-ui-static" {
		t.Fatalf("expected static dir from GO_UI_STATIC_DIR, got %q", got.StaticDir)
	}
}

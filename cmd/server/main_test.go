package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/httpui"
	"github.com/ryancheng/telegram-downloader/internal/httpv2"
	"github.com/ryancheng/telegram-downloader/internal/queue"
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

	got := buildUIConfig(input, nil)
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

	got := buildUIConfig(config.Config{}, nil)
	if got.StaticDir != "/tmp/go-ui-static" {
		t.Fatalf("expected static dir from GO_UI_STATIC_DIR, got %q", got.StaticDir)
	}
}

func TestBuildUIConfigInjectsSettingsStore(t *testing.T) {
	store := &fakeSettingsStoreForUIConfig{}
	got := buildUIConfig(config.Config{}, store)
	if got.SettingsStore != store {
		t.Fatalf("expected settings store to be injected")
	}
}

func TestBuildLegacyRouterOptionsInjectsV2Dependencies(t *testing.T) {
	t.Setenv("KOMGA_LIBRARY_ROOT", "")
	cfg := config.Config{
		UpstreamBaseURL:  "http://python.local",
		InternalToken:    "secret-token",
		DownloadTimeout:  66,
		DownloadRetries:  7,
		ImageConcurrency: 5,
	}
	legacyQueue := &fakeDownloadQueueForLegacyRouterOptions{}
	v2Store := httpv2.NewPostgresTaskStore(nil)
	v2Queue := httpv2.NewV2TaskQueue(nil)

	got := buildLegacyRouterOptions(cfg, legacyQueue, v2Store, v2Queue)

	if got.V2TaskStore != v2Store {
		t.Fatalf("expected v2 task store to be injected")
	}
	if got.V2TaskQueue != v2Queue {
		t.Fatalf("expected v2 task queue to be injected")
	}
	if got.V2ArtifactService == nil {
		t.Fatalf("expected v2 artifact service to be injected")
	}
	if got.DownloadQueue != legacyQueue {
		t.Fatalf("expected legacy download queue to be preserved")
	}
	if got.UpstreamBaseURL != cfg.UpstreamBaseURL {
		t.Fatalf("expected upstream base url=%q, got %q", cfg.UpstreamBaseURL, got.UpstreamBaseURL)
	}
	if got.InternalToken != cfg.InternalToken {
		t.Fatalf("expected internal token=%q, got %q", cfg.InternalToken, got.InternalToken)
	}
	if got.DownloadTimeout != cfg.DownloadTimeout {
		t.Fatalf("expected timeout=%d, got %d", cfg.DownloadTimeout, got.DownloadTimeout)
	}
	if got.DownloadRetries != cfg.DownloadRetries {
		t.Fatalf("expected retries=%d, got %d", cfg.DownloadRetries, got.DownloadRetries)
	}
	if got.ImageConcurrency != cfg.ImageConcurrency {
		t.Fatalf("expected image_concurrency=%d, got %d", cfg.ImageConcurrency, got.ImageConcurrency)
	}
	if got.KomgaRootDir != "/app/komga/myReadingManga" {
		t.Fatalf("expected default komga root dir /app/komga/myReadingManga, got %q", got.KomgaRootDir)
	}
	if !got.DisableRootRoutes {
		t.Fatalf("expected DisableRootRoutes=true")
	}
}

func TestBuildLegacyRouterOptionsUsesKomgaRootDirFromEnv(t *testing.T) {
	t.Setenv("KOMGA_LIBRARY_ROOT", "/app/custom-komga")

	got := buildLegacyRouterOptions(config.Config{}, &fakeDownloadQueueForLegacyRouterOptions{}, httpv2.NewPostgresTaskStore(nil), httpv2.NewV2TaskQueue(nil))

	if got.KomgaRootDir != "/app/custom-komga" {
		t.Fatalf("expected komga root from env, got %q", got.KomgaRootDir)
	}
}

type fakeSettingsStoreForUIConfig struct{}

func (f *fakeSettingsStoreForUIConfig) GetSettings(_ context.Context) (config.SettingsSnapshot, error) {
	return config.SettingsSnapshot{}, nil
}

func (f *fakeSettingsStoreForUIConfig) UpdateSettings(_ context.Context, snapshot config.SettingsSnapshot) (config.SettingsSnapshot, error) {
	return snapshot, nil
}

type fakeDownloadQueueForLegacyRouterOptions struct{}

func (f *fakeDownloadQueueForLegacyRouterOptions) EnqueueDownload(_ context.Context, _ queue.EnqueueMessage) error {
	return nil
}

package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/httpui"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
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
			ImageConcurrency: 6,
			Timeout:          88,
			Retries:          0,
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
	uploadTaskStore := postgres.NewUploadTaskStore(nil)

	got := buildLegacyRouterOptions(cfg, uploadTaskStore)

	if got.UploadTaskStore != uploadTaskStore {
		t.Fatalf("expected upload task store to be injected")
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

	got := buildLegacyRouterOptions(config.Config{}, postgres.NewUploadTaskStore(nil))

	if got.KomgaRootDir != "/app/custom-komga" {
		t.Fatalf("expected komga root from env, got %q", got.KomgaRootDir)
	}
}

func TestBuildLegacyRouterOptionsUsesSharedUploadTempDir(t *testing.T) {
	t.Setenv("TEMP_PATH", "")

	got := buildLegacyRouterOptions(config.Config{}, postgres.NewUploadTaskStore(nil))

	if got.UploadTempDir != "/app/temp_downloads" {
		t.Fatalf("expected taskcore upload temp dir /app/temp_downloads, got %q", got.UploadTempDir)
	}
}

func TestBuildLegacyRouterOptionsUsesTempPathFromEnv(t *testing.T) {
	t.Setenv("TEMP_PATH", "/app/shared-temp")

	got := buildLegacyRouterOptions(config.Config{}, postgres.NewUploadTaskStore(nil))

	if got.UploadTempDir != "/app/shared-temp" {
		t.Fatalf("expected upload temp dir from TEMP_PATH, got %q", got.UploadTempDir)
	}
}

type fakeSettingsStoreForUIConfig struct{}

func (f *fakeSettingsStoreForUIConfig) GetSettings(_ context.Context) (config.SettingsSnapshot, error) {
	return config.SettingsSnapshot{}, nil
}

func (f *fakeSettingsStoreForUIConfig) UpdateSettings(_ context.Context, snapshot config.SettingsSnapshot) (config.SettingsSnapshot, error) {
	return snapshot, nil
}

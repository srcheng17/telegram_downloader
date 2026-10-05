package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/httpapi"
	"github.com/ryancheng/telegram-downloader/internal/httpui"
	pgmigrations "github.com/ryancheng/telegram-downloader/internal/store/postgres/migrations"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("create postgres pool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping postgres: %v", err)
	}
	if err := pgmigrations.Run(ctx, pool); err != nil {
		log.Fatalf("run postgres migrations: %v", err)
	}

	rootRouter, err := buildWorkspaceRouter(ctx, pool, cfg)
	if err != nil {
		log.Fatalf("initialize protected workspace: %v", err)
	}

	server := &http.Server{
		Addr:         cfg.Addr,
		Handler:      rootRouter,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	go func() {
		log.Printf("go-backend listening on %s", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

func buildUIConfig(cfg config.Config, settingsStore httpui.SettingsStore) httpui.Config {
	return httpui.Config{
		Settings: httpui.Settings{
			ImageConcurrency: cfg.ImageConcurrency,
			Timeout:          cfg.DownloadTimeout,
			Retries:          cfg.DownloadRetries,
		},
		Guardrails: httpui.Guardrails{
			AllowedDomains: []string{"telegra.ph", "www.telegra.ph", "graph.org", "www.graph.org"},
			MaxImages:      config.MaxImagesPerTask,
			MaxImageBytes:  config.MaxBytesPerImage,
			MaxTotalBytes:  config.MaxBytesPerTask,
		},
		StaticDir:     resolveUIStaticDir(),
		SettingsStore: settingsStore,
	}
}

func resolveUIStaticDir() string {
	candidates := []string{
		os.Getenv("GO_UI_STATIC_DIR"),
		os.Getenv("UI_STATIC_DIR"),
		os.Getenv("STATIC_DIR"),
	}
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" {
			return trimmed
		}
	}
	return "/app/static"
}

func buildLegacyRouterOptions(
	cfg config.Config,
	uploadTaskStore httpapi.UploadTaskStore,
) httpapi.RouterOptions {
	return httpapi.RouterOptions{
		UpstreamBaseURL:   cfg.UpstreamBaseURL,
		InternalToken:     cfg.InternalToken,
		DisableRootRoutes: true,
		DownloadTimeout:   cfg.DownloadTimeout,
		DownloadRetries:   cfg.DownloadRetries,
		ImageConcurrency:  cfg.ImageConcurrency,
		UploadTempDir:     resolveUploadTempDir(),
		KomgaRootDir:      resolveKomgaRootDir(),
		UploadTaskStore:   uploadTaskStore,
	}
}

func resolveUploadTempDir() string {
	if candidate := strings.TrimSpace(os.Getenv("TEMP_PATH")); candidate != "" {
		return candidate
	}
	return "/app/temp_downloads"
}

func resolveKomgaRootDir() string {
	if candidate := strings.TrimSpace(os.Getenv("KOMGA_LIBRARY_ROOT")); candidate != "" {
		return candidate
	}
	return "/app/komga/myReadingManga"
}

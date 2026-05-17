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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apptaskcore "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/httpapi"
	"github.com/ryancheng/telegram-downloader/internal/httpui"
	"github.com/ryancheng/telegram-downloader/internal/httpv2"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
	pgmigrations "github.com/ryancheng/telegram-downloader/internal/store/postgres/migrations"
	pgtaskcore "github.com/ryancheng/telegram-downloader/internal/store/postgres/taskcore"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
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

	store := postgres.NewStore(pool)
	v2Store := httpv2.NewPostgresTaskStore(pool)
	uploadTaskStore := postgres.NewUploadTaskStore(pool)
	taskCoreStore := pgtaskcore.NewStore(pool)
	taskCoreService := apptaskcore.NewService(taskCoreStore, apptaskcore.Config{LeaseTTL: 30 * time.Second, MaxAttempts: 3})
	if cfg.UpstreamBaseURL == "" {
		log.Printf("PYTHON_WEB_BASE_URL not set, go-api will use local defaults for runtime settings")
	}

	legacyRouterOptions := buildLegacyRouterOptions(cfg, uploadTaskStore)
	legacyRouterOptions.TaskCoreService = taskCoreService
	legacyRouterOptions.ReadyzChecker = func(ctx context.Context) (bool, error) {
		pending, err := pgmigrations.PendingCount(ctx, pool)
		if err != nil {
			return false, err
		}
		return pending == 0, nil
	}
	legacyRouter := httpapi.NewRouterWithOptions(store, legacyRouterOptions)
	rootRouter := chi.NewRouter()
	httpui.RegisterRoutesWithConfig(rootRouter, buildUIConfig(cfg, v2Store))
	httpv2.RegisterSettings(rootRouter, httpv2.NewSettingsHandler(v2Store))
	rootRouter.Mount("/", legacyRouter)

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

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

func buildUIConfig(cfg config.Config, settingsStore httpui.SettingsStore) httpui.Config {
	return httpui.Config{
		Settings: httpui.Settings{
			TaskConcurrency:   2,
			ImageConcurrency:  cfg.ImageConcurrency,
			Timeout:           cfg.DownloadTimeout,
			Retries:           cfg.DownloadRetries,
			LogRetentionDays:  7,
			FileRetentionDays: 7,
		},
		Guardrails: httpui.Guardrails{
			AllowedDomains: []string{"telegra.ph", "www.telegra.ph", "graph.org", "www.graph.org"},
			MaxImages:      300,
			MaxImageBytes:  25 * 1024 * 1024,
			MaxTotalBytes:  500 * 1024 * 1024,
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

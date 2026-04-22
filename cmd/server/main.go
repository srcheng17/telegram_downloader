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
	"github.com/redis/go-redis/v9"

	apptaskcore "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/httpapi"
	"github.com/ryancheng/telegram-downloader/internal/httpui"
	"github.com/ryancheng/telegram-downloader/internal/httpv2"
	"github.com/ryancheng/telegram-downloader/internal/queue"
	"github.com/ryancheng/telegram-downloader/internal/queue/redisstream"
	queuev2 "github.com/ryancheng/telegram-downloader/internal/queue/v2"
	"github.com/ryancheng/telegram-downloader/internal/service"
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

	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatalf("parse redis url: %v", err)
	}
	redisClient := redis.NewClient(redisOptions)
	defer func() {
		_ = redisClient.Close()
	}()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("ping redis: %v", err)
	}

	store := postgres.NewStore(pool)
	downloadQueue := redisstream.NewProducer(redisClient, cfg.StreamName)
	v2Store := httpv2.NewPostgresTaskStore(pool)
	v2Queue := httpv2.NewV2TaskQueue(queuev2.NewProducer(redisClient, cfg.V2StreamName))
	taskCoreStore := pgtaskcore.NewStore(pool)
	taskCoreService := apptaskcore.NewService(taskCoreStore, apptaskcore.Config{LeaseTTL: 30 * time.Second, MaxAttempts: 3})
	if cfg.UpstreamBaseURL == "" {
		log.Printf("PYTHON_WEB_BASE_URL not set, go-api will use local defaults for runtime settings")
	}

	legacyRouterOptions := buildLegacyRouterOptions(cfg, downloadQueue, v2Store, v2Queue)
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
	httpv2.RegisterRoutes(rootRouter, httpv2.NewTasksHandler(v2Store, v2Queue))
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
	downloadQueue queue.DownloadQueue,
	v2Store httpapi.LegacyV2TaskStore,
	v2Queue httpapi.LegacyV2TaskQueue,
) httpapi.RouterOptions {
	return httpapi.RouterOptions{
		UpstreamBaseURL:   cfg.UpstreamBaseURL,
		InternalToken:     cfg.InternalToken,
		DisableRootRoutes: true,
		DownloadQueue:     downloadQueue,
		DownloadTimeout:   cfg.DownloadTimeout,
		DownloadRetries:   cfg.DownloadRetries,
		ImageConcurrency:  cfg.ImageConcurrency,
		KomgaRootDir:      resolveKomgaRootDir(),
		V2TaskStore:       v2Store,
		V2TaskQueue:       v2Queue,
		V2ArtifactService: service.NewV2ArtifactService(service.V2ArtifactServiceConfig{}),
	}
}

func resolveKomgaRootDir() string {
	if candidate := strings.TrimSpace(os.Getenv("KOMGA_LIBRARY_ROOT")); candidate != "" {
		return candidate
	}
	return "/app/komga/myReadingManga"
}

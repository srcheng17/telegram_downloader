package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/config"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/httpapi"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres"
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

	store := postgres.NewStore(pool)
	if cfg.UpstreamBaseURL == "" {
		log.Printf("PYTHON_WEB_BASE_URL not set, write endpoints will return 503")
	}
	server := &http.Server{
		Addr: cfg.Addr,
		Handler: httpapi.NewRouterWithOptions(
			store,
			httpapi.RouterOptions{
				UpstreamBaseURL:  cfg.UpstreamBaseURL,
				InternalToken:    cfg.InternalToken,
				DownloadTimeout:  cfg.DownloadTimeout,
				DownloadRetries:  cfg.DownloadRetries,
				ImageConcurrency: cfg.ImageConcurrency,
			},
		),
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

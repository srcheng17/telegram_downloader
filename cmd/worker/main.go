package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	appmetadata "github.com/ryancheng/telegram-downloader/internal/app/metadata"
	apptaskcore "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/downloader"
	appruntime "github.com/ryancheng/telegram-downloader/internal/runtime"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/metadatadoc"
	pgtaskcore "github.com/ryancheng/telegram-downloader/internal/store/postgres/taskcore"
	workertaskcore "github.com/ryancheng/telegram-downloader/internal/worker/taskcore"
)

const (
	defaultDownloadDir = "downloaded_images"
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

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "go-worker"
	}
	consumerName := strings.TrimSpace(cfg.ConsumerName)
	if consumerName == "" {
		consumerName = hostname
	}

	downloadRoot := strings.TrimSpace(os.Getenv("DOWNLOAD_PATH"))
	if downloadRoot == "" {
		downloadRoot = defaultDownloadDir
	}

	downloadService := &downloader.Service{
		HTTPClient: &http.Client{
			Timeout: time.Duration(cfg.DownloadTimeout) * time.Second,
		},
		DownloadRetries:  cfg.DownloadRetries,
		ImageConcurrency: cfg.ImageConcurrency,
		MaxImages:        config.MaxImagesPerTask,
		MaxImageBytes:    config.MaxBytesPerImage,
		MaxTotalBytes:    config.MaxBytesPerTask,
	}

	taskCoreStore := pgtaskcore.NewStore(pool)
	metadataStore := metadatadoc.NewStore(pool)
	taskCoreService := apptaskcore.NewService(taskCoreStore, apptaskcore.Config{LeaseTTL: 30 * time.Second, MaxAttempts: 3, MetadataEncoder: appmetadata.NewService(metadataStore)})
	telegram, err := appruntime.Telegram(ctx, pool, cfg)
	if err != nil {
		log.Fatalf("initialize Telegram runtime: %v", err)
	}
	var telegramDownloader workertaskcore.TelegramDownloader
	if telegram != nil {
		telegramDownloader = telegram
		defer telegram.Close()
	}
	taskCoreDownloader := workertaskcore.NewTaskDownloader(workertaskcore.TaskDownloaderConfig{
		Registry:        metadataStore,
		RetentionRoot:   cfg.SourceRetentionRoot,
		RecordRetention: taskCoreStore.RecordRetention,
		Telegram:        telegramDownloader,
		Tasks:           taskCoreService,
		Service:         downloadService,
		DownloadRoot:    downloadRoot,
	})
	taskCoreExecutor := &workertaskcore.Executor{
		Service:           taskCoreService,
		Downloader:        taskCoreDownloader,
		WorkerID:          consumerName,
		HeartbeatInterval: 5 * time.Second,
	}

	runCtx := ctx

	log.Printf(
		"go-worker running task-core executor consumer=%s",
		consumerName,
	)

	errCh := make(chan error, 2)
	go func() {
		errCh <- runTaskCoreExecutor(runCtx, taskCoreExecutor)
	}()
	go func() {
		errCh <- runTaskCoreRecovery(runCtx, taskCoreExecutor, 30*time.Second)
	}()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			stop()
			log.Fatalf("worker pipeline exited with error: %v", err)
		}
	}

	log.Printf("go-worker stopped")
}

func runTaskCoreExecutor(ctx context.Context, executor *workertaskcore.Executor) error {
	for {
		processed, err := executor.ProcessOne(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("task-core executor error: %v", err)
			if err := sleepOrDone(ctx, time.Second); err != nil {
				return nil
			}
			continue
		}
		if processed {
			continue
		}

		if err := sleepOrDone(ctx, time.Second); err != nil {
			return nil
		}
	}
}

func runTaskCoreRecovery(ctx context.Context, executor *workertaskcore.Executor, interval time.Duration) error {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := executor.RecoverExpired(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				log.Printf("task-core recovery error: %v", err)
			}
		}
	}
}

func sleepOrDone(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

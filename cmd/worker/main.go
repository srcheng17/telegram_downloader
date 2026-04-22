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
	"github.com/redis/go-redis/v9"

	apptaskcore "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/downloader"
	queuev2 "github.com/ryancheng/telegram-downloader/internal/queue/v2"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
	pgtaskcore "github.com/ryancheng/telegram-downloader/internal/store/postgres/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/worker"
	workertaskcore "github.com/ryancheng/telegram-downloader/internal/worker/taskcore"
)

const (
	maxImagesPerTask   = 300
	maxBytesPerImage   = 25 << 20
	maxBytesPerTask    = 500 << 20
	defaultDownloadDir = "downloaded_images"
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

	stream := worker.NewRedisStream(redisClient, cfg.StreamName)
	if err := stream.CreateGroup(ctx, cfg.ConsumerGroup); err != nil {
		log.Fatalf("create stream consumer group: %v", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "go-worker"
	}
	consumerName := strings.TrimSpace(cfg.ConsumerName)
	if consumerName == "" {
		consumerName = hostname
	}

	store := postgres.NewStore(pool)
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
		MaxImages:        maxImagesPerTask,
		MaxImageBytes:    maxBytesPerImage,
		MaxTotalBytes:    maxBytesPerTask,
	}

	taskDownloader := worker.NewTaskDownloader(worker.TaskDownloaderConfig{
		Store:        store,
		Service:      downloadService,
		DownloadRoot: downloadRoot,
	})

	taskCoreStore := pgtaskcore.NewStore(pool)
	taskCoreService := apptaskcore.NewService(taskCoreStore, apptaskcore.Config{LeaseTTL: 30 * time.Second, MaxAttempts: 3})
	taskCoreExecutor := &workertaskcore.Executor{
		Service:           taskCoreService,
		Downloader:        taskDownloader,
		WorkerID:          consumerName,
		HeartbeatInterval: 5 * time.Second,
	}

	executor := &worker.Executor{
		Store:      store,
		Downloader: taskDownloader,
	}

	consumer := worker.NewConsumer(worker.ConsumerConfig{
		Stream:   stream,
		Store:    store,
		Group:    cfg.ConsumerGroup,
		Consumer: consumerName,
		Block:    5 * time.Second,
		Executor: executor,
	})

	v2Repo := worker.NewV2PostgresExecutionRepo(pool, postgres.NewV2TaskRepo(pool))
	v2Executor := worker.NewV2Executor(worker.V2ExecutorConfig{
		Repo:           v2Repo,
		Worker:         consumerName,
		Download:       &worker.V2ServiceDownloader{Service: downloadService, DownloadRoot: downloadRoot, ProgressReporter: v2Repo},
		TransientRetry: cfg.DownloadRetries,
	})
	if err := ensureV2ConsumerGroup(ctx, redisClient, cfg.V2StreamName, cfg.ConsumerGroup); err != nil {
		log.Fatalf("create v2 stream consumer group: %v", err)
	}
	v2Consumer := queuev2.NewConsumer(redisClient, cfg.V2StreamName, cfg.ConsumerGroup, consumerName)

	runCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf(
		"go-worker consuming stream=%s and v2_stream=%s group=%s consumer=%s",
		cfg.StreamName,
		cfg.V2StreamName,
		cfg.ConsumerGroup,
		consumerName,
	)

	errCh := make(chan error, 4)
	go func() {
		errCh <- consumer.Run(runCtx)
	}()
	go func() {
		errCh <- v2Executor.Run(runCtx, v2Consumer)
	}()
	go func() {
		errCh <- runTaskCoreExecutor(runCtx, taskCoreExecutor)
	}()
	go func() {
		errCh <- runTaskCoreRecovery(runCtx, taskCoreExecutor, 30*time.Second)
	}()

	for i := 0; i < 4; i++ {
		if err := <-errCh; err != nil {
			stop()
			log.Fatalf("worker pipeline exited with error: %v", err)
		}
	}

	log.Printf("go-worker stopped")
}

func isBusyGroupErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToUpper(err.Error()), "BUSYGROUP")
}

func ensureV2ConsumerGroup(ctx context.Context, redisClient redis.Cmdable, streamName, groupName string) error {
	err := redisClient.XGroupCreateMkStream(ctx, strings.TrimSpace(streamName), strings.TrimSpace(groupName), "0").Err()
	if err != nil && !isBusyGroupErr(err) {
		return err
	}
	return nil
}

func runTaskCoreExecutor(ctx context.Context, executor *workertaskcore.Executor) error {
	for {
		processed, err := executor.ProcessOne(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if processed {
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
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
				return err
			}
		}
	}
}

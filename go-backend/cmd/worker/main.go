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

	"github.com/ryancheng/telegram-downloader/go-backend/internal/config"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/downloader"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/worker"
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

	taskDownloader := worker.NewTaskDownloader(worker.TaskDownloaderConfig{
		Store: store,
		Service: &downloader.Service{
			HTTPClient: &http.Client{
				Timeout: time.Duration(cfg.DownloadTimeout) * time.Second,
			},
			MaxImages:     maxImagesPerTask,
			MaxImageBytes: maxBytesPerImage,
			MaxTotalBytes: maxBytesPerTask,
		},
		DownloadRoot: downloadRoot,
	})

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

	runCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf(
		"go-worker consuming stream=%s group=%s consumer=%s",
		cfg.StreamName,
		cfg.ConsumerGroup,
		consumerName,
	)

	if err := consumer.Run(runCtx); err != nil {
		log.Fatalf("worker consumer exited with error: %v", err)
	}

	log.Printf("go-worker stopped")
}

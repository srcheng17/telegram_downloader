package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/config"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/worker"
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
	consumerName := fmt.Sprintf("%s-%d", hostname, os.Getpid())

	consumer := worker.NewConsumer(worker.ConsumerConfig{
		Stream:   stream,
		Store:    postgres.NewStore(pool),
		Group:    cfg.ConsumerGroup,
		Consumer: consumerName,
		Block:    5 * time.Second,
		Handler: func(_ context.Context, msg worker.Message) error {
			return fmt.Errorf("executor not configured for task %s", msg.TaskID)
		},
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

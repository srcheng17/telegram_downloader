package main

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	queuev2 "github.com/ryancheng/telegram-downloader/go-backend/internal/queue/v2"
)

func TestEnsureV2ConsumerGroupStartsAtZeroToConsumeBacklog(t *testing.T) {
	ctx := context.Background()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.XAdd(ctx, &redis.XAddArgs{
		Stream: "download_tasks_v2",
		Values: map[string]any{
			"task_id": "task-backlog-1",
			"token":   "token-backlog-1",
		},
	}).Result()
	if err != nil {
		t.Fatalf("seed v2 stream backlog: %v", err)
	}

	if err := ensureV2ConsumerGroup(ctx, client, "download_tasks_v2", "go-workers"); err != nil {
		t.Fatalf("ensure v2 consumer group: %v", err)
	}

	consumer := queuev2.NewConsumer(client, "download_tasks_v2", "go-workers", "worker-a")
	messages, err := consumer.ReadGroup(ctx, 1, time.Millisecond)
	if err != nil {
		t.Fatalf("read group: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one backlog message, got %d", len(messages))
	}
	if messages[0].TaskID != "task-backlog-1" {
		t.Fatalf("expected task_id task-backlog-1, got %q", messages[0].TaskID)
	}
}

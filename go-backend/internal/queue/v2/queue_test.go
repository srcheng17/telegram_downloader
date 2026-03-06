package v2

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestProducerWritesTaskMessage(t *testing.T) {
	ctx := context.Background()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	producer := NewProducer(client, "v2-tasks")
	err = producer.Produce(ctx, TaskMessage{
		TaskID: "task-v2-1",
		Token:  "token-v2-1",
	})
	if err != nil {
		t.Fatalf("produce: %v", err)
	}

	entries, err := client.XRange(ctx, "v2-tasks", "-", "+").Result()
	if err != nil {
		t.Fatalf("xrange entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 stream entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Values["task_id"] != "task-v2-1" {
		t.Fatalf("expected task_id task-v2-1, got %#v", entry.Values["task_id"])
	}
	if entry.Values["token"] != "token-v2-1" {
		t.Fatalf("expected token token-v2-1, got %#v", entry.Values["token"])
	}
}

func TestConsumerParsesTaskMessage(t *testing.T) {
	ctx := context.Background()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.XAdd(ctx, &redis.XAddArgs{
		Stream: "v2-tasks",
		Values: map[string]any{
			"task_id": "task-v2-2",
			"token":   "token-v2-2",
		},
	}).Result()
	if err != nil {
		t.Fatalf("seed stream: %v", err)
	}

	consumer := NewConsumer(client, "v2-tasks")
	messages, err := consumer.Read(ctx, 1, time.Millisecond)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	message := messages[0]
	if message.TaskID != "task-v2-2" {
		t.Fatalf("expected task id task-v2-2, got %q", message.TaskID)
	}
	if message.Token != "token-v2-2" {
		t.Fatalf("expected token token-v2-2, got %q", message.Token)
	}
}

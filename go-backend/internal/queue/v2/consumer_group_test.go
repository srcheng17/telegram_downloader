package v2

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestConsumerReadsViaGroupAndAcks(t *testing.T) {
	ctx := context.Background()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	if err := client.XGroupCreateMkStream(ctx, "v2-tasks", "go-workers", "$").Err(); err != nil {
		t.Fatalf("create consumer group: %v", err)
	}
	messageID, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: "v2-tasks",
		Values: map[string]any{
			"task_id": "task-v2-ack",
			"token":   "token-v2-ack",
		},
	}).Result()
	if err != nil {
		t.Fatalf("seed stream: %v", err)
	}

	consumer := NewConsumer(client, "v2-tasks", "go-workers", "worker-a")
	messages, err := consumer.ReadGroup(ctx, 1, time.Millisecond)
	if err != nil {
		t.Fatalf("read group: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %d", len(messages))
	}
	if messages[0].MessageID != messageID {
		t.Fatalf("expected message id %q, got %q", messageID, messages[0].MessageID)
	}
	if messages[0].TaskID != "task-v2-ack" {
		t.Fatalf("expected task id task-v2-ack, got %q", messages[0].TaskID)
	}
	if messages[0].Token != "token-v2-ack" {
		t.Fatalf("expected token token-v2-ack, got %q", messages[0].Token)
	}

	pendingBeforeAck, err := client.XPending(ctx, "v2-tasks", "go-workers").Result()
	if err != nil {
		t.Fatalf("read pending before ack: %v", err)
	}
	if pendingBeforeAck.Count != 1 {
		t.Fatalf("expected pending count 1 before ack, got %d", pendingBeforeAck.Count)
	}

	if err := consumer.Ack(ctx, messages[0].MessageID); err != nil {
		t.Fatalf("ack message: %v", err)
	}
	pendingAfterAck, err := client.XPending(ctx, "v2-tasks", "go-workers").Result()
	if err != nil {
		t.Fatalf("read pending after ack: %v", err)
	}
	if pendingAfterAck.Count != 0 {
		t.Fatalf("expected pending count 0 after ack, got %d", pendingAfterAck.Count)
	}
}

func TestConsumerReclaimsPendingMessage(t *testing.T) {
	ctx := context.Background()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	if err := client.XGroupCreateMkStream(ctx, "v2-tasks", "go-workers", "$").Err(); err != nil {
		t.Fatalf("create consumer group: %v", err)
	}
	_, err = client.XAdd(ctx, &redis.XAddArgs{
		Stream: "v2-tasks",
		Values: map[string]any{
			"task_id": "task-v2-reclaim",
			"token":   "token-v2-reclaim",
		},
	}).Result()
	if err != nil {
		t.Fatalf("seed stream: %v", err)
	}

	consumerA := NewConsumer(client, "v2-tasks", "go-workers", "worker-a")
	readByA, err := consumerA.ReadGroup(ctx, 1, time.Millisecond)
	if err != nil {
		t.Fatalf("consumer A read group: %v", err)
	}
	if len(readByA) != 1 {
		t.Fatalf("expected consumer A to read one message, got %d", len(readByA))
	}

	consumerB := NewConsumer(client, "v2-tasks", "go-workers", "worker-b")
	reclaimed, err := consumerB.ClaimPending(ctx, 0, 1)
	if err != nil {
		t.Fatalf("consumer B reclaim pending: %v", err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("expected consumer B to reclaim one message, got %d", len(reclaimed))
	}
	if reclaimed[0].MessageID != readByA[0].MessageID {
		t.Fatalf("expected reclaimed message id %q, got %q", readByA[0].MessageID, reclaimed[0].MessageID)
	}
	if reclaimed[0].TaskID != "task-v2-reclaim" {
		t.Fatalf("expected reclaimed task id task-v2-reclaim, got %q", reclaimed[0].TaskID)
	}
	if reclaimed[0].Token != "token-v2-reclaim" {
		t.Fatalf("expected reclaimed token token-v2-reclaim, got %q", reclaimed[0].Token)
	}

	if err := consumerB.Ack(ctx, reclaimed[0].MessageID); err != nil {
		t.Fatalf("ack reclaimed message: %v", err)
	}
	pendingAfterAck, err := client.XPending(ctx, "v2-tasks", "go-workers").Result()
	if err != nil {
		t.Fatalf("read pending after ack: %v", err)
	}
	if pendingAfterAck.Count != 0 {
		t.Fatalf("expected pending count 0 after ack, got %d", pendingAfterAck.Count)
	}
}

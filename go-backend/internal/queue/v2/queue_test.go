package v2

import (
	"context"
	"strings"
	"sync"
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

func TestProducerRejectsEmptyTaskMessageFields(t *testing.T) {
	ctx := context.Background()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	producer := NewProducer(client, "v2-tasks")

	tests := []struct {
		name    string
		msg     TaskMessage
		wantErr string
	}{
		{
			name:    "empty task id",
			msg:     TaskMessage{Token: "token-v2"},
			wantErr: "task_id is required",
		},
		{
			name:    "empty token",
			msg:     TaskMessage{TaskID: "task-v2"},
			wantErr: "token is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := producer.Produce(ctx, tt.msg)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error to contain %q, got %q", tt.wantErr, err.Error())
			}
		})
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

func TestConsumerReadAdvancesCursorWithoutDuplicates(t *testing.T) {
	ctx := context.Background()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	for i := 1; i <= 2; i++ {
		_, err = client.XAdd(ctx, &redis.XAddArgs{
			Stream: "v2-tasks",
			Values: map[string]any{
				"task_id": "task-v2-" + string(rune('0'+i)),
				"token":   "token-v2-" + string(rune('0'+i)),
			},
		}).Result()
		if err != nil {
			t.Fatalf("seed stream message %d: %v", i, err)
		}
	}

	consumer := NewConsumer(client, "v2-tasks")

	firstBatch, err := consumer.Read(ctx, 1, time.Millisecond)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if len(firstBatch) != 1 {
		t.Fatalf("expected first batch size 1, got %d", len(firstBatch))
	}
	if firstBatch[0].TaskID != "task-v2-1" {
		t.Fatalf("expected first task task-v2-1, got %q", firstBatch[0].TaskID)
	}

	secondBatch, err := consumer.Read(ctx, 1, time.Millisecond)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if len(secondBatch) != 1 {
		t.Fatalf("expected second batch size 1, got %d", len(secondBatch))
	}
	if secondBatch[0].TaskID != "task-v2-2" {
		t.Fatalf("expected second task task-v2-2, got %q", secondBatch[0].TaskID)
	}
}

func TestConsumerReadConcurrentCallsDoNotDuplicateOrRollback(t *testing.T) {
	ctx := context.Background()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	for i := 1; i <= 2; i++ {
		_, err = client.XAdd(ctx, &redis.XAddArgs{
			Stream: "v2-tasks",
			Values: map[string]any{
				"task_id": "task-v2-" + string(rune('0'+i)),
				"token":   "token-v2-" + string(rune('0'+i)),
			},
		}).Result()
		if err != nil {
			t.Fatalf("seed stream message %d: %v", i, err)
		}
	}

	gatedClient := &gatedXReadCmdable{
		Cmdable:           client,
		waitForSecondRead: 200 * time.Millisecond,
	}

	consumer := NewConsumer(gatedClient, "v2-tasks")

	type readResult struct {
		messages []TaskMessage
		err      error
	}

	results := make(chan readResult, 2)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			messages, err := consumer.Read(ctx, 1, 5*time.Millisecond)
			results <- readResult{messages: messages, err: err}
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	taskIDs := make([]string, 0, 2)
	for result := range results {
		if result.err != nil {
			t.Fatalf("read failed: %v", result.err)
		}
		if len(result.messages) != 1 {
			t.Fatalf("expected each read to return one message, got %d", len(result.messages))
		}
		taskIDs = append(taskIDs, result.messages[0].TaskID)
	}

	seen := map[string]int{}
	for _, taskID := range taskIDs {
		seen[taskID]++
	}

	if seen["task-v2-1"] != 1 {
		t.Fatalf("expected task-v2-1 consumed once, got counts %#v", seen)
	}
	if seen["task-v2-2"] != 1 {
		t.Fatalf("expected task-v2-2 consumed once, got counts %#v", seen)
	}
}

func TestConsumerReadReturnsEmptySliceWhenNoMessage(t *testing.T) {
	ctx := context.Background()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	consumer := NewConsumer(client, "v2-empty-tasks")
	messages, err := consumer.Read(ctx, 1, time.Millisecond)
	if err != nil {
		t.Fatalf("expected nil error when no message, got %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected empty messages, got %d", len(messages))
	}
}

type gatedXReadCmdable struct {
	redis.Cmdable
	waitForSecondRead time.Duration

	mu         sync.Mutex
	xReadCalls int

	secondReadReached sync.Once
	secondReadSignal  chan struct{}
}

func (g *gatedXReadCmdable) XRead(ctx context.Context, args *redis.XReadArgs) *redis.XStreamSliceCmd {
	g.mu.Lock()
	g.xReadCalls++
	call := g.xReadCalls
	g.mu.Unlock()

	if call == 1 {
		select {
		case <-g.ensureSecondReadSignal():
		case <-time.After(g.waitForSecondRead):
		case <-ctx.Done():
		}
	}

	if call == 2 {
		g.secondReadReached.Do(func() { close(g.ensureSecondReadSignal()) })
	}

	return g.Cmdable.XRead(ctx, args)
}

func (g *gatedXReadCmdable) ensureSecondReadSignal() chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.secondReadSignal == nil {
		g.secondReadSignal = make(chan struct{})
	}

	return g.secondReadSignal
}

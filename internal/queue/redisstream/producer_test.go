package redisstream

import (
	"context"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/ryancheng/telegram-downloader/internal/queue"
)

func TestProducerEnqueueDownloadValidation(t *testing.T) {
	ctx := context.Background()
	validMessage := queue.EnqueueMessage{
		TaskID:       "task-1",
		EnqueueToken: "token-1",
	}

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	tests := []struct {
		name    string
		p       *Producer
		msg     queue.EnqueueMessage
		wantErr string
	}{
		{
			name:    "missing redis client",
			p:       NewProducer(nil, "download-jobs"),
			msg:     validMessage,
			wantErr: "requires redis client",
		},
		{
			name:    "missing stream name",
			p:       NewProducer(client, "  "),
			msg:     validMessage,
			wantErr: "requires stream name",
		},
		{
			name:    "missing task id",
			p:       NewProducer(client, "download-jobs"),
			msg:     queue.EnqueueMessage{EnqueueToken: "token-1"},
			wantErr: "task_id is required",
		},
		{
			name:    "missing enqueue token",
			p:       NewProducer(client, "download-jobs"),
			msg:     queue.EnqueueMessage{TaskID: "task-1"},
			wantErr: "enqueue_token is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.EnqueueDownload(ctx, tt.msg)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error to contain %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestProducerEnqueueDownloadWritesTaskIDAndToken(t *testing.T) {
	ctx := context.Background()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	p := NewProducer(client, "download-jobs")
	err = p.EnqueueDownload(ctx, queue.EnqueueMessage{
		TaskID:       "task-queue-1",
		EnqueueToken: "token-queue-1",
	})
	if err != nil {
		t.Fatalf("enqueue download: %v", err)
	}

	entries, err := client.XRange(ctx, "download-jobs", "-", "+").Result()
	if err != nil {
		t.Fatalf("xrange entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one stream entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Values["task_id"] != "task-queue-1" {
		t.Fatalf("expected task_id task-queue-1, got %#v", entry.Values["task_id"])
	}
	if entry.Values["enqueue_token"] != "token-queue-1" {
		t.Fatalf("expected enqueue_token token-queue-1, got %#v", entry.Values["enqueue_token"])
	}
}

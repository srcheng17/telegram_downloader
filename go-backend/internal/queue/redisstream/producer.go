package redisstream

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/queue"
)

type Producer struct {
	redisClient redis.Cmdable
	streamName  string
}

func NewProducer(redisClient redis.Cmdable, streamName string) *Producer {
	return &Producer{
		redisClient: redisClient,
		streamName:  strings.TrimSpace(streamName),
	}
}

func (p *Producer) EnqueueDownload(ctx context.Context, msg queue.EnqueueMessage) error {
	if p.redisClient == nil {
		return errors.New("redis stream producer requires redis client")
	}
	if p.streamName == "" {
		return errors.New("redis stream producer requires stream name")
	}
	if strings.TrimSpace(msg.TaskID) == "" {
		return errors.New("enqueue message task_id is required")
	}
	if strings.TrimSpace(msg.EnqueueToken) == "" {
		return errors.New("enqueue message enqueue_token is required")
	}

	if _, err := p.redisClient.XAdd(ctx, &redis.XAddArgs{
		Stream: p.streamName,
		Values: map[string]any{
			"task_id":       msg.TaskID,
			"enqueue_token": msg.EnqueueToken,
		},
	}).Result(); err != nil {
		return fmt.Errorf("enqueue download message: %w", err)
	}

	return nil
}

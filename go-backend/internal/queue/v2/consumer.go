package v2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Consumer struct {
	redisClient redis.Cmdable
	streamName  string
}

func NewConsumer(redisClient redis.Cmdable, streamName string) *Consumer {
	return &Consumer{
		redisClient: redisClient,
		streamName:  strings.TrimSpace(streamName),
	}
}

func (c *Consumer) Read(ctx context.Context, count int64, block time.Duration) ([]TaskMessage, error) {
	if c.redisClient == nil {
		return nil, errors.New("v2 queue consumer requires redis client")
	}
	if c.streamName == "" {
		return nil, errors.New("v2 queue consumer requires stream name")
	}
	if count <= 0 {
		count = 1
	}

	streams, err := c.redisClient.XRead(ctx, &redis.XReadArgs{
		Streams: []string{c.streamName, "0"},
		Count:   count,
		Block:   block,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("read task messages: %w", err)
	}

	messages := make([]TaskMessage, 0)
	for _, stream := range streams {
		for _, raw := range stream.Messages {
			messages = append(messages, TaskMessage{
				TaskID: valueAsString(raw.Values["task_id"]),
				Token:  valueAsString(raw.Values["token"]),
			})
		}
	}
	return messages, nil
}

func valueAsString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(typed)
	}
}

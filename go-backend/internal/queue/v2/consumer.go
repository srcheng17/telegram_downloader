package v2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const initialConsumerCursor = "0-0"

type Consumer struct {
	redisClient redis.Cmdable
	streamName  string
	mu          sync.Mutex
	lastID      string
}

func NewConsumer(redisClient redis.Cmdable, streamName string) *Consumer {
	return &Consumer{
		redisClient: redisClient,
		streamName:  strings.TrimSpace(streamName),
		lastID:      initialConsumerCursor,
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
		Streams: []string{c.streamName, c.cursor()},
		Count:   count,
		Block:   block,
	}).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return []TaskMessage{}, nil
		}
		return nil, fmt.Errorf("read task messages: %w", err)
	}

	messages := make([]TaskMessage, 0)
	lastID := ""
	for _, stream := range streams {
		for _, raw := range stream.Messages {
			messages = append(messages, TaskMessage{
				TaskID: valueAsString(raw.Values["task_id"]),
				Token:  valueAsString(raw.Values["token"]),
			})
			lastID = raw.ID
		}
	}

	if strings.TrimSpace(lastID) != "" {
		c.advanceCursor(lastID)
	}
	if len(messages) == 0 {
		return []TaskMessage{}, nil
	}
	return messages, nil
}

func (c *Consumer) cursor() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.lastID
}

func (c *Consumer) advanceCursor(lastID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastID = lastID
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

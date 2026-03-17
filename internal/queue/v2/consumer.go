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
	groupName   string
	consumer    string
}

func NewConsumer(redisClient redis.Cmdable, streamName, groupName, consumer string) *Consumer {
	return &Consumer{
		redisClient: redisClient,
		streamName:  strings.TrimSpace(streamName),
		groupName:   strings.TrimSpace(groupName),
		consumer:    strings.TrimSpace(consumer),
	}
}

func (c *Consumer) ReadGroup(ctx context.Context, count int64, block time.Duration) ([]TaskMessage, error) {
	if err := c.validateConfigured(); err != nil {
		return nil, err
	}
	if count <= 0 {
		count = 1
	}

	streams, err := c.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.groupName,
		Consumer: c.consumer,
		Streams:  []string{c.streamName, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return []TaskMessage{}, nil
		}
		return nil, fmt.Errorf("read group task messages: %w", err)
	}
	return parseTaskMessages(streams), nil
}

func (c *Consumer) Ack(ctx context.Context, messageIDs ...string) error {
	if err := c.validateConfigured(); err != nil {
		return err
	}

	ids := make([]string, 0, len(messageIDs))
	for _, id := range messageIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		ids = append(ids, trimmed)
	}
	if len(ids) == 0 {
		return nil
	}

	if _, err := c.redisClient.XAck(ctx, c.streamName, c.groupName, ids...).Result(); err != nil {
		return fmt.Errorf("ack task messages: %w", err)
	}
	return nil
}

func (c *Consumer) ClaimPending(ctx context.Context, minIdle time.Duration, count int64) ([]TaskMessage, error) {
	if err := c.validateConfigured(); err != nil {
		return nil, err
	}
	if count <= 0 {
		count = 1
	}
	if minIdle < 0 {
		minIdle = 0
	}

	pending, err := c.redisClient.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: c.streamName,
		Group:  c.groupName,
		Start:  "-",
		End:    "+",
		Count:  count,
		Idle:   minIdle,
	}).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return []TaskMessage{}, nil
		}
		return nil, fmt.Errorf("list pending task messages: %w", err)
	}
	if len(pending) == 0 {
		return []TaskMessage{}, nil
	}

	ids := make([]string, 0, len(pending))
	for _, message := range pending {
		if trimmed := strings.TrimSpace(message.ID); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	if len(ids) == 0 {
		return []TaskMessage{}, nil
	}

	claimed, err := c.redisClient.XClaim(ctx, &redis.XClaimArgs{
		Stream:   c.streamName,
		Group:    c.groupName,
		Consumer: c.consumer,
		MinIdle:  minIdle,
		Messages: ids,
	}).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return []TaskMessage{}, nil
		}
		return nil, fmt.Errorf("claim pending task messages: %w", err)
	}
	if len(claimed) == 0 {
		return []TaskMessage{}, nil
	}

	streams := []redis.XStream{{Stream: c.streamName, Messages: claimed}}
	return parseTaskMessages(streams), nil
}

func (c *Consumer) Read(ctx context.Context, count int64, block time.Duration) ([]TaskMessage, error) {
	return c.ReadGroup(ctx, count, block)
}

func (c *Consumer) validateConfigured() error {
	if c.redisClient == nil {
		return errors.New("v2 queue consumer requires redis client")
	}
	if c.streamName == "" {
		return errors.New("v2 queue consumer requires stream name")
	}
	if c.groupName == "" {
		return errors.New("v2 queue consumer requires consumer group")
	}
	if c.consumer == "" {
		return errors.New("v2 queue consumer requires consumer name")
	}
	return nil
}

func parseTaskMessages(streams []redis.XStream) []TaskMessage {
	messages := make([]TaskMessage, 0)
	for _, stream := range streams {
		for _, raw := range stream.Messages {
			messages = append(messages, TaskMessage{
				MessageID: strings.TrimSpace(raw.ID),
				TaskID:    valueAsString(raw.Values["task_id"]),
				Token:     valueAsString(raw.Values["token"]),
			})
		}
	}
	if len(messages) == 0 {
		return []TaskMessage{}
	}
	return messages
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

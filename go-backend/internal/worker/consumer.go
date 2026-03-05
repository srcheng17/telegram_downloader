package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Message struct {
	ID           string
	TaskID       string
	EnqueueToken string
}

type StreamClient interface {
	ReadGroup(ctx context.Context, group, consumer string, count int64, block time.Duration) ([]Message, error)
	Ack(ctx context.Context, group string, ids ...string) error
}

type TaskStore interface {
	TransitionPendingToInProgress(ctx context.Context, taskID, token, worker string) (bool, error)
}

type Handler func(ctx context.Context, msg Message) error

type ConsumerConfig struct {
	Stream   StreamClient
	Store    TaskStore
	Group    string
	Consumer string
	Block    time.Duration
	Handler  Handler
}

type Consumer struct {
	stream   StreamClient
	store    TaskStore
	group    string
	consumer string
	block    time.Duration
	handler  Handler
}

func NewConsumer(cfg ConsumerConfig) *Consumer {
	handler := cfg.Handler
	if handler == nil {
		handler = func(context.Context, Message) error { return nil }
	}

	block := cfg.Block
	if block <= 0 {
		block = 5 * time.Second
	}

	return &Consumer{
		stream:   cfg.Stream,
		store:    cfg.Store,
		group:    strings.TrimSpace(cfg.Group),
		consumer: strings.TrimSpace(cfg.Consumer),
		block:    block,
		handler:  handler,
	}
}

func (c *Consumer) Run(ctx context.Context) error {
	if c.stream == nil {
		return errors.New("worker consumer requires stream client")
	}
	if c.store == nil {
		return errors.New("worker consumer requires task store")
	}
	if c.group == "" {
		return errors.New("worker consumer requires consumer group")
	}
	if c.consumer == "" {
		return errors.New("worker consumer requires consumer name")
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		messages, err := c.stream.ReadGroup(ctx, c.group, c.consumer, 1, c.block)
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read group messages: %w", err)
		}

		for _, msg := range messages {
			claimed, err := c.store.TransitionPendingToInProgress(ctx, msg.TaskID, msg.EnqueueToken, c.consumer)
			if err != nil {
				return fmt.Errorf("claim task %s: %w", msg.TaskID, err)
			}
			if !claimed {
				if err := c.stream.Ack(ctx, c.group, msg.ID); err != nil {
					return fmt.Errorf("ack unclaimed message %s: %w", msg.ID, err)
				}
				continue
			}

			if err := c.handler(ctx, msg); err != nil {
				return fmt.Errorf("handle task %s: %w", msg.TaskID, err)
			}

			if err := c.stream.Ack(ctx, c.group, msg.ID); err != nil {
				return fmt.Errorf("ack message %s: %w", msg.ID, err)
			}
		}
	}
}

type RedisStream struct {
	redisClient redis.Cmdable
	streamName  string
}

func NewRedisStream(redisClient redis.Cmdable, streamName string) *RedisStream {
	return &RedisStream{
		redisClient: redisClient,
		streamName:  strings.TrimSpace(streamName),
	}
}

func (s *RedisStream) CreateGroup(ctx context.Context, group string) error {
	if s.redisClient == nil {
		return errors.New("worker redis stream requires redis client")
	}
	if s.streamName == "" {
		return errors.New("worker redis stream requires stream name")
	}
	if strings.TrimSpace(group) == "" {
		return errors.New("worker redis stream requires consumer group")
	}

	err := s.redisClient.XGroupCreateMkStream(ctx, s.streamName, group, "0").Err()
	if err != nil && !isBusyGroupError(err) {
		return fmt.Errorf("create consumer group %q for stream %q: %w", group, s.streamName, err)
	}
	return nil
}

func (s *RedisStream) ReadGroup(
	ctx context.Context,
	group,
	consumer string,
	count int64,
	block time.Duration,
) ([]Message, error) {
	if s.redisClient == nil {
		return nil, errors.New("worker redis stream requires redis client")
	}
	if s.streamName == "" {
		return nil, errors.New("worker redis stream requires stream name")
	}

	streams, err := s.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    strings.TrimSpace(group),
		Consumer: strings.TrimSpace(consumer),
		Streams:  []string{s.streamName, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if err != nil {
		return nil, err
	}

	messages := make([]Message, 0, len(streams))
	for _, stream := range streams {
		for _, raw := range stream.Messages {
			messages = append(messages, Message{
				ID:           raw.ID,
				TaskID:       valueAsString(raw.Values["task_id"]),
				EnqueueToken: valueAsString(raw.Values["enqueue_token"]),
			})
		}
	}
	return messages, nil
}

func (s *RedisStream) Ack(ctx context.Context, group string, ids ...string) error {
	if s.redisClient == nil {
		return errors.New("worker redis stream requires redis client")
	}
	if s.streamName == "" {
		return errors.New("worker redis stream requires stream name")
	}
	if len(ids) == 0 {
		return nil
	}

	if err := s.redisClient.XAck(ctx, s.streamName, strings.TrimSpace(group), ids...).Err(); err != nil {
		return fmt.Errorf("xack stream message: %w", err)
	}
	return nil
}

func isBusyGroupError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
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

package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres"
)

const (
	baseErrorBackoff = 50 * time.Millisecond
	maxErrorBackoff  = 2 * time.Second
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
	TransitionToTerminal(ctx context.Context, input postgres.TransitionTerminalInput) error
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

	consecutiveErrors := 0

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
			log.Printf("worker consumer read group failed: %v", err)
			if !waitWithBackoff(ctx, &consecutiveErrors) {
				return nil
			}
			continue
		}
		for _, msg := range messages {
			claimed, err := c.store.TransitionPendingToInProgress(ctx, msg.TaskID, msg.EnqueueToken, c.consumer)
			if err != nil {
				log.Printf(
					"worker consumer claim failed message_id=%s task_id=%s: %v",
					msg.ID,
					msg.TaskID,
					err,
				)
				if !waitWithBackoff(ctx, &consecutiveErrors) {
					return nil
				}
				continue
			}
			if !claimed {
				if err := c.ackMessage(ctx, msg, "unclaimed"); err != nil {
					if !waitWithBackoff(ctx, &consecutiveErrors) {
						return nil
					}
					continue
				}
				consecutiveErrors = 0
				continue
			}

			if err := c.handler(ctx, msg); err != nil {
				log.Printf(
					"worker consumer handler failed message_id=%s task_id=%s: %v",
					msg.ID,
					msg.TaskID,
					err,
				)
				errMsg := err.Error()
				transitionErr := c.store.TransitionToTerminal(ctx, postgres.TransitionTerminalInput{
					TaskID: msg.TaskID,
					Worker: c.consumer,
					Status: domain.StatusFailed,
					Error:  &errMsg,
				})
				if transitionErr != nil {
					log.Printf(
						"worker consumer fail transition failed message_id=%s task_id=%s: %v",
						msg.ID,
						msg.TaskID,
						transitionErr,
					)
					if !waitWithBackoff(ctx, &consecutiveErrors) {
						return nil
					}
					continue
				}
				if err := c.ackMessage(ctx, msg, "handler_error"); err != nil {
					if !waitWithBackoff(ctx, &consecutiveErrors) {
						return nil
					}
					continue
				}
				consecutiveErrors = 0
				continue
			}

			if err := c.ackMessage(ctx, msg, "success"); err != nil {
				if !waitWithBackoff(ctx, &consecutiveErrors) {
					return nil
				}
				continue
			}
			consecutiveErrors = 0
		}
	}
}

func (c *Consumer) ackMessage(ctx context.Context, msg Message, reason string) error {
	if strings.TrimSpace(msg.ID) == "" {
		return fmt.Errorf("worker consumer cannot ack empty message id task_id=%s reason=%s", msg.TaskID, reason)
	}
	if err := c.stream.Ack(ctx, c.group, msg.ID); err != nil {
		log.Printf(
			"worker consumer ack failed message_id=%s task_id=%s reason=%s: %v",
			msg.ID,
			msg.TaskID,
			reason,
			err,
		)
		return err
	}
	return nil
}

func waitWithBackoff(ctx context.Context, consecutiveErrors *int) bool {
	if consecutiveErrors == nil {
		return true
	}

	*consecutiveErrors = *consecutiveErrors + 1
	delay := baseErrorBackoff
	for i := 1; i < *consecutiveErrors; i++ {
		delay *= 2
		if delay >= maxErrorBackoff {
			delay = maxErrorBackoff
			break
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type RedisStream struct {
	redisClient redis.Cmdable
	streamName  string
	mu          sync.Mutex
	pendingNext bool
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

	group = strings.TrimSpace(group)
	consumer = strings.TrimSpace(consumer)

	pendingFirst := s.nextPendingFirst()

	firstID := ">"
	firstBlock := block
	secondID := "0"
	secondBlock := time.Duration(-1)
	if pendingFirst {
		firstID = "0"
		firstBlock = -1
		secondID = ">"
		secondBlock = block
	}

	firstMessages, err := s.readGroupByID(ctx, group, consumer, count, firstBlock, firstID)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	if len(firstMessages) > 0 {
		return firstMessages, nil
	}

	secondMessages, err := s.readGroupByID(ctx, group, consumer, count, secondBlock, secondID)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	if len(secondMessages) > 0 {
		return secondMessages, nil
	}
	return nil, redis.Nil
}

func (s *RedisStream) nextPendingFirst() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	pendingFirst := s.pendingNext
	s.pendingNext = !s.pendingNext
	return pendingFirst
}

func (s *RedisStream) readGroupByID(
	ctx context.Context,
	group string,
	consumer string,
	count int64,
	block time.Duration,
	messageID string,
) ([]Message, error) {
	streams, err := s.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{s.streamName, messageID},
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

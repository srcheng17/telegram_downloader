package v2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
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

func (p *Producer) Produce(ctx context.Context, msg TaskMessage) error {
	if p.redisClient == nil {
		return errors.New("v2 queue producer requires redis client")
	}
	if p.streamName == "" {
		return errors.New("v2 queue producer requires stream name")
	}

	taskID := strings.TrimSpace(msg.TaskID)
	if taskID == "" {
		return errors.New("task message task_id is required")
	}
	token := strings.TrimSpace(msg.Token)
	if token == "" {
		return errors.New("task message token is required")
	}

	if _, err := p.redisClient.XAdd(ctx, &redis.XAddArgs{
		Stream: p.streamName,
		Values: map[string]any{
			"task_id": taskID,
			"token":   token,
		},
	}).Result(); err != nil {
		return fmt.Errorf("produce task message: %w", err)
	}

	return nil
}

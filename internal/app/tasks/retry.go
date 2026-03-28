package tasks

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	taskdomain "github.com/ryancheng/telegram-downloader/internal/domain/task"
)

var ErrTaskNotRetryable = errors.New("task not retryable")

type RetryTaskRecord struct {
	ID                string
	Status            string
	TaskType          string
	URL               string
	SourceArchivePath *string
}

type RetryStore interface {
	GetTaskForRetry(ctx context.Context, taskID string) (*RetryTaskRecord, error)
	RetryTask(ctx context.Context, taskID, enqueueToken string) error
}

type RetryService struct {
	Store          RetryStore
	Queue          TaskQueue
	TokenGenerator func() string
}

func NewRetryService(store RetryStore, queue TaskQueue) *RetryService {
	return &RetryService{
		Store:          store,
		Queue:          queue,
		TokenGenerator: uuid.NewString,
	}
}

func (s *RetryService) Retry(ctx context.Context, taskID string) (CreateResult, error) {
	if s == nil || s.Store == nil || s.Queue == nil {
		return CreateResult{}, errors.New("retry service dependencies are not configured")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return CreateResult{}, errors.New("task id is required")
	}

	task, err := s.Store.GetTaskForRetry(ctx, taskID)
	if err != nil {
		return CreateResult{}, err
	}
	if task == nil {
		return CreateResult{}, ErrTaskNotFound
	}

	if !taskdomain.CanRetry(
		task.TaskType,
		task.Status,
		strings.TrimSpace(task.URL) != "",
		task.SourceArchivePath != nil && strings.TrimSpace(*task.SourceArchivePath) != "",
	) {
		return CreateResult{}, ErrTaskNotRetryable
	}

	tokenGenerator := s.TokenGenerator
	if tokenGenerator == nil {
		tokenGenerator = uuid.NewString
	}
	enqueueToken := tokenGenerator()
	if err := s.Store.RetryTask(ctx, taskID, enqueueToken); err != nil {
		return CreateResult{}, err
	}
	if err := s.Queue.Enqueue(ctx, QueueMessage{TaskID: taskID, Token: enqueueToken}); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{TaskID: taskID, Status: StatusQueued}, nil
}


package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CreateStore interface {
	CreateTask(ctx context.Context, in CreateTaskInput) (TaskRecord, error)
	MarkTaskFailed(ctx context.Context, taskID, message string) error
}

type CreateService struct {
	Store               CreateStore
	Queue               TaskQueue
	CompensationTimeout time.Duration
	IDGenerator         func() string
	TokenGenerator      func() string
}

func NewCreateService(store CreateStore, queue TaskQueue) *CreateService {
	return &CreateService{
		Store:               store,
		Queue:               queue,
		CompensationTimeout: defaultCompensationTimeout,
		IDGenerator:         uuid.NewString,
		TokenGenerator:      uuid.NewString,
	}
}

func (s *CreateService) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	if s == nil || s.Store == nil || s.Queue == nil {
		return CreateResult{}, errors.New("task service dependencies are not configured")
	}

	url := strings.TrimSpace(in.URL)
	if url == "" {
		return CreateResult{}, errors.New("url is required")
	}

	idGenerator := s.IDGenerator
	if idGenerator == nil {
		idGenerator = uuid.NewString
	}
	tokenGenerator := s.TokenGenerator
	if tokenGenerator == nil {
		tokenGenerator = uuid.NewString
	}

	canonicalURL := normalizeCanonicalURL(url, in.CanonicalURL)
	createInput := CreateTaskInput{
		ID:           idGenerator(),
		URL:          url,
		CanonicalURL: stringPtr(canonicalURL),
		EnqueueToken: tokenGenerator(),
	}

	task, err := s.Store.CreateTask(ctx, createInput)
	if err != nil {
		return CreateResult{}, err
	}
	status := normalizeStatus(task.Status)
	if status == "" {
		status = StatusQueued
	}

	if err := s.Queue.Enqueue(ctx, QueueMessage{TaskID: task.ID, Token: createInput.EnqueueToken}); err != nil {
		compensationCtx, compensationCancel := context.WithTimeout(context.Background(), s.compensationTimeout())
		defer compensationCancel()
		_ = s.Store.MarkTaskFailed(compensationCtx, task.ID, fmt.Sprintf("enqueue failed: %v", err))
		return CreateResult{}, err
	}

	return CreateResult{TaskID: task.ID, Status: status}, nil
}

func (s *CreateService) compensationTimeout() time.Duration {
	if s.CompensationTimeout <= 0 {
		return defaultCompensationTimeout
	}
	return s.CompensationTimeout
}


package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const defaultCompensationTimeout = 3 * time.Second

const (
	StatusQueued          = "QUEUED"
	StatusRunning         = "RUNNING"
	StatusCancelRequested = "CANCEL_REQUESTED"
	StatusSuccess         = "SUCCESS"
	StatusFailed          = "FAILED"
	StatusCanceled        = "CANCELED"
)

var (
	ErrTaskNotFound       = errors.New("task not found")
	ErrTaskNotCancelable  = errors.New("task not cancelable")
	ErrTaskStatusConflict = errors.New("task status conflict")
)

type CreateTaskInput struct {
	ID           string
	URL          string
	CanonicalURL *string
	EnqueueToken string
}

type TaskRecord struct {
	ID           string
	URL          string
	CanonicalURL *string
	Status       string
}

type QueueMessage struct {
	TaskID string
	Token  string
}

type CreateInput struct {
	URL          string
	CanonicalURL *string
}

type CreateResult struct {
	TaskID string
	Status string
}

type CancelResult struct {
	TaskID string
	Status string
}

type TaskStore interface {
	CreateTask(ctx context.Context, in CreateTaskInput) (TaskRecord, error)
	GetTask(ctx context.Context, taskID string) (*TaskRecord, error)
	CancelTask(ctx context.Context, taskID, fromStatus string) error
	MarkTaskFailed(ctx context.Context, taskID, message string) error
}

type TaskQueue interface {
	Enqueue(ctx context.Context, message QueueMessage) error
}

type Service struct {
	Store               TaskStore
	Queue               TaskQueue
	CompensationTimeout time.Duration
}

func NewService(store TaskStore, queue TaskQueue) *Service {
	return &Service{
		Store:               store,
		Queue:               queue,
		CompensationTimeout: defaultCompensationTimeout,
	}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	if s == nil || s.Store == nil || s.Queue == nil {
		return CreateResult{}, errors.New("task service dependencies are not configured")
	}

	url := strings.TrimSpace(in.URL)
	if url == "" {
		return CreateResult{}, errors.New("url is required")
	}

	canonicalURL := normalizeCanonicalURL(url, in.CanonicalURL)
	createInput := CreateTaskInput{
		ID:           uuid.NewString(),
		URL:          url,
		CanonicalURL: stringPtr(canonicalURL),
		EnqueueToken: uuid.NewString(),
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

func (s *Service) Cancel(ctx context.Context, taskID string) (CancelResult, error) {
	cancelService := NewCancelService(cancelStoreAdapter{store: s.Store})
	return cancelService.Cancel(ctx, taskID)
}

func (s *Service) compensationTimeout() time.Duration {
	if s.CompensationTimeout <= 0 {
		return defaultCompensationTimeout
	}
	return s.CompensationTimeout
}

func normalizeStatus(status string) string {
	return strings.ToUpper(strings.TrimSpace(status))
}

func normalizeCanonicalURL(url string, canonicalURL *string) string {
	if canonicalURL == nil {
		return strings.TrimSpace(url)
	}
	normalized := strings.TrimSpace(*canonicalURL)
	if normalized == "" {
		return strings.TrimSpace(url)
	}
	return normalized
}

func stringPtr(value string) *string {
	return &value
}

type cancelStoreAdapter struct {
	store TaskStore
}

func (a cancelStoreAdapter) GetTaskForCancel(ctx context.Context, taskID string) (*CancelTaskRecord, error) {
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil || task == nil {
		return nil, err
	}
	return &CancelTaskRecord{
		ID:     task.ID,
		Status: task.Status,
	}, nil
}

func (a cancelStoreAdapter) CancelTask(ctx context.Context, taskID, fromStatus string) error {
	return a.store.CancelTask(ctx, taskID, fromStatus)
}

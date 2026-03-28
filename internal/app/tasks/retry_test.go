package tasks

import (
	"context"
	"errors"
	"testing"
)

type fakeRetryStore struct {
	task         *RetryTaskRecord
	getErr       error
	retryTaskID  string
	retryToken   string
	retryCalls   int
	retryErr     error
}

func (f *fakeRetryStore) GetTaskForRetry(_ context.Context, taskID string) (*RetryTaskRecord, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.task == nil {
		return nil, nil
	}
	copy := *f.task
	copy.ID = taskID
	return &copy, nil
}

func (f *fakeRetryStore) RetryTask(_ context.Context, taskID, enqueueToken string) error {
	f.retryCalls++
	f.retryTaskID = taskID
	f.retryToken = enqueueToken
	return f.retryErr
}

type fakeRetryQueue struct {
	messages []QueueMessage
	err      error
}

func (f *fakeRetryQueue) Enqueue(_ context.Context, msg QueueMessage) error {
	if f.err != nil {
		return f.err
	}
	f.messages = append(f.messages, msg)
	return nil
}

func TestRetryServiceRequeuesSameURLTaskID(t *testing.T) {
	store := &fakeRetryStore{
		task: &RetryTaskRecord{
			Status:   StatusFailed,
			TaskType: "url",
			URL:      "https://telegra.ph/retry-me",
		},
	}
	queue := &fakeRetryQueue{}
	svc := NewRetryService(store, queue)
	svc.TokenGenerator = func() string { return "retry-token" }

	result, err := svc.Retry(context.Background(), "task-url-retry")
	if err != nil {
		t.Fatalf("Retry returned error: %v", err)
	}
	if result.TaskID != "task-url-retry" || result.Status != StatusQueued {
		t.Fatalf("unexpected retry result %#v", result)
	}
	if store.retryCalls != 1 || store.retryTaskID != "task-url-retry" || store.retryToken != "retry-token" {
		t.Fatalf("unexpected retry store call state %#v", store)
	}
	if len(queue.messages) != 1 || queue.messages[0].TaskID != "task-url-retry" || queue.messages[0].Token != "retry-token" {
		t.Fatalf("unexpected queue messages %#v", queue.messages)
	}
}

func TestRetryServiceRejectsIneligibleTask(t *testing.T) {
	store := &fakeRetryStore{
		task: &RetryTaskRecord{
			Status:   "UPLOADING",
			TaskType: "upload",
		},
	}
	svc := NewRetryService(store, &fakeRetryQueue{})

	_, err := svc.Retry(context.Background(), "task-uploading")
	if !errors.Is(err, ErrTaskNotRetryable) {
		t.Fatalf("expected ErrTaskNotRetryable, got %v", err)
	}
	if store.retryCalls != 0 {
		t.Fatalf("expected no retry call, got %d", store.retryCalls)
	}
}

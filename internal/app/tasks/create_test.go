package tasks

import (
	"context"
	"errors"
	"testing"
)

type fakeCreateStore struct {
	createdTask     TaskRecord
	createdInput    CreateTaskInput
	createdCalls    int
	markFailedTask  string
	markFailedMsg   string
	markFailedCalls int
}

func (f *fakeCreateStore) CreateTask(_ context.Context, in CreateTaskInput) (TaskRecord, error) {
	f.createdCalls++
	f.createdInput = in
	return f.createdTask, nil
}

func (f *fakeCreateStore) MarkTaskFailed(_ context.Context, taskID, message string) error {
	f.markFailedCalls++
	f.markFailedTask = taskID
	f.markFailedMsg = message
	return nil
}

type fakeCreateQueue struct {
	messages []QueueMessage
	err      error
}

func (f *fakeCreateQueue) Enqueue(_ context.Context, msg QueueMessage) error {
	if f.err != nil {
		return f.err
	}
	f.messages = append(f.messages, msg)
	return nil
}

func TestCreateServiceEnqueuesCreatedTask(t *testing.T) {
	store := &fakeCreateStore{
		createdTask: TaskRecord{ID: "task-create", Status: StatusQueued},
	}
	queue := &fakeCreateQueue{}
	svc := NewCreateService(store, queue)
	svc.IDGenerator = func() string { return "task-id" }
	svc.TokenGenerator = func() string { return "enqueue-token" }

	result, err := svc.Create(context.Background(), CreateInput{URL: "https://telegra.ph/demo"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if result.TaskID != "task-create" || result.Status != StatusQueued {
		t.Fatalf("unexpected create result %#v", result)
	}
	if store.createdCalls != 1 {
		t.Fatalf("expected one create call, got %d", store.createdCalls)
	}
	if store.createdInput.ID != "task-id" || store.createdInput.EnqueueToken != "enqueue-token" {
		t.Fatalf("unexpected create input %#v", store.createdInput)
	}
	if len(queue.messages) != 1 || queue.messages[0].TaskID != "task-create" || queue.messages[0].Token != "enqueue-token" {
		t.Fatalf("unexpected queue messages %#v", queue.messages)
	}
}

func TestCreateServiceMarksTaskFailedWhenEnqueueFails(t *testing.T) {
	store := &fakeCreateStore{
		createdTask: TaskRecord{ID: "task-create", Status: StatusQueued},
	}
	queue := &fakeCreateQueue{err: errors.New("queue unavailable")}
	svc := NewCreateService(store, queue)
	svc.IDGenerator = func() string { return "task-id" }
	svc.TokenGenerator = func() string { return "enqueue-token" }

	_, err := svc.Create(context.Background(), CreateInput{URL: "https://telegra.ph/demo"})
	if err == nil {
		t.Fatalf("expected enqueue failure")
	}
	if store.markFailedCalls != 1 || store.markFailedTask != "task-create" {
		t.Fatalf("expected task failure compensation, got %#v", store)
	}
	if store.markFailedMsg == "" {
		t.Fatalf("expected failure message to be recorded")
	}
}


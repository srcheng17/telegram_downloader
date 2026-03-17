package tasks

import (
	"context"
	"errors"
	"testing"
)

type fakeServiceStore struct {
	createdTask      TaskRecord
	createdInput     CreateTaskInput
	createdCalls     int
	getTask          *TaskRecord
	cancelTaskID     string
	cancelFromStatus string
	cancelCalls      int
	cancelErr        error
}

func (f *fakeServiceStore) CreateTask(_ context.Context, in CreateTaskInput) (TaskRecord, error) {
	f.createdCalls++
	f.createdInput = in
	return f.createdTask, nil
}

func (f *fakeServiceStore) GetTask(_ context.Context, taskID string) (*TaskRecord, error) {
	if f.getTask == nil {
		return nil, nil
	}
	copy := *f.getTask
	copy.ID = taskID
	return &copy, nil
}

func (f *fakeServiceStore) CancelTask(_ context.Context, taskID, fromStatus string) error {
	f.cancelCalls++
	f.cancelTaskID = taskID
	f.cancelFromStatus = fromStatus
	return f.cancelErr
}

func (f *fakeServiceStore) MarkTaskFailed(context.Context, string, string) error {
	return nil
}

type fakeServiceQueue struct {
	messages []QueueMessage
}

func (f *fakeServiceQueue) Enqueue(_ context.Context, msg QueueMessage) error {
	f.messages = append(f.messages, msg)
	return nil
}

func TestServiceCreateEnqueuesAndReturnsAcceptedTask(t *testing.T) {
	store := &fakeServiceStore{
		createdTask: TaskRecord{ID: "task-service-create", Status: StatusQueued},
	}
	queue := &fakeServiceQueue{}
	service := NewService(store, queue)

	result, err := service.Create(context.Background(), CreateInput{URL: "https://telegra.ph/demo"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result.TaskID != "task-service-create" {
		t.Fatalf("expected task id task-service-create, got %q", result.TaskID)
	}
	if result.Status != StatusQueued {
		t.Fatalf("expected status %s, got %q", StatusQueued, result.Status)
	}
	if store.createdCalls != 1 {
		t.Fatalf("expected one create call, got %d", store.createdCalls)
	}
	if len(queue.messages) != 1 {
		t.Fatalf("expected one queue message, got %d", len(queue.messages))
	}
}

func TestServiceCancelMapsFinishedTaskToConflict(t *testing.T) {
	store := &fakeServiceStore{
		getTask: &TaskRecord{Status: StatusSuccess},
	}
	service := NewService(store, &fakeServiceQueue{})

	_, err := service.Cancel(context.Background(), "task-finished")
	if !errors.Is(err, ErrTaskNotCancelable) {
		t.Fatalf("expected ErrTaskNotCancelable, got %v", err)
	}
	if store.cancelCalls != 0 {
		t.Fatalf("expected no cancel calls, got %d", store.cancelCalls)
	}
}

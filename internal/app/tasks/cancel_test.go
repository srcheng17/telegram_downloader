package tasks

import (
	"context"
	"errors"
	"testing"
)

type fakeCancelStore struct {
	task             *CancelTaskRecord
	getErr           error
	cancelTaskID     string
	cancelFromStatus string
	cancelCalls      int
	cancelErr        error
}

func (f *fakeCancelStore) GetTaskForCancel(_ context.Context, taskID string) (*CancelTaskRecord, error) {
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

func (f *fakeCancelStore) CancelTask(_ context.Context, taskID, fromStatus string) error {
	f.cancelCalls++
	f.cancelTaskID = taskID
	f.cancelFromStatus = fromStatus
	return f.cancelErr
}

func TestCancelServiceReturnsConflictForFinishedTask(t *testing.T) {
	store := &fakeCancelStore{
		task: &CancelTaskRecord{Status: StatusSuccess},
	}
	svc := NewCancelService(store)

	_, err := svc.Cancel(context.Background(), "task-finished")
	if !errors.Is(err, ErrTaskNotCancelable) {
		t.Fatalf("expected ErrTaskNotCancelable, got %v", err)
	}
	if store.cancelCalls != 0 {
		t.Fatalf("expected no cancel calls, got %d", store.cancelCalls)
	}
}

func TestCancelServiceRequestsCancellationForActiveTask(t *testing.T) {
	store := &fakeCancelStore{
		task: &CancelTaskRecord{Status: StatusRunning},
	}
	svc := NewCancelService(store)

	result, err := svc.Cancel(context.Background(), "task-running")
	if err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if result.TaskID != "task-running" || result.Status != StatusCancelRequested {
		t.Fatalf("unexpected cancel result %#v", result)
	}
	if store.cancelCalls != 1 || store.cancelTaskID != "task-running" || store.cancelFromStatus != StatusRunning {
		t.Fatalf("unexpected cancel store state %#v", store)
	}
}


package worker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres"
)

func TestExecutorTransitionsToCanceledWhenCancelRequested(t *testing.T) {
	store := &fakeExecutorStore{
		statusSequence: []string{
			domain.StatusInProgress,
			domain.StatusCancelRequested,
		},
		fallbackStatus: domain.StatusCancelRequested,
	}
	downloader := &fakeExecutorDownloader{
		executeFn: func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	executor := &Executor{
		Store:             store,
		Downloader:        downloader,
		HeartbeatInterval: 5 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := executor.Handle(ctx, "worker-1", Message{TaskID: "task-cancel"})
	if err != nil {
		t.Fatalf("executor handle: %v", err)
	}

	if downloader.calls != 1 {
		t.Fatalf("expected downloader to run once, got %d", downloader.calls)
	}
	if store.heartbeatCalls == 0 {
		t.Fatalf("expected at least one heartbeat update while running")
	}
	if len(store.terminalCalls) != 1 {
		t.Fatalf("expected one terminal transition, got %d", len(store.terminalCalls))
	}
	transition := store.terminalCalls[0]
	if transition.TaskID != "task-cancel" {
		t.Fatalf("expected task-cancel transition, got %q", transition.TaskID)
	}
	if transition.Worker != "worker-1" {
		t.Fatalf("expected worker-1 transition worker, got %q", transition.Worker)
	}
	if transition.Status != domain.StatusCanceled {
		t.Fatalf("expected status CANCELED, got %q", transition.Status)
	}
	if transition.ResultZipPath != nil {
		t.Fatalf("expected canceled result_zip_path to be nil, got %q", *transition.ResultZipPath)
	}
	if transition.Error == nil || !strings.Contains(strings.ToLower(*transition.Error), "cancel") {
		t.Fatalf("expected canceled error message, got %#v", transition.Error)
	}
}

func TestExecutorTransitionsToFailedOnDownloadError(t *testing.T) {
	downloadErr := errors.New("download exploded")
	store := &fakeExecutorStore{
		statusSequence: []string{
			domain.StatusInProgress,
		},
		fallbackStatus: domain.StatusInProgress,
	}
	downloader := &fakeExecutorDownloader{
		executeFn: func(context.Context, string) (string, error) {
			return "", downloadErr
		},
	}
	executor := &Executor{
		Store:      store,
		Downloader: downloader,
	}

	err := executor.Handle(context.Background(), "worker-1", Message{TaskID: "task-failed"})
	if err != nil {
		t.Fatalf("executor handle: %v", err)
	}

	if len(store.terminalCalls) != 1 {
		t.Fatalf("expected one terminal transition, got %d", len(store.terminalCalls))
	}
	transition := store.terminalCalls[0]
	if transition.TaskID != "task-failed" {
		t.Fatalf("expected task-failed transition, got %q", transition.TaskID)
	}
	if transition.Worker != "worker-1" {
		t.Fatalf("expected worker-1 transition worker, got %q", transition.Worker)
	}
	if transition.Status != domain.StatusFailed {
		t.Fatalf("expected status FAILED, got %q", transition.Status)
	}
	if transition.ResultZipPath != nil {
		t.Fatalf("expected failed result_zip_path to be nil, got %q", *transition.ResultZipPath)
	}
	if transition.Error == nil || !strings.Contains(*transition.Error, "download exploded") {
		t.Fatalf("expected failure error to include download exploded, got %#v", transition.Error)
	}
}

func TestExecutorTransitionsToCanceledWhenDownloadErrorAndCancelRequested(t *testing.T) {
	store := &fakeExecutorStore{
		statusSequence: []string{
			domain.StatusInProgress,
			domain.StatusCancelRequested,
		},
		fallbackStatus: domain.StatusCancelRequested,
	}
	downloader := &fakeExecutorDownloader{
		executeFn: func(context.Context, string) (string, error) {
			return "", errors.New("download failed")
		},
	}
	executor := &Executor{
		Store:      store,
		Downloader: downloader,
	}

	err := executor.Handle(context.Background(), "worker-1", Message{TaskID: "task-cancel-on-error"})
	if err != nil {
		t.Fatalf("executor handle: %v", err)
	}

	if len(store.terminalCalls) != 1 {
		t.Fatalf("expected one terminal transition, got %d", len(store.terminalCalls))
	}
	transition := store.terminalCalls[0]
	if transition.Status != domain.StatusCanceled {
		t.Fatalf("expected status CANCELED, got %q", transition.Status)
	}
}

func TestExecutorSuccessRaceWithCancelRequestedEndsCanceled(t *testing.T) {
	store := &fakeExecutorStore{
		statusSequence: []string{
			domain.StatusInProgress,
			domain.StatusInProgress,
			domain.StatusCancelRequested,
		},
		fallbackStatus: domain.StatusCancelRequested,
	}
	downloader := &fakeExecutorDownloader{
		executeFn: func(context.Context, string) (string, error) {
			return "/tmp/task.cbz", nil
		},
	}
	executor := &Executor{
		Store:      store,
		Downloader: downloader,
	}

	err := executor.Handle(context.Background(), "worker-1", Message{TaskID: "task-success-race"})
	if err != nil {
		t.Fatalf("executor handle: %v", err)
	}

	if len(store.terminalCalls) == 0 {
		t.Fatalf("expected at least one terminal transition")
	}
	lastTransition := store.terminalCalls[len(store.terminalCalls)-1]
	if lastTransition.Status != domain.StatusCanceled {
		t.Fatalf("expected final status CANCELED, got %q", lastTransition.Status)
	}
}

type fakeExecutorStore struct {
	mu             sync.Mutex
	statusSequence []string
	fallbackStatus string
	heartbeatCalls int
	terminalCalls  []postgres.TransitionTerminalInput
}

func (f *fakeExecutorStore) TransitionPendingToInProgress(context.Context, string, string, string) (bool, error) {
	return true, nil
}

func (f *fakeExecutorStore) UpdateTaskHeartbeat(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.heartbeatCalls++
	return nil
}

func (f *fakeExecutorStore) GetTask(_ context.Context, taskID string) (*domain.TaskLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	status := f.fallbackStatus
	if len(f.statusSequence) > 0 {
		status = f.statusSequence[0]
		f.statusSequence = f.statusSequence[1:]
	}
	return &domain.TaskLog{
		ID:     taskID,
		Status: status,
	}, nil
}

func (f *fakeExecutorStore) TransitionToTerminal(_ context.Context, input postgres.TransitionTerminalInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.terminalCalls = append(f.terminalCalls, input)
	return nil
}

type fakeExecutorDownloader struct {
	calls     int
	executeFn func(ctx context.Context, taskID string) (string, error)
}

func (f *fakeExecutorDownloader) Execute(ctx context.Context, taskID string) (string, error) {
	f.calls++
	if f.executeFn != nil {
		return f.executeFn(ctx, taskID)
	}
	return "", nil
}

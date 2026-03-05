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
		status: domain.StatusInProgress,
		statusSequence: []string{
			domain.StatusInProgress,
			domain.StatusCancelRequested,
		},
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
		status: domain.StatusInProgress,
		statusSequence: []string{
			domain.StatusInProgress,
		},
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
	if store.status != domain.StatusFailed {
		t.Fatalf("expected final task status FAILED, got %q", store.status)
	}
}

func TestExecutorDownloadErrorRaceToCancelRequestedEndsCanceled(t *testing.T) {
	store := &fakeExecutorStore{
		status: domain.StatusInProgress,
		statusSequence: []string{
			domain.StatusInProgress,
			domain.StatusInProgress,
		},
		beforeTransition: func(input postgres.TransitionTerminalInput, currentStatus string) string {
			if input.Status == domain.StatusFailed {
				return domain.StatusCancelRequested
			}
			return currentStatus
		},
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

	if len(store.terminalCalls) != 2 {
		t.Fatalf("expected failed then canceled transitions, got %d", len(store.terminalCalls))
	}
	if store.terminalCalls[0].Status != domain.StatusFailed {
		t.Fatalf("expected first transition FAILED, got %q", store.terminalCalls[0].Status)
	}
	if store.terminalCalls[1].Status != domain.StatusCanceled {
		t.Fatalf("expected second transition CANCELED, got %q", store.terminalCalls[1].Status)
	}
	if store.status != domain.StatusCanceled {
		t.Fatalf("expected final status CANCELED, got %q", store.status)
	}
}

func TestExecutorReturnsErrorWhenDownloadErrorLeavesTaskNonTerminal(t *testing.T) {
	store := &fakeExecutorStore{
		status: domain.StatusInProgress,
		statusSequence: []string{
			domain.StatusInProgress,
			domain.StatusInProgress,
		},
		beforeTransition: func(input postgres.TransitionTerminalInput, currentStatus string) string {
			if input.Status == domain.StatusFailed {
				return domain.StatusPending
			}
			return currentStatus
		},
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

	err := executor.Handle(context.Background(), "worker-1", Message{TaskID: "task-non-terminal"})
	if err == nil {
		t.Fatalf("expected non-terminal race to return error")
	}
	if !strings.Contains(err.Error(), "non-terminal") {
		t.Fatalf("expected non-terminal error, got %v", err)
	}
	if len(store.terminalCalls) != 1 {
		t.Fatalf("expected only failed transition attempt, got %d", len(store.terminalCalls))
	}
	if store.terminalCalls[0].Status != domain.StatusFailed {
		t.Fatalf("expected failed transition attempt, got %q", store.terminalCalls[0].Status)
	}
	if store.status != domain.StatusPending {
		t.Fatalf("expected final status to remain PENDING, got %q", store.status)
	}
}

func TestExecutorReturnsErrorWhenSuccessTransitionLeavesNonTerminal(t *testing.T) {
	store := &fakeExecutorStore{
		status: domain.StatusInProgress,
		statusSequence: []string{
			domain.StatusInProgress,
			domain.StatusInProgress,
		},
		beforeTransition: func(input postgres.TransitionTerminalInput, currentStatus string) string {
			if input.Status == domain.StatusSuccess {
				return domain.StatusPending
			}
			return currentStatus
		},
	}
	downloader := &fakeExecutorDownloader{
		executeFn: func(context.Context, string) (string, error) {
			return "/tmp/success.cbz", nil
		},
	}
	executor := &Executor{
		Store:      store,
		Downloader: downloader,
	}

	err := executor.Handle(context.Background(), "worker-1", Message{TaskID: "task-success-non-terminal"})
	if err == nil {
		t.Fatalf("expected success transition race to return error")
	}
	if !strings.Contains(err.Error(), "non-terminal") {
		t.Fatalf("expected non-terminal error, got %v", err)
	}
	if len(store.terminalCalls) != 1 {
		t.Fatalf("expected one success transition attempt, got %d", len(store.terminalCalls))
	}
	if store.terminalCalls[0].Status != domain.StatusSuccess {
		t.Fatalf("expected attempted status SUCCESS, got %q", store.terminalCalls[0].Status)
	}
	if store.status != domain.StatusPending {
		t.Fatalf("expected final status PENDING, got %q", store.status)
	}
}

func TestExecutorSuccessRaceWithCancelRequestedEndsCanceled(t *testing.T) {
	store := &fakeExecutorStore{
		status: domain.StatusInProgress,
		statusSequence: []string{
			domain.StatusInProgress,
			domain.StatusInProgress,
		},
		beforeTransition: func(input postgres.TransitionTerminalInput, currentStatus string) string {
			if input.Status == domain.StatusSuccess {
				return domain.StatusCancelRequested
			}
			return currentStatus
		},
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

	if len(store.terminalCalls) != 2 {
		t.Fatalf("expected success then canceled transitions, got %d", len(store.terminalCalls))
	}
	if store.terminalCalls[0].Status != domain.StatusSuccess {
		t.Fatalf("expected first transition SUCCESS, got %q", store.terminalCalls[0].Status)
	}
	if store.terminalCalls[1].Status != domain.StatusCanceled {
		t.Fatalf("expected second transition CANCELED, got %q", store.terminalCalls[1].Status)
	}
	if store.status != domain.StatusCanceled {
		t.Fatalf("expected final status CANCELED, got %q", store.status)
	}
}

type fakeExecutorStore struct {
	mu               sync.Mutex
	status           string
	statusSequence   []string
	heartbeatCalls   int
	terminalCalls    []postgres.TransitionTerminalInput
	transitionErr    error
	beforeTransition func(input postgres.TransitionTerminalInput, currentStatus string) string
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

	status := f.status
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
	if f.beforeTransition != nil {
		f.status = f.beforeTransition(input, f.status)
	}
	if f.transitionErr != nil {
		return f.transitionErr
	}

	if fakeTransitionAllowed(f.status, input.Status) {
		f.status = input.Status
	}
	return nil
}

func fakeTransitionAllowed(currentStatus, targetStatus string) bool {
	switch targetStatus {
	case domain.StatusCanceled:
		return currentStatus == domain.StatusInProgress || currentStatus == domain.StatusCancelRequested
	case domain.StatusSuccess, domain.StatusFailed:
		return currentStatus == domain.StatusInProgress
	default:
		return false
	}
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

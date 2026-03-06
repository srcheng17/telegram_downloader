package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/httpv2"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres"
)

func TestLegacyAdapterCreateOrReuseDownloadTaskCreatesAndEnqueues(t *testing.T) {
	store := &fakeLegacyV2Store{
		createTask: httpv2.Task{
			ID:           "task-v2-adapter-created",
			URL:          "https://telegra.ph/adapter-created",
			CanonicalURL: stringPtr("https://telegra.ph/adapter-created"),
			Status:       "QUEUED",
		},
		listResult: httpv2.ListTasksResult{
			Tasks: []httpv2.Task{},
		},
	}
	queue := &fakeLegacyV2Queue{}
	adapter := NewLegacyAdapter(store, queue, nil)

	result, err := adapter.CreateOrReuseDownloadTask(context.Background(), LegacyDownloadInput{
		RawURL:       "https://telegra.ph/adapter-created",
		CanonicalURL: "https://telegra.ph/adapter-created",
		Force:        false,
	})
	if err != nil {
		t.Fatalf("create or reuse download task: %v", err)
	}
	if result.Decision != LegacyDownloadDecisionCreated {
		t.Fatalf("expected decision %q, got %q", LegacyDownloadDecisionCreated, result.Decision)
	}
	if result.TaskID != "task-v2-adapter-created" {
		t.Fatalf("expected task id task-v2-adapter-created, got %q", result.TaskID)
	}
	if len(store.createCalls) != 1 {
		t.Fatalf("expected one create task call, got %d", len(store.createCalls))
	}
	if len(queue.calls) != 1 {
		t.Fatalf("expected one enqueue call, got %d", len(queue.calls))
	}
	if queue.calls[0].TaskID != "task-v2-adapter-created" {
		t.Fatalf("expected enqueued task id task-v2-adapter-created, got %q", queue.calls[0].TaskID)
	}
}

func TestLegacyAdapterReadLogsMapsFromV2Tasks(t *testing.T) {
	store := &fakeLegacyV2Store{
		listResult: httpv2.ListTasksResult{
			Tasks: []httpv2.Task{
				{
					ID:           "task-v2-log-adapter",
					URL:          "https://telegra.ph/log-adapter",
					CanonicalURL: stringPtr("https://telegra.ph/log-adapter"),
					Status:       "RUNNING",
					CreatedAt:    time.Unix(1700000123, 0).UTC(),
					UpdatedAt:    time.Unix(1700000125, 0).UTC(),
				},
			},
			Total:      1,
			Page:       1,
			PerPage:    25,
			TotalPages: 1,
		},
		statusCounts: map[string]int{
			"RUNNING": 1,
		},
	}
	adapter := NewLegacyAdapter(store, nil, nil)

	response, err := adapter.ReadLogs(context.Background(), domain.LogQuery{
		Page:    1,
		PerPage: 25,
		Status:  domain.StatusInProgress,
		Keyword: "log-adapter",
	})
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	if len(store.listCalls) != 1 {
		t.Fatalf("expected one list call, got %d", len(store.listCalls))
	}
	if store.listCalls[0].Status != "RUNNING" {
		t.Fatalf("expected v2 list status RUNNING, got %q", store.listCalls[0].Status)
	}
	if len(response.Logs) != 1 {
		t.Fatalf("expected one log row, got %d", len(response.Logs))
	}
	if response.Logs[0].Status != domain.StatusInProgress {
		t.Fatalf("expected mapped status %q, got %q", domain.StatusInProgress, response.Logs[0].Status)
	}
	if !response.HasActiveTasks {
		t.Fatalf("expected has_active_tasks=true")
	}
	if response.Summary.InProgressTasks != 1 {
		t.Fatalf("expected summary.in_progress_tasks=1, got %d", response.Summary.InProgressTasks)
	}
}

func TestLegacyAdapterCreateOrReuseDownloadTaskPrefersAtomicClaimPath(t *testing.T) {
	store := &fakeLegacyAtomicClaimStore{
		claimResult: httpv2.LegacyClaimTaskResult{
			Decision:     httpv2.LegacyClaimTaskDecisionCreated,
			Task:         httpv2.Task{ID: "task-v2-atomic-claim-created"},
			EnqueueToken: "token-v2-atomic-claim",
		},
	}
	queue := &fakeLegacyV2Queue{}
	adapter := NewLegacyAdapter(store, queue, nil)

	result, err := adapter.CreateOrReuseDownloadTask(context.Background(), LegacyDownloadInput{
		RawURL:       "https://telegra.ph/atomic-claim",
		CanonicalURL: "https://telegra.ph/atomic-claim",
		Force:        false,
	})
	if err != nil {
		t.Fatalf("create or reuse with atomic claim: %v", err)
	}
	if result.Decision != LegacyDownloadDecisionCreated {
		t.Fatalf("expected created decision, got %q", result.Decision)
	}
	if len(store.claimCalls) != 1 {
		t.Fatalf("expected one atomic claim call, got %d", len(store.claimCalls))
	}
	if len(store.createCalls) != 0 {
		t.Fatalf("expected fallback create path not called, got %d", len(store.createCalls))
	}
	if len(store.listCalls) != 0 {
		t.Fatalf("expected fallback list path not called, got %d", len(store.listCalls))
	}
	if len(queue.calls) != 1 {
		t.Fatalf("expected one enqueue call, got %d", len(queue.calls))
	}
	if queue.calls[0].TaskID != "task-v2-atomic-claim-created" {
		t.Fatalf("expected enqueued task id task-v2-atomic-claim-created, got %q", queue.calls[0].TaskID)
	}
	if queue.calls[0].Token != "token-v2-atomic-claim" {
		t.Fatalf("expected enqueue token token-v2-atomic-claim, got %q", queue.calls[0].Token)
	}
}

func TestLegacyAdapterCancelTaskMismatchUsesLatestStatus(t *testing.T) {
	store := &fakeLegacyV2Store{
		getTasks: []*httpv2.Task{
			{ID: "task-v2-cancel-race", Status: httpv2.TaskStatusRunning},
			{ID: "task-v2-cancel-race", Status: httpv2.TaskStatusSuccess},
		},
		cancelErrs: []error{
			postgres.ErrV2TaskStatusMismatchOrNotFound,
		},
	}
	adapter := NewLegacyAdapter(store, nil, nil)

	result, err := adapter.CancelTask(context.Background(), "task-v2-cancel-race")
	if err != nil {
		t.Fatalf("cancel task with mismatch: %v", err)
	}
	if result.Decision != LegacyCancelDecisionAlreadyFinished {
		t.Fatalf("expected already finished decision, got %q", result.Decision)
	}
	if result.Status != domain.StatusSuccess {
		t.Fatalf("expected latest status %q, got %q", domain.StatusSuccess, result.Status)
	}
	if len(store.cancelCalls) != 1 {
		t.Fatalf("expected one cancel attempt before latest decided terminal, got %d", len(store.cancelCalls))
	}
}

type fakeLegacyAtomicClaimStore struct {
	fakeLegacyV2Store
	claimCalls  []httpv2.LegacyClaimTaskInput
	claimResult httpv2.LegacyClaimTaskResult
	claimErr    error
}

func (f *fakeLegacyAtomicClaimStore) ClaimTaskForLegacy(
	_ context.Context,
	in httpv2.LegacyClaimTaskInput,
) (httpv2.LegacyClaimTaskResult, error) {
	f.claimCalls = append(f.claimCalls, in)
	if f.claimErr != nil {
		return httpv2.LegacyClaimTaskResult{}, f.claimErr
	}
	if f.claimResult.Decision != "" || strings.TrimSpace(f.claimResult.Task.ID) != "" || strings.TrimSpace(f.claimResult.EnqueueToken) != "" {
		return f.claimResult, nil
	}
	return httpv2.LegacyClaimTaskResult{
		Decision: httpv2.LegacyClaimTaskDecisionCreated,
		Task: httpv2.Task{
			ID: strings.TrimSpace(in.ID),
		},
		EnqueueToken: strings.TrimSpace(in.EnqueueToken),
	}, nil
}

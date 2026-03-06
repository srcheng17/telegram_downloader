package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/httpv2"
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

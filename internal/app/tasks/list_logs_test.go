package tasks

import (
	"context"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/domain"
)

type fakeLogsStore struct {
	listQuery    domain.LogQuery
	listResult   domain.LogListResult
	listErr      error
	summary      domain.Summary
	summaryErr   error
	summaryCalls int
}

func (f *fakeLogsStore) ListLogs(_ context.Context, query domain.LogQuery) (domain.LogListResult, error) {
	f.listQuery = query
	return f.listResult, f.listErr
}

func (f *fakeLogsStore) BuildSummary(context.Context) (domain.Summary, error) {
	f.summaryCalls++
	return f.summary, f.summaryErr
}

func TestListLogsServiceCombinesLogsAndSummary(t *testing.T) {
	store := &fakeLogsStore{
		listResult: domain.LogListResult{
			Logs: []domain.TaskLog{
				{ID: "task-1", Status: domain.StatusInProgress},
			},
			Total:      1,
			Page:       2,
			PerPage:    10,
			TotalPages: 3,
		},
		summary: domain.Summary{
			ActiveTasks:     1,
			InProgressTasks: 1,
		},
	}
	svc := NewListLogsService(store)

	result, err := svc.List(context.Background(), domain.LogQuery{
		Page:    2,
		PerPage: 10,
		Status:  domain.StatusInProgress,
		Keyword: "demo",
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if result.Total != 1 || result.Page != 2 || result.PerPage != 10 || result.TotalPages != 3 {
		t.Fatalf("unexpected pagination result %#v", result)
	}
	if len(result.Logs) != 1 || result.Logs[0].ID != "task-1" {
		t.Fatalf("unexpected logs result %#v", result.Logs)
	}
	if !result.HasActiveTasks || result.Summary.InProgressTasks != 1 {
		t.Fatalf("unexpected summary result %#v", result.Summary)
	}
	if result.Filters.Status != domain.StatusInProgress || result.Filters.Query != "demo" {
		t.Fatalf("unexpected filters %#v", result.Filters)
	}
	if result.StatusCatalog == nil || len(result.StatusCatalog) == 0 {
		t.Fatalf("expected status catalog")
	}
}


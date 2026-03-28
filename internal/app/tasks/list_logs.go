package tasks

import (
	"context"

	"github.com/ryancheng/telegram-downloader/internal/domain"
)

type LogsStore interface {
	ListLogs(ctx context.Context, query domain.LogQuery) (domain.LogListResult, error)
	BuildSummary(ctx context.Context) (domain.Summary, error)
}

type ListLogsService struct {
	Store LogsStore
}

func NewListLogsService(store LogsStore) *ListLogsService {
	return &ListLogsService{Store: store}
}

func (s *ListLogsService) List(ctx context.Context, query domain.LogQuery) (domain.LogsResponse, error) {
	result, err := s.Store.ListLogs(ctx, query)
	if err != nil {
		return domain.LogsResponse{}, err
	}
	summary, err := s.Store.BuildSummary(ctx)
	if err != nil {
		return domain.LogsResponse{}, err
	}
	return domain.LogsResponse{
		Logs:           result.Logs,
		Total:          result.Total,
		Page:           result.Page,
		PerPage:        result.PerPage,
		TotalPages:     result.TotalPages,
		HasActiveTasks: summary.ActiveTasks > 0,
		Filters: domain.LogFilters{
			Status: query.Status,
			Query:  query.Keyword,
		},
		StatusCatalog: domain.CopyStatusCatalog(),
		Summary:       summary,
	}, nil
}


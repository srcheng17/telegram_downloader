package httpapi

import (
	"context"
	"math"
	"strings"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	legacydomain "github.com/ryancheng/telegram-downloader/internal/domain"
	taskcoredomain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

type taskCoreLogsResponse struct {
	Logs           []taskCoreView                     `json:"logs"`
	Total          int                                `json:"total"`
	Page           int                                `json:"page"`
	PerPage        int                                `json:"per_page"`
	TotalPages     int                                `json:"total_pages"`
	HasActiveTasks bool                               `json:"has_active_tasks"`
	Filters        legacydomain.LogFilters            `json:"filters"`
	StatusCatalog  map[string]legacydomain.StatusMeta `json:"status_catalog"`
	Summary        legacydomain.Summary               `json:"summary"`
}

func (a *API) readTaskCoreLogs(ctx context.Context, query legacydomain.LogQuery, rawStatus string) (taskCoreLogsResponse, error) {

	statusFilter := normalizeTaskCoreStatusFilter(taskCoreFirstNonEmpty(rawStatus, query.Status))
	keyword := strings.TrimSpace(query.Keyword)
	perPage := clampInt(query.PerPage, 1, legacydomain.MaxLogsPerPage)
	page := clampInt(query.Page, 1, math.MaxInt/perPage)
	result, err := a.taskCoreService.QueryTasks(ctx, app.TaskQuery{Status: statusFilter, Keyword: keyword, Limit: perPage, Offset: (page - 1) * perPage})
	if err != nil {
		return taskCoreLogsResponse{}, err
	}
	total := result.Total
	totalPages := 1
	if total > 0 {
		totalPages = (total-1)/perPage + 1
	}
	if page > totalPages {
		page = totalPages
		result, err = a.taskCoreService.QueryTasks(ctx, app.TaskQuery{Status: statusFilter, Keyword: keyword, Limit: perPage, Offset: (page - 1) * perPage})
		if err != nil {
			return taskCoreLogsResponse{}, err
		}
	}
	logs := make([]taskCoreView, 0, len(result.Tasks))
	for _, view := range result.Tasks {
		logs = append(logs, presentTaskCoreView(view, strings.TrimSpace(a.komgaRootDir) != ""))
	}
	summary, err := a.buildTaskCoreSummary(ctx)
	if err != nil {
		return taskCoreLogsResponse{}, err
	}

	return taskCoreLogsResponse{
		Logs:           logs,
		Total:          total,
		Page:           page,
		PerPage:        perPage,
		TotalPages:     totalPages,
		HasActiveTasks: summary.ActiveTasks > 0,
		Filters: legacydomain.LogFilters{
			Status: string(statusFilter),
			Query:  keyword,
		},
		StatusCatalog: taskCoreStatusCatalog(),
		Summary:       summary,
	}, nil
}

func (a *API) buildTaskCoreSummary(ctx context.Context) (legacydomain.Summary, error) {
	counts, err := a.taskCoreService.StatusCounts(ctx)
	if err != nil {
		return legacydomain.Summary{}, err
	}
	legacyCounts := map[string]int{}
	for status, count := range counts {
		switch status {
		case taskcoredomain.StatusCreated, taskcoredomain.StatusReady:
			legacyCounts[legacydomain.StatusPending] += count
		case taskcoredomain.StatusRunning:
			legacyCounts[legacydomain.StatusInProgress] += count
		case taskcoredomain.StatusCanceling:
			legacyCounts[legacydomain.StatusCancelRequested] += count
		case taskcoredomain.StatusSucceeded:
			legacyCounts[legacydomain.StatusSuccess] += count
		case taskcoredomain.StatusFailed:
			legacyCounts[legacydomain.StatusFailed] += count
		case taskcoredomain.StatusCanceled:
			legacyCounts[legacydomain.StatusCanceled] += count
		default:
			legacyCounts[string(status)] += count
		}
	}
	return buildSummaryFromCounts(legacyCounts, legacydomain.DefaultStartupRecovery()), nil
}

func normalizeTaskCoreStatusFilter(raw string) taskcoredomain.Status {
	normalized := taskcoredomain.NormalizeStatus(raw)
	switch normalized {
	case taskcoredomain.StatusCreated,
		taskcoredomain.StatusReady,
		taskcoredomain.StatusRunning,
		taskcoredomain.StatusCanceling,
		taskcoredomain.StatusSucceeded,
		taskcoredomain.StatusFailed,
		taskcoredomain.StatusCanceled:
		return normalized
	}

	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case legacydomain.StatusPending, "QUEUED":
		return taskcoredomain.StatusReady
	case legacydomain.StatusInProgress:
		return taskcoredomain.StatusRunning
	case legacydomain.StatusCancelRequested:
		return taskcoredomain.StatusCanceling
	case legacydomain.StatusSuccess:
		return taskcoredomain.StatusSucceeded
	case legacydomain.StatusFailed:
		return taskcoredomain.StatusFailed
	case legacydomain.StatusCanceled:
		return taskcoredomain.StatusCanceled
	default:
		return ""
	}
}

func taskCoreFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func taskCoreStatusCatalog() map[string]legacydomain.StatusMeta {
	return map[string]legacydomain.StatusMeta{
		string(taskcoredomain.StatusCreated): {
			Label:       taskCoreStatusLabel(taskcoredomain.StatusCreated),
			CanCancel:   true,
			CanDownload: false,
			Terminal:    false,
		},
		string(taskcoredomain.StatusReady): {
			Label:       taskCoreStatusLabel(taskcoredomain.StatusReady),
			CanCancel:   true,
			CanDownload: false,
			Terminal:    false,
		},
		string(taskcoredomain.StatusRunning): {
			Label:       taskCoreStatusLabel(taskcoredomain.StatusRunning),
			CanCancel:   true,
			CanDownload: false,
			Terminal:    false,
		},
		string(taskcoredomain.StatusCanceling): {
			Label:       taskCoreStatusLabel(taskcoredomain.StatusCanceling),
			CanCancel:   false,
			CanDownload: false,
			Terminal:    false,
		},
		string(taskcoredomain.StatusSucceeded): {
			Label:       taskCoreStatusLabel(taskcoredomain.StatusSucceeded),
			CanCancel:   false,
			CanDownload: true,
			Terminal:    true,
		},
		string(taskcoredomain.StatusFailed): {
			Label:       taskCoreStatusLabel(taskcoredomain.StatusFailed),
			CanCancel:   false,
			CanDownload: false,
			Terminal:    true,
		},
		string(taskcoredomain.StatusCanceled): {
			Label:       taskCoreStatusLabel(taskcoredomain.StatusCanceled),
			CanCancel:   false,
			CanDownload: false,
			Terminal:    true,
		},
	}
}

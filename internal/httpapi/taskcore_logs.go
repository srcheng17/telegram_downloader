package httpapi

import (
	"context"
	"math"
	"strings"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	legacydomain "github.com/ryancheng/telegram-downloader/internal/domain"
	taskcoredomain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

const (
	taskCoreLogsBatchSize = 500
	taskCoreLogsMaxScan   = 10000
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
	views, err := a.listAllTaskCoreViews(ctx)
	if err != nil {
		return taskCoreLogsResponse{}, err
	}

	statusFilter := normalizeTaskCoreStatusFilter(taskCoreFirstNonEmpty(rawStatus, query.Status))
	keyword := strings.TrimSpace(query.Keyword)
	filtered := make([]app.TaskView, 0, len(views))
	for _, view := range views {
		if statusFilter != "" && view.Task.Status != statusFilter {
			continue
		}
		if keyword != "" && !taskCoreViewMatchesKeyword(view, keyword) {
			continue
		}
		filtered = append(filtered, view)
	}

	page := clampInt(query.Page, 1, math.MaxInt)
	perPage := clampInt(query.PerPage, 1, legacydomain.MaxLogsPerPage)
	total := len(filtered)
	totalPages := 1
	if total > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * perPage
	end := start + perPage
	if end > total {
		end = total
	}
	if start > end {
		start = end
	}

	logs := make([]taskCoreView, 0, end-start)
	for _, view := range filtered[start:end] {
		logs = append(logs, presentTaskCoreView(view, strings.TrimSpace(a.komgaRootDir) != ""))
	}
	summary := buildTaskCoreSummaryFromViews(views)

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
	views, err := a.listAllTaskCoreViews(ctx)
	if err != nil {
		return legacydomain.Summary{}, err
	}
	return buildTaskCoreSummaryFromViews(views), nil
}

func (a *API) listAllTaskCoreViews(ctx context.Context) ([]app.TaskView, error) {
	if a == nil || a.taskCoreService == nil {
		return nil, nil
	}
	views := make([]app.TaskView, 0)
	for offset := 0; offset < taskCoreLogsMaxScan; {
		limit := taskCoreLogsBatchSize
		if remaining := taskCoreLogsMaxScan - offset; remaining < limit {
			limit = remaining
		}
		page, err := a.taskCoreService.ListTasks(ctx, limit, offset)
		if err != nil {
			return nil, err
		}
		views = append(views, page...)
		if len(page) < limit {
			break
		}
		offset += len(page)
		if len(page) == 0 {
			break
		}
	}
	return views, nil
}

func buildTaskCoreSummaryFromViews(views []app.TaskView) legacydomain.Summary {
	counts := map[string]int{}
	for _, view := range views {
		switch view.Task.Status {
		case taskcoredomain.StatusCreated, taskcoredomain.StatusReady:
			counts[legacydomain.StatusPending]++
		case taskcoredomain.StatusRunning:
			counts[legacydomain.StatusInProgress]++
		case taskcoredomain.StatusCanceling:
			counts[legacydomain.StatusCancelRequested]++
		case taskcoredomain.StatusSucceeded:
			counts[legacydomain.StatusSuccess]++
		case taskcoredomain.StatusFailed:
			counts[legacydomain.StatusFailed]++
		case taskcoredomain.StatusCanceled:
			counts[legacydomain.StatusCanceled]++
		default:
			counts[string(view.Task.Status)]++
		}
	}
	return buildSummaryFromCounts(counts, legacydomain.DefaultStartupRecovery())
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

func taskCoreViewMatchesKeyword(view app.TaskView, keyword string) bool {
	needle := strings.ToLower(strings.TrimSpace(keyword))
	if needle == "" {
		return true
	}
	values := []string{
		view.Task.ID,
		string(view.Task.Kind),
		string(view.Task.Status),
		view.Task.LastError,
		view.Input.URL,
		view.Input.CanonicalURL,
		view.Input.SourceArchiveName,
		view.Input.SourceArchivePath,
	}
	if view.Result != nil {
		values = append(values, view.Result.ArtifactName, view.Result.ArtifactPath, view.Result.KomgaTargetPath)
	}
	for _, value := range view.Input.Metadata {
		values = append(values, value)
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
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

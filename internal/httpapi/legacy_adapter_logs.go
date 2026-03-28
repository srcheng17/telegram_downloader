package httpapi

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
	"github.com/ryancheng/telegram-downloader/internal/domain"
	"github.com/ryancheng/telegram-downloader/internal/httpv2"
)

func (a *LegacyAdapter) ReadLogs(ctx context.Context, query domain.LogQuery) (domain.LogsResponse, error) {
	if !a.SupportsLogs() {
		return domain.LogsResponse{}, errors.New("legacy adapter log dependencies are not configured")
	}

	service := apptasks.NewListLogsService(legacyLogsStoreAdapter{adapter: a})
	return service.List(ctx, query)
}

func (a *LegacyAdapter) BuildSummary(ctx context.Context) (domain.Summary, error) {
	if a.bridge != nil {
		result, err := a.bridge.BuildSummary(ctx)
		if err != nil {
			return domain.Summary{}, err
		}
		return result.Summary, nil
	}
	return a.buildSummary(ctx)
}

func (a *LegacyAdapter) buildSummary(ctx context.Context) (domain.Summary, error) {
	if !a.SupportsSummary() {
		return domain.Summary{}, errors.New("legacy adapter summary dependencies are not configured")
	}

	counts, err := a.getStatusCounts(ctx)
	if err != nil {
		return domain.Summary{}, err
	}

	pending := safeCount(counts[httpv2.TaskStatusQueued])
	uploading := safeCount(counts[httpv2.TaskStatusUploading])
	inProgress := safeCount(counts[httpv2.TaskStatusRunning])
	cancelRequested := safeCount(counts[httpv2.TaskStatusCancelRequested])
	success := safeCount(counts[httpv2.TaskStatusSuccess])
	failed := safeCount(counts[httpv2.TaskStatusFailed])
	canceled := safeCount(counts[httpv2.TaskStatusCanceled])

	return buildSummaryFromCounts(map[string]int{
		domain.StatusUploading:       uploading,
		domain.StatusPending:         pending,
		domain.StatusInProgress:      inProgress,
		domain.StatusCancelRequested: cancelRequested,
		domain.StatusSuccess:         success,
		domain.StatusFailed:          failed,
		domain.StatusCanceled:        canceled,
	}, domain.DefaultStartupRecovery()), nil
}

type legacyBridgeSummaryReader struct {
	adapter *LegacyAdapter
}

func (r legacyBridgeSummaryReader) GetStatusCounts(ctx context.Context) (map[string]int, error) {
	if r.adapter == nil {
		return nil, errors.New("legacy bridge summary reader is not configured")
	}
	return r.adapter.getStatusCounts(ctx)
}

func (a *LegacyAdapter) getStatusCounts(ctx context.Context) (map[string]int, error) {
	if summaryStore, ok := any(a.store).(httpv2.TaskSummaryStore); ok {
		return summaryStore.GetTaskStatusCounts(ctx)
	}

	counts := map[string]int{
		httpv2.TaskStatusUploading:       0,
		httpv2.TaskStatusQueued:          0,
		httpv2.TaskStatusRunning:         0,
		httpv2.TaskStatusCancelRequested: 0,
		httpv2.TaskStatusSuccess:         0,
		httpv2.TaskStatusFailed:          0,
		httpv2.TaskStatusCanceled:        0,
	}
	statuses := []string{
		httpv2.TaskStatusUploading,
		httpv2.TaskStatusQueued,
		httpv2.TaskStatusRunning,
		httpv2.TaskStatusCancelRequested,
		httpv2.TaskStatusSuccess,
		httpv2.TaskStatusFailed,
		httpv2.TaskStatusCanceled,
	}
	for _, status := range statuses {
		result, err := a.store.ListTasks(ctx, httpv2.ListTasksQuery{
			Page:    1,
			PerPage: 1,
			Status:  status,
		})
		if err != nil {
			return nil, err
		}
		counts[status] = safeCount(result.Total)
	}
	return counts, nil
}

func mapV2TaskToLegacyLog(task httpv2.Task) domain.TaskLog {
	startTime := float64(task.CreatedAt.UnixNano()) / float64(time.Second)
	return domain.TaskLog{
		ID:                strings.TrimSpace(task.ID),
		URL:               strings.TrimSpace(task.URL),
		CanonicalURL:      task.CanonicalURL,
		Status:            mapV2StatusToLegacy(task.Status),
		Progress:          task.Progress,
		TotalImages:       task.TotalImages,
		TaskType:          task.TaskType,
		SourceArchiveName: task.SourceArchiveName,
		UploadLoadedBytes: task.UploadLoadedBytes,
		UploadTotalBytes:  task.UploadTotalBytes,
		Retryable:         canRetryTask(&task),
		StartTime:         startTime,
		Error:             task.Error,
		ImageConcurrency:  0,
		ResultZipPath:     task.ResultZipPath,
		Author:            task.Author,
		SeriesName:        task.SeriesName,
		ComicName:         task.ComicName,
		Summary:           task.Summary,
		TagsRaw:           task.TagsRaw,
		TagsNormalized:    task.TagsNormalized,
		GenresRaw:         task.GenresRaw,
		GenresNormalized:  task.GenresNormalized,
	}
}

type legacyLogsStoreAdapter struct {
	adapter *LegacyAdapter
}

func (a legacyLogsStoreAdapter) ListLogs(ctx context.Context, query domain.LogQuery) (domain.LogListResult, error) {
	if a.adapter == nil || a.adapter.store == nil {
		return domain.LogListResult{}, errors.New("legacy logs store adapter is not configured")
	}

	page := clampInt(query.Page, 1, math.MaxInt)
	perPage := clampInt(query.PerPage, 1, domain.MaxLogsPerPage)
	v2Status := mapLegacyStatusToV2(query.Status)

	listResult, err := a.adapter.store.ListTasks(ctx, httpv2.ListTasksQuery{
		Page:    page,
		PerPage: perPage,
		Status:  v2Status,
		Query:   strings.TrimSpace(query.Keyword),
	})
	if err != nil {
		return domain.LogListResult{}, err
	}

	if listResult.Page <= 0 {
		listResult.Page = page
	}
	if listResult.PerPage <= 0 {
		listResult.PerPage = perPage
	}
	if listResult.TotalPages <= 0 && listResult.Total > 0 && listResult.PerPage > 0 {
		listResult.TotalPages = int(math.Ceil(float64(listResult.Total) / float64(listResult.PerPage)))
	}
	if listResult.Tasks == nil {
		listResult.Tasks = make([]httpv2.Task, 0)
	}

	logs := make([]domain.TaskLog, 0, len(listResult.Tasks))
	for _, task := range listResult.Tasks {
		logs = append(logs, mapV2TaskToLegacyLog(task))
	}

	return domain.LogListResult{
		Logs:       logs,
		Total:      listResult.Total,
		Page:       listResult.Page,
		PerPage:    listResult.PerPage,
		TotalPages: listResult.TotalPages,
	}, nil
}

func (a legacyLogsStoreAdapter) BuildSummary(ctx context.Context) (domain.Summary, error) {
	if a.adapter == nil {
		return domain.Summary{}, errors.New("legacy logs store adapter is not configured")
	}
	return a.adapter.BuildSummary(ctx)
}

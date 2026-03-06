package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/httpv2"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/service"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres"
)

const (
	legacyAdapterScanPerPage = 100
)

var (
	ErrLegacyAdapterEnqueueFailed       = errors.New("legacy adapter enqueue failed")
	ErrLegacyAdapterTaskNotFound        = errors.New("legacy adapter task not found")
	ErrLegacyAdapterTaskNotReady        = errors.New("legacy adapter task not ready")
	ErrLegacyAdapterTaskOutputNotFound  = errors.New("legacy adapter task output not found")
	ErrLegacyAdapterArtifactUnavailable = errors.New("legacy adapter artifact unavailable")
)

type LegacyV2TaskStore interface {
	CreateTask(ctx context.Context, in httpv2.CreateTaskInput) (httpv2.Task, error)
	ListTasks(ctx context.Context, in httpv2.ListTasksQuery) (httpv2.ListTasksResult, error)
	GetTask(ctx context.Context, taskID string) (*httpv2.Task, error)
	CancelTask(ctx context.Context, taskID, fromStatus string) error
	MarkTaskFailed(ctx context.Context, taskID, message string) error
}

type LegacyV2TaskQueue interface {
	Enqueue(ctx context.Context, message httpv2.TaskQueueMessage) error
}

type LegacyV2ArtifactService interface {
	OpenArtifact(resultZipPath string) (*service.OpenedV2Artifact, error)
}

type LegacyDownloadDecision string

const (
	LegacyDownloadDecisionCreated      LegacyDownloadDecision = "created"
	LegacyDownloadDecisionReuseSuccess LegacyDownloadDecision = "reuse_success"
	LegacyDownloadDecisionReuseActive  LegacyDownloadDecision = "reuse_active"
)

type LegacyDownloadInput struct {
	RawURL       string
	CanonicalURL string
	Force        bool
}

type LegacyDownloadResult struct {
	Decision LegacyDownloadDecision
	TaskID   string
}

type LegacyCancelDecision string

const (
	LegacyCancelDecisionNotFound         LegacyCancelDecision = "not_found"
	LegacyCancelDecisionAlreadyFinished  LegacyCancelDecision = "already_finished"
	LegacyCancelDecisionAlreadyRequested LegacyCancelDecision = "already_requested"
	LegacyCancelDecisionRequested        LegacyCancelDecision = "requested"
)

type LegacyCancelResult struct {
	Decision LegacyCancelDecision
	Status   string
}

type LegacyAdapter struct {
	store     LegacyV2TaskStore
	queue     LegacyV2TaskQueue
	artifacts LegacyV2ArtifactService
}

func NewLegacyAdapter(store LegacyV2TaskStore, queue LegacyV2TaskQueue, artifacts LegacyV2ArtifactService) *LegacyAdapter {
	if store == nil {
		return nil
	}
	if artifacts == nil {
		artifacts = service.NewV2ArtifactService(service.V2ArtifactServiceConfig{})
	}
	return &LegacyAdapter{
		store:     store,
		queue:     queue,
		artifacts: artifacts,
	}
}

func (a *LegacyAdapter) SupportsSummary() bool {
	return a != nil && a.store != nil
}

func (a *LegacyAdapter) SupportsLogs() bool {
	return a != nil && a.store != nil
}

func (a *LegacyAdapter) SupportsDownload() bool {
	return a != nil && a.store != nil && a.queue != nil
}

func (a *LegacyAdapter) SupportsTaskActions() bool {
	return a != nil && a.store != nil
}

func (a *LegacyAdapter) SupportsArtifactDownload() bool {
	return a != nil && a.store != nil && a.artifacts != nil
}

func (a *LegacyAdapter) CreateOrReuseDownloadTask(ctx context.Context, input LegacyDownloadInput) (LegacyDownloadResult, error) {
	if !a.SupportsDownload() {
		return LegacyDownloadResult{}, errors.New("legacy adapter download dependencies are not configured")
	}

	rawURL := strings.TrimSpace(input.RawURL)
	canonicalURL := canonicalizeURL(input.CanonicalURL, rawURL)

	if !input.Force {
		reusableTask, err := a.findReusableSuccessTask(ctx, canonicalURL)
		if err != nil {
			return LegacyDownloadResult{}, err
		}
		if reusableTask != nil {
			return LegacyDownloadResult{
				Decision: LegacyDownloadDecisionReuseSuccess,
				TaskID:   strings.TrimSpace(reusableTask.ID),
			}, nil
		}
	}

	activeTask, err := a.findActiveTask(ctx, canonicalURL)
	if err != nil {
		return LegacyDownloadResult{}, err
	}
	if activeTask != nil {
		return LegacyDownloadResult{
			Decision: LegacyDownloadDecisionReuseActive,
			TaskID:   strings.TrimSpace(activeTask.ID),
		}, nil
	}

	createInput := httpv2.CreateTaskInput{
		ID:           uuid.NewString(),
		URL:          rawURL,
		CanonicalURL: stringPtr(canonicalURL),
		EnqueueToken: uuid.NewString(),
	}
	createdTask, err := a.store.CreateTask(ctx, createInput)
	if err != nil {
		return LegacyDownloadResult{}, err
	}

	taskID := strings.TrimSpace(createdTask.ID)
	if taskID == "" {
		return LegacyDownloadResult{}, errors.New("created v2 task missing id")
	}

	if err := a.queue.Enqueue(ctx, httpv2.TaskQueueMessage{
		TaskID: taskID,
		Token:  createInput.EnqueueToken,
	}); err != nil {
		compensationCtx, compensationCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = a.store.MarkTaskFailed(compensationCtx, taskID, fmt.Sprintf("enqueue failed: %v", err))
		compensationCancel()
		return LegacyDownloadResult{}, fmt.Errorf("%w: %v", ErrLegacyAdapterEnqueueFailed, err)
	}

	return LegacyDownloadResult{
		Decision: LegacyDownloadDecisionCreated,
		TaskID:   taskID,
	}, nil
}

func (a *LegacyAdapter) ReadLogs(ctx context.Context, query domain.LogQuery) (domain.LogsResponse, error) {
	if !a.SupportsLogs() {
		return domain.LogsResponse{}, errors.New("legacy adapter log dependencies are not configured")
	}

	page := clampInt(query.Page, 1, math.MaxInt)
	perPage := clampInt(query.PerPage, 1, domain.MaxLogsPerPage)
	v2Status := mapLegacyStatusToV2(query.Status)

	listResult, err := a.store.ListTasks(ctx, httpv2.ListTasksQuery{
		Page:    page,
		PerPage: perPage,
		Status:  v2Status,
		Query:   strings.TrimSpace(query.Keyword),
	})
	if err != nil {
		return domain.LogsResponse{}, err
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

	summary, err := a.BuildSummary(ctx)
	if err != nil {
		return domain.LogsResponse{}, err
	}

	response := domain.LogsResponse{
		Logs:           logs,
		Total:          listResult.Total,
		Page:           listResult.Page,
		PerPage:        listResult.PerPage,
		TotalPages:     listResult.TotalPages,
		HasActiveTasks: summary.ActiveTasks > 0,
		Filters: domain.LogFilters{
			Status: query.Status,
			Query:  query.Keyword,
		},
		StatusCatalog: domain.CopyStatusCatalog(),
		Summary:       summary,
	}
	return response, nil
}

func (a *LegacyAdapter) BuildSummary(ctx context.Context) (domain.Summary, error) {
	if !a.SupportsSummary() {
		return domain.Summary{}, errors.New("legacy adapter summary dependencies are not configured")
	}

	counts, err := a.getStatusCounts(ctx)
	if err != nil {
		return domain.Summary{}, err
	}

	pending := safeCount(counts[httpv2.TaskStatusQueued])
	inProgress := safeCount(counts[httpv2.TaskStatusRunning])
	success := safeCount(counts[httpv2.TaskStatusSuccess])
	failed := safeCount(counts[httpv2.TaskStatusFailed])
	canceled := safeCount(counts[httpv2.TaskStatusCanceled])

	summary := domain.Summary{
		PendingTasks:         pending,
		InProgressTasks:      inProgress,
		CancelRequestedTasks: 0,
		CanceledTasks:        canceled,
		SuccessTasks:         success,
		FailedTasks:          failed,
		StartupRecovery:      domain.DefaultStartupRecovery(),
	}
	summary.TotalTasks = pending + inProgress + success + failed + canceled
	summary.ActiveTasks = pending + inProgress
	summary.FinishedTasks = success + failed + canceled
	if summary.FinishedTasks > 0 {
		rate := float64(summary.SuccessTasks) / float64(summary.FinishedTasks) * 100
		rounded := math.Round(rate*10) / 10
		summary.SuccessRate = &rounded
	}
	return summary, nil
}

func (a *LegacyAdapter) CancelTask(ctx context.Context, taskID string) (LegacyCancelResult, error) {
	if !a.SupportsTaskActions() {
		return LegacyCancelResult{}, errors.New("legacy adapter cancel dependencies are not configured")
	}

	normalizedID := strings.TrimSpace(taskID)
	task, err := a.store.GetTask(ctx, normalizedID)
	if err != nil {
		return LegacyCancelResult{}, err
	}
	if task == nil {
		return LegacyCancelResult{Decision: LegacyCancelDecisionNotFound}, nil
	}

	status := httpv2.NormalizeTaskStatus(task.Status)
	switch status {
	case httpv2.TaskStatusCanceled:
		return LegacyCancelResult{
			Decision: LegacyCancelDecisionAlreadyRequested,
			Status:   domain.StatusCanceled,
		}, nil
	case httpv2.TaskStatusSuccess, httpv2.TaskStatusFailed:
		return LegacyCancelResult{
			Decision: LegacyCancelDecisionAlreadyFinished,
			Status:   mapV2StatusToLegacy(status),
		}, nil
	case httpv2.TaskStatusQueued, httpv2.TaskStatusRunning:
	default:
		return LegacyCancelResult{
			Decision: LegacyCancelDecisionAlreadyFinished,
			Status:   mapV2StatusToLegacy(status),
		}, nil
	}

	if err := a.store.CancelTask(ctx, normalizedID, status); err != nil {
		if errors.Is(err, postgres.ErrV2TaskStatusMismatchOrNotFound) {
			latestTask, latestErr := a.store.GetTask(ctx, normalizedID)
			if latestErr != nil {
				return LegacyCancelResult{}, latestErr
			}
			if latestTask != nil && httpv2.NormalizeTaskStatus(latestTask.Status) == httpv2.TaskStatusCanceled {
				return LegacyCancelResult{
					Decision: LegacyCancelDecisionAlreadyRequested,
					Status:   domain.StatusCanceled,
				}, nil
			}
			return LegacyCancelResult{
				Decision: LegacyCancelDecisionAlreadyFinished,
				Status:   mapV2StatusToLegacy(status),
			}, nil
		}
		return LegacyCancelResult{}, err
	}

	return LegacyCancelResult{
		Decision: LegacyCancelDecisionRequested,
		Status:   domain.StatusCanceled,
	}, nil
}

func (a *LegacyAdapter) OpenTaskArtifact(ctx context.Context, taskID string) (*service.OpenedV2Artifact, error) {
	if !a.SupportsArtifactDownload() {
		return nil, errors.New("legacy adapter artifact dependencies are not configured")
	}

	normalizedID := strings.TrimSpace(taskID)
	task, err := a.store.GetTask(ctx, normalizedID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, ErrLegacyAdapterTaskNotFound
	}
	if httpv2.NormalizeTaskStatus(task.Status) != httpv2.TaskStatusSuccess {
		return nil, ErrLegacyAdapterTaskNotReady
	}

	resultPath := ""
	if task.ResultZipPath != nil {
		resultPath = strings.TrimSpace(*task.ResultZipPath)
	}
	if resultPath == "" {
		return nil, ErrLegacyAdapterTaskOutputNotFound
	}

	artifact, err := a.artifacts.OpenArtifact(resultPath)
	if err != nil {
		if errors.Is(err, service.ErrV2ArtifactNotFound) || errors.Is(err, service.ErrV2ArtifactPathInvalid) {
			return nil, ErrLegacyAdapterArtifactUnavailable
		}
		return nil, err
	}
	return artifact, nil
}

func (a *LegacyAdapter) getStatusCounts(ctx context.Context) (map[string]int, error) {
	if summaryStore, ok := any(a.store).(httpv2.TaskSummaryStore); ok {
		return summaryStore.GetTaskStatusCounts(ctx)
	}

	counts := map[string]int{
		httpv2.TaskStatusQueued:   0,
		httpv2.TaskStatusRunning:  0,
		httpv2.TaskStatusSuccess:  0,
		httpv2.TaskStatusFailed:   0,
		httpv2.TaskStatusCanceled: 0,
	}
	statuses := []string{
		httpv2.TaskStatusQueued,
		httpv2.TaskStatusRunning,
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

func (a *LegacyAdapter) findReusableSuccessTask(ctx context.Context, canonicalURL string) (*httpv2.Task, error) {
	tasks, err := a.listMatchingTasks(ctx, httpv2.TaskStatusSuccess, canonicalURL)
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		resultPath := ""
		if tasks[i].ResultZipPath != nil {
			resultPath = strings.TrimSpace(*tasks[i].ResultZipPath)
		}
		if isSafeExistingDownloadFile(resultPath) {
			return &tasks[i], nil
		}
	}
	return nil, nil
}

func (a *LegacyAdapter) findActiveTask(ctx context.Context, canonicalURL string) (*httpv2.Task, error) {
	queuedTasks, err := a.listMatchingTasks(ctx, httpv2.TaskStatusQueued, canonicalURL)
	if err != nil {
		return nil, err
	}
	runningTasks, err := a.listMatchingTasks(ctx, httpv2.TaskStatusRunning, canonicalURL)
	if err != nil {
		return nil, err
	}
	var selected *httpv2.Task
	selectTask := func(candidate *httpv2.Task) {
		if candidate == nil {
			return
		}
		if selected == nil || candidate.CreatedAt.After(selected.CreatedAt) {
			selected = candidate
		}
	}
	if len(queuedTasks) > 0 {
		selectTask(&queuedTasks[0])
	}
	if len(runningTasks) > 0 {
		selectTask(&runningTasks[0])
	}
	return selected, nil
}

func (a *LegacyAdapter) listMatchingTasks(ctx context.Context, status string, canonicalURL string) ([]httpv2.Task, error) {
	result, err := a.store.ListTasks(ctx, httpv2.ListTasksQuery{
		Page:    1,
		PerPage: legacyAdapterScanPerPage,
		Status:  strings.TrimSpace(status),
		Query:   canonicalURL,
	})
	if err != nil {
		return nil, err
	}
	if result.Tasks == nil {
		return []httpv2.Task{}, nil
	}
	matched := make([]httpv2.Task, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		if canonicalizeURL(taskCanonicalURL(task), task.URL) == canonicalURL {
			matched = append(matched, task)
		}
	}
	return matched, nil
}

func mapLegacyStatusToV2(status string) string {
	switch strings.TrimSpace(status) {
	case domain.StatusPending:
		return httpv2.TaskStatusQueued
	case domain.StatusInProgress, domain.StatusCancelRequested:
		return httpv2.TaskStatusRunning
	case domain.StatusSuccess:
		return httpv2.TaskStatusSuccess
	case domain.StatusFailed:
		return httpv2.TaskStatusFailed
	case domain.StatusCanceled:
		return httpv2.TaskStatusCanceled
	default:
		return ""
	}
}

func mapV2StatusToLegacy(status string) string {
	switch httpv2.NormalizeTaskStatus(status) {
	case httpv2.TaskStatusQueued:
		return domain.StatusPending
	case httpv2.TaskStatusRunning:
		return domain.StatusInProgress
	case httpv2.TaskStatusSuccess:
		return domain.StatusSuccess
	case httpv2.TaskStatusFailed:
		return domain.StatusFailed
	case httpv2.TaskStatusCanceled:
		return domain.StatusCanceled
	default:
		return httpv2.NormalizeTaskStatus(status)
	}
}

func mapV2TaskToLegacyLog(task httpv2.Task) domain.TaskLog {
	startTime := float64(task.CreatedAt.UnixNano()) / float64(time.Second)
	return domain.TaskLog{
		ID:               strings.TrimSpace(task.ID),
		URL:              strings.TrimSpace(task.URL),
		CanonicalURL:     task.CanonicalURL,
		Status:           mapV2StatusToLegacy(task.Status),
		StartTime:        startTime,
		Error:            task.Error,
		Progress:         0,
		TotalImages:      0,
		ImageConcurrency: 0,
		ResultZipPath:    task.ResultZipPath,
		Author:           nil,
		SeriesName:       nil,
		ComicName:        nil,
		Summary:          nil,
		TagsRaw:          nil,
		TagsNormalized:   nil,
		GenresRaw:        nil,
		GenresNormalized: nil,
	}
}

func taskCanonicalURL(task httpv2.Task) string {
	if task.CanonicalURL != nil {
		candidate := strings.TrimSpace(*task.CanonicalURL)
		if candidate != "" {
			normalized := normalizeTelegraphURL(candidate)
			if normalized != "" {
				return normalized
			}
			return candidate
		}
	}
	candidate := strings.TrimSpace(task.URL)
	normalized := normalizeTelegraphURL(candidate)
	if normalized != "" {
		return normalized
	}
	return candidate
}

func canonicalizeURL(canonicalCandidate string, fallbackURL string) string {
	canonical := strings.TrimSpace(canonicalCandidate)
	if canonical == "" {
		canonical = strings.TrimSpace(fallbackURL)
	}
	normalized := normalizeTelegraphURL(canonical)
	if normalized != "" {
		return normalized
	}
	return canonical
}

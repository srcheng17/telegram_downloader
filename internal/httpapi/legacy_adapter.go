package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
	"github.com/ryancheng/telegram-downloader/internal/domain"
	"github.com/ryancheng/telegram-downloader/internal/httpv2"
	"github.com/ryancheng/telegram-downloader/internal/service"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
)

const (
	legacyAdapterFallbackScanPerPage = 100
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
	CreateUploadTask(ctx context.Context, in httpv2.CreateTaskInput) (httpv2.Task, error)
	ListTasks(ctx context.Context, in httpv2.ListTasksQuery) (httpv2.ListTasksResult, error)
	GetTask(ctx context.Context, taskID string) (*httpv2.Task, error)
	CancelTask(ctx context.Context, taskID, fromStatus string) error
	MarkTaskFailed(ctx context.Context, taskID, message string) error
	UpdateUploadProgress(ctx context.Context, taskID string, loadedBytes, totalBytes int64) error
	MarkUploadTaskQueued(ctx context.Context, taskID, sourceArchivePath string, totalBytes int64) error
	RetryUploadTask(ctx context.Context, taskID, enqueueToken string) error
	InsertMetadataHistory(ctx context.Context, entry httpv2.MetadataHistoryEntry) error
	ListMetadataHistory(ctx context.Context, limit int) ([]httpv2.MetadataHistoryEntry, error)
}

type legacyAtomicClaimStore interface {
	ClaimTaskForLegacy(ctx context.Context, in httpv2.LegacyClaimTaskInput) (httpv2.LegacyClaimTaskResult, error)
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
	RawURL           string
	CanonicalURL     string
	Force            bool
	Author           *string
	SeriesName       *string
	ComicName        *string
	Summary          *string
	TagsRaw          *string
	TagsNormalized   *string
	GenresRaw        *string
	GenresNormalized *string
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
	bridge    *apptasks.LegacyBridge
}

func NewLegacyAdapter(store LegacyV2TaskStore, queue LegacyV2TaskQueue, artifacts LegacyV2ArtifactService) *LegacyAdapter {
	if store == nil {
		return nil
	}
	if artifacts == nil {
		artifacts = service.NewV2ArtifactService(service.V2ArtifactServiceConfig{})
	}
	adapter := &LegacyAdapter{
		store:     store,
		queue:     queue,
		artifacts: artifacts,
	}
	adapter.bridge = apptasks.NewLegacyBridge(
		legacyBridgeClaimer{adapter: adapter},
		legacyBridgeSummaryReader{adapter: adapter},
	)
	return adapter
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
	if a.bridge != nil {
		result, err := a.bridge.Submit(ctx, apptasks.LegacySubmitInput{
			RawURL:       input.RawURL,
			CanonicalURL: input.CanonicalURL,
			ReuseSuccess: !input.Force,
			Metadata: apptasks.MetadataInput{
				Author:     input.Author,
				SeriesName: input.SeriesName,
				ComicName:  input.ComicName,
				Summary:    input.Summary,
				TagsRaw:    input.TagsRaw,
				GenresRaw:  input.GenresRaw,
			},
		})
		if err != nil {
			return LegacyDownloadResult{}, err
		}
		return LegacyDownloadResult{
			Decision: LegacyDownloadDecision(result.Decision),
			TaskID:   result.TaskID,
		}, nil
	}
	return a.createOrReuseDownloadTask(ctx, input)
}

func (a *LegacyAdapter) createOrReuseDownloadTask(ctx context.Context, input LegacyDownloadInput) (LegacyDownloadResult, error) {
	if !a.SupportsDownload() {
		return LegacyDownloadResult{}, errors.New("legacy adapter download dependencies are not configured")
	}

	rawURL := strings.TrimSpace(input.RawURL)
	canonicalURL := canonicalizeURL(input.CanonicalURL, rawURL)
	createInput := httpv2.CreateTaskInput{
		ID:               uuid.NewString(),
		URL:              rawURL,
		CanonicalURL:     stringPtr(canonicalURL),
		EnqueueToken:     uuid.NewString(),
		Author:           input.Author,
		SeriesName:       input.SeriesName,
		ComicName:        input.ComicName,
		Summary:          input.Summary,
		TagsRaw:          input.TagsRaw,
		TagsNormalized:   input.TagsNormalized,
		GenresRaw:        input.GenresRaw,
		GenresNormalized: input.GenresNormalized,
	}
	if claimStore, ok := any(a.store).(legacyAtomicClaimStore); ok {
		reuseSuccess := !input.Force
		retriedAfterInvalidReusable := false

		for {
			claim, err := claimStore.ClaimTaskForLegacy(ctx, httpv2.LegacyClaimTaskInput{
				ID:               createInput.ID,
				URL:              createInput.URL,
				CanonicalURL:     createInput.CanonicalURL,
				EnqueueToken:     createInput.EnqueueToken,
				ReuseSuccess:     reuseSuccess,
				Author:           createInput.Author,
				SeriesName:       createInput.SeriesName,
				ComicName:        createInput.ComicName,
				Summary:          createInput.Summary,
				TagsRaw:          createInput.TagsRaw,
				TagsNormalized:   createInput.TagsNormalized,
				GenresRaw:        createInput.GenresRaw,
				GenresNormalized: createInput.GenresNormalized,
			})
			if err != nil {
				return LegacyDownloadResult{}, err
			}

			switch claim.Decision {
			case httpv2.LegacyClaimTaskDecisionReuseSuccess:
				if isSafeExistingDownloadFile(stringValue(claim.Task.ResultZipPath)) {
					return LegacyDownloadResult{
						Decision: LegacyDownloadDecisionReuseSuccess,
						TaskID:   strings.TrimSpace(claim.Task.ID),
					}, nil
				}
				if retriedAfterInvalidReusable || !reuseSuccess {
					return LegacyDownloadResult{}, errors.New("reusable v2 artifact is unavailable")
				}
				retriedAfterInvalidReusable = true
				reuseSuccess = false
				continue
			case httpv2.LegacyClaimTaskDecisionReuseActive:
				return LegacyDownloadResult{
					Decision: LegacyDownloadDecisionReuseActive,
					TaskID:   strings.TrimSpace(claim.Task.ID),
				}, nil
			case httpv2.LegacyClaimTaskDecisionCreated:
				enqueueToken := strings.TrimSpace(claim.EnqueueToken)
				if enqueueToken == "" {
					enqueueToken = createInput.EnqueueToken
				}
				taskID := strings.TrimSpace(claim.Task.ID)
				if taskID == "" {
					return LegacyDownloadResult{}, errors.New("created v2 task missing id")
				}
				if err := a.enqueueCreatedTask(ctx, taskID, enqueueToken); err != nil {
					return LegacyDownloadResult{}, err
				}
				return LegacyDownloadResult{
					Decision: LegacyDownloadDecisionCreated,
					TaskID:   taskID,
				}, nil
			default:
				return LegacyDownloadResult{}, fmt.Errorf("unknown legacy claim decision: %s", claim.Decision)
			}
		}
	}

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

	createdTask, err := a.store.CreateTask(ctx, createInput)
	if err != nil {
		return LegacyDownloadResult{}, err
	}

	taskID := strings.TrimSpace(createdTask.ID)
	if taskID == "" {
		return LegacyDownloadResult{}, errors.New("created v2 task missing id")
	}
	if err := a.enqueueCreatedTask(ctx, taskID, createInput.EnqueueToken); err != nil {
		return LegacyDownloadResult{}, err
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

type legacyBridgeClaimer struct {
	adapter *LegacyAdapter
}

func (c legacyBridgeClaimer) ClaimOrReuse(ctx context.Context, in apptasks.LegacyClaimInput) (apptasks.LegacyClaimResult, error) {
	if c.adapter == nil {
		return apptasks.LegacyClaimResult{}, errors.New("legacy bridge claimer is not configured")
	}
	result, err := c.adapter.createOrReuseDownloadTask(ctx, LegacyDownloadInput{
		RawURL:           in.RawURL,
		CanonicalURL:     in.CanonicalURL,
		Force:            !in.ReuseSuccess,
		Author:           in.Metadata.Author,
		SeriesName:       in.Metadata.SeriesName,
		ComicName:        in.Metadata.ComicName,
		Summary:          in.Metadata.Summary,
		TagsRaw:          in.Metadata.TagsRaw,
		TagsNormalized:   in.Metadata.TagsNormalized,
		GenresRaw:        in.Metadata.GenresRaw,
		GenresNormalized: in.Metadata.GenresNormalized,
	})
	if err != nil {
		return apptasks.LegacyClaimResult{}, err
	}
	return apptasks.LegacyClaimResult{
		Decision: apptasks.LegacyDownloadDecision(result.Decision),
		TaskID:   result.TaskID,
	}, nil
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

func (a *LegacyAdapter) enqueueCreatedTask(ctx context.Context, taskID string, enqueueToken string) error {
	if err := a.queue.Enqueue(ctx, httpv2.TaskQueueMessage{
		TaskID: strings.TrimSpace(taskID),
		Token:  strings.TrimSpace(enqueueToken),
	}); err != nil {
		compensationCtx, compensationCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = a.store.MarkTaskFailed(compensationCtx, strings.TrimSpace(taskID), fmt.Sprintf("enqueue failed: %v", err))
		compensationCancel()
		return fmt.Errorf("%w: %v", ErrLegacyAdapterEnqueueFailed, err)
	}
	return nil
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
	case httpv2.TaskStatusCancelRequested:
		return LegacyCancelResult{
			Decision: LegacyCancelDecisionAlreadyRequested,
			Status:   domain.StatusCancelRequested,
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
			return a.resolveCancelMismatchByLatestStatus(ctx, normalizedID, latestTask)
		}
		return LegacyCancelResult{}, err
	}

	return LegacyCancelResult{
		Decision: LegacyCancelDecisionRequested,
		Status:   domain.StatusCancelRequested,
	}, nil
}

func (a *LegacyAdapter) resolveCancelMismatchByLatestStatus(
	ctx context.Context,
	taskID string,
	latestTask *httpv2.Task,
) (LegacyCancelResult, error) {
	if latestTask == nil {
		return LegacyCancelResult{Decision: LegacyCancelDecisionNotFound}, nil
	}

	latestStatus := httpv2.NormalizeTaskStatus(latestTask.Status)
	switch latestStatus {
	case httpv2.TaskStatusCanceled:
		return LegacyCancelResult{
			Decision: LegacyCancelDecisionAlreadyRequested,
			Status:   domain.StatusCanceled,
		}, nil
	case httpv2.TaskStatusCancelRequested:
		return LegacyCancelResult{
			Decision: LegacyCancelDecisionAlreadyRequested,
			Status:   domain.StatusCancelRequested,
		}, nil
	case httpv2.TaskStatusSuccess, httpv2.TaskStatusFailed:
		return LegacyCancelResult{
			Decision: LegacyCancelDecisionAlreadyFinished,
			Status:   mapV2StatusToLegacy(latestStatus),
		}, nil
	case httpv2.TaskStatusQueued, httpv2.TaskStatusRunning:
		retryErr := a.store.CancelTask(ctx, taskID, latestStatus)
		if retryErr == nil {
			return LegacyCancelResult{
				Decision: LegacyCancelDecisionRequested,
				Status:   domain.StatusCancelRequested,
			}, nil
		}
		if !errors.Is(retryErr, postgres.ErrV2TaskStatusMismatchOrNotFound) {
			return LegacyCancelResult{}, retryErr
		}

		refreshedTask, refreshedErr := a.store.GetTask(ctx, taskID)
		if refreshedErr != nil {
			return LegacyCancelResult{}, refreshedErr
		}
		if refreshedTask == nil {
			return LegacyCancelResult{Decision: LegacyCancelDecisionNotFound}, nil
		}

		refreshedStatus := httpv2.NormalizeTaskStatus(refreshedTask.Status)
		switch refreshedStatus {
		case httpv2.TaskStatusCanceled:
			return LegacyCancelResult{
				Decision: LegacyCancelDecisionAlreadyRequested,
				Status:   domain.StatusCanceled,
			}, nil
		case httpv2.TaskStatusCancelRequested:
			return LegacyCancelResult{
				Decision: LegacyCancelDecisionAlreadyRequested,
				Status:   domain.StatusCancelRequested,
			}, nil
		case httpv2.TaskStatusSuccess, httpv2.TaskStatusFailed:
			return LegacyCancelResult{
				Decision: LegacyCancelDecisionAlreadyFinished,
				Status:   mapV2StatusToLegacy(refreshedStatus),
			}, nil
		default:
			return LegacyCancelResult{
				Decision: LegacyCancelDecisionAlreadyFinished,
				Status:   mapV2StatusToLegacy(refreshedStatus),
			}, nil
		}
	default:
		return LegacyCancelResult{
			Decision: LegacyCancelDecisionAlreadyFinished,
			Status:   mapV2StatusToLegacy(latestStatus),
		}, nil
	}
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
	cancelRequestedTasks, err := a.listMatchingTasks(ctx, httpv2.TaskStatusCancelRequested, canonicalURL)
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
	if len(cancelRequestedTasks) > 0 {
		selectTask(&cancelRequestedTasks[0])
	}
	return selected, nil
}

func (a *LegacyAdapter) listMatchingTasks(ctx context.Context, status string, canonicalURL string) ([]httpv2.Task, error) {
	result, err := a.store.ListTasks(ctx, httpv2.ListTasksQuery{
		Page:    1,
		PerPage: legacyAdapterFallbackScanPerPage,
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
	case domain.StatusUploading:
		return httpv2.TaskStatusUploading
	case domain.StatusPending:
		return httpv2.TaskStatusQueued
	case domain.StatusInProgress:
		return httpv2.TaskStatusRunning
	case domain.StatusCancelRequested:
		return httpv2.TaskStatusCancelRequested
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
	case httpv2.TaskStatusUploading:
		return domain.StatusUploading
	case httpv2.TaskStatusQueued:
		return domain.StatusPending
	case httpv2.TaskStatusRunning:
		return domain.StatusInProgress
	case httpv2.TaskStatusCancelRequested:
		return domain.StatusCancelRequested
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
		ID:                strings.TrimSpace(task.ID),
		URL:               strings.TrimSpace(task.URL),
		CanonicalURL:      task.CanonicalURL,
		Status:            mapV2StatusToLegacy(task.Status),
		TaskType:          task.TaskType,
		SourceArchiveName: task.SourceArchiveName,
		UploadLoadedBytes: task.UploadLoadedBytes,
		UploadTotalBytes:  task.UploadTotalBytes,
		Retryable:         task.Retryable,
		StartTime:         startTime,
		Error:             task.Error,
		Progress:          0,
		TotalImages:       0,
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

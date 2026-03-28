package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
	"github.com/ryancheng/telegram-downloader/internal/domain"
	"github.com/ryancheng/telegram-downloader/internal/httpv2"
	"github.com/ryancheng/telegram-downloader/internal/service"
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

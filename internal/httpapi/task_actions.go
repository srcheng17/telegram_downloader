package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
	taskdomain "github.com/ryancheng/telegram-downloader/internal/domain/task"
	"github.com/ryancheng/telegram-downloader/internal/httpv2"
)

func (a *API) handleTaskRetry(w http.ResponseWriter, r *http.Request) {
	if a.v2TaskStore == nil || a.v2TaskQueue == nil {
		writeInternalError(w, errors.New("retry dependencies are not configured"))
		return
	}
	taskID := strings.TrimSpace(chi.URLParam(r, "task_id"))
	if taskID == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Task not found.", nil)
		return
	}
	retryService := apptasks.NewRetryService(
		retryTaskStoreAdapter{store: a.v2TaskStore},
		retryTaskQueueAdapter{queue: a.v2TaskQueue},
	)
	retryService.TokenGenerator = uuid.NewString

	result, err := retryService.Retry(r.Context(), taskID)
	if err != nil {
		switch {
		case errors.Is(err, apptasks.ErrTaskNotFound):
			writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
		case errors.Is(err, apptasks.ErrTaskNotRetryable):
			writeAPIErrorResponse(w, http.StatusConflict, apiErrorCodeTaskAlreadyDone, "Only failed retryable tasks can be retried.", nil)
		default:
			writeInternalError(w, err)
		}
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"task_id": result.TaskID,
		"status":  result.Status,
	})
}

func canRetryTask(task *httpv2.Task) bool {
	if task == nil {
		return false
	}
	taskType := ""
	if task.TaskType != nil {
		taskType = strings.ToLower(strings.TrimSpace(*task.TaskType))
	}
	return taskdomain.CanRetry(
		taskType,
		task.Status,
		strings.TrimSpace(task.URL) != "",
		task.SourceArchivePath != nil && strings.TrimSpace(*task.SourceArchivePath) != "",
	)
}

type retryTaskStoreAdapter struct {
	store LegacyV2TaskStore
}

func (a retryTaskStoreAdapter) GetTaskForRetry(ctx context.Context, taskID string) (*apptasks.RetryTaskRecord, error) {
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil || task == nil {
		return nil, err
	}
	return &apptasks.RetryTaskRecord{
		ID:                strings.TrimSpace(task.ID),
		Status:            task.Status,
		TaskType:          stringValue(task.TaskType),
		URL:               task.URL,
		SourceArchivePath: task.SourceArchivePath,
	}, nil
}

func (a retryTaskStoreAdapter) RetryTask(ctx context.Context, taskID, enqueueToken string) error {
	return a.store.RetryUploadTask(ctx, taskID, enqueueToken)
}

type retryTaskQueueAdapter struct {
	queue LegacyV2TaskQueue
}

func (a retryTaskQueueAdapter) Enqueue(ctx context.Context, msg apptasks.QueueMessage) error {
	return a.queue.Enqueue(ctx, httpv2.TaskQueueMessage{
		TaskID: msg.TaskID,
		Token:  msg.Token,
	})
}

type copyTaskStoreAdapter struct {
	store LegacyV2TaskStore
}

func (a copyTaskStoreAdapter) GetTaskForCopy(ctx context.Context, taskID string) (*apptasks.CopyTaskRecord, error) {
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil || task == nil {
		return nil, err
	}
	return &apptasks.CopyTaskRecord{
		ID:            strings.TrimSpace(task.ID),
		Status:        task.Status,
		ResultZipPath: task.ResultZipPath,
		SeriesName:    task.SeriesName,
	}, nil
}

type copyArtifactDescriberAdapter struct {
	artifacts LegacyV2ArtifactService
}

func (a copyArtifactDescriberAdapter) DescribeArtifact(resultPath string) (apptasks.ArtifactDescriptor, error) {
	artifact, err := a.artifacts.OpenArtifact(resultPath)
	if err != nil {
		return apptasks.ArtifactDescriptor{}, ErrLegacyAdapterArtifactUnavailable
	}
	defer artifact.Close()
	return apptasks.ArtifactDescriptor{FileName: artifact.FileName}, nil
}

func (a *API) handleTaskCopyToKomga(w http.ResponseWriter, r *http.Request) {
	if a.v2TaskStore == nil || a.v2ArtifactService == nil {
		writeInternalError(w, errors.New("copy dependencies are not configured"))
		return
	}
	taskID := strings.TrimSpace(chi.URLParam(r, "task_id"))
	if taskID == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Task not found.", nil)
		return
	}
	copyService := apptasks.NewCopyResultService(
		copyTaskStoreAdapter{store: a.v2TaskStore},
		copyArtifactDescriberAdapter{artifacts: a.v2ArtifactService},
		apptasks.NewKomgaCopier(apptasks.KomgaCopyConfig{Root: a.komgaRootDirOrDefault()}),
	)
	result, err := copyService.CopyToKomga(r.Context(), taskID)
	if err != nil {
		switch {
		case errors.Is(err, apptasks.ErrTaskNotFound):
			writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
		case errors.Is(err, apptasks.ErrTaskResultNotReady):
			writeAPIErrorResponse(w, http.StatusConflict, apiErrorCodeTaskNotReady, "Task is not completed yet.", nil)
		case errors.Is(err, ErrLegacyAdapterArtifactUnavailable):
			writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeArtifactUnavailable, "Stored file is unavailable.", nil)
		default:
			writeInternalError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"task_id":     result.TaskID,
		"target_path": result.TargetPath,
	})
}

func (a *API) komgaRootDirOrDefault() string {
	if strings.TrimSpace(a.komgaRootDir) != "" {
		return strings.TrimSpace(a.komgaRootDir)
	}
	return "/Users/ryancheng/docker_data/komga/data/myReadingManga"
}

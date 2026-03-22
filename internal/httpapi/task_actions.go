package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
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
	task, err := a.v2TaskStore.GetTask(r.Context(), taskID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if task == nil {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
		return
	}
	taskType := ""
	if task.TaskType != nil {
		taskType = strings.ToLower(strings.TrimSpace(*task.TaskType))
	}
	canRetry := strings.TrimSpace(task.Status) == "FAILED" && task.Retryable
	if taskType == "upload" {
		canRetry = canRetry && task.SourceArchivePath != nil && strings.TrimSpace(*task.SourceArchivePath) != ""
	}
	if taskType == "url" {
		canRetry = canRetry && strings.TrimSpace(task.URL) != ""
	}
	if !canRetry {
		writeAPIErrorResponse(w, http.StatusConflict, apiErrorCodeTaskAlreadyDone, "Only failed retryable tasks can be retried.", nil)
		return
	}

	enqueueToken := uuid.NewString()
	if err := a.v2TaskStore.RetryUploadTask(r.Context(), taskID, enqueueToken); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := a.v2TaskQueue.Enqueue(r.Context(), httpv2.TaskQueueMessage{
		TaskID: taskID,
		Token:  enqueueToken,
	}); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"task_id": taskID,
		"status":  "QUEUED",
	})
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
	task, err := a.v2TaskStore.GetTask(r.Context(), taskID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if task == nil {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
		return
	}
	if strings.TrimSpace(task.Status) != "SUCCESS" || task.ResultZipPath == nil || strings.TrimSpace(*task.ResultZipPath) == "" {
		writeAPIErrorResponse(w, http.StatusConflict, apiErrorCodeTaskNotReady, "Task is not completed yet.", nil)
		return
	}

	artifact, err := a.v2ArtifactService.OpenArtifact(*task.ResultZipPath)
	if err != nil {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeArtifactUnavailable, "Stored file is unavailable.", nil)
		return
	}
	defer artifact.Close()

	copier := apptasks.NewKomgaCopier(apptasks.KomgaCopyConfig{Root: a.komgaRootDirOrDefault()})
	targetPath, err := copier.CopyFromPath(*task.ResultZipPath, artifact.FileName, stringValue(task.SeriesName))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"task_id":     taskID,
		"target_path": targetPath,
	})
}

func (a *API) komgaRootDirOrDefault() string {
	if strings.TrimSpace(a.komgaRootDir) != "" {
		return strings.TrimSpace(a.komgaRootDir)
	}
	return "/Users/ryancheng/docker_data/komga/data/myReadingManga"
}

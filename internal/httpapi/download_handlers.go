package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ryancheng/telegram-downloader/internal/domain"
	"github.com/ryancheng/telegram-downloader/internal/queue"
)

func (a *API) handleDownload(w http.ResponseWriter, r *http.Request) {
	rawURL, forceDownload, metadata, err := extractDownloadRequest(r)
	if err != nil {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Please provide a Telegraph URL.", nil)
		return
	}

	if rawURL == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Please provide a Telegraph URL.", nil)
		return
	}
	if !isAllowedTelegraphURL(rawURL) {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Only telegra.ph or graph.org URLs are supported.", nil)
		return
	}

	canonicalURL := normalizeTelegraphURL(rawURL)
	if canonicalURL == "" {
		canonicalURL = rawURL
	}

	if a.legacyAdapter != nil && a.legacyAdapter.SupportsDownload() {
		result, err := a.legacyAdapter.CreateOrReuseDownloadTask(r.Context(), LegacyDownloadInput{
			RawURL:           rawURL,
			CanonicalURL:     canonicalURL,
			Force:            forceDownload,
			Author:           metadata.author,
			SeriesName:       metadata.seriesName,
			ComicName:        metadata.comicName,
			Summary:          metadata.summary,
			TagsRaw:          metadata.tagsRaw,
			TagsNormalized:   metadata.tagsNormalized,
			GenresRaw:        metadata.genresRaw,
			GenresNormalized: metadata.genresNormalized,
		})
		if err != nil {
			if errors.Is(err, ErrLegacyAdapterEnqueueFailed) {
				writeAPIErrorResponse(w, http.StatusBadGateway, apiErrorCodeEnqueueFailed, "Failed to enqueue task.", nil)
				return
			}
			writeInternalError(w, err)
			return
		}

		statusCode, payload, err := buildLegacyDownloadDecisionResponse(result.TaskID, result.Decision, forceDownload)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeJSON(w, statusCode, payload)
		return
	}

	runtimeSettings := a.resolveDownloadSettings(r.Context())
	claim, enqueueToken, err := a.claimDownloadTask(
		r.Context(),
		rawURL,
		canonicalURL,
		metadata,
		!forceDownload,
		runtimeSettings.ImageConcurrency,
	)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	taskID := claim.Task.ID
	if taskID == "" {
		writeInternalError(w, errors.New("claimed task missing id"))
		return
	}

	switch claim.Decision {
	case domain.ClaimDecisionReuseSuccess:
		statusCode, payload, err := buildClaimDownloadDecisionResponse(taskID, claim.Decision, forceDownload)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeJSON(w, statusCode, payload)
		return
	case domain.ClaimDecisionReuseActive:
		statusCode, payload, err := buildClaimDownloadDecisionResponse(taskID, claim.Decision, forceDownload)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeJSON(w, statusCode, payload)
		return
	case domain.ClaimDecisionCreated:
		switch {
		case a.downloadQueue != nil:
			err := a.downloadQueue.EnqueueDownload(r.Context(), queue.EnqueueMessage{
				TaskID:       taskID,
				EnqueueToken: enqueueToken,
			})
			if err != nil {
				_ = a.store.MarkTaskFailed(r.Context(), taskID, fmt.Sprintf("failed to enqueue task: %v", err))
				writeAPIErrorResponse(w, http.StatusBadGateway, apiErrorCodeEnqueueFailed, "Failed to enqueue task.", nil)
				return
			}
		case a.downloadSubmitter != nil:
			err := a.downloadSubmitter.SubmitDownload(r.Context(), DownloadSubmitRequest{
				TaskID:           taskID,
				URL:              rawURL,
				Timeout:          runtimeSettings.Timeout,
				Retries:          runtimeSettings.Retries,
				ImageConcurrency: runtimeSettings.ImageConcurrency,
			})
			if err != nil {
				_ = a.store.MarkTaskFailed(r.Context(), taskID, fmt.Sprintf("failed to enqueue task: %v", err))
				writeAPIErrorResponse(w, http.StatusBadGateway, apiErrorCodeEnqueueFailed, "Failed to enqueue task.", nil)
				return
			}
		default:
			_ = a.store.MarkTaskFailed(r.Context(), taskID, "enqueue bridge not configured")
			writeAPIErrorResponse(w, http.StatusServiceUnavailable, apiErrorCodeServiceUnavailable, "Task queue bridge is unavailable.", nil)
			return
		}

		statusCode, payload, err := buildClaimDownloadDecisionResponse(taskID, claim.Decision, forceDownload)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeJSON(w, statusCode, payload)
		return
	default:
		writeInternalError(w, fmt.Errorf("unknown claim decision: %s", claim.Decision))
		return
	}
}

func (a *API) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(chi.URLParam(r, "task_id"))
	if a.legacyAdapter != nil && a.legacyAdapter.SupportsTaskActions() {
		result, err := a.legacyAdapter.CancelTask(r.Context(), taskID)
		if err != nil {
			writeInternalError(w, err)
			return
		}

		switch result.Decision {
		case LegacyCancelDecisionNotFound:
			writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
			return
		case LegacyCancelDecisionAlreadyRequested:
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":      true,
				"message": "Cancellation already requested.",
			})
			return
		case LegacyCancelDecisionAlreadyFinished:
			writeAPIErrorResponse(w, http.StatusConflict, apiErrorCodeTaskAlreadyDone, "Task already finished with status "+strings.TrimSpace(result.Status)+".", nil)
			return
		case LegacyCancelDecisionRequested:
			writeJSON(w, http.StatusAccepted, map[string]any{
				"ok":      true,
				"message": "Cancellation requested.",
			})
			return
		default:
			writeInternalError(w, fmt.Errorf("unknown legacy cancel decision: %s", result.Decision))
			return
		}
	}

	task, err := a.store.GetTask(r.Context(), taskID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if task == nil {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
		return
	}

	status := strings.TrimSpace(task.Status)
	if isTerminalStatus(status) {
		writeAPIErrorResponse(w, http.StatusConflict, apiErrorCodeTaskAlreadyDone, "Task already finished with status "+status+".", nil)
		return
	}

	if status == domain.StatusCancelRequested {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "Cancellation already requested.",
		})
		return
	}

	if err := a.store.RequestTaskCancel(r.Context(), taskID, "Cancellation requested by user."); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"message": "Cancellation requested.",
	})
}

func (a *API) handleTaskDownload(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(chi.URLParam(r, "task_id"))
	if a.legacyAdapter != nil && a.legacyAdapter.SupportsArtifactDownload() {
		artifact, err := a.legacyAdapter.OpenTaskArtifact(r.Context(), taskID)
		if err != nil {
			switch {
			case errors.Is(err, ErrLegacyAdapterTaskNotFound):
				writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
			case errors.Is(err, ErrLegacyAdapterTaskNotReady):
				writeAPIErrorResponse(w, http.StatusConflict, apiErrorCodeTaskNotReady, "Task is not completed yet.", nil)
			case errors.Is(err, ErrLegacyAdapterTaskOutputNotFound):
				writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeArtifactNotFound, "Output file not found for this task.", nil)
			case errors.Is(err, ErrLegacyAdapterArtifactUnavailable):
				writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeArtifactUnavailable, "Stored file is unavailable.", nil)
			default:
				writeInternalError(w, err)
			}
			return
		}

		if r.Method == http.MethodHead {
			artifact.Close()
			w.WriteHeader(http.StatusOK)
			return
		}
		defer artifact.Close()

		w.Header().Set("Content-Type", artifact.ContentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", artifact.FileName))
		http.ServeContent(w, r, artifact.FileName, artifact.ModTime, artifact.File)
		return
	}

	task, err := a.store.GetTask(r.Context(), taskID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if task == nil {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
		return
	}

	if strings.TrimSpace(task.Status) != domain.StatusSuccess {
		writeAPIErrorResponse(w, http.StatusConflict, apiErrorCodeTaskNotReady, "Task is not completed yet.", nil)
		return
	}

	zipPath := stringValue(task.ResultZipPath)
	if zipPath == "" {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeArtifactNotFound, "Output file not found for this task.", nil)
		return
	}
	if !isSafeExistingDownloadFile(zipPath) {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeArtifactUnavailable, "Stored file is unavailable.", nil)
		return
	}

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	file, err := os.Open(zipPath)
	if err != nil {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeArtifactUnavailable, "Stored file is unavailable.", nil)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeArtifactUnavailable, "Stored file is unavailable.", nil)
		return
	}

	fileName := filepath.Base(zipPath)
	w.Header().Set("Content-Type", downloadMimeType(zipPath))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	http.ServeContent(w, r, fileName, info.ModTime(), file)
}

func (a *API) claimDownloadTask(
	ctx context.Context,
	rawURL string,
	canonicalURL string,
	metadata downloadMetadata,
	reuseSuccess bool,
	imageConcurrency int,
) (domain.ClaimDownloadTaskResult, string, error) {
	baseTask := domain.TaskLog{
		ID:               uuid.NewString(),
		URL:              rawURL,
		CanonicalURL:     stringPtr(canonicalURL),
		Status:           domain.StatusPending,
		StartTime:        float64(time.Now().UnixNano()) / float64(time.Second),
		Progress:         0,
		TotalImages:      0,
		ImageConcurrency: imageConcurrency,
		ResultZipPath:    nil,
		Author:           metadata.author,
		SeriesName:       metadata.seriesName,
		ComicName:        metadata.comicName,
		Summary:          metadata.summary,
		TagsRaw:          metadata.tagsRaw,
		TagsNormalized:   metadata.tagsNormalized,
		GenresRaw:        metadata.genresRaw,
		GenresNormalized: metadata.genresNormalized,
	}
	enqueueToken := uuid.NewString()

	for attempt := 0; attempt < 6; attempt++ {
		claim, err := a.store.ClaimDownloadTask(ctx, domain.ClaimDownloadTaskInput{
			Task:           baseTask,
			EnqueueToken:   enqueueToken,
			ActiveStatuses: domain.ActiveTaskStatuses,
			ReuseSuccess:   reuseSuccess,
		})
		if err != nil {
			return domain.ClaimDownloadTaskResult{}, "", err
		}
		if claim.Decision != domain.ClaimDecisionReuseSuccess {
			return claim, enqueueToken, nil
		}
		if isSafeExistingDownloadFile(stringValue(claim.Task.ResultZipPath)) {
			return claim, enqueueToken, nil
		}
		taskID := strings.TrimSpace(claim.Task.ID)
		if taskID == "" {
			return claim, enqueueToken, nil
		}
		if err := a.store.ClearResultZipPath(ctx, taskID); err != nil {
			return domain.ClaimDownloadTaskResult{}, "", err
		}
	}

	return domain.ClaimDownloadTaskResult{}, "", errors.New("failed to resolve stale reusable task after retries")
}

func (a *API) resolveDownloadSettings(ctx context.Context) DownloadRuntimeSettings {
	settings := DownloadRuntimeSettings{
		Timeout:          clampInt(a.downloadTimeout, 1, maxTimeoutSeconds),
		Retries:          clampInt(a.downloadRetries, 0, maxRetries),
		ImageConcurrency: clampInt(a.imageConcurrency, 1, maxImageConcurrency),
	}
	if a.runtimeSettingsProvider == nil {
		return settings
	}

	remote, err := a.runtimeSettingsProvider.GetDownloadSettings(ctx)
	if err != nil {
		log.Printf("fetch runtime settings failed, fallback to defaults: %v", err)
		return settings
	}
	if remote.Timeout > 0 {
		settings.Timeout = clampInt(remote.Timeout, 1, maxTimeoutSeconds)
	}
	if remote.Retries >= 0 {
		settings.Retries = clampInt(remote.Retries, 0, maxRetries)
	}
	if remote.ImageConcurrency > 0 {
		settings.ImageConcurrency = clampInt(remote.ImageConcurrency, 1, maxImageConcurrency)
	}
	return settings
}

func isTerminalStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case domain.StatusSuccess, domain.StatusFailed, domain.StatusCanceled:
		return true
	default:
		return false
	}
}

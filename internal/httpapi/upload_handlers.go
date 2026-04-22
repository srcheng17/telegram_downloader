package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
	"github.com/ryancheng/telegram-downloader/internal/httpv2"
)

func (a *API) handleUploadInit(w http.ResponseWriter, r *http.Request) {
	if a.v2TaskStore == nil {
		writeInternalError(w, errors.New("v2 upload store is not configured"))
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Invalid upload init payload.", nil)
		return
	}

	fileName := normalizePayloadText(payload["file_name"], maxMetadataFieldLength)
	if _, ext := apptasks.NormalizeUploadArchiveName(fileName); fileName == "" || ext == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Only zip, rar, and 7z archives are supported.", nil)
		return
	}

	fileSize := coerceInt64(payload["file_size"])
	_, _, metadata := extractFromMap(payload)
	initService := apptasks.NewUploadInitService(uploadInitStoreAdapter{store: a.v2TaskStore})
	result, err := initService.Init(r.Context(), apptasks.UploadInitInput{
		FileName: fileName,
		FileSize: fileSize,
		Metadata: apptasks.MetadataInput{
			Author:     metadata.author,
			SeriesName: metadata.seriesName,
			ComicName:  metadata.comicName,
			Summary:    metadata.summary,
			TagsRaw:    metadata.tagsRaw,
			GenresRaw:  metadata.genresRaw,
		},
	})
	if err != nil {
		if errors.Is(err, apptasks.ErrInvalidUploadArchive) {
			writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Only zip, rar, and 7z archives are supported.", nil)
			return
		}
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":           true,
		"task_id":      result.TaskID,
		"status":       result.Status,
		"upload_token": result.UploadToken,
		"upload_url":   result.UploadURL,
		"logs_url":     result.LogsURL,
	})
}

func (a *API) handleUploadSource(w http.ResponseWriter, r *http.Request) {
	if a.v2TaskStore == nil || a.v2TaskQueue == nil {
		writeInternalError(w, errors.New("upload dependencies are not configured"))
		return
	}

	taskID := strings.TrimSpace(chi.URLParam(r, "task_id"))
	if taskID == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Task not found.", nil)
		return
	}
	uploadToken := strings.TrimSpace(r.Header.Get("X-Upload-Token"))
	if uploadToken == "" {
		uploadToken = strings.TrimSpace(r.URL.Query().Get("upload_token"))
	}
	if uploadToken == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Missing upload token.", nil)
		return
	}

	sourceService := apptasks.NewUploadSourceService(
		uploadSourceStoreAdapter{store: a.v2TaskStore},
		retryTaskQueueAdapter{queue: a.v2TaskQueue},
		a.uploadTempDirOrDefault(),
	)
	result, err := sourceService.Attach(r.Context(), apptasks.UploadSourceInput{
		TaskID:      taskID,
		UploadToken: uploadToken,
		Body:        r.Body,
		ContentSize: r.ContentLength,
	})
	if err != nil {
		switch {
		case errors.Is(err, apptasks.ErrTaskNotFound):
			writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
		case errors.Is(err, apptasks.ErrInvalidUploadArchive):
			writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Only zip, rar, and 7z archives are supported.", nil)
		case strings.Contains(strings.ToLower(err.Error()), "enqueue"):
			writeAPIErrorResponse(w, http.StatusBadGateway, apiErrorCodeEnqueueFailed, "Failed to enqueue task.", nil)
		default:
			writeInternalError(w, err)
		}
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":       true,
		"task_id":  result.TaskID,
		"status":   result.Status,
		"logs_url": result.LogsURL,
	})
}

func (a *API) handleMetadataHistory(w http.ResponseWriter, r *http.Request) {
	if a.v2TaskStore == nil {
		writeInternalError(w, errors.New("metadata history store is not configured"))
		return
	}
	limit := 20
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil {
			limit = parsed
		}
	}

	entries, err := a.v2TaskStore.ListMetadataHistory(r.Context(), limit)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func coerceInt64(value any) int64 {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed
		}
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func (a *API) uploadTempDirOrDefault() string {
	if strings.TrimSpace(a.uploadTempDir) != "" {
		return strings.TrimSpace(a.uploadTempDir)
	}
	if tempPath := strings.TrimSpace(os.Getenv("TEMP_PATH")); tempPath != "" {
		return tempPath
	}
	return "temp_downloads"
}

type uploadInitStoreAdapter struct {
	store LegacyV2TaskStore
}

func (a uploadInitStoreAdapter) CreateUploadTask(ctx context.Context, in apptasks.UploadInitRecord) (apptasks.UploadTaskRecord, error) {
	task, err := a.store.CreateUploadTask(ctx, httpv2.CreateTaskInput{
		ID:                in.ID,
		URL:               "",
		CanonicalURL:      in.CanonicalURL,
		EnqueueToken:      in.EnqueueToken,
		TaskType:          stringPtr(in.TaskType),
		SourceArchiveName: in.SourceArchiveName,
		UploadTotalBytes:  in.UploadTotalBytes,
		Author:            in.Author,
		SeriesName:        in.SeriesName,
		ComicName:         in.ComicName,
		Summary:           in.Summary,
		TagsRaw:           in.TagsRaw,
		TagsNormalized:    in.TagsNormalized,
		GenresRaw:         in.GenresRaw,
		GenresNormalized:  in.GenresNormalized,
	})
	if err != nil {
		return apptasks.UploadTaskRecord{}, err
	}
	return apptasks.UploadTaskRecord{
		ID:                task.ID,
		Status:            task.Status,
		TaskType:          stringValue(task.TaskType),
		SourceArchiveName: task.SourceArchiveName,
		UploadTotalBytes:  task.UploadTotalBytes,
	}, nil
}

func (a uploadInitStoreAdapter) InsertMetadataHistory(ctx context.Context, in apptasks.MetadataHistoryRecord) error {
	return a.store.InsertMetadataHistory(ctx, httpv2.MetadataHistoryEntry{
		TaskType:   in.TaskType,
		Author:     in.Author,
		SeriesName: in.SeriesName,
		ComicName:  in.ComicName,
		Summary:    in.Summary,
		Tags:       in.Tags,
		Genres:     in.Genres,
	})
}

type uploadSourceStoreAdapter struct {
	store LegacyV2TaskStore
}

func (a uploadSourceStoreAdapter) GetUploadTask(ctx context.Context, taskID string) (*apptasks.UploadTaskRecord, error) {
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil || task == nil {
		return nil, err
	}
	return &apptasks.UploadTaskRecord{
		ID:                task.ID,
		Status:            task.Status,
		TaskType:          stringValue(task.TaskType),
		SourceArchiveName: task.SourceArchiveName,
		UploadTotalBytes:  task.UploadTotalBytes,
	}, nil
}

func (a uploadSourceStoreAdapter) UpdateUploadProgress(ctx context.Context, taskID string, loadedBytes, totalBytes int64) error {
	return a.store.UpdateUploadProgress(ctx, taskID, loadedBytes, totalBytes)
}

func (a uploadSourceStoreAdapter) MarkUploadTaskQueued(ctx context.Context, taskID, sourceArchivePath string, totalBytes int64) error {
	return a.store.MarkUploadTaskQueued(ctx, taskID, sourceArchivePath, totalBytes)
}

func (a uploadSourceStoreAdapter) MarkTaskFailed(ctx context.Context, taskID, message string) error {
	return a.store.MarkTaskFailed(ctx, taskID, message)
}

package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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

	fileName, ext := normalizeUploadArchiveName(normalizePayloadText(payload["file_name"], maxMetadataFieldLength))
	if fileName == "" || ext == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Only zip, rar, and 7z archives are supported.", nil)
		return
	}

	fileSize := coerceInt64(payload["file_size"])
	if fileSize < 0 {
		fileSize = 0
	}

	_, _, metadata := extractFromMap(payload)
	taskID := uuid.NewString()
	uploadToken := uuid.NewString()
	uploadTask, err := a.v2TaskStore.CreateUploadTask(r.Context(), httpv2.CreateTaskInput{
		ID:                taskID,
		URL:               "",
		CanonicalURL:      stringPtr("upload:" + taskID),
		EnqueueToken:      uploadToken,
		TaskType:          stringPtr("upload"),
		SourceArchiveName: stringPtr(fileName),
		UploadTotalBytes:  fileSize,
		Author:            metadata.author,
		SeriesName:        metadata.seriesName,
		ComicName:         metadata.comicName,
		Summary:           metadata.summary,
		TagsRaw:           metadata.tagsRaw,
		TagsNormalized:    metadata.tagsNormalized,
		GenresRaw:         metadata.genresRaw,
		GenresNormalized:  metadata.genresNormalized,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	if err := a.v2TaskStore.InsertMetadataHistory(r.Context(), httpv2.MetadataHistoryEntry{
		TaskType:   "upload",
		Author:     metadata.author,
		SeriesName: metadata.seriesName,
		ComicName:  metadata.comicName,
		Summary:    metadata.summary,
		Tags:       metadata.tagsNormalized,
		Genres:     metadata.genresNormalized,
	}); err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":           true,
		"task_id":      uploadTask.ID,
		"status":       uploadTask.Status,
		"upload_token": uploadToken,
		"upload_url":   "/api/tasks/" + urlPathEscape(uploadTask.ID) + "/upload-source",
		"logs_url":     "/logs",
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

	task, err := a.v2TaskStore.GetTask(r.Context(), taskID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if task == nil {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
		return
	}
	fileName := stringValue(task.SourceArchiveName)
	_, ext := normalizeUploadArchiveName(fileName)
	if ext == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Only zip, rar, and 7z archives are supported.", nil)
		return
	}

	targetDir := filepath.Join(a.uploadTempDirOrDefault(), taskID)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		writeInternalError(w, fmt.Errorf("create upload temp dir: %w", err))
		return
	}
	targetPath := filepath.Join(targetDir, "source"+ext)
	file, err := os.Create(targetPath)
	if err != nil {
		writeInternalError(w, fmt.Errorf("create upload target: %w", err))
		return
	}

	var loaded int64
	totalBytes := task.UploadTotalBytes
	if totalBytes <= 0 && r.ContentLength > 0 {
		totalBytes = r.ContentLength
	}
	buffer := make([]byte, 64*1024)
	writeErr := func(err error) {
		_ = file.Close()
		_ = os.Remove(targetPath)
		_ = a.v2TaskStore.MarkTaskFailed(r.Context(), taskID, strings.TrimSpace(err.Error()))
	}

	for {
		n, readErr := r.Body.Read(buffer)
		if n > 0 {
			if _, err := file.Write(buffer[:n]); err != nil {
				writeErr(fmt.Errorf("write upload source: %w", err))
				writeInternalError(w, err)
				return
			}
			loaded += int64(n)
			if err := a.v2TaskStore.UpdateUploadProgress(r.Context(), taskID, loaded, totalBytes); err != nil {
				writeErr(fmt.Errorf("update upload progress: %w", err))
				writeInternalError(w, err)
				return
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			writeErr(fmt.Errorf("read upload source: %w", readErr))
			writeInternalError(w, readErr)
			return
		}
	}
	if err := file.Close(); err != nil {
		writeErr(fmt.Errorf("close upload source: %w", err))
		writeInternalError(w, err)
		return
	}

	if err := a.v2TaskStore.MarkUploadTaskQueued(r.Context(), taskID, targetPath, loaded); err != nil {
		_ = os.Remove(targetPath)
		writeInternalError(w, err)
		return
	}
	if err := a.v2TaskQueue.Enqueue(r.Context(), httpv2.TaskQueueMessage{
		TaskID: taskID,
		Token:  uploadToken,
	}); err != nil {
		_ = a.v2TaskStore.MarkTaskFailed(r.Context(), taskID, fmt.Sprintf("enqueue failed: %v", err))
		writeAPIErrorResponse(w, http.StatusBadGateway, apiErrorCodeEnqueueFailed, "Failed to enqueue task.", nil)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":       true,
		"task_id":  taskID,
		"status":   httpv2.TaskStatusQueued,
		"logs_url": "/logs",
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

func normalizeUploadArchiveName(raw string) (string, string) {
	fileName := filepath.Base(strings.TrimSpace(raw))
	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".zip", ".rar", ".7z":
		return fileName, ext
	default:
		return "", ""
	}
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
	return "temp_uploads"
}

func urlPathEscape(value string) string {
	return url.PathEscape(strings.TrimSpace(value))
}

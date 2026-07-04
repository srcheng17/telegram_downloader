package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
)

type TaskCoreService interface {
	CreateURLTask(ctx context.Context, in app.CreateURLInput) (app.Task, error)
	InitUploadTask(ctx context.Context, in app.InitUploadInput) (app.Task, error)
	AttachUploadSource(ctx context.Context, in app.AttachUploadSourceInput) (app.Task, error)
	RequestCancel(ctx context.Context, taskID string) (app.Task, error)
	Retry(ctx context.Context, taskID string) (app.Task, error)
	ListTasks(ctx context.Context, limit int, offset int) ([]app.TaskView, error)
	GetTask(ctx context.Context, taskID string) (*app.TaskView, error)
}

type taskCoreHandlers struct {
	service              TaskCoreService
	metadataHistoryStore UploadTaskStore
	komgaConfigured      bool
	uploadTempDir        string
	komgaRootDir         string
}

func newTaskCoreHandlers(service TaskCoreService, metadataHistoryStore UploadTaskStore, komgaConfigured bool, uploadTempDir string, komgaRootDir string) *taskCoreHandlers {
	return &taskCoreHandlers{
		service:              service,
		metadataHistoryStore: metadataHistoryStore,
		komgaConfigured:      komgaConfigured,
		uploadTempDir:        strings.TrimSpace(uploadTempDir),
		komgaRootDir:         strings.TrimSpace(komgaRootDir),
	}
}

func (h *taskCoreHandlers) registerRoutes(router chi.Router) {
	router.Post("/download", h.handleCreateURLTask)
	router.Get("/api/tasks", h.handleListTasks)
	router.Post("/api/tasks/upload/init", h.handleUploadInit)
	router.Put("/api/tasks/{task_id}/upload-source", h.handleUploadSource)
	router.Post("/api/tasks/{task_id}/cancel", h.handleCancelTask)
	router.Post("/api/tasks/{task_id}/retry", h.handleRetryTask)
	router.Post("/api/tasks/{task_id}/copy-to-komga", h.handleCopyToKomga)
	router.Get("/api/tasks/{task_id}/download", h.handleDownload)
	router.Head("/api/tasks/{task_id}/download", h.handleDownload)
}

func (h *taskCoreHandlers) handleCreateURLTask(w http.ResponseWriter, r *http.Request) {
	rawURL, _, metadata, err := extractDownloadRequest(r)
	if err != nil || rawURL == "" {
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

	task, err := h.service.CreateURLTask(r.Context(), app.CreateURLInput{
		ID:           uuid.NewString(),
		URL:          rawURL,
		CanonicalURL: canonicalURL,
		Metadata:     taskCoreMetadataMap(metadata),
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	h.recordMetadataHistory(r.Context(), "url", rawURL, metadata)

	view := app.TaskView{
		Task:     task,
		Input:    app.Input{TaskID: task.ID, URL: rawURL, CanonicalURL: canonicalURL, Metadata: taskCoreMetadataMap(metadata)},
		Progress: domain.NewProgress(domain.PhasePreparing, 0, 0, domain.UnitNone, "准备下载"),
	}
	writeJSON(w, http.StatusAccepted, h.taskPayload(view))
}

func (h *taskCoreHandlers) handleListTasks(w http.ResponseWriter, r *http.Request) {
	limit := parseInt(r.URL.Query().Get("per_page"), 50)
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	page := parseInt(r.URL.Query().Get("page"), 1)
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	views, err := h.service.ListTasks(r.Context(), limit, offset)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	out := make([]taskCoreView, 0, len(views))
	for _, view := range views {
		out = append(out, presentTaskCoreView(view, h.komgaConfigured))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"tasks":    out,
		"total":    len(out),
		"page":     page,
		"per_page": limit,
	})
}

func (h *taskCoreHandlers) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	taskID := taskCoreTaskID(r)
	if taskID == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Task not found.", nil)
		return
	}
	task, err := h.service.RequestCancel(r.Context(), taskID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.taskPayload(app.TaskView{Task: task}))
}

func (h *taskCoreHandlers) handleRetryTask(w http.ResponseWriter, r *http.Request) {
	taskID := taskCoreTaskID(r)
	if taskID == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Task not found.", nil)
		return
	}
	task, err := h.service.Retry(r.Context(), taskID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.taskPayload(app.TaskView{Task: task}))
}

func (h *taskCoreHandlers) handleDownload(w http.ResponseWriter, r *http.Request) {
	taskID := taskCoreTaskID(r)
	if taskID == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Task not found.", nil)
		return
	}
	view, err := h.service.GetTask(r.Context(), taskID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if view == nil {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
		return
	}
	if !taskCoreActionAvailable(*view, domain.ActionDownload, h.komgaConfigured) {
		writeAPIErrorResponse(w, http.StatusConflict, apiErrorCodeTaskNotReady, "Task is not completed yet.", nil)
		return
	}
	file, info, fileName, err := openTaskCoreArtifact(*view)
	if err != nil {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeArtifactUnavailable, "Stored file is unavailable.", nil)
		return
	}
	defer file.Close()
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", downloadMimeType(fileName))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	http.ServeContent(w, r, fileName, info.ModTime(), file)
}

func (h *taskCoreHandlers) handleCopyToKomga(w http.ResponseWriter, r *http.Request) {
	taskID := taskCoreTaskID(r)
	if taskID == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Task not found.", nil)
		return
	}
	view, err := h.service.GetTask(r.Context(), taskID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if view == nil {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
		return
	}
	if !taskCoreActionAvailable(*view, domain.ActionCopyToKomga, h.komgaConfigured) {
		writeAPIErrorResponse(w, http.StatusConflict, apiErrorCodeTaskNotReady, "Task result cannot be copied to Komga.", nil)
		return
	}
	artifactPath, fileName, err := safeTaskCoreArtifactPath(*view)
	if err != nil {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeArtifactUnavailable, "Stored file is unavailable.", nil)
		return
	}
	copier := apptasks.NewKomgaCopier(apptasks.KomgaCopyConfig{Root: h.komgaRootDir})
	targetPath, err := copier.CopyFromPath(artifactPath, fileName, taskCoreSeriesName(*view))
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

func (h *taskCoreHandlers) handleUploadInit(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Invalid upload init payload.", nil)
		return
	}
	_, _, metadata := extractFromMap(payload)
	fileName := taskCoreUploadArchiveBaseName(normalizePayloadText(payload["file_name"], maxMetadataFieldLength))
	task, err := h.service.InitUploadTask(r.Context(), app.InitUploadInput{
		ID:       uuid.NewString(),
		Metadata: taskCoreMetadataMap(metadata),
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	h.recordMetadataHistory(r.Context(), "upload", "", metadata)

	view := app.TaskView{
		Task:     task,
		Input:    app.Input{TaskID: task.ID, Metadata: taskCoreMetadataMap(metadata)},
		Progress: domain.NewProgress(domain.PhaseUploading, 0, 0, domain.UnitBytes, "等待上传"),
	}
	uploadURL := "/api/tasks/" + url.PathEscape(strings.TrimSpace(task.ID)) + "/upload-source"
	if fileName != "" {
		uploadURL += "?file_name=" + url.QueryEscape(fileName)
	}
	payloadOut := h.taskPayload(view)
	payloadOut["upload_url"] = uploadURL
	writeJSON(w, http.StatusAccepted, payloadOut)
}

func (h *taskCoreHandlers) handleUploadSource(w http.ResponseWriter, r *http.Request) {
	taskID := taskCoreTaskID(r)
	if taskID == "" {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Task not found.", nil)
		return
	}
	existingView, err := h.service.GetTask(r.Context(), taskID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if existingView == nil {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
		return
	}
	if !taskCoreCanAttachUploadSource(*existingView) {
		writeAPIErrorResponse(w, http.StatusConflict, apiErrorCodeTaskAlreadyDone, "Task state does not allow upload source attachment.", nil)
		return
	}

	archiveName := h.uploadArchiveName(r, taskID)
	path, size, err := h.saveUploadSource(r, taskID, archiveName)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	task, err := h.service.AttachUploadSource(r.Context(), app.AttachUploadSourceInput{
		TaskID: taskID,
		Name:   archiveName,
		Path:   path,
		Size:   size,
	})
	if err != nil {
		_ = os.Remove(path)
		h.writeServiceError(w, err)
		return
	}
	view := app.TaskView{
		Task: task,
		Input: app.Input{
			TaskID:            task.ID,
			SourceArchiveName: archiveName,
			SourceArchivePath: path,
			SourceArchiveSize: size,
		},
		Progress: domain.NewProgress(domain.PhasePreparing, size, size, domain.UnitBytes, "上传完成，等待处理"),
	}
	writeJSON(w, http.StatusAccepted, h.taskPayload(view))
}

func (h *taskCoreHandlers) taskPayload(view app.TaskView) map[string]any {
	presented := presentTaskCoreView(view, h.komgaConfigured)
	return map[string]any{
		"ok":      true,
		"task":    presented,
		"task_id": presented.ID,
		"status":  presented.Status,
	}
}

func (h *taskCoreHandlers) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrInvalidInput):
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Invalid task request.", nil)
	case errors.Is(err, app.ErrNotFound):
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
	case errors.Is(err, app.ErrConflict):
		writeAPIErrorResponse(w, http.StatusConflict, apiErrorCodeTaskAlreadyDone, "Task state does not allow this action.", nil)
	default:
		writeInternalError(w, err)
	}
}

func (h *taskCoreHandlers) uploadArchiveName(r *http.Request, taskID string) string {
	if raw := strings.TrimSpace(r.URL.Query().Get("file_name")); raw != "" {
		if name := taskCoreUploadArchiveBaseName(raw); name != "" {
			return name
		}
	}
	if _, params, err := mime.ParseMediaType(r.Header.Get("Content-Disposition")); err == nil {
		if raw := strings.TrimSpace(params["filename"]); raw != "" {
			if name := taskCoreUploadArchiveBaseName(raw); name != "" {
				return name
			}
		}
	}
	if ext := strings.ToLower(filepath.Ext(r.URL.Path)); ext == ".zip" || ext == ".rar" || ext == ".7z" {
		return taskID + ext
	}
	return taskID + ".upload"
}

func taskCoreUploadArchiveBaseName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	base := strings.TrimSpace(filepath.Base(strings.ReplaceAll(raw, "\\", "/")))
	if base == "." || base == "/" {
		return ""
	}
	return base
}

func (h *taskCoreHandlers) saveUploadSource(r *http.Request, taskID string, archiveName string) (string, int64, error) {
	dir := h.uploadTempDir
	if dir == "" {
		dir = "temp_downloads"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	ext := filepath.Ext(archiveName)
	file, err := os.CreateTemp(dir, taskID+"-*"+ext)
	if err != nil {
		return "", 0, err
	}
	path := file.Name()
	defer file.Close()

	size, err := io.Copy(file, r.Body)
	if err != nil {
		_ = os.Remove(path)
		return "", 0, err
	}
	return path, size, nil
}

func taskCoreTaskID(r *http.Request) string {
	if taskID := strings.TrimSpace(chi.URLParam(r, "task_id")); taskID != "" {
		return taskID
	}
	return strings.TrimSpace(chi.URLParam(r, "id"))
}

func taskCoreMetadataMap(metadata downloadMetadata) map[string]string {
	out := map[string]string{}
	putOptional := func(key string, value *string) {
		if value == nil {
			return
		}
		trimmed := strings.TrimSpace(*value)
		if trimmed != "" {
			out[key] = trimmed
		}
	}
	putOptional("author", metadata.author)
	putOptional("series_name", metadata.seriesName)
	putOptional("series_number", metadata.seriesNumber)
	putOptional("comic_name", metadata.comicName)
	putOptional("summary", metadata.summary)
	putOptional("tags", metadata.tagsRaw)
	putOptional("tags_normalized", metadata.tagsNormalized)
	putOptional("genres", metadata.genresRaw)
	putOptional("genres_normalized", metadata.genresNormalized)
	return out
}

func (h *taskCoreHandlers) recordMetadataHistory(ctx context.Context, taskType string, rawURL string, metadata downloadMetadata) {
	if h.metadataHistoryStore == nil {
		return
	}
	entry := taskCoreMetadataHistoryEntry(taskType, rawURL, metadata)
	if !taskCoreMetadataHistoryHasMetadata(entry) {
		return
	}

	entries, err := h.metadataHistoryStore.ListMetadataHistory(ctx, 20)
	if err != nil {
		log.Printf("metadata history duplicate check failed: %v", err)
		return
	}
	for _, existing := range entries {
		if sameTaskCoreMetadataHistoryEntry(existing, entry) {
			return
		}
	}
	if err := h.metadataHistoryStore.InsertMetadataHistory(ctx, entry); err != nil {
		log.Printf("metadata history insert failed: %v", err)
	}
}

func taskCoreMetadataHistoryEntry(taskType string, rawURL string, metadata downloadMetadata) postgres.MetadataHistoryEntry {
	normalizedTaskType := strings.ToLower(strings.TrimSpace(taskType))
	entry := postgres.MetadataHistoryEntry{
		TaskType:     normalizedTaskType,
		Author:       optionalString(stringValue(metadata.author)),
		SeriesName:   optionalString(stringValue(metadata.seriesName)),
		SeriesNumber: optionalString(stringValue(metadata.seriesNumber)),
		ComicName:    optionalString(stringValue(metadata.comicName)),
		Summary:      optionalString(stringValue(metadata.summary)),
		Tags:         optionalString(stringValue(metadata.tagsRaw)),
		Genres:       optionalString(stringValue(metadata.genresRaw)),
	}
	if normalizedTaskType == "url" {
		entry.URL = optionalString(rawURL)
	}
	return entry
}

func taskCoreMetadataHistoryHasMetadata(entry postgres.MetadataHistoryEntry) bool {
	return stringValue(entry.Author) != "" ||
		stringValue(entry.SeriesName) != "" ||
		stringValue(entry.SeriesNumber) != "" ||
		stringValue(entry.ComicName) != "" ||
		stringValue(entry.Summary) != "" ||
		stringValue(entry.Tags) != "" ||
		stringValue(entry.Genres) != ""
}

func sameTaskCoreMetadataHistoryEntry(left postgres.MetadataHistoryEntry, right postgres.MetadataHistoryEntry) bool {
	return strings.EqualFold(strings.TrimSpace(left.TaskType), strings.TrimSpace(right.TaskType)) &&
		stringValue(left.URL) == stringValue(right.URL) &&
		stringValue(left.Author) == stringValue(right.Author) &&
		stringValue(left.SeriesName) == stringValue(right.SeriesName) &&
		stringValue(left.SeriesNumber) == stringValue(right.SeriesNumber) &&
		stringValue(left.ComicName) == stringValue(right.ComicName) &&
		stringValue(left.Summary) == stringValue(right.Summary) &&
		stringValue(left.Tags) == stringValue(right.Tags) &&
		stringValue(left.Genres) == stringValue(right.Genres)
}

func taskCoreActionAvailable(view app.TaskView, action domain.Action, komgaConfigured bool) bool {
	hasResult := view.Result != nil && strings.TrimSpace(view.Result.ArtifactPath) != ""
	for _, candidate := range domain.AvailableActions(view.Task.Status, hasResult, komgaConfigured) {
		if candidate == action {
			return true
		}
	}
	return false
}

func taskCoreCanAttachUploadSource(view app.TaskView) bool {
	return view.Task.Kind == domain.KindUpload && view.Task.Status == domain.StatusCreated
}

func safeTaskCoreArtifactPath(view app.TaskView) (string, string, error) {
	if view.Result == nil {
		return "", "", errors.New("missing artifact")
	}
	artifactPath := strings.TrimSpace(view.Result.ArtifactPath)
	if !isSafeExistingDownloadFile(artifactPath) {
		return "", "", errors.New("artifact unavailable")
	}
	fileName := strings.TrimSpace(view.Result.ArtifactName)
	if fileName == "" {
		fileName = filepath.Base(artifactPath)
	}
	return artifactPath, fileName, nil
}

func openTaskCoreArtifact(view app.TaskView) (*os.File, os.FileInfo, string, error) {
	artifactPath, fileName, err := safeTaskCoreArtifactPath(view)
	if err != nil {
		return nil, nil, "", err
	}
	file, err := os.Open(artifactPath)
	if err != nil {
		return nil, nil, "", err
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		_ = file.Close()
		if err != nil {
			return nil, nil, "", err
		}
		return nil, nil, "", errors.New("artifact is directory")
	}
	return file, info, fileName, nil
}

func taskCoreSeriesName(view app.TaskView) string {
	if view.Input.Metadata == nil {
		return ""
	}
	return strings.TrimSpace(view.Input.Metadata["series_name"])
}

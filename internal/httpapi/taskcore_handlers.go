package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/ryancheng/telegram-downloader/internal/config"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

type TaskCoreService interface {
	CreateURLTask(ctx context.Context, in app.CreateURLInput) (app.CreateURLResult, error)
	QueryTasks(ctx context.Context, query app.TaskQuery) (app.TaskPage, error)
	StatusCounts(ctx context.Context) (map[domain.Status]int, error)
	InitUploadTask(ctx context.Context, in app.InitUploadInput) (app.Task, error)
	AttachUploadSource(ctx context.Context, in app.AttachUploadSourceInput) (app.Task, error)
	RequestCancel(ctx context.Context, taskID string) (app.Task, error)
	Retry(ctx context.Context, taskID string) (app.Task, error)
	ListTasks(ctx context.Context, limit int, offset int) ([]app.TaskView, error)
	GetTask(ctx context.Context, taskID string) (*app.TaskView, error)
}

type KomgaDeliveryService interface {
	Deliver(context.Context, string, string, string) (apptasks.KomgaDeliveryResult, error)
}

type taskCoreHandlers struct {
	komgaDelivery        KomgaDeliveryService
	service              TaskCoreService
	metadataHistoryStore UploadTaskStore
	komgaConfigured      bool
	uploadTempDir        string
	komgaRootDir         string
	settingsProvider     TaskSettingsProvider
	defaultSettings      *config.SettingsSnapshot
}

func newTaskCoreHandlers(service TaskCoreService, metadataHistoryStore UploadTaskStore, komgaConfigured bool, uploadTempDir string, komgaRootDir string, settingsProvider TaskSettingsProvider, defaultSettings *config.SettingsSnapshot) *taskCoreHandlers {
	return &taskCoreHandlers{
		service:              service,
		metadataHistoryStore: metadataHistoryStore,
		komgaConfigured:      komgaConfigured,
		uploadTempDir:        strings.TrimSpace(uploadTempDir),
		komgaRootDir:         strings.TrimSpace(komgaRootDir),
		settingsProvider:     settingsProvider, defaultSettings: defaultSettings,
	}
}

func (h *taskCoreHandlers) registerRoutes(router chi.Router) {
	router.Post("/download", h.handleCreateURLTask)
	router.Get("/api/tasks", h.handleListTasks)
	router.Get("/api/tasks/{task_id}", h.handleGetTask)
	router.Get("/api/tasks/submissions/{key}", h.handleGetSubmission)
	router.Post("/api/tasks/upload/init", h.handleUploadInit)
	router.Put("/api/tasks/{task_id}/upload-source", h.handleUploadSource)
	router.Post("/api/tasks/{task_id}/cancel", h.handleCancelTask)
	router.Post("/api/tasks/{task_id}/retry", h.handleRetryTask)
	router.Post("/api/tasks/{task_id}/copy-to-komga", h.handleCopyToKomga)
	router.Get("/api/tasks/{task_id}/download", h.handleDownload)
	router.Head("/api/tasks/{task_id}/download", h.handleDownload)
}

func (h *taskCoreHandlers) handleCreateURLTask(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	rawURL, force, metadata, err := extractDownloadRequest(r)
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

	settings, err := h.taskSettings(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	result, err := h.service.CreateURLTask(r.Context(), app.CreateURLInput{
		ID:             uuid.NewString(),
		IdempotencyKey: metadata.idempotencyKey, DeliveryTarget: metadata.deliveryTarget,
		URL:              rawURL,
		CanonicalURL:     canonicalURL,
		Metadata:         taskCoreMetadataMap(metadata),
		MetadataDocument: metadata.document,
		Force:            force, RuntimeSettings: settings,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	task := result.Task

	view := app.TaskView{
		Task:     task,
		Input:    app.Input{TaskID: task.ID, URL: rawURL, CanonicalURL: canonicalURL, Metadata: taskCoreMetadataMap(metadata), MetadataDocument: metadata.document},
		Progress: domain.NewProgress(domain.PhasePreparing, 0, 0, domain.UnitNone, "准备下载"),
		Result:   result.Result,
	}
	if result.Reused {
		existing, err := h.service.GetTask(r.Context(), task.ID)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		if existing != nil {
			view = *existing
		}
	}
	payload := h.taskPayload(view)
	payload["logs_url"] = "/logs"
	if result.Reused {
		payload["duplicate"] = true
		payload["active"] = !result.NeedsConfirmation
		if result.NeedsConfirmation {
			payload["needs_confirmation"] = true
			payload["download_url"] = "/api/tasks/" + url.PathEscape(task.ID) + "/download"
			writeJSON(w, http.StatusOK, payload)
			return
		}
	}
	writeJSON(w, http.StatusAccepted, payload)
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
	if page > int(^uint(0)>>1)/limit {
		page = int(^uint(0)>>1) / limit
	}
	offset := (page - 1) * limit

	result, err := h.service.QueryTasks(r.Context(), app.TaskQuery{Limit: limit, Offset: offset})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	out := make([]taskCoreView, 0, len(result.Tasks))
	for _, view := range result.Tasks {
		out = append(out, presentTaskCoreView(view, h.komgaConfigured))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"tasks":    out,
		"total":    result.Total,
		"page":     page,
		"per_page": limit,
	})
}

func (h *taskCoreHandlers) handleGetTask(w http.ResponseWriter, r *http.Request) {
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
	if view == nil || view.Task.ID != taskID {
		writeAPIErrorResponse(w, http.StatusNotFound, apiErrorCodeTaskNotFound, "Task not found.", nil)
		return
	}
	writeJSON(w, http.StatusOK, h.taskPayload(*view))
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
	var delivery apptasks.KomgaDeliveryResult
	if h.komgaDelivery != nil {
		delivery, err = h.komgaDelivery.Deliver(r.Context(), artifactPath, fileName, taskCoreSeriesName(*view))
	} else {
		copier := apptasks.NewKomgaCopier(apptasks.KomgaCopyConfig{Root: h.komgaRootDir})
		delivery.TargetPath, err = copier.CopyFromPath(artifactPath, fileName, taskCoreSeriesName(*view))
		delivery.Copied, delivery.Status, delivery.Indexed, delivery.Reason = err == nil, "pending", "pending", "connection_unconfigured"
	}
	if err != nil {
		if errors.Is(err, apptasks.ErrKomgaTargetConflict) {
			writeAPIErrorResponse(w, http.StatusConflict, "komga_target_conflict", "书库已有同名且内容不同的文件，未覆盖。", nil)
		} else {
			writeInternalError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK     bool   `json:"ok"`
		TaskID string `json:"task_id"`
		apptasks.KomgaDeliveryResult
	}{true, taskID, delivery})
}

func (h *taskCoreHandlers) handleUploadInit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	payload, err := decodeTaskJSON(r.Body)
	if err != nil {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Invalid upload init payload.", nil)
		return
	}
	_, _, metadata := extractFromMap(payload)
	if err := extractDocumentPayload(payload, &metadata); err != nil {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "元数据格式无效。", nil)
		return
	}
	fileName := taskCoreUploadArchiveBaseName(normalizePayloadText(payload["file_name"], maxMetadataFieldLength))
	if !supportedArchiveName(fileName) {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "请选择 ZIP、RAR 或 7Z 文件。", nil)
		return
	}
	var fileSize int64
	if raw, ok := payload["file_size"]; ok {
		n, valid := raw.(json.Number)
		size, err := n.Int64()
		if !valid || err != nil || size < 0 || size > config.MaxUploadBytes {
			writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "上传文件不得超过 64 MiB。", nil)
			return
		}
		fileSize = size
	}
	fileHash, hashValid := payload["file_sha256"].(string)
	if _, exists := payload["file_sha256"]; exists && !hashValid {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "Invalid source fingerprint.", nil)
		return
	}
	settings, err := h.taskSettings(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	task, err := h.service.InitUploadTask(r.Context(), app.InitUploadInput{
		IdempotencyKey: metadata.idempotencyKey, DeliveryTarget: metadata.deliveryTarget,
		FileName: fileName, FileSize: fileSize, FileSHA256: fileHash,
		ID:               uuid.NewString(),
		Metadata:         taskCoreMetadataMap(metadata),
		MetadataDocument: metadata.document,
		RuntimeSettings:  settings,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	view := app.TaskView{
		Task:     task,
		Input:    app.Input{TaskID: task.ID, Metadata: taskCoreMetadataMap(metadata), MetadataDocument: metadata.document},
		Progress: domain.NewProgress(domain.PhaseUploading, 0, 0, domain.UnitBytes, "等待上传"),
	}
	if metadata.idempotencyKey != "" {
		existing, err := h.service.GetTask(r.Context(), task.ID)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		if existing != nil {
			view = *existing
		}
	}
	uploadURL := taskCoreUploadURL(task.ID, fileName)
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
	if !supportedArchiveName(archiveName) {
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "请选择 ZIP、RAR 或 7Z 文件。", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, config.MaxUploadBytes)
	path, size, err := h.saveUploadSource(r, taskID, archiveName)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeAPIErrorResponse(w, http.StatusRequestEntityTooLarge, apiErrorCodeValidation, "上传文件不得超过 64 MiB。", nil)
		} else {
			writeInternalError(w, err)
		}
		return
	}
	if size == 0 {
		_ = os.Remove(path)
		writeAPIErrorResponse(w, http.StatusBadRequest, apiErrorCodeValidation, "上传文件不能为空。", nil)
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
	case errors.Is(err, app.ErrSubmissionSourceConflict):
		writeAPIErrorResponse(w, http.StatusConflict, "submission_source_conflict", "已有同来源任务使用不同作品信息，请先核对该任务。", nil)
	case errors.Is(err, app.ErrIdempotencyConflict):
		writeAPIErrorResponse(w, http.StatusConflict, "idempotency_conflict", "提交内容与已确认的请求不同，请重新核对。", nil)
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
	if ext := strings.ToLower(filepath.Ext(r.URL.Path)); ext == ".zip" || ext == ".cbz" || ext == ".rar" || ext == ".7z" {
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
	if err == nil {
		err = file.Close()
	}
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
	if metadata.document != nil {
		return metadata.explicitLegacy
	}
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

func taskCoreActionAvailable(view app.TaskView, action domain.Action, komgaConfigured bool) bool {
	hasResult := view.Result != nil && strings.TrimSpace(view.Result.ArtifactPath) != ""
	for _, candidate := range domain.AvailableActions(view.Task.Status, view.Input.HasSource(view.Task.Kind), hasResult, komgaConfigured) {
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
	artifact, err := taskCoreArtifact(view)
	if err != nil {
		return "", "", err
	}
	defer artifact.Close()
	return artifact.File.Name(), artifact.FileName, nil
}
func taskCoreArtifact(view app.TaskView) (*apptasks.OpenedArtifact, error) {
	if view.Result == nil {
		return nil, apptasks.ErrArtifactUnavailable
	}
	access := apptasks.NewArtifactAccess(apptasks.ArtifactAccessConfig{DownloadRoot: os.Getenv("DOWNLOAD_PATH")})
	artifact, err := access.Open(view.Result.ArtifactPath)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(view.Result.ArtifactName); name != "" {
		artifact.FileName = filepath.Base(name)
	}
	return artifact, nil
}
func openTaskCoreArtifact(view app.TaskView) (*os.File, os.FileInfo, string, error) {
	artifact, err := taskCoreArtifact(view)
	if err != nil {
		return nil, nil, "", err
	}
	info, err := artifact.File.Stat()
	if err != nil {
		_ = artifact.Close()
		return nil, nil, "", err
	}
	return artifact.File, info, artifact.FileName, nil
}

func taskCoreSeriesName(view app.TaskView) string {
	if view.Input.Metadata == nil {
		return ""
	}
	return strings.TrimSpace(view.Input.Metadata["series_name"])
}

func supportedArchiveName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip", ".cbz", ".rar", ".7z":
		return true
	default:
		return false
	}
}
func (h *taskCoreHandlers) taskSettings(ctx context.Context) (*config.SettingsSnapshot, error) {
	snapshot := config.DefaultSettingsSnapshot()
	if h.defaultSettings != nil {
		snapshot = *h.defaultSettings
	}
	if h.settingsProvider != nil {
		var err error
		snapshot, err = h.settingsProvider.GetSettings(ctx)
		if err != nil {
			return nil, err
		}
	}
	snapshot = config.NormalizeSettingsSnapshot(snapshot)
	return &snapshot, nil
}

func taskCoreUploadURL(taskID, fileName string) string {
	return "/api/tasks/" + url.PathEscape(taskID) + "/upload-source?file_name=" + url.QueryEscape(fileName)
}

func (h *taskCoreHandlers) handleGetSubmission(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(interface {
		GetSubmission(context.Context, string) (*app.TaskView, error)
	})
	if !ok {
		h.writeServiceError(w, app.ErrNotFound)
		return
	}
	view, err := service.GetSubmission(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if view == nil {
		h.writeServiceError(w, app.ErrNotFound)
		return
	}
	payload := h.taskPayload(*view)
	if view.Task.Kind == domain.KindUpload && view.Task.Status == domain.StatusCreated {
		payload["upload_url"] = taskCoreUploadURL(view.Task.ID, view.Input.SourceArchiveName)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, payload)
}

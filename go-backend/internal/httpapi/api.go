package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/queue"
)

const (
	defaultTimeoutSeconds   = 30
	defaultRetries          = 10
	defaultImageConcurrency = 2
	maxURLLength            = 2048
	maxMetadataFieldLength  = 4000
	internalEnqueueAPIPath  = "/api/internal/enqueue-download"
	internalSettingsAPIPath = "/api/internal/settings-snapshot"
	maxTimeoutSeconds       = 300
	maxRetries              = 20
	maxImageConcurrency     = 20
)

var allowedTelegraphHosts = map[string]struct{}{
	"telegra.ph":     {},
	"www.telegra.ph": {},
	"graph.org":      {},
	"www.graph.org":  {},
}

type TaskReader interface {
	GetStatusCounts(ctx context.Context) (map[string]int, error)
	ListLogs(ctx context.Context, query domain.LogQuery) (domain.LogListResult, error)
	HasActiveTasks(ctx context.Context, statuses []string) (bool, error)
	ClaimDownloadTask(ctx context.Context, input domain.ClaimDownloadTaskInput) (domain.ClaimDownloadTaskResult, error)
	ClearResultZipPath(ctx context.Context, taskID string) error
	MarkTaskFailed(ctx context.Context, taskID, message string) error
	GetTask(ctx context.Context, taskID string) (*domain.TaskLog, error)
	RequestTaskCancel(ctx context.Context, taskID string, cancelError string) error
}

type DownloadSubmitRequest struct {
	TaskID           string `json:"task_id"`
	URL              string `json:"url"`
	Timeout          int    `json:"timeout"`
	Retries          int    `json:"retries"`
	ImageConcurrency int    `json:"image_concurrency"`
}

type DownloadRuntimeSettings struct {
	Timeout          int
	Retries          int
	ImageConcurrency int
}

type DownloadSubmitter interface {
	SubmitDownload(ctx context.Context, request DownloadSubmitRequest) error
}

type DownloadSettingsProvider interface {
	GetDownloadSettings(ctx context.Context) (DownloadRuntimeSettings, error)
}

type API struct {
	store                   TaskReader
	upstreamBaseURL         string
	httpClient              *http.Client
	downloadSubmitter       DownloadSubmitter
	downloadQueue           queue.DownloadQueue
	runtimeSettingsProvider DownloadSettingsProvider
	downloadTimeout         int
	downloadRetries         int
	imageConcurrency        int
}

type RouterOptions struct {
	UpstreamBaseURL         string
	HTTPClient              *http.Client
	DownloadSubmitter       DownloadSubmitter
	DownloadQueue           queue.DownloadQueue
	RuntimeSettingsProvider DownloadSettingsProvider
	InternalToken           string
	DownloadTimeout         int
	DownloadRetries         int
	ImageConcurrency        int
}

func NewRouter(store TaskReader) http.Handler {
	return NewRouterWithOptions(store, RouterOptions{})
}

func NewRouterWithOptions(store TaskReader, options RouterOptions) http.Handler {
	upstreamBaseURL := strings.TrimRight(strings.TrimSpace(options.UpstreamBaseURL), "/")
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	downloadSubmitter := options.DownloadSubmitter
	if downloadSubmitter == nil && upstreamBaseURL != "" {
		downloadSubmitter = NewHTTPDownloadSubmitter(upstreamBaseURL, httpClient, options.InternalToken)
	}
	runtimeSettingsProvider := options.RuntimeSettingsProvider
	if runtimeSettingsProvider == nil && upstreamBaseURL != "" {
		runtimeSettingsProvider = NewHTTPDownloadSettingsProvider(upstreamBaseURL, httpClient, options.InternalToken)
	}

	api := &API{
		store:                   store,
		upstreamBaseURL:         upstreamBaseURL,
		httpClient:              httpClient,
		downloadSubmitter:       downloadSubmitter,
		downloadQueue:           options.DownloadQueue,
		runtimeSettingsProvider: runtimeSettingsProvider,
		downloadTimeout:         positiveOrDefault(options.DownloadTimeout, defaultTimeoutSeconds),
		downloadRetries:         nonNegativeOrDefault(options.DownloadRetries, defaultRetries),
		imageConcurrency:        positiveOrDefault(options.ImageConcurrency, defaultImageConcurrency),
	}

	router := chi.NewRouter()
	router.Get("/healthz", api.handleHealthz)
	router.Get("/api/summary", api.handleSummary)
	router.Get("/api/logs", api.handleLogs)
	router.Post("/download", api.handleDownload)
	router.Post("/api/tasks/{task_id}/cancel", api.handleTaskCancel)
	router.Get("/api/tasks/{task_id}/download", api.handleTaskDownload)
	router.Head("/api/tasks/{task_id}/download", api.handleTaskDownload)
	return router
}

func (a *API) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "go-backend"})
}

func (a *API) handleSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := a.buildSummary(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (a *API) handleLogs(w http.ResponseWriter, r *http.Request) {
	query := normalizeLogQuery(r)

	result, err := a.store.ListLogs(r.Context(), query)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	hasActiveTasks, err := a.store.HasActiveTasks(r.Context(), domain.ActiveTaskStatuses)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	summary, err := a.buildSummary(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}

	payload := domain.LogsResponse{
		Logs:           result.Logs,
		Total:          result.Total,
		Page:           result.Page,
		PerPage:        result.PerPage,
		TotalPages:     result.TotalPages,
		HasActiveTasks: hasActiveTasks,
		Filters: domain.LogFilters{
			Status: query.Status,
			Query:  query.Keyword,
		},
		StatusCatalog: domain.CopyStatusCatalog(),
		Summary:       summary,
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *API) handleDownload(w http.ResponseWriter, r *http.Request) {
	rawURL, forceDownload, metadata, err := extractDownloadRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "Invalid request payload.",
		})
		return
	}

	if rawURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "Please provide a Telegraph URL.",
		})
		return
	}
	if !isAllowedTelegraphURL(rawURL) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "Only telegra.ph or graph.org URLs are supported.",
		})
		return
	}

	canonicalURL := normalizeTelegraphURL(rawURL)
	if canonicalURL == "" {
		canonicalURL = rawURL
	}

	runtimeSettings := a.resolveDownloadSettings(r.Context())
	claim, err := a.claimDownloadTask(
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
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                 true,
			"duplicate":          true,
			"needs_confirmation": true,
			"task_id":            taskID,
			"download_url":       "/api/tasks/" + url.PathEscape(taskID) + "/download",
			"force_applied":      forceDownload,
		})
		return
	case domain.ClaimDecisionReuseActive:
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":            true,
			"duplicate":     true,
			"active":        true,
			"task_id":       taskID,
			"logs_url":      "/logs",
			"force_applied": forceDownload,
		})
		return
	case domain.ClaimDecisionCreated:
		switch {
		case a.downloadQueue != nil:
			err := a.downloadQueue.EnqueueDownload(r.Context(), queue.EnqueueMessage{
				TaskID:       taskID,
				EnqueueToken: uuid.NewString(),
			})
			if err != nil {
				_ = a.store.MarkTaskFailed(r.Context(), taskID, fmt.Sprintf("failed to enqueue task: %v", err))
				writeJSON(w, http.StatusBadGateway, map[string]any{
					"ok":      false,
					"message": "Failed to enqueue task.",
				})
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
				writeJSON(w, http.StatusBadGateway, map[string]any{
					"ok":      false,
					"message": "Failed to enqueue task.",
				})
				return
			}
		default:
			_ = a.store.MarkTaskFailed(r.Context(), taskID, "enqueue bridge not configured")
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"ok":      false,
				"message": "Task queue bridge is unavailable.",
			})
			return
		}

		writeJSON(w, http.StatusAccepted, map[string]any{
			"ok":            true,
			"duplicate":     false,
			"active":        false,
			"task_id":       taskID,
			"logs_url":      "/logs",
			"force_applied": forceDownload,
		})
		return
	default:
		writeInternalError(w, fmt.Errorf("unknown claim decision: %s", claim.Decision))
		return
	}
}

func (a *API) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(chi.URLParam(r, "task_id"))
	task, err := a.store.GetTask(r.Context(), taskID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"ok":      false,
			"message": "Task not found.",
		})
		return
	}

	status := strings.TrimSpace(task.Status)
	if isTerminalStatus(status) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"ok":      false,
			"message": "Task already finished with status " + status + ".",
		})
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
	task, err := a.store.GetTask(r.Context(), taskID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"ok":      false,
			"message": "Task not found.",
		})
		return
	}

	if strings.TrimSpace(task.Status) != domain.StatusSuccess {
		writeJSON(w, http.StatusConflict, map[string]any{
			"ok":      false,
			"message": "Task is not completed yet.",
		})
		return
	}

	zipPath := stringValue(task.ResultZipPath)
	if zipPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"ok":      false,
			"message": "Output file not found for this task.",
		})
		return
	}
	if !isSafeExistingDownloadFile(zipPath) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"ok":      false,
			"message": "Stored file is unavailable.",
		})
		return
	}

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	file, err := os.Open(zipPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"ok":      false,
			"message": "Stored file is unavailable.",
		})
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"ok":      false,
			"message": "Stored file is unavailable.",
		})
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
) (domain.ClaimDownloadTaskResult, error) {
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

	for attempt := 0; attempt < 6; attempt++ {
		claim, err := a.store.ClaimDownloadTask(ctx, domain.ClaimDownloadTaskInput{
			Task:           baseTask,
			ActiveStatuses: domain.ActiveTaskStatuses,
			ReuseSuccess:   reuseSuccess,
		})
		if err != nil {
			return domain.ClaimDownloadTaskResult{}, err
		}
		if claim.Decision != domain.ClaimDecisionReuseSuccess {
			return claim, nil
		}
		if isSafeExistingDownloadFile(stringValue(claim.Task.ResultZipPath)) {
			return claim, nil
		}
		taskID := strings.TrimSpace(claim.Task.ID)
		if taskID == "" {
			return claim, nil
		}
		if err := a.store.ClearResultZipPath(ctx, taskID); err != nil {
			return domain.ClaimDownloadTaskResult{}, err
		}
	}

	return domain.ClaimDownloadTaskResult{}, errors.New("failed to resolve stale reusable task after retries")
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

func (a *API) buildSummary(ctx context.Context) (domain.Summary, error) {
	statusCounts, err := a.store.GetStatusCounts(ctx)
	if err != nil {
		return domain.Summary{}, err
	}

	summary := domain.Summary{
		PendingTasks:         safeCount(statusCounts[domain.StatusPending]),
		InProgressTasks:      safeCount(statusCounts[domain.StatusInProgress]),
		CancelRequestedTasks: safeCount(statusCounts[domain.StatusCancelRequested]),
		CanceledTasks:        safeCount(statusCounts[domain.StatusCanceled]),
		SuccessTasks:         safeCount(statusCounts[domain.StatusSuccess]),
		FailedTasks:          safeCount(statusCounts[domain.StatusFailed]),
		StartupRecovery:      domain.DefaultStartupRecovery(),
	}

	summary.TotalTasks = 0
	for _, count := range statusCounts {
		summary.TotalTasks += safeCount(count)
	}

	summary.ActiveTasks = summary.PendingTasks + summary.InProgressTasks + summary.CancelRequestedTasks
	summary.FinishedTasks = summary.SuccessTasks + summary.FailedTasks + summary.CanceledTasks

	if summary.FinishedTasks > 0 {
		rate := float64(summary.SuccessTasks) / float64(summary.FinishedTasks) * 100
		rounded := math.Round(rate*10) / 10
		summary.SuccessRate = &rounded
	}

	return summary, nil
}

func normalizeLogQuery(r *http.Request) domain.LogQuery {
	query := r.URL.Query()

	page := clampInt(parseInt(query.Get("page"), 1), 1, math.MaxInt)
	perPage := clampInt(parseInt(query.Get("per_page"), domain.DefaultLogsPerPage), 1, domain.MaxLogsPerPage)
	status := normalizeStatusFilter(query.Get("status"))
	keyword := strings.TrimSpace(query.Get("q"))
	if len(keyword) > 120 {
		keyword = keyword[:120]
	}

	return domain.LogQuery{
		Page:    page,
		PerPage: perPage,
		Status:  status,
		Keyword: keyword,
	}
}

func normalizeStatusFilter(rawStatus string) string {
	normalized := strings.ToUpper(strings.TrimSpace(rawStatus))
	if domain.IsKnownStatus(normalized) {
		return normalized
	}
	return ""
}

type downloadMetadata struct {
	author           *string
	seriesName       *string
	comicName        *string
	summary          *string
	tagsRaw          *string
	tagsNormalized   *string
	genresRaw        *string
	genresNormalized *string
}

func extractDownloadRequest(r *http.Request) (string, bool, downloadMetadata, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return "", false, downloadMetadata{}, err
		}
		rawURL, forceDownload, metadata := extractFromMap(payload)
		return rawURL, forceDownload, metadata, nil
	}

	if err := r.ParseForm(); err != nil {
		return "", false, downloadMetadata{}, err
	}
	payload := map[string]any{}
	for key, values := range r.PostForm {
		if len(values) == 0 {
			continue
		}
		payload[key] = values[0]
	}
	rawURL, forceDownload, metadata := extractFromMap(payload)
	return rawURL, forceDownload, metadata, nil
}

func extractFromMap(payload map[string]any) (string, bool, downloadMetadata) {
	rawURL := normalizePayloadText(payload["url"], maxURLLength)
	forceDownload := coerceBool(payload["force"])
	tagsRaw := normalizePayloadText(payload["tags"], maxMetadataFieldLength)
	genresRaw := normalizePayloadText(payload["genres"], maxMetadataFieldLength)

	metadata := downloadMetadata{
		author:           optionalString(normalizePayloadText(payload["author"], maxMetadataFieldLength)),
		seriesName:       optionalString(normalizePayloadText(payload["series_name"], maxMetadataFieldLength)),
		comicName:        optionalString(normalizePayloadText(payload["comic_name"], maxMetadataFieldLength)),
		summary:          optionalString(normalizePayloadText(payload["summary"], maxMetadataFieldLength)),
		tagsRaw:          optionalString(tagsRaw),
		tagsNormalized:   optionalString(strings.ReplaceAll(tagsRaw, "，", ",")),
		genresRaw:        optionalString(genresRaw),
		genresNormalized: optionalString(strings.ReplaceAll(genresRaw, "，", ",")),
	}
	return rawURL, forceDownload, metadata
}

func isAllowedTelegraphURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	_, ok := allowedTelegraphHosts[host]
	return ok
}

func normalizeTelegraphURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "www.") {
		host = strings.TrimPrefix(host, "www.")
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	path = collapsePathSlashes(path)
	if path != "/" {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}
	canonical := &url.URL{
		Scheme: "https",
		Host:   host,
		Path:   path,
	}
	return canonical.String()
}

func collapsePathSlashes(path string) string {
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
}

func coerceBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized == "1" || normalized == "true" || normalized == "t" || normalized == "yes" || normalized == "y" || normalized == "on"
	case json.Number:
		parsed, err := typed.Int64()
		return err == nil && parsed != 0
	case float64:
		return typed != 0
	case float32:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	default:
		return false
	}
}

func normalizePayloadText(value any, maxLen int) string {
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case json.Number:
		text = typed.String()
	case nil:
		text = ""
	default:
		text = fmt.Sprintf("%v", typed)
	}
	text = strings.TrimSpace(text)
	if maxLen > 0 && len(text) > maxLen {
		text = text[:maxLen]
	}
	return text
}

func optionalString(value string) *string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return nil
	}
	copied := normalized
	return &copied
}

func stringPtr(value string) *string {
	copied := value
	return &copied
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func positiveOrDefault(value, defaultValue int) int {
	if value > 0 {
		return value
	}
	return defaultValue
}

func nonNegativeOrDefault(value, defaultValue int) int {
	if value >= 0 {
		return value
	}
	return defaultValue
}

func parseInt(raw string, defaultValue int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return defaultValue
	}
	return parsed
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func safeCount(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("encode response failed: %v", err)
	}
}

func writeInternalError(w http.ResponseWriter, err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("request failed: %v", err)
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"ok":      false,
		"message": "internal server error",
	})
}

func (a *API) proxyToUpstream(w http.ResponseWriter, r *http.Request, upstreamPath string) {
	baseURL := a.upstreamBaseURL
	if baseURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":      false,
			"message": "upstream service is not configured",
		})
		return
	}

	upstreamURL := baseURL + upstreamPath
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":      false,
			"message": "failed to build upstream request",
		})
		return
	}
	copyHeaders(upstreamReq.Header, r.Header)

	client := a.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":      false,
			"message": "upstream request failed",
		})
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead {
		return
	}

	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("copy upstream response failed: %v", err)
	}
}

func copyHeaders(dst, src http.Header) {
	for name, values := range src {
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

func isTerminalStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case domain.StatusSuccess, domain.StatusFailed, domain.StatusCanceled:
		return true
	default:
		return false
	}
}

func downloadMimeType(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".cbz":
		return "application/vnd.comicbook+zip"
	case ".zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}

func isSafeExistingDownloadFile(filePath string) bool {
	if filePath == "" {
		return false
	}
	if !isSafeDownloadPath(filePath) {
		return false
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func isSafeDownloadPath(candidatePath string) bool {
	downloadRoot := strings.TrimSpace(os.Getenv("DOWNLOAD_PATH"))
	if downloadRoot == "" {
		downloadRoot = "downloaded_images"
	}
	rootAbs, err := filepath.Abs(downloadRoot)
	if err != nil {
		return false
	}
	candidateAbs, err := filepath.Abs(candidatePath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return false
	}
	if rel == ".." {
		return false
	}
	if strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}

type HTTPDownloadSubmitter struct {
	endpointURL   string
	client        *http.Client
	internalToken string
}

func NewHTTPDownloadSubmitter(baseURL string, client *http.Client, internalToken string) *HTTPDownloadSubmitter {
	httpClient := client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &HTTPDownloadSubmitter{
		endpointURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/") + internalEnqueueAPIPath,
		client:        httpClient,
		internalToken: strings.TrimSpace(internalToken),
	}
}

func (s *HTTPDownloadSubmitter) SubmitDownload(ctx context.Context, request DownloadSubmitRequest) error {
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpointURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if s.internalToken != "" {
		httpReq.Header.Set("X-Internal-Token", s.internalToken)
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("enqueue api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
}

type HTTPDownloadSettingsProvider struct {
	endpointURL   string
	client        *http.Client
	internalToken string
}

func NewHTTPDownloadSettingsProvider(baseURL string, client *http.Client, internalToken string) *HTTPDownloadSettingsProvider {
	httpClient := client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &HTTPDownloadSettingsProvider{
		endpointURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/") + internalSettingsAPIPath,
		client:        httpClient,
		internalToken: strings.TrimSpace(internalToken),
	}
}

func (s *HTTPDownloadSettingsProvider) GetDownloadSettings(ctx context.Context) (DownloadRuntimeSettings, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpointURL, nil)
	if err != nil {
		return DownloadRuntimeSettings{}, err
	}
	httpReq.Header.Set("Accept", "application/json")
	if s.internalToken != "" {
		httpReq.Header.Set("X-Internal-Token", s.internalToken)
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return DownloadRuntimeSettings{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return DownloadRuntimeSettings{}, fmt.Errorf(
			"settings api returned %d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}

	var payload struct {
		OK               bool `json:"ok"`
		Timeout          int  `json:"timeout"`
		Retries          int  `json:"retries"`
		ImageConcurrency int  `json:"image_concurrency"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return DownloadRuntimeSettings{}, err
	}
	if !payload.OK {
		return DownloadRuntimeSettings{}, errors.New("settings api returned ok=false")
	}
	return DownloadRuntimeSettings{
		Timeout:          payload.Timeout,
		Retries:          payload.Retries,
		ImageConcurrency: payload.ImageConcurrency,
	}, nil
}

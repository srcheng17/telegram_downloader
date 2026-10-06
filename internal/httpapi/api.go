package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/domain"
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

type ReadyzChecker func(ctx context.Context) (bool, error)

type TaskSettingsProvider interface {
	GetSettings(ctx context.Context) (config.SettingsSnapshot, error)
}

type API struct {
	store                   TaskReader
	taskCoreService         TaskCoreService
	uploadTaskStore         UploadTaskStore
	upstreamBaseURL         string
	httpClient              *http.Client
	downloadSubmitter       DownloadSubmitter
	runtimeSettingsProvider DownloadSettingsProvider
	readyzChecker           ReadyzChecker
	downloadTimeout         int
	downloadRetries         int
	imageConcurrency        int
	uploadTempDir           string
	komgaRootDir            string
}

type RouterOptions struct {
	KomgaDelivery           KomgaDeliveryService
	UpstreamBaseURL         string
	HTTPClient              *http.Client
	DownloadSubmitter       DownloadSubmitter
	TaskCoreService         TaskCoreService
	SettingsProvider        TaskSettingsProvider
	UploadTaskStore         UploadTaskStore
	RuntimeSettingsProvider DownloadSettingsProvider
	InternalToken           string
	DisableRootRoutes       bool
	DownloadTimeout         int
	DownloadRetries         int
	ImageConcurrency        int
	ReadyzChecker           ReadyzChecker
	UploadTempDir           string
	KomgaRootDir            string
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
		taskCoreService:         options.TaskCoreService,
		uploadTaskStore:         options.UploadTaskStore,
		upstreamBaseURL:         upstreamBaseURL,
		httpClient:              httpClient,
		downloadSubmitter:       downloadSubmitter,
		runtimeSettingsProvider: runtimeSettingsProvider,
		readyzChecker:           options.ReadyzChecker,
		downloadTimeout:         positiveOrDefault(options.DownloadTimeout, defaultTimeoutSeconds),
		downloadRetries:         nonNegativeOrDefault(options.DownloadRetries, defaultRetries),
		imageConcurrency:        positiveOrDefault(options.ImageConcurrency, defaultImageConcurrency),
		uploadTempDir:           strings.TrimSpace(options.UploadTempDir),
		komgaRootDir:            strings.TrimSpace(options.KomgaRootDir),
	}

	router := chi.NewRouter()
	if !options.DisableRootRoutes {
		router.Get("/", api.handleRoot)
		router.Get("/logs", api.handleLogsPageRedirect)
	}
	router.Get("/healthz", api.handleHealthz)
	router.Get("/readyz", api.handleReadyz)
	router.Get("/api/summary", api.handleSummary)
	router.Get("/api/logs", api.handleLogs)
	router.Get("/api/metadata-history", api.handleMetadataHistory)
	if options.TaskCoreService != nil {
		taskCore := newTaskCoreHandlers(
			options.TaskCoreService,
			options.UploadTaskStore,
			strings.TrimSpace(options.KomgaRootDir) != "",
			api.uploadTempDirOrDefault(),
			strings.TrimSpace(options.KomgaRootDir),
			options.SettingsProvider,
			&config.SettingsSnapshot{Timeout: api.downloadTimeout, Retries: api.downloadRetries, ImageConcurrency: api.imageConcurrency},
		)
		taskCore.komgaDelivery = options.KomgaDelivery
		taskCore.registerRoutes(router)
	}
	return router
}

func normalizeStatusFilter(rawStatus string) string {
	normalized := strings.ToUpper(strings.TrimSpace(rawStatus))
	if domain.IsKnownStatus(normalized) {
		return normalized
	}
	return ""
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
	if parsed.RawPath != "" {
		// Parse has validated every percent escape. Escape any literal Unicode
		// without losing encoded separators when RawPath mixes both forms.
		path = strings.ReplaceAll((&url.URL{Path: parsed.RawPath}).EscapedPath(), "%25", "%")
	}
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
	decodedPath, err := url.PathUnescape(path)
	if err != nil {
		return ""
	}
	canonical := &url.URL{
		Scheme:  "https",
		Host:    host,
		Path:    decodedPath,
		RawPath: path,
	}
	return canonical.String()
}

func collapsePathSlashes(path string) string {
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
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

func writeHTML(w http.ResponseWriter, statusCode int, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	if _, err := io.WriteString(w, html); err != nil {
		log.Printf("write html response failed: %v", err)
	}
}

func writeInternalError(w http.ResponseWriter, err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("request failed: %v", err)
	}
	writeAPIErrorResponse(w, http.StatusInternalServerError, apiErrorCodeInternal, "internal server error", nil)
}

func (a *API) proxyToUpstream(w http.ResponseWriter, r *http.Request, upstreamPath string) {
	baseURL := a.upstreamBaseURL
	if baseURL == "" {
		writeAPIErrorResponse(w, http.StatusServiceUnavailable, apiErrorCodeUpstreamUnavailable, "upstream service is not configured", nil)
		return
	}

	upstreamURL := baseURL + upstreamPath
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, r.Body)
	if err != nil {
		writeAPIErrorResponse(w, http.StatusBadGateway, apiErrorCodeUpstreamBuildFailed, "failed to build upstream request", nil)
		return
	}
	copyHeaders(upstreamReq.Header, r.Header)

	client := a.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		writeAPIErrorResponse(w, http.StatusBadGateway, apiErrorCodeUpstreamRequestFailed, "upstream request failed", nil)
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

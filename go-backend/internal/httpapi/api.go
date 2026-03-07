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

const v2DashboardPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>V2 任务创建</title>
</head>
<body>
  <h1>V2 任务创建</h1>
  <p><a href="/v2/tasks-ui">查看任务列表</a></p>
  <form id="v2-create-task-form">
    <label for="v2-url">Telegraph URL</label>
    <input id="v2-url" name="url" type="url" required placeholder="https://telegra.ph/...">
    <button type="submit">创建任务</button>
  </form>
  <pre id="v2-feedback"></pre>
  <script>
    (function () {
      const form = document.getElementById('v2-create-task-form');
      const feedback = document.getElementById('v2-feedback');
      form.addEventListener('submit', async function (event) {
        event.preventDefault();
        const url = String(document.getElementById('v2-url').value || '').trim();
        if (!url) {
          feedback.textContent = '请输入 URL';
          return;
        }
        feedback.textContent = '提交中...';
        try {
          const response = await fetch('/v2/tasks', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
            body: JSON.stringify({ url: url }),
          });
          const payload = await response.json().catch(() => ({}));
          if (!response.ok) {
            feedback.textContent = payload.error || ('请求失败(' + response.status + ')');
            return;
          }
          feedback.textContent = '任务已创建: ' + (payload.task_id || '-') + ' (' + (payload.status || '-') + ')';
          form.reset();
        } catch (error) {
          feedback.textContent = '网络错误';
        }
      });
    })();
  </script>
</body>
</html>
`

const v2TasksPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>V2 任务列表</title>
</head>
<body>
  <h1>V2 任务列表</h1>
  <p><a href="/v2">创建任务</a></p>
  <form id="v2-filter-form">
    <label for="status">状态</label>
    <select id="status" name="status">
      <option value="">全部</option>
      <option value="QUEUED">QUEUED</option>
      <option value="RUNNING">RUNNING</option>
      <option value="SUCCESS">SUCCESS</option>
      <option value="FAILED">FAILED</option>
      <option value="CANCELED">CANCELED</option>
    </select>
    <label for="q">关键词</label>
    <input id="q" name="q" type="text">
    <label for="page">页码</label>
    <input id="page" name="page" type="number" min="1" value="1">
    <label for="per_page">每页</label>
    <input id="per_page" name="per_page" type="number" min="1" max="100" value="20">
    <button type="submit">查询</button>
  </form>
  <pre id="v2-tasks-feedback"></pre>
  <table border="1" cellpadding="6">
    <thead>
      <tr><th>ID</th><th>URL</th><th>状态</th><th>错误</th><th>操作</th></tr>
    </thead>
    <tbody id="v2-tasks-table-body"></tbody>
  </table>
  <script>
    (function () {
      const feedback = document.getElementById('v2-tasks-feedback');
      const tbody = document.getElementById('v2-tasks-table-body');
      const form = document.getElementById('v2-filter-form');

      function escapeHTML(input) {
        return String(input || '')
          .replaceAll('&', '&amp;')
          .replaceAll('<', '&lt;')
          .replaceAll('>', '&gt;')
          .replaceAll('"', '&quot;')
          .replaceAll("'", '&#39;');
      }

      function buildQuery() {
        const params = new URLSearchParams();
        const status = String(document.getElementById('status').value || '').trim();
        const q = String(document.getElementById('q').value || '').trim();
        const page = String(document.getElementById('page').value || '').trim();
        const perPage = String(document.getElementById('per_page').value || '').trim();
        if (status) params.set('status', status);
        if (q) params.set('q', q);
        if (page) params.set('page', page);
        if (perPage) params.set('per_page', perPage);
        return params.toString();
      }

      async function loadTasks() {
        feedback.textContent = '加载中...';
        const query = buildQuery();
        const response = await fetch('/v2/tasks' + (query ? ('?' + query) : ''), {
          headers: { 'Accept': 'application/json' },
        });
        const payload = await response.json().catch(() => ({}));
        if (!response.ok) {
          feedback.textContent = payload.error || ('加载失败(' + response.status + ')');
          tbody.innerHTML = '';
          return;
        }
        const tasks = Array.isArray(payload.tasks) ? payload.tasks : [];
        if (!tasks.length) {
          tbody.innerHTML = '<tr><td colspan="5">暂无任务</td></tr>';
        } else {
          tbody.innerHTML = tasks.map((task) => {
            const status = String(task.status || '').toUpperCase();
            const canCancel = status === 'QUEUED' || status === 'RUNNING';
            const canDownload = status === 'SUCCESS';
            const buttons = []
            if (canCancel) {
              buttons.push('<button data-action="cancel" data-task-id="' + escapeHTML(task.id) + '">取消</button>');
            }
            if (canDownload) {
              buttons.push('<button data-action="download" data-task-id="' + escapeHTML(task.id) + '">下载</button>');
            }
            return '<tr>' +
              '<td>' + escapeHTML(task.id) + '</td>' +
              '<td>' + escapeHTML(task.canonical_url || task.url || '') + '</td>' +
              '<td>' + escapeHTML(task.status || '') + '</td>' +
              '<td>' + escapeHTML(task.error || '') + '</td>' +
              '<td>' + buttons.join(' ') + '</td>' +
              '</tr>';
          }).join('');
        }
        feedback.textContent = '共 ' + (payload.total || 0) + ' 条';
      }

      tbody.addEventListener('click', async function (event) {
        const target = event.target;
        if (!(target instanceof HTMLElement)) {
          return;
        }
        const action = String(target.getAttribute('data-action') || '');
        const taskID = String(target.getAttribute('data-task-id') || '');
        if (!action || !taskID) {
          return;
        }
        if (action === 'download') {
          window.location.href = '/v2/tasks/' + encodeURIComponent(taskID) + '/artifact';
          return;
        }
        if (action === 'cancel') {
          target.setAttribute('disabled', 'disabled');
          try {
            const response = await fetch('/v2/tasks/' + encodeURIComponent(taskID) + '/cancel', {
              method: 'POST',
              headers: { 'Accept': 'application/json' },
            });
            const payload = await response.json().catch(() => ({}));
            if (!response.ok) {
              feedback.textContent = payload.error || ('取消失败(' + response.status + ')');
              return;
            }
            feedback.textContent = '任务 ' + taskID + ' 已提交取消';
            await loadTasks();
          } finally {
            target.removeAttribute('disabled');
          }
        }
      });

      form.addEventListener('submit', function (event) {
        event.preventDefault();
        void loadTasks();
      });
      void loadTasks();
    })();
  </script>
</body>
</html>
`

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

type API struct {
	store                   TaskReader
	legacyAdapter           *LegacyAdapter
	upstreamBaseURL         string
	httpClient              *http.Client
	downloadSubmitter       DownloadSubmitter
	downloadQueue           queue.DownloadQueue
	runtimeSettingsProvider DownloadSettingsProvider
	readyzChecker           ReadyzChecker
	downloadTimeout         int
	downloadRetries         int
	imageConcurrency        int
}

type RouterOptions struct {
	UpstreamBaseURL         string
	HTTPClient              *http.Client
	DownloadSubmitter       DownloadSubmitter
	DownloadQueue           queue.DownloadQueue
	V2TaskStore             LegacyV2TaskStore
	V2TaskQueue             LegacyV2TaskQueue
	V2ArtifactService       LegacyV2ArtifactService
	RuntimeSettingsProvider DownloadSettingsProvider
	InternalToken           string
	DisableRootRoutes       bool
	DownloadTimeout         int
	DownloadRetries         int
	ImageConcurrency        int
	ReadyzChecker           ReadyzChecker
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
		legacyAdapter:           NewLegacyAdapter(options.V2TaskStore, options.V2TaskQueue, options.V2ArtifactService),
		upstreamBaseURL:         upstreamBaseURL,
		httpClient:              httpClient,
		downloadSubmitter:       downloadSubmitter,
		downloadQueue:           options.DownloadQueue,
		runtimeSettingsProvider: runtimeSettingsProvider,
		readyzChecker:           options.ReadyzChecker,
		downloadTimeout:         positiveOrDefault(options.DownloadTimeout, defaultTimeoutSeconds),
		downloadRetries:         nonNegativeOrDefault(options.DownloadRetries, defaultRetries),
		imageConcurrency:        positiveOrDefault(options.ImageConcurrency, defaultImageConcurrency),
	}

	router := chi.NewRouter()
	if !options.DisableRootRoutes {
		router.Get("/", api.handleRoot)
		router.Get("/logs", api.handleLogsPageRedirect)
	}
	router.Get("/v2", api.handleV2DashboardPage)
	router.Get("/v2/tasks-ui", api.handleV2TasksPage)
	router.Get("/healthz", api.handleHealthz)
	router.Get("/readyz", api.handleReadyz)
	router.Get("/api/summary", api.handleSummary)
	router.Get("/api/logs", api.handleLogs)
	router.Post("/download", api.handleDownload)
	router.Post("/api/tasks/{task_id}/cancel", api.handleTaskCancel)
	router.Get("/api/tasks/{task_id}/download", api.handleTaskDownload)
	router.Head("/api/tasks/{task_id}/download", api.handleTaskDownload)
	return router
}

func (a *API) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/v2", http.StatusFound)
}

func (a *API) handleLogsPageRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/v2/tasks-ui", http.StatusFound)
}

func (a *API) handleV2DashboardPage(w http.ResponseWriter, _ *http.Request) {
	writeHTML(w, http.StatusOK, v2DashboardPageHTML)
}

func (a *API) handleV2TasksPage(w http.ResponseWriter, _ *http.Request) {
	writeHTML(w, http.StatusOK, v2TasksPageHTML)
}

func (a *API) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "go-backend"})
}

func (a *API) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if a.readyzChecker == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ready": true, "service": "go-backend"})
		return
	}

	ready, err := a.readyzChecker(r.Context())
	if err != nil {
		writeInternalError(w, fmt.Errorf("readyz check failed: %w", err))
		return
	}
	if !ready {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":      false,
			"ready":   false,
			"service": "go-backend",
			"reason":  "migrations_pending",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ready": true, "service": "go-backend"})
}

func (a *API) handleSummary(w http.ResponseWriter, r *http.Request) {
	if a.legacyAdapter != nil && a.legacyAdapter.SupportsSummary() {
		summary, err := a.legacyAdapter.BuildSummary(r.Context())
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, summary)
		return
	}

	summary, err := a.buildSummary(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (a *API) handleLogs(w http.ResponseWriter, r *http.Request) {
	query := normalizeLogQuery(r)

	if a.legacyAdapter != nil && a.legacyAdapter.SupportsLogs() {
		payload, err := a.legacyAdapter.ReadLogs(r.Context(), query)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
		return
	}

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
			"message": "Please provide a Telegraph URL.",
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
				writeJSON(w, http.StatusBadGateway, map[string]any{
					"ok":      false,
					"message": "Failed to enqueue task.",
				})
				return
			}
			writeInternalError(w, err)
			return
		}

		switch result.Decision {
		case LegacyDownloadDecisionReuseSuccess:
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":                 true,
				"duplicate":          true,
				"needs_confirmation": true,
				"task_id":            result.TaskID,
				"download_url":       "/api/tasks/" + url.PathEscape(result.TaskID) + "/download",
				"force_applied":      forceDownload,
			})
			return
		case LegacyDownloadDecisionReuseActive:
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":            true,
				"duplicate":     true,
				"active":        true,
				"task_id":       result.TaskID,
				"logs_url":      "/logs",
				"force_applied": forceDownload,
			})
			return
		case LegacyDownloadDecisionCreated:
			writeJSON(w, http.StatusAccepted, map[string]any{
				"ok":            true,
				"duplicate":     false,
				"active":        false,
				"task_id":       result.TaskID,
				"logs_url":      "/logs",
				"force_applied": forceDownload,
			})
			return
		default:
			writeInternalError(w, fmt.Errorf("unknown legacy adapter decision: %s", result.Decision))
			return
		}
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
				EnqueueToken: enqueueToken,
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
	if a.legacyAdapter != nil && a.legacyAdapter.SupportsTaskActions() {
		result, err := a.legacyAdapter.CancelTask(r.Context(), taskID)
		if err != nil {
			writeInternalError(w, err)
			return
		}

		switch result.Decision {
		case LegacyCancelDecisionNotFound:
			writeJSON(w, http.StatusNotFound, map[string]any{
				"ok":      false,
				"message": "Task not found.",
			})
			return
		case LegacyCancelDecisionAlreadyRequested:
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":      true,
				"message": "Cancellation already requested.",
			})
			return
		case LegacyCancelDecisionAlreadyFinished:
			writeJSON(w, http.StatusConflict, map[string]any{
				"ok":      false,
				"message": "Task already finished with status " + strings.TrimSpace(result.Status) + ".",
			})
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
	if a.legacyAdapter != nil && a.legacyAdapter.SupportsArtifactDownload() {
		artifact, err := a.legacyAdapter.OpenTaskArtifact(r.Context(), taskID)
		if err != nil {
			switch {
			case errors.Is(err, ErrLegacyAdapterTaskNotFound):
				writeJSON(w, http.StatusNotFound, map[string]any{
					"ok":      false,
					"message": "Task not found.",
				})
			case errors.Is(err, ErrLegacyAdapterTaskNotReady):
				writeJSON(w, http.StatusConflict, map[string]any{
					"ok":      false,
					"message": "Task is not completed yet.",
				})
			case errors.Is(err, ErrLegacyAdapterTaskOutputNotFound):
				writeJSON(w, http.StatusNotFound, map[string]any{
					"ok":      false,
					"message": "Output file not found for this task.",
				})
			case errors.Is(err, ErrLegacyAdapterArtifactUnavailable):
				writeJSON(w, http.StatusNotFound, map[string]any{
					"ok":      false,
					"message": "Stored file is unavailable.",
				})
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

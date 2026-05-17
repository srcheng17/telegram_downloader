package httpv2

import (
	"math"
	"net/http"
	"strings"

	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
	"github.com/go-chi/chi/v5"
)

func (h *TasksHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusInternalServerError, "task store is not configured")
		return
	}

	query := ListTasksQuery{
		Page:    normalizePage(parseInt(r.URL.Query().Get("page"), defaultListPage)),
		PerPage: normalizePerPage(parseInt(r.URL.Query().Get("per_page"), defaultListPerPage)),
		Status:  strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
		Query:   strings.TrimSpace(r.URL.Query().Get("q")),
	}

	result, err := h.store.ListTasks(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list tasks")
		return
	}

	if result.Page <= 0 {
		result.Page = query.Page
	}
	if result.PerPage <= 0 {
		result.PerPage = query.PerPage
	}
	if result.TotalPages <= 0 && result.Total > 0 {
		result.TotalPages = int(math.Ceil(float64(result.Total) / float64(result.PerPage)))
	}
	if result.Tasks == nil {
		result.Tasks = make([]Task, 0)
	}
	result.StatusCatalog = apptasks.Catalog()

	writeJSON(w, http.StatusOK, result)
}

func (h *TasksHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusInternalServerError, "task store is not configured")
		return
	}

	taskID := strings.TrimSpace(chi.URLParam(r, "task_id"))
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}

	task, err := h.store.GetTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get task")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	writeJSON(w, http.StatusOK, task)
}

func (h *TasksHandler) GetDashboardSummary(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusInternalServerError, "task store is not configured")
		return
	}

	summaryStore, ok := any(h.store).(TaskSummaryStore)
	if !ok {
		writeError(w, http.StatusInternalServerError, "task summary store is not configured")
		return
	}

	statusCounts, err := summaryStore.GetTaskStatusCounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get dashboard summary")
		return
	}

	summary := buildDashboardSummary(statusCounts)
	writeJSON(w, http.StatusOK, summary)
}

func buildDashboardSummary(statusCounts map[string]int) DashboardSummary {
	safeCount := func(status string) int {
		value := statusCounts[strings.ToUpper(strings.TrimSpace(status))]
		if value < 0 {
			return 0
		}
		return value
	}

	uploading := safeCount(TaskStatusUploading)
	queued := safeCount(TaskStatusQueued)
	running := safeCount(TaskStatusRunning)
	cancelRequested := safeCount(TaskStatusCancelRequested)
	success := safeCount(TaskStatusSuccess)
	failed := safeCount(TaskStatusFailed)
	canceled := safeCount(TaskStatusCanceled)

	total := uploading + queued + running + cancelRequested + success + failed + canceled
	active := uploading + queued + running + cancelRequested
	finished := success + failed + canceled
	successRate := 0.0
	if finished > 0 {
		successRate = math.Round((float64(success)/float64(finished))*1000) / 10
	}

	return DashboardSummary{
		TotalTasks:    total,
		QueuedTasks:   queued,
		RunningTasks:  running,
		SuccessTasks:  success,
		FailedTasks:   failed,
		CanceledTasks: canceled,
		ActiveTasks:   active,
		FinishedTasks: finished,
		SuccessRate:   successRate,
		StatusCatalog: apptasks.Catalog(),
	}
}

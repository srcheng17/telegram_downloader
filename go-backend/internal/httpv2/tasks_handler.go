package httpv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domainv2 "github.com/ryancheng/telegram-downloader/go-backend/internal/domain/v2"
	queuev2 "github.com/ryancheng/telegram-downloader/go-backend/internal/queue/v2"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/service"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres"
)

const (
	defaultListPage     = 1
	defaultListPerPage  = 20
	maxListPerPage      = 100
	compensationTimeout = 3 * time.Second

	TaskStatusQueued   = string(domainv2.StatusQueued)
	TaskStatusRunning  = string(domainv2.StatusRunning)
	TaskStatusSuccess  = string(domainv2.StatusSuccess)
	TaskStatusFailed   = string(domainv2.StatusFailed)
	TaskStatusCanceled = string(domainv2.StatusCanceled)
)

func NormalizeTaskStatus(status string) string {
	return strings.ToUpper(strings.TrimSpace(status))
}

func IsActiveTaskStatus(status string) bool {
	switch NormalizeTaskStatus(status) {
	case TaskStatusQueued, TaskStatusRunning:
		return true
	default:
		return false
	}
}

type CreateTaskInput struct {
	ID           string
	URL          string
	CanonicalURL *string
	EnqueueToken string
}

type Task struct {
	ID            string    `json:"id"`
	URL           string    `json:"url"`
	CanonicalURL  *string   `json:"canonical_url,omitempty"`
	Status        string    `json:"status"`
	Error         *string   `json:"error,omitempty"`
	ResultZipPath *string   `json:"result_zip_path,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

type ListTasksQuery struct {
	Page    int
	PerPage int
	Status  string
	Query   string
}

type ListTasksResult struct {
	Tasks      []Task `json:"tasks"`
	Total      int    `json:"total"`
	Page       int    `json:"page"`
	PerPage    int    `json:"per_page"`
	TotalPages int    `json:"total_pages"`
}

type TaskStore interface {
	CreateTask(ctx context.Context, in CreateTaskInput) (Task, error)
	ListTasks(ctx context.Context, in ListTasksQuery) (ListTasksResult, error)
	GetTask(ctx context.Context, taskID string) (*Task, error)
	CancelTask(ctx context.Context, taskID, fromStatus string) error
	MarkTaskFailed(ctx context.Context, taskID, message string) error
}

type TaskSummaryStore interface {
	GetTaskStatusCounts(ctx context.Context) (map[string]int, error)
}

type TaskQueueMessage struct {
	TaskID string
	Token  string
}

type TaskQueue interface {
	Enqueue(ctx context.Context, message TaskQueueMessage) error
}

type ArtifactService interface {
	OpenArtifact(resultZipPath string) (*service.OpenedV2Artifact, error)
}

type DashboardSummary struct {
	TotalTasks    int     `json:"total_tasks"`
	QueuedTasks   int     `json:"queued_tasks"`
	RunningTasks  int     `json:"running_tasks"`
	SuccessTasks  int     `json:"success_tasks"`
	FailedTasks   int     `json:"failed_tasks"`
	CanceledTasks int     `json:"canceled_tasks"`
	ActiveTasks   int     `json:"active_tasks"`
	FinishedTasks int     `json:"finished_tasks"`
	SuccessRate   float64 `json:"success_rate"`
}

type TasksHandler struct {
	store     TaskStore
	queue     TaskQueue
	artifacts ArtifactService
}

func NewTasksHandler(store TaskStore, queue TaskQueue) *TasksHandler {
	return NewTasksHandlerWithArtifactService(
		store,
		queue,
		service.NewV2ArtifactService(service.V2ArtifactServiceConfig{}),
	)
}

func NewTasksHandlerWithArtifactService(store TaskStore, queue TaskQueue, artifacts ArtifactService) *TasksHandler {
	return &TasksHandler{
		store:     store,
		queue:     queue,
		artifacts: artifacts,
	}
}

func (h *TasksHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	if h.store == nil || h.queue == nil {
		writeError(w, http.StatusInternalServerError, "task handler dependencies are not configured")
		return
	}

	var request struct {
		URL          string  `json:"url"`
		CanonicalURL *string `json:"canonical_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	url := strings.TrimSpace(request.URL)
	if url == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	canonicalURL := normalizeCanonicalURL(url, request.CanonicalURL)
	createInput := CreateTaskInput{
		ID:           uuid.NewString(),
		URL:          url,
		CanonicalURL: stringPtr(canonicalURL),
		EnqueueToken: uuid.NewString(),
	}

	task, err := h.store.CreateTask(r.Context(), createInput)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create task")
		return
	}
	if strings.TrimSpace(task.Status) == "" {
		task.Status = TaskStatusQueued
	}

	if err := h.queue.Enqueue(r.Context(), TaskQueueMessage{TaskID: task.ID, Token: createInput.EnqueueToken}); err != nil {
		compensationCtx, compensationCancel := context.WithTimeout(context.Background(), compensationTimeout)
		compensationErr := h.store.MarkTaskFailed(
			compensationCtx,
			task.ID,
			fmt.Sprintf("enqueue failed: %v", err),
		)
		compensationCancel()
		if compensationErr != nil {
			log.Printf(
				"httpv2 task compensation failed task_id=%s err=%v",
				strings.TrimSpace(task.ID),
				compensationErr,
			)
		}
		writeError(w, http.StatusInternalServerError, "enqueue task")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"task_id": task.ID,
		"status":  task.Status,
	})
}

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

func (h *TasksHandler) CancelTask(w http.ResponseWriter, r *http.Request) {
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

	status := NormalizeTaskStatus(task.Status)
	switch status {
	case TaskStatusCanceled:
		writeJSON(w, http.StatusAccepted, map[string]any{
			"task_id": taskID,
			"status":  TaskStatusCanceled,
		})
		return
	case TaskStatusSuccess, TaskStatusFailed:
		writeError(w, http.StatusConflict, "task already completed")
		return
	}

	if err := h.store.CancelTask(r.Context(), taskID, status); err != nil {
		if errors.Is(err, postgres.ErrV2TaskStatusMismatchOrNotFound) {
			latestTask, latestErr := h.store.GetTask(r.Context(), taskID)
			if latestErr != nil {
				writeError(w, http.StatusInternalServerError, "get task")
				return
			}
			if latestTask != nil && NormalizeTaskStatus(latestTask.Status) == TaskStatusCanceled {
				writeJSON(w, http.StatusAccepted, map[string]any{
					"task_id": taskID,
					"status":  TaskStatusCanceled,
				})
				return
			}
			writeError(w, http.StatusConflict, "task status conflict")
			return
		}
		writeError(w, http.StatusInternalServerError, "cancel task")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"task_id": taskID,
		"status":  TaskStatusCanceled,
	})
}

func (h *TasksHandler) DownloadArtifact(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusInternalServerError, "task store is not configured")
		return
	}
	if h.artifacts == nil {
		writeError(w, http.StatusInternalServerError, "artifact service is not configured")
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
	if NormalizeTaskStatus(task.Status) != TaskStatusSuccess {
		writeError(w, http.StatusConflict, "task artifact is not ready")
		return
	}

	resultPath := ""
	if task.ResultZipPath != nil {
		resultPath = strings.TrimSpace(*task.ResultZipPath)
	}

	artifact, err := h.artifacts.OpenArtifact(resultPath)
	if err != nil {
		if errors.Is(err, service.ErrV2ArtifactNotFound) || errors.Is(err, service.ErrV2ArtifactPathInvalid) {
			writeError(w, http.StatusNotFound, "artifact not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "open artifact")
		return
	}
	defer artifact.Close()

	w.Header().Set("Content-Type", artifact.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", artifact.FileName))
	http.ServeContent(w, r, artifact.FileName, artifact.ModTime, artifact.File)
}

type pgxQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type PostgresTaskStore struct {
	writer postgres.V2TaskRepo
	db     pgxQueryer
}

func NewPostgresTaskStore(pool *pgxpool.Pool) *PostgresTaskStore {
	if pool == nil {
		return &PostgresTaskStore{}
	}
	return &PostgresTaskStore{
		writer: postgres.NewV2TaskRepo(pool),
		db:     pool,
	}
}

func (s *PostgresTaskStore) CreateTask(ctx context.Context, in CreateTaskInput) (Task, error) {
	if s.writer == nil {
		return Task{}, errors.New("v2 task writer is not configured")
	}

	record, err := s.writer.CreateTask(ctx, postgres.CreateTaskInput{
		ID:           strings.TrimSpace(in.ID),
		URL:          strings.TrimSpace(in.URL),
		CanonicalURL: in.CanonicalURL,
		EnqueueToken: strings.TrimSpace(in.EnqueueToken),
	})
	if err != nil {
		return Task{}, err
	}

	return Task{
		ID:           record.ID,
		URL:          record.URL,
		CanonicalURL: record.CanonicalURL,
		Status:       record.Status,
		CreatedAt:    record.CreatedAt,
		UpdatedAt:    record.UpdatedAt,
	}, nil
}

func (s *PostgresTaskStore) ListTasks(ctx context.Context, in ListTasksQuery) (ListTasksResult, error) {
	if s.db == nil {
		return ListTasksResult{}, errors.New("v2 task database is not configured")
	}

	page := normalizePage(in.Page)
	perPage := normalizePerPage(in.PerPage)
	status := strings.ToUpper(strings.TrimSpace(in.Status))
	q := strings.TrimSpace(in.Query)
	offset := (page - 1) * perPage

	var total int
	if err := s.db.QueryRow(
		ctx,
		`
		SELECT COUNT(*)
		FROM v2_tasks
		WHERE
			($1 = '' OR status = $1)
			AND (
				$2 = ''
				OR canonical_url ILIKE '%' || $2 || '%'
				OR url ILIKE '%' || $2 || '%'
			)
		`,
		status,
		q,
	).Scan(&total); err != nil {
		return ListTasksResult{}, err
	}

	rows, err := s.db.Query(
		ctx,
		`
		SELECT
			id,
			url,
			canonical_url,
			status,
			error,
			result_zip_path,
			created_at,
			updated_at
		FROM v2_tasks
		WHERE
			($1 = '' OR status = $1)
			AND (
				$2 = ''
				OR canonical_url ILIKE '%' || $2 || '%'
				OR url ILIKE '%' || $2 || '%'
			)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
		`,
		status,
		q,
		perPage,
		offset,
	)
	if err != nil {
		return ListTasksResult{}, err
	}
	defer rows.Close()

	tasks := make([]Task, 0, perPage)
	for rows.Next() {
		var row Task
		if err := rows.Scan(
			&row.ID,
			&row.URL,
			&row.CanonicalURL,
			&row.Status,
			&row.Error,
			&row.ResultZipPath,
			&row.CreatedAt,
			&row.UpdatedAt,
		); err != nil {
			return ListTasksResult{}, err
		}
		tasks = append(tasks, row)
	}
	if err := rows.Err(); err != nil {
		return ListTasksResult{}, err
	}

	totalPages := 0
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(perPage)))
	}

	return ListTasksResult{
		Tasks:      tasks,
		Total:      total,
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
	}, nil
}

func (s *PostgresTaskStore) GetTask(ctx context.Context, taskID string) (*Task, error) {
	if s.db == nil {
		return nil, errors.New("v2 task database is not configured")
	}

	row := s.db.QueryRow(
		ctx,
		`
		SELECT
			id,
			url,
			canonical_url,
			status,
			error,
			result_zip_path,
			created_at,
			updated_at
		FROM v2_tasks
		WHERE id = $1
		`,
		strings.TrimSpace(taskID),
	)

	var task Task
	if err := row.Scan(
		&task.ID,
		&task.URL,
		&task.CanonicalURL,
		&task.Status,
		&task.Error,
		&task.ResultZipPath,
		&task.CreatedAt,
		&task.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &task, nil
}

func (s *PostgresTaskStore) GetTaskStatusCounts(ctx context.Context) (map[string]int, error) {
	if s.db == nil {
		return nil, errors.New("v2 task database is not configured")
	}

	rows, err := s.db.Query(
		ctx,
		`
		SELECT status, COUNT(*)
		FROM v2_tasks
		GROUP BY status
		`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[strings.ToUpper(strings.TrimSpace(status))] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

func (s *PostgresTaskStore) MarkTaskFailed(ctx context.Context, taskID, message string) error {
	if s.writer == nil {
		return errors.New("v2 task writer is not configured")
	}

	reason := strings.TrimSpace(message)
	if reason == "" {
		reason = "enqueue failed"
	}
	return s.writer.UpdateTaskStatus(
		ctx,
		strings.TrimSpace(taskID),
		TaskStatusQueued,
		TaskStatusFailed,
		postgres.StatusPatch{Error: stringPtr(reason)},
	)
}

func (s *PostgresTaskStore) CancelTask(ctx context.Context, taskID, fromStatus string) error {
	if s.writer == nil {
		return errors.New("v2 task writer is not configured")
	}

	status := NormalizeTaskStatus(fromStatus)
	switch status {
	case TaskStatusQueued, TaskStatusRunning:
	default:
		return fmt.Errorf("task cannot be canceled from status %s", status)
	}

	reason := "Cancelled by user."
	return s.writer.TransitionTaskWithEvent(ctx, postgres.TransitionTaskWithEventInput{
		TaskID:     strings.TrimSpace(taskID),
		FromStatus: status,
		ToStatus:   TaskStatusCanceled,
		Patch: postgres.StatusPatch{
			Error: stringPtr(reason),
		},
		EventType:   "STATUS_TRANSITION",
		PayloadJSON: `{"action":"cancel"}`,
	})
}

type V2QueueProducer interface {
	Produce(ctx context.Context, msg queuev2.TaskMessage) error
}

type V2TaskQueue struct {
	producer V2QueueProducer
}

func NewV2TaskQueue(producer V2QueueProducer) *V2TaskQueue {
	return &V2TaskQueue{producer: producer}
}

func (q *V2TaskQueue) Enqueue(ctx context.Context, message TaskQueueMessage) error {
	if q == nil || q.producer == nil {
		return errors.New("v2 queue producer is not configured")
	}

	return q.producer.Produce(ctx, queuev2.TaskMessage{
		TaskID: strings.TrimSpace(message.TaskID),
		Token:  strings.TrimSpace(message.Token),
	})
}

func parseInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return value
}

func normalizePage(page int) int {
	if page <= 0 {
		return defaultListPage
	}
	return page
}

func normalizePerPage(perPage int) int {
	if perPage <= 0 {
		return defaultListPerPage
	}
	if perPage > maxListPerPage {
		return maxListPerPage
	}
	return perPage
}

func normalizeCanonicalURL(url string, canonicalURL *string) string {
	if canonicalURL == nil {
		return strings.TrimSpace(url)
	}
	normalized := strings.TrimSpace(*canonicalURL)
	if normalized == "" {
		return strings.TrimSpace(url)
	}
	return normalized
}

func buildDashboardSummary(statusCounts map[string]int) DashboardSummary {
	safeCount := func(status string) int {
		value := statusCounts[strings.ToUpper(strings.TrimSpace(status))]
		if value < 0 {
			return 0
		}
		return value
	}

	queued := safeCount(TaskStatusQueued)
	running := safeCount(TaskStatusRunning)
	success := safeCount(TaskStatusSuccess)
	failed := safeCount(TaskStatusFailed)
	canceled := safeCount(TaskStatusCanceled)

	total := queued + running + success + failed + canceled
	active := queued + running
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
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": strings.TrimSpace(message),
	})
}

func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

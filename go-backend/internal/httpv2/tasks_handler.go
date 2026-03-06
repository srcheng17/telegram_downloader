package httpv2

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	queuev2 "github.com/ryancheng/telegram-downloader/go-backend/internal/queue/v2"
	"github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres"
)

const (
	defaultListPage    = 1
	defaultListPerPage = 20
	maxListPerPage     = 100
)

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
}

type TaskQueueMessage struct {
	TaskID string
	Token  string
}

type TaskQueue interface {
	Enqueue(ctx context.Context, message TaskQueueMessage) error
}

type TasksHandler struct {
	store TaskStore
	queue TaskQueue
}

func NewTasksHandler(store TaskStore, queue TaskQueue) *TasksHandler {
	return &TasksHandler{store: store, queue: queue}
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
		task.Status = "QUEUED"
	}

	if err := h.queue.Enqueue(r.Context(), TaskQueueMessage{TaskID: task.ID, Token: createInput.EnqueueToken}); err != nil {
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

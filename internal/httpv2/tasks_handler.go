package httpv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	domainv2 "github.com/ryancheng/telegram-downloader/internal/domain/v2"
	queuev2 "github.com/ryancheng/telegram-downloader/internal/queue/v2"
	"github.com/ryancheng/telegram-downloader/internal/service"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
)

const (
	defaultListPage    = 1
	defaultListPerPage = 20
	maxListPerPage     = 100

	TaskStatusUploading       = "UPLOADING"
	TaskStatusQueued          = string(domainv2.StatusQueued)
	TaskStatusRunning         = string(domainv2.StatusRunning)
	TaskStatusCancelRequested = string(domainv2.StatusCancelRequested)
	TaskStatusSuccess         = string(domainv2.StatusSuccess)
	TaskStatusFailed          = string(domainv2.StatusFailed)
	TaskStatusCanceled        = string(domainv2.StatusCanceled)
)

func NormalizeTaskStatus(status string) string {
	return strings.ToUpper(strings.TrimSpace(status))
}

func IsActiveTaskStatus(status string) bool {
	switch NormalizeTaskStatus(status) {
	case TaskStatusUploading, TaskStatusQueued, TaskStatusRunning, TaskStatusCancelRequested:
		return true
	default:
		return false
	}
}

type CreateTaskInput struct {
	ID                string
	URL               string
	CanonicalURL      *string
	EnqueueToken      string
	Progress          int
	TotalImages       int
	TaskType          *string
	SourceArchivePath *string
	SourceArchiveName *string
	UploadLoadedBytes int64
	UploadTotalBytes  int64
	Retryable         bool
	Author            *string
	SeriesName        *string
	ComicName         *string
	Summary           *string
	TagsRaw           *string
	TagsNormalized    *string
	GenresRaw         *string
	GenresNormalized  *string
}

type Task struct {
	ID                string    `json:"id"`
	URL               string    `json:"url"`
	CanonicalURL      *string   `json:"canonical_url,omitempty"`
	Status            string    `json:"status"`
	Progress          int       `json:"progress,omitempty"`
	TotalImages       int       `json:"total_images,omitempty"`
	TaskType          *string   `json:"task_type,omitempty"`
	SourceArchivePath *string   `json:"-"`
	SourceArchiveName *string   `json:"source_archive_name,omitempty"`
	UploadLoadedBytes int64     `json:"upload_loaded_bytes,omitempty"`
	UploadTotalBytes  int64     `json:"upload_total_bytes,omitempty"`
	Retryable         bool      `json:"retryable,omitempty"`
	Error             *string   `json:"error,omitempty"`
	ResultZipPath     *string   `json:"result_zip_path,omitempty"`
	Author            *string   `json:"author,omitempty"`
	SeriesName        *string   `json:"series_name,omitempty"`
	ComicName         *string   `json:"comic_name,omitempty"`
	Summary           *string   `json:"summary,omitempty"`
	TagsRaw           *string   `json:"tags_raw,omitempty"`
	TagsNormalized    *string   `json:"tags_normalized,omitempty"`
	GenresRaw         *string   `json:"genres_raw,omitempty"`
	GenresNormalized  *string   `json:"genres_normalized,omitempty"`
	CreatedAt         time.Time `json:"created_at,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

type ListTasksQuery struct {
	Page    int
	PerPage int
	Status  string
	Query   string
}

type LegacyClaimTaskDecision string

const (
	LegacyClaimTaskDecisionCreated      LegacyClaimTaskDecision = "created"
	LegacyClaimTaskDecisionReuseSuccess LegacyClaimTaskDecision = "reuse_success"
	LegacyClaimTaskDecisionReuseActive  LegacyClaimTaskDecision = "reuse_active"
)

type LegacyClaimTaskInput struct {
	ID                string
	URL               string
	CanonicalURL      *string
	EnqueueToken      string
	ReuseSuccess      bool
	Progress          int
	TotalImages       int
	TaskType          *string
	SourceArchivePath *string
	SourceArchiveName *string
	UploadLoadedBytes int64
	UploadTotalBytes  int64
	Retryable         bool
	Author            *string
	SeriesName        *string
	ComicName         *string
	Summary           *string
	TagsRaw           *string
	TagsNormalized    *string
	GenresRaw         *string
	GenresNormalized  *string
}

type LegacyClaimTaskResult struct {
	Decision     LegacyClaimTaskDecision
	Task         Task
	EnqueueToken string
}

type ListTasksResult struct {
	Tasks         []Task                         `json:"tasks"`
	Total         int                            `json:"total"`
	Page          int                            `json:"page"`
	PerPage       int                            `json:"per_page"`
	TotalPages    int                            `json:"total_pages"`
	StatusCatalog map[string]apptasks.StatusMeta `json:"status_catalog,omitempty"`
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

type taskServiceStoreAdapter struct {
	store TaskStore
}

func (a taskServiceStoreAdapter) CreateTask(ctx context.Context, in apptasks.CreateTaskInput) (apptasks.TaskRecord, error) {
	if a.store == nil {
		return apptasks.TaskRecord{}, errors.New("task store is not configured")
	}
	task, err := a.store.CreateTask(ctx, CreateTaskInput{
		ID:           strings.TrimSpace(in.ID),
		URL:          strings.TrimSpace(in.URL),
		CanonicalURL: in.CanonicalURL,
		EnqueueToken: strings.TrimSpace(in.EnqueueToken),
	})
	if err != nil {
		return apptasks.TaskRecord{}, err
	}
	return apptasks.TaskRecord{
		ID:           task.ID,
		URL:          task.URL,
		CanonicalURL: task.CanonicalURL,
		Status:       task.Status,
	}, nil
}

func (a taskServiceStoreAdapter) GetTask(ctx context.Context, taskID string) (*apptasks.TaskRecord, error) {
	if a.store == nil {
		return nil, errors.New("task store is not configured")
	}
	task, err := a.store.GetTask(ctx, strings.TrimSpace(taskID))
	if err != nil || task == nil {
		return nil, err
	}
	return &apptasks.TaskRecord{
		ID:           task.ID,
		URL:          task.URL,
		CanonicalURL: task.CanonicalURL,
		Status:       task.Status,
	}, nil
}

func (a taskServiceStoreAdapter) CancelTask(ctx context.Context, taskID, fromStatus string) error {
	if a.store == nil {
		return errors.New("task store is not configured")
	}
	err := a.store.CancelTask(ctx, strings.TrimSpace(taskID), NormalizeTaskStatus(fromStatus))
	if errors.Is(err, postgres.ErrV2TaskStatusMismatchOrNotFound) {
		return apptasks.ErrTaskStatusConflict
	}
	return err
}

func (a taskServiceStoreAdapter) MarkTaskFailed(ctx context.Context, taskID, message string) error {
	if a.store == nil {
		return errors.New("task store is not configured")
	}
	return a.store.MarkTaskFailed(ctx, strings.TrimSpace(taskID), message)
}

type taskServiceQueueAdapter struct {
	queue TaskQueue
}

func (a taskServiceQueueAdapter) Enqueue(ctx context.Context, message apptasks.QueueMessage) error {
	if a.queue == nil {
		return errors.New("task queue is not configured")
	}
	return a.queue.Enqueue(ctx, TaskQueueMessage{
		TaskID: strings.TrimSpace(message.TaskID),
		Token:  strings.TrimSpace(message.Token),
	})
}

type DashboardSummary struct {
	TotalTasks    int                            `json:"total_tasks"`
	QueuedTasks   int                            `json:"queued_tasks"`
	RunningTasks  int                            `json:"running_tasks"`
	SuccessTasks  int                            `json:"success_tasks"`
	FailedTasks   int                            `json:"failed_tasks"`
	CanceledTasks int                            `json:"canceled_tasks"`
	ActiveTasks   int                            `json:"active_tasks"`
	FinishedTasks int                            `json:"finished_tasks"`
	SuccessRate   float64                        `json:"success_rate"`
	StatusCatalog map[string]apptasks.StatusMeta `json:"status_catalog"`
}

type TasksHandler struct {
	store     TaskStore
	queue     TaskQueue
	artifacts ArtifactService
	tasks     *apptasks.Service
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
		tasks:     apptasks.NewService(taskServiceStoreAdapter{store: store}, taskServiceQueueAdapter{queue: queue}),
	}
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

func (h *TasksHandler) CancelTask(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeError(w, http.StatusInternalServerError, "task store is not configured")
		return
	}

	taskID := strings.TrimSpace(chi.URLParam(r, "task_id"))
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}

	result, err := h.tasks.Cancel(r.Context(), taskID)
	if err != nil {
		switch {
		case errors.Is(err, apptasks.ErrTaskNotFound):
			writeError(w, http.StatusNotFound, "task not found")
		case errors.Is(err, apptasks.ErrTaskNotCancelable):
			writeError(w, http.StatusConflict, "task already completed")
		case errors.Is(err, apptasks.ErrTaskStatusConflict):
			writeError(w, http.StatusConflict, "task status conflict")
		default:
			writeError(w, http.StatusInternalServerError, "cancel task")
		}
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"task_id": result.TaskID,
		"status":  result.Status,
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
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
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
		ID:                strings.TrimSpace(in.ID),
		URL:               strings.TrimSpace(in.URL),
		CanonicalURL:      in.CanonicalURL,
		EnqueueToken:      strings.TrimSpace(in.EnqueueToken),
		Progress:          in.Progress,
		TotalImages:       in.TotalImages,
		TaskType:          in.TaskType,
		SourceArchivePath: in.SourceArchivePath,
		SourceArchiveName: in.SourceArchiveName,
		UploadLoadedBytes: in.UploadLoadedBytes,
		UploadTotalBytes:  in.UploadTotalBytes,
		Retryable:         in.Retryable,
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
		return Task{}, err
	}

	return Task{
		ID:                record.ID,
		URL:               record.URL,
		CanonicalURL:      record.CanonicalURL,
		Status:            record.Status,
		Progress:          record.Progress,
		TotalImages:       record.TotalImages,
		TaskType:          record.TaskType,
		SourceArchivePath: record.SourceArchivePath,
		SourceArchiveName: record.SourceArchiveName,
		UploadLoadedBytes: record.UploadLoadedBytes,
		UploadTotalBytes:  record.UploadTotalBytes,
		Retryable:         record.Retryable,
		Author:            record.Author,
		SeriesName:        record.SeriesName,
		ComicName:         record.ComicName,
		Summary:           record.Summary,
		TagsRaw:           record.TagsRaw,
		TagsNormalized:    record.TagsNormalized,
		GenresRaw:         record.GenresRaw,
		GenresNormalized:  record.GenresNormalized,
		CreatedAt:         record.CreatedAt,
		UpdatedAt:         record.UpdatedAt,
	}, nil
}

func (s *PostgresTaskStore) ClaimTaskForLegacy(ctx context.Context, in LegacyClaimTaskInput) (LegacyClaimTaskResult, error) {
	if s.db == nil {
		return LegacyClaimTaskResult{}, errors.New("v2 task database is not configured")
	}

	taskID := strings.TrimSpace(in.ID)
	if taskID == "" {
		return LegacyClaimTaskResult{}, errors.New("legacy claim task id is required")
	}
	rawURL := strings.TrimSpace(in.URL)
	if rawURL == "" {
		return LegacyClaimTaskResult{}, errors.New("legacy claim url is required")
	}
	canonicalURL := normalizeCanonicalURL(rawURL, in.CanonicalURL)
	enqueueToken := strings.TrimSpace(in.EnqueueToken)
	if enqueueToken == "" {
		return LegacyClaimTaskResult{}, errors.New("legacy claim enqueue token is required")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return LegacyClaimTaskResult{}, err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", canonicalURL); err != nil {
		return LegacyClaimTaskResult{}, err
	}

	if in.ReuseSuccess {
		reusedSuccess, err := querySingleTask(
			ctx,
			tx,
			`
			SELECT
				id,
				url,
				canonical_url,
				status,
				progress,
				total_images,
				task_type,
				source_archive_path,
				source_archive_name,
				upload_loaded_bytes,
				upload_total_bytes,
				retryable,
				error,
				result_zip_path,
				author,
				series_name,
				comic_name,
				summary,
				tags_raw,
				tags_normalized,
				genres_raw,
				genres_normalized,
				created_at,
				updated_at
			FROM v2_tasks
			WHERE
				status = $1
				AND canonical_url = $2
				AND result_zip_path IS NOT NULL
				AND result_zip_path != ''
			ORDER BY created_at DESC
			LIMIT 1
			`,
			TaskStatusSuccess,
			canonicalURL,
		)
		if err != nil {
			return LegacyClaimTaskResult{}, err
		}
		if reusedSuccess != nil {
			if err := tx.Commit(ctx); err != nil {
				return LegacyClaimTaskResult{}, err
			}
			committed = true
			return LegacyClaimTaskResult{
				Decision: LegacyClaimTaskDecisionReuseSuccess,
				Task:     *reusedSuccess,
			}, nil
		}
	}

	reusedActive, err := querySingleTask(
		ctx,
		tx,
		`
			SELECT
				id,
				url,
				canonical_url,
				status,
				progress,
				total_images,
				task_type,
				source_archive_path,
				source_archive_name,
				upload_loaded_bytes,
				upload_total_bytes,
				retryable,
				error,
				result_zip_path,
				author,
				series_name,
				comic_name,
				summary,
				tags_raw,
				tags_normalized,
				genres_raw,
				genres_normalized,
				created_at,
				updated_at
			FROM v2_tasks
		WHERE
			status = ANY($1)
			AND canonical_url = $2
		ORDER BY created_at DESC
		LIMIT 1
		`,
		[]string{TaskStatusQueued, TaskStatusRunning, TaskStatusCancelRequested},
		canonicalURL,
	)
	if err != nil {
		return LegacyClaimTaskResult{}, err
	}
	if reusedActive != nil {
		if err := tx.Commit(ctx); err != nil {
			return LegacyClaimTaskResult{}, err
		}
		committed = true
		return LegacyClaimTaskResult{
			Decision: LegacyClaimTaskDecisionReuseActive,
			Task:     *reusedActive,
		}, nil
	}

	now := time.Now().UTC()
	if _, err := tx.Exec(
		ctx,
		`
		INSERT INTO v2_tasks (
			id,
			url,
			canonical_url,
			status,
			progress,
			total_images,
			enqueue_token,
			task_type,
			source_archive_path,
			source_archive_name,
			upload_loaded_bytes,
			upload_total_bytes,
			retryable,
			author,
			series_name,
			comic_name,
			summary,
			tags_raw,
			tags_normalized,
			genres_raw,
			genres_normalized,
			created_at,
			updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23
		)
		`,
		taskID,
		rawURL,
		canonicalURL,
		TaskStatusQueued,
		in.Progress,
		in.TotalImages,
		enqueueToken,
		stringPtr(taskTypeURL),
		in.SourceArchivePath,
		in.SourceArchiveName,
		in.UploadLoadedBytes,
		in.UploadTotalBytes,
		in.Retryable,
		in.Author,
		in.SeriesName,
		in.ComicName,
		in.Summary,
		in.TagsRaw,
		in.TagsNormalized,
		in.GenresRaw,
		in.GenresNormalized,
		now,
		now,
	); err != nil {
		return LegacyClaimTaskResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return LegacyClaimTaskResult{}, err
	}
	committed = true
	return LegacyClaimTaskResult{
		Decision: LegacyClaimTaskDecisionCreated,
		Task: Task{
			ID:               taskID,
			URL:              rawURL,
			CanonicalURL:     stringPtr(canonicalURL),
			Status:           TaskStatusQueued,
			Progress:         in.Progress,
			TotalImages:      in.TotalImages,
			TaskType:         stringPtr(taskTypeURL),
			Author:           in.Author,
			SeriesName:       in.SeriesName,
			ComicName:        in.ComicName,
			Summary:          in.Summary,
			TagsRaw:          in.TagsRaw,
			TagsNormalized:   in.TagsNormalized,
			GenresRaw:        in.GenresRaw,
			GenresNormalized: in.GenresNormalized,
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		EnqueueToken: enqueueToken,
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
	filterSQL, filterArgs := buildTaskListFilter(status, q)

	countQuery := "SELECT COUNT(*) FROM v2_tasks"
	if filterSQL != "" {
		countQuery += " WHERE " + filterSQL
	}

	var total int
	if err := s.db.QueryRow(ctx, countQuery, filterArgs...).Scan(&total); err != nil {
		return ListTasksResult{}, err
	}

	rowsQuery := `
		SELECT
			id,
			url,
			canonical_url,
			status,
			progress,
			total_images,
			task_type,
			source_archive_path,
			source_archive_name,
			upload_loaded_bytes,
			upload_total_bytes,
			retryable,
			error,
			result_zip_path,
			author,
			series_name,
			comic_name,
			summary,
			tags_raw,
			tags_normalized,
			genres_raw,
			genres_normalized,
			created_at,
			updated_at
		FROM v2_tasks
	`
	if filterSQL != "" {
		rowsQuery += " WHERE " + filterSQL
	}
	rowsQuery += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT $%d OFFSET $%d", len(filterArgs)+1, len(filterArgs)+2)

	rowsArgs := append(append([]any{}, filterArgs...), perPage, offset)
	rows, err := s.db.Query(ctx, rowsQuery, rowsArgs...)
	if err != nil {
		return ListTasksResult{}, err
	}
	defer rows.Close()

	tasks := make([]Task, 0, perPage)
	for rows.Next() {
		row, err := scanTaskRow(rows)
		if err != nil {
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
			progress,
			total_images,
			task_type,
			source_archive_path,
			source_archive_name,
			upload_loaded_bytes,
			upload_total_bytes,
			retryable,
			error,
			result_zip_path,
			author,
			series_name,
			comic_name,
			summary,
			tags_raw,
			tags_normalized,
			genres_raw,
			genres_normalized,
			created_at,
			updated_at
		FROM v2_tasks
		WHERE id = $1
		`,
		strings.TrimSpace(taskID),
	)

	task, err := scanTaskRow(row)
	if err != nil {
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

	reason := "Cancellation requested by user."
	return s.writer.TransitionTaskWithEvent(ctx, postgres.TransitionTaskWithEventInput{
		TaskID:     strings.TrimSpace(taskID),
		FromStatus: status,
		ToStatus:   TaskStatusCancelRequested,
		Patch: postgres.StatusPatch{
			Error: stringPtr(reason),
		},
		EventType:   "CANCEL_REQUESTED",
		PayloadJSON: `{"action":"cancel_requested"}`,
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

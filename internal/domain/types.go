package domain

type StartupRecovery struct {
	Happened          bool    `json:"happened"`
	RecoveredTotal    int     `json:"recovered_total"`
	RecoveredFailed   int     `json:"recovered_failed"`
	RecoveredCanceled int     `json:"recovered_canceled"`
	OccurredAt        *string `json:"occurred_at"`
}

func DefaultStartupRecovery() StartupRecovery {
	return StartupRecovery{
		Happened:          false,
		RecoveredTotal:    0,
		RecoveredFailed:   0,
		RecoveredCanceled: 0,
		OccurredAt:        nil,
	}
}

type Summary struct {
	TotalTasks           int             `json:"total_tasks"`
	PendingTasks         int             `json:"pending_tasks"`
	InProgressTasks      int             `json:"in_progress_tasks"`
	CancelRequestedTasks int             `json:"cancel_requested_tasks"`
	CanceledTasks        int             `json:"canceled_tasks"`
	SuccessTasks         int             `json:"success_tasks"`
	FailedTasks          int             `json:"failed_tasks"`
	ActiveTasks          int             `json:"active_tasks"`
	FinishedTasks        int             `json:"finished_tasks"`
	SuccessRate          *float64        `json:"success_rate"`
	StartupRecovery      StartupRecovery `json:"startup_recovery"`
}

type StatusMeta struct {
	Label       string `json:"label"`
	CanCancel   bool   `json:"can_cancel"`
	CanDownload bool   `json:"can_download"`
	Terminal    bool   `json:"terminal"`
}

type TaskLog struct {
	ID                string  `json:"id"`
	URL               string  `json:"url"`
	CanonicalURL      *string `json:"canonical_url"`
	Status            string  `json:"status"`
	TaskType          *string `json:"task_type"`
	SourceArchiveName *string `json:"source_archive_name"`
	UploadLoadedBytes int64   `json:"upload_loaded_bytes"`
	UploadTotalBytes  int64   `json:"upload_total_bytes"`
	Retryable         bool    `json:"retryable"`
	StartTime         float64 `json:"start_time"`
	Error             *string `json:"error"`
	Progress          int     `json:"progress"`
	TotalImages       int     `json:"total_images"`
	ImageConcurrency  int     `json:"image_concurrency"`
	ResultZipPath     *string `json:"result_zip_path"`
	Author            *string `json:"author"`
	SeriesName        *string `json:"series_name"`
	ComicName         *string `json:"comic_name"`
	Summary           *string `json:"summary"`
	TagsRaw           *string `json:"tags_raw"`
	TagsNormalized    *string `json:"tags_normalized"`
	GenresRaw         *string `json:"genres_raw"`
	GenresNormalized  *string `json:"genres_normalized"`
}

type LogQuery struct {
	Page    int
	PerPage int
	Status  string
	Keyword string
}

type LogListResult struct {
	Logs       []TaskLog
	Total      int
	Page       int
	PerPage    int
	TotalPages int
}

type LogFilters struct {
	Status string `json:"status"`
	Query  string `json:"q"`
}

type LogsResponse struct {
	Logs           []TaskLog             `json:"logs"`
	Total          int                   `json:"total"`
	Page           int                   `json:"page"`
	PerPage        int                   `json:"per_page"`
	TotalPages     int                   `json:"total_pages"`
	HasActiveTasks bool                  `json:"has_active_tasks"`
	Filters        LogFilters            `json:"filters"`
	StatusCatalog  map[string]StatusMeta `json:"status_catalog"`
	Summary        Summary               `json:"summary"`
}

type ClaimDecision string

const (
	ClaimDecisionCreated      ClaimDecision = "created"
	ClaimDecisionReuseSuccess ClaimDecision = "reuse_success"
	ClaimDecisionReuseActive  ClaimDecision = "reuse_active"
)

type ClaimDownloadTaskInput struct {
	Task           TaskLog
	EnqueueToken   string
	ActiveStatuses []string
	ReuseSuccess   bool
}

type ClaimDownloadTaskResult struct {
	Decision ClaimDecision
	Task     TaskLog
}

type DownloadedImage struct {
	URL         string
	ContentType string
	Data        []byte
}

type DownloadResult struct {
	Images           []DownloadedImage
	TotalImages      int
	DownloadedImages int
	TotalBytes       int64
}

package taskcore

import (
	"context"
	"strings"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/config"

	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

type Config struct {
	LeaseTTL    time.Duration
	MaxAttempts int
}

type Task struct {
	ID             string
	Kind           domain.Kind
	Status         domain.Status
	Attempt        int
	Generation     int64
	LastError      string
	LeaseOwner     string
	LeaseExpiresAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Input struct {
	TaskID            string
	URL               string
	CanonicalURL      string
	SourceArchiveName string
	SourceArchivePath string
	SourceArchiveSize int64
	Metadata          map[string]string
	RuntimeSettings   *config.SettingsSnapshot
}

// HasSource reports whether the persisted input can execute the task kind.
func (in Input) HasSource(kind domain.Kind) bool {
	switch kind {
	case domain.KindUpload:
		return strings.TrimSpace(in.SourceArchivePath) != ""
	case domain.KindURL:
		return strings.TrimSpace(in.CanonicalURL) != "" || strings.TrimSpace(in.URL) != ""
	default:
		return false
	}
}

type Result struct {
	TaskID          string
	ArtifactPath    string
	ArtifactName    string
	ArtifactSize    int64
	ArtifactKind    string
	KomgaTargetPath string
}

type TaskView struct {
	Task     Task
	Input    Input
	Progress domain.Progress
	Result   *Result
}

type CreateURLInput struct {
	ID              string
	URL             string
	CanonicalURL    string
	Metadata        map[string]string
	Force           bool
	RuntimeSettings *config.SettingsSnapshot
}

type CreateURLResult struct {
	Task
	Reused            bool
	NeedsConfirmation bool
	Result            *Result
}

type TaskQuery struct {
	Status  domain.Status
	Keyword string
	Limit   int
	Offset  int
}

type TaskPage struct {
	Tasks []TaskView
	Total int
}

type InitUploadInput struct {
	ID              string
	Metadata        map[string]string
	RuntimeSettings *config.SettingsSnapshot
}

type AttachUploadSourceInput struct {
	TaskID string
	Name   string
	Path   string
	Size   int64
}

type ClaimResult struct {
	Task *Task
}

type HeartbeatResult struct {
	CancelRequested bool
}

type CompleteInput struct {
	TaskID       string
	WorkerID     string
	Attempt      int
	Generation   int64
	ArtifactPath string
	ArtifactName string
	ArtifactSize int64
}

type FailInput struct {
	TaskID     string
	WorkerID   string
	Attempt    int
	Generation int64
	Message    string
}

type RecoveryResult struct {
	Requeued int
	Failed   int
	Canceled int
}

type Repository interface {
	CreateURLTask(ctx context.Context, task Task, input Input, progress domain.Progress, force bool) (CreateURLResult, error)
	QueryTasks(ctx context.Context, query TaskQuery) (TaskPage, error)
	StatusCounts(ctx context.Context) (map[domain.Status]int, error)
	CreateTask(ctx context.Context, task Task, input Input, progress domain.Progress) (Task, error)
	GetTask(ctx context.Context, taskID string) (*TaskView, error)
	Transition(ctx context.Context, taskID string, to domain.Status, actor domain.Actor, message string) (Task, error)
	AttachUploadSource(ctx context.Context, input AttachUploadSourceInput, progress domain.Progress) (Task, error)
	Retry(ctx context.Context, taskID string, progress domain.Progress) (Task, error)
	ClaimNext(ctx context.Context, workerID string, leaseTTL time.Duration) (*Task, error)
	Heartbeat(ctx context.Context, taskID string, workerID string, generation int64, leaseTTL time.Duration) (HeartbeatResult, error)
	UpdateProgress(ctx context.Context, taskID string, workerID string, generation int64, progress domain.Progress) error
	Complete(ctx context.Context, in CompleteInput) error
	Fail(ctx context.Context, in FailInput) error
	AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int, generation int64) error
	RecoverExpired(ctx context.Context, maxAttempts int) (RecoveryResult, error)
	ListTasks(ctx context.Context, limit int, offset int) ([]TaskView, error)
}

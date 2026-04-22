package taskcore

import (
	"context"
	"time"

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
	ID           string
	URL          string
	CanonicalURL string
	Metadata     map[string]string
}

type InitUploadInput struct {
	ID       string
	Metadata map[string]string
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
	ArtifactPath string
	ArtifactName string
	ArtifactSize int64
}

type FailInput struct {
	TaskID   string
	WorkerID string
	Attempt  int
	Message  string
}

type RecoveryResult struct {
	Requeued int
	Failed   int
	Canceled int
}

type Repository interface {
	CreateTask(ctx context.Context, task Task, input Input, progress domain.Progress) (Task, error)
	GetTask(ctx context.Context, taskID string) (*TaskView, error)
	Transition(ctx context.Context, taskID string, to domain.Status, actor domain.Actor, message string) (Task, error)
	AttachUploadSource(ctx context.Context, input AttachUploadSourceInput, progress domain.Progress) (Task, error)
	Retry(ctx context.Context, taskID string, progress domain.Progress) (Task, error)
	ClaimNext(ctx context.Context, workerID string, leaseTTL time.Duration) (*Task, error)
	Heartbeat(ctx context.Context, taskID string, workerID string, leaseTTL time.Duration) (HeartbeatResult, error)
	UpdateProgress(ctx context.Context, taskID string, progress domain.Progress) error
	Complete(ctx context.Context, in CompleteInput) error
	Fail(ctx context.Context, in FailInput) error
	AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int) error
	RecoverExpired(ctx context.Context, maxAttempts int) (RecoveryResult, error)
	ListTasks(ctx context.Context, limit int, offset int) ([]TaskView, error)
}

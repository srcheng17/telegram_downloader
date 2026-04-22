package taskcore

import (
	"context"
	"errors"
	"strings"
	"time"

	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

var (
	ErrInvalidInput = errors.New("invalid task core input")
	ErrNotFound     = errors.New("task not found")
	ErrConflict     = errors.New("task state conflict")
)

type Service struct {
	repo Repository
	cfg  Config
}

func NewService(repo Repository, cfg Config) *Service {
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 30 * time.Second
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	return &Service{repo: repo, cfg: cfg}
}

func (s *Service) CreateURLTask(ctx context.Context, in CreateURLInput) (Task, error) {
	id := strings.TrimSpace(in.ID)
	url := strings.TrimSpace(in.URL)
	if id == "" || url == "" {
		return Task{}, ErrInvalidInput
	}
	canonical := strings.TrimSpace(in.CanonicalURL)
	if canonical == "" {
		canonical = url
	}

	return s.repo.CreateTask(ctx,
		Task{ID: id, Kind: domain.KindURL, Status: domain.StatusReady},
		Input{TaskID: id, URL: url, CanonicalURL: canonical, Metadata: in.Metadata},
		domain.NewProgress(domain.PhasePreparing, 0, 0, domain.UnitNone, "准备下载"),
	)
}

func (s *Service) InitUploadTask(ctx context.Context, in InitUploadInput) (Task, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return Task{}, ErrInvalidInput
	}

	return s.repo.CreateTask(ctx,
		Task{ID: id, Kind: domain.KindUpload, Status: domain.StatusCreated},
		Input{TaskID: id, Metadata: in.Metadata},
		domain.NewProgress(domain.PhaseUploading, 0, 0, domain.UnitBytes, "等待上传"),
	)
}

func (s *Service) AttachUploadSource(ctx context.Context, in AttachUploadSourceInput) (Task, error) {
	in.TaskID = strings.TrimSpace(in.TaskID)
	in.Path = strings.TrimSpace(in.Path)
	in.Name = strings.TrimSpace(in.Name)
	if in.TaskID == "" || in.Path == "" || in.Name == "" || in.Size < 0 {
		return Task{}, ErrInvalidInput
	}

	return s.repo.AttachUploadSource(ctx, in, domain.NewProgress(domain.PhasePreparing, in.Size, in.Size, domain.UnitBytes, "上传完成，等待处理"))
}

func (s *Service) RequestCancel(ctx context.Context, taskID string) (Task, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Task{}, ErrInvalidInput
	}
	return s.repo.Transition(ctx, taskID, domain.StatusCanceling, domain.ActorAPI, "cancel requested")
}

func (s *Service) Retry(ctx context.Context, taskID string) (Task, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Task{}, ErrInvalidInput
	}
	return s.repo.Retry(ctx, taskID, domain.NewProgress(domain.PhasePreparing, 0, 0, domain.UnitNone, "等待重试"))
}

func (s *Service) ClaimNext(ctx context.Context, workerID string) (ClaimResult, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return ClaimResult{}, ErrInvalidInput
	}
	task, err := s.repo.ClaimNext(ctx, workerID, s.cfg.LeaseTTL)
	if err != nil || task == nil {
		return ClaimResult{Task: task}, err
	}
	return ClaimResult{Task: task}, nil
}

func (s *Service) Heartbeat(ctx context.Context, taskID string, workerID string) (HeartbeatResult, error) {
	taskID = strings.TrimSpace(taskID)
	workerID = strings.TrimSpace(workerID)
	if taskID == "" || workerID == "" {
		return HeartbeatResult{}, ErrInvalidInput
	}
	return s.repo.Heartbeat(ctx, taskID, workerID, s.cfg.LeaseTTL)
}

func (s *Service) ReportProgress(ctx context.Context, taskID string, progress domain.Progress) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ErrInvalidInput
	}
	return s.repo.UpdateProgress(ctx, taskID, progress)
}

func (s *Service) Complete(ctx context.Context, in CompleteInput) error {
	in.TaskID = strings.TrimSpace(in.TaskID)
	in.WorkerID = strings.TrimSpace(in.WorkerID)
	in.ArtifactPath = strings.TrimSpace(in.ArtifactPath)
	in.ArtifactName = strings.TrimSpace(in.ArtifactName)
	if in.TaskID == "" || in.WorkerID == "" || in.Attempt <= 0 || in.ArtifactPath == "" || in.ArtifactName == "" || in.ArtifactSize < 0 {
		return ErrInvalidInput
	}
	return s.repo.Complete(ctx, in)
}

func (s *Service) Fail(ctx context.Context, in FailInput) error {
	in.TaskID = strings.TrimSpace(in.TaskID)
	in.WorkerID = strings.TrimSpace(in.WorkerID)
	if in.TaskID == "" || in.WorkerID == "" || in.Attempt <= 0 {
		return ErrInvalidInput
	}
	if strings.TrimSpace(in.Message) == "" {
		in.Message = "task failed"
	}
	return s.repo.Fail(ctx, in)
}

func (s *Service) AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int) error {
	taskID = strings.TrimSpace(taskID)
	workerID = strings.TrimSpace(workerID)
	if taskID == "" || workerID == "" || attempt <= 0 {
		return ErrInvalidInput
	}
	return s.repo.AcknowledgeCancel(ctx, taskID, workerID, attempt)
}

func (s *Service) GetTask(ctx context.Context, taskID string) (*TaskView, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, ErrInvalidInput
	}
	return s.repo.GetTask(ctx, taskID)
}

func (s *Service) ListTasks(ctx context.Context, limit int, offset int) ([]TaskView, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.ListTasks(ctx, limit, offset)
}

func (s *Service) RecoverExpired(ctx context.Context) (RecoveryResult, error) {
	return s.repo.RecoverExpired(ctx, s.cfg.MaxAttempts)
}

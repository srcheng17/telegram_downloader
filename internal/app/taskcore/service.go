package taskcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ryancheng/telegram-downloader/internal/app/telegram"
	metadata "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"strings"
	"time"

	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
	"github.com/ryancheng/telegram-downloader/internal/config"

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

func (s *Service) CreateURLTask(ctx context.Context, in CreateURLInput) (CreateURLResult, error) {
	id := strings.TrimSpace(in.ID)
	url := strings.TrimSpace(in.URL)
	if id == "" || url == "" {
		return CreateURLResult{}, ErrInvalidInput
	}
	canonical := strings.TrimSpace(in.CanonicalURL)
	if canonical == "" {
		canonical = url
	}
	fingerprint := in
	fingerprint.ID, fingerprint.IdempotencyKey, fingerprint.URL = "", "", canonical
	fingerprint.RuntimeSettings = nil
	submission, err := newSubmission(in.IdempotencyKey, in.DeliveryTarget, fingerprint)
	if err != nil {
		return CreateURLResult{}, err
	}
	if replay, err := s.replaySubmission(ctx, submission); err != nil || replay != nil {
		if replay != nil {
			return *replay, nil
		}
		return CreateURLResult{}, err
	}
	document, projection, err := s.normalizeMetadata(ctx, in.MetadataDocument, in.Metadata)
	if err != nil {
		return CreateURLResult{}, err
	}
	task := Task{ID: id, Kind: domain.KindURL, Status: domain.StatusReady}
	input := Input{Submission: submission, CanReuseResult: reusableTaskArtifact, TaskID: id, URL: url, CanonicalURL: canonical, Metadata: projection, MetadataDocument: &document, RuntimeSettings: normalizedRuntimeSettings(in.RuntimeSettings)}
	progress := domain.NewProgress(domain.PhasePreparing, 0, 0, domain.UnitNone, "准备下载")
	return s.repo.CreateURLTask(ctx, task, input, progress, in.Force)
}

func normalizedRuntimeSettings(snapshot *config.SettingsSnapshot) *config.SettingsSnapshot {
	if snapshot == nil {
		return nil
	}
	normalized := config.NormalizeSettingsSnapshot(*snapshot)
	return &normalized
}

func (s *Service) CreateTelegramTask(ctx context.Context, in CreateTelegramInput) (CreateURLResult, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" || telegram.ValidateInput(in.Source) != nil {
		return CreateURLResult{}, ErrInvalidInput
	}
	document, projection, err := s.normalizeMetadata(ctx, in.MetadataDocument, nil)
	if err != nil {
		return CreateURLResult{}, err
	}
	task := Task{ID: id, Kind: domain.KindTelegram, Status: domain.StatusReady}
	source := in.Source
	input := Input{TaskID: id, URL: source.MessageURL, CanonicalURL: source.MessageURL, Telegram: &source, Metadata: projection, MetadataDocument: &document, RuntimeSettings: normalizedRuntimeSettings(in.RuntimeSettings)}
	progress := domain.NewProgress(domain.PhasePreparing, 0, 0, domain.UnitNone, "等待 Telegram 附件下载")
	result, err := s.repo.CreateURLTask(ctx, task, input, progress, in.Force)
	if err != nil || !result.NeedsConfirmation {
		return result, err
	}
	if result.Result != nil {
		artifact, openErr := apptasks.NewArtifactAccess(apptasks.ArtifactAccessConfig{}).Open(result.Result.ArtifactPath)
		if openErr == nil {
			_ = artifact.Close()
			return result, nil
		}
	}
	return s.repo.CreateURLTask(ctx, task, input, progress, true)
}

func (s *Service) InitUploadTask(ctx context.Context, in InitUploadInput) (Task, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return Task{}, ErrInvalidInput
	}
	if in.IdempotencyKey != "" && (!sourceHashPattern.MatchString(in.FileSHA256) || in.FileName == "" || in.FileSize <= 0 || in.FileSize > config.MaxUploadBytes) {
		return Task{}, ErrInvalidInput
	}
	fingerprint := in
	fingerprint.ID, fingerprint.IdempotencyKey = "", ""
	fingerprint.RuntimeSettings = nil
	submission, err := newSubmission(in.IdempotencyKey, in.DeliveryTarget, fingerprint)
	if err != nil {
		return Task{}, err
	}
	if replay, err := s.replaySubmission(ctx, submission); err != nil || replay != nil {
		if replay != nil {
			return replay.Task, nil
		}
		return Task{}, err
	}
	document, projection, err := s.normalizeMetadata(ctx, in.MetadataDocument, in.Metadata)
	if err != nil {
		return Task{}, err
	}
	input := Input{Submission: submission, TaskID: id, Metadata: projection, MetadataDocument: &document, RuntimeSettings: normalizedRuntimeSettings(in.RuntimeSettings)}
	if submission != nil {
		input.SourceArchiveName, input.SourceArchiveSize, input.SourceSHA256 = in.FileName, in.FileSize, in.FileSHA256
	}
	return s.repo.CreateTask(ctx, Task{ID: id, Kind: domain.KindUpload, Status: domain.StatusCreated}, input,
		domain.NewProgress(domain.PhaseUploading, 0, 0, domain.UnitBytes, "等待上传"))
}

func reusableTaskArtifact(result *Result) bool {
	if result == nil {
		return false
	}
	artifact, err := apptasks.NewArtifactAccess(apptasks.ArtifactAccessConfig{}).Open(result.ArtifactPath)
	if err != nil {
		return false
	}
	_ = artifact.Close()
	return true
}

func (s *Service) AttachUploadSource(ctx context.Context, in AttachUploadSourceInput) (Task, error) {
	in.TaskID = strings.TrimSpace(in.TaskID)
	in.Path = strings.TrimSpace(in.Path)
	in.Name = strings.TrimSpace(in.Name)
	if in.TaskID == "" || in.Path == "" || in.Name == "" || in.Size < 0 {
		return Task{}, ErrInvalidInput
	}

	view, err := s.repo.GetTask(ctx, in.TaskID)
	if err != nil {
		return Task{}, err
	}
	if view == nil {
		return Task{}, ErrNotFound
	}
	if err := verifyUploadSnapshot(view.Input, in); err != nil {
		return Task{}, err
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
	view, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	if view == nil {
		return Task{}, ErrNotFound
	}
	if !domain.CanRetry(view.Task.Status, view.Input.HasSource(view.Task.Kind)) {
		return Task{}, ErrConflict
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

func (s *Service) Heartbeat(ctx context.Context, taskID string, workerID string, generation int64) (HeartbeatResult, error) {
	taskID = strings.TrimSpace(taskID)
	workerID = strings.TrimSpace(workerID)
	if taskID == "" || workerID == "" || generation <= 0 {
		return HeartbeatResult{}, ErrInvalidInput
	}
	return s.repo.Heartbeat(ctx, taskID, workerID, generation, s.cfg.LeaseTTL)
}

func (s *Service) ReportProgress(ctx context.Context, taskID string, workerID string, generation int64, progress domain.Progress) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || strings.TrimSpace(workerID) == "" || generation <= 0 {
		return ErrInvalidInput
	}
	return s.repo.UpdateProgress(ctx, taskID, workerID, generation, progress)
}

func (s *Service) Complete(ctx context.Context, in CompleteInput) error {
	in.TaskID = strings.TrimSpace(in.TaskID)
	in.WorkerID = strings.TrimSpace(in.WorkerID)
	in.ArtifactPath = strings.TrimSpace(in.ArtifactPath)
	in.ArtifactName = strings.TrimSpace(in.ArtifactName)
	if in.TaskID == "" || in.WorkerID == "" || in.Attempt <= 0 || in.Generation <= 0 || in.ArtifactPath == "" || in.ArtifactName == "" || in.ArtifactSize < 0 {
		return ErrInvalidInput
	}
	if in.EffectiveMetadataDocument != nil {
		view, err := s.repo.GetTask(ctx, in.TaskID)
		if err != nil {
			return err
		}
		if view == nil || view.Input.MetadataDocument == nil || view.Input.MetadataDocument.DefinitionsVersion != in.EffectiveMetadataDocument.DefinitionsVersion {
			return ErrInvalidInput
		}
		var data []byte
		if s.cfg.MetadataEncoder != nil {
			data, err = s.cfg.MetadataEncoder.Encode(ctx, *in.EffectiveMetadataDocument)
		} else {
			data, err = json.Marshal(in.EffectiveMetadataDocument)
		}
		if err != nil {
			return err
		}
		validated, err := metadata.DecodeStored(data)
		if err != nil {
			return err
		}
		in.EffectiveMetadataDocument = &validated
	}
	if in.RetentionManifest != nil {
		if err := domain.ValidateRetentionManifest(*in.RetentionManifest); err != nil {
			return ErrInvalidInput
		}
	}
	return s.repo.Complete(ctx, in)
}

func (s *Service) Fail(ctx context.Context, in FailInput) error {
	in.TaskID = strings.TrimSpace(in.TaskID)
	in.WorkerID = strings.TrimSpace(in.WorkerID)
	if in.TaskID == "" || in.WorkerID == "" || in.Attempt <= 0 || in.Generation <= 0 {
		return ErrInvalidInput
	}
	if strings.TrimSpace(in.Message) == "" {
		in.Message = "task failed"
	}
	return s.repo.Fail(ctx, in)
}

func (s *Service) AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int, generation int64) error {
	taskID = strings.TrimSpace(taskID)
	workerID = strings.TrimSpace(workerID)
	if taskID == "" || workerID == "" || attempt <= 0 || generation <= 0 {
		return ErrInvalidInput
	}
	return s.repo.AcknowledgeCancel(ctx, taskID, workerID, attempt, generation)
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

func (s *Service) QueryTasks(ctx context.Context, query TaskQuery) (TaskPage, error) {
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 100 {
		query.Limit = 100
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	query.Keyword = strings.TrimSpace(query.Keyword)
	return s.repo.QueryTasks(ctx, query)
}

func (s *Service) StatusCounts(ctx context.Context) (map[domain.Status]int, error) {
	return s.repo.StatusCounts(ctx)
}

// normalizeMetadata freezes the document at creation; retries never consult the registry.
func (s *Service) normalizeMetadata(ctx context.Context, doc *metadata.Document, values map[string]string) (metadata.Document, map[string]string, error) {
	legacy := LegacyMetadata(values)
	var out metadata.Document
	var projection metadata.Legacy
	var err error
	if s.cfg.MetadataNormalizer != nil {
		out, projection, err = s.cfg.MetadataNormalizer.NormalizeInput(ctx, doc, legacy)
	} else if doc == nil {
		out, err = metadata.FromLegacy(legacy)
		if err == nil {
			projection = metadata.ToLegacy(out)
		}
	} else {
		return metadata.Document{}, nil, fmt.Errorf("%w: metadata registry unavailable", ErrInvalidInput)
	}
	if err != nil {
		return metadata.Document{}, nil, fmt.Errorf("%w: metadata validation: %w", ErrInvalidInput, err)
	}
	return out, MetadataProjection(projection), nil
}

func LegacyMetadata(values map[string]string) metadata.Legacy {
	get := func(key string) *string {
		v, ok := values[key]
		if !ok {
			return nil
		}
		return &v
	}
	return metadata.Legacy{Author: get("author"), ComicName: get("comic_name"), SeriesName: get("series_name"), SeriesNumber: get("series_number"), Summary: get("summary"), Tags: get("tags"), Genres: get("genres")}
}

func MetadataProjection(in metadata.Legacy) map[string]string {
	out := map[string]string{}
	for key, value := range map[string]*string{"author": in.Author, "comic_name": in.ComicName, "series_name": in.SeriesName, "series_number": in.SeriesNumber, "summary": in.Summary, "tags": in.Tags, "genres": in.Genres} {
		if value != nil {
			out[key] = *value
		}
	}
	out["tags_normalized"] = out["tags"]
	out["genres_normalized"] = out["genres"]
	return out
}

package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

var ErrInvalidUploadArchive = errors.New("invalid upload archive")

type UploadInitRecord struct {
	ID                string
	EnqueueToken      string
	TaskType          string
	CanonicalURL      *string
	SourceArchiveName *string
	UploadTotalBytes  int64
	Author            *string
	SeriesName        *string
	ComicName         *string
	Summary           *string
	TagsRaw           *string
	TagsNormalized    *string
	GenresRaw         *string
	GenresNormalized  *string
}

type UploadTaskRecord struct {
	ID                string
	Status            string
	TaskType          string
	SourceArchiveName *string
	UploadTotalBytes  int64
}

type MetadataHistoryRecord struct {
	TaskType   string
	Author     *string
	SeriesName *string
	ComicName  *string
	Summary    *string
	Tags       *string
	Genres     *string
}

type UploadInitStore interface {
	CreateUploadTask(ctx context.Context, in UploadInitRecord) (UploadTaskRecord, error)
	InsertMetadataHistory(ctx context.Context, in MetadataHistoryRecord) error
}

type UploadInitService struct {
	Store          UploadInitStore
	IDGenerator    func() string
	TokenGenerator func() string
}

type UploadInitInput struct {
	FileName string
	FileSize int64
	Metadata MetadataInput
}

type UploadInitResult struct {
	TaskID      string
	Status      string
	UploadToken string
	UploadURL   string
	LogsURL     string
}

func NewUploadInitService(store UploadInitStore) *UploadInitService {
	return &UploadInitService{
		Store:          store,
		IDGenerator:    uuid.NewString,
		TokenGenerator: uuid.NewString,
	}
}

func (s *UploadInitService) Init(ctx context.Context, in UploadInitInput) (UploadInitResult, error) {
	if s == nil || s.Store == nil {
		return UploadInitResult{}, errors.New("upload init store is not configured")
	}
	fileName, ext := NormalizeUploadArchiveName(in.FileName)
	if fileName == "" || ext == "" {
		return UploadInitResult{}, ErrInvalidUploadArchive
	}

	idGenerator := s.IDGenerator
	if idGenerator == nil {
		idGenerator = uuid.NewString
	}
	tokenGenerator := s.TokenGenerator
	if tokenGenerator == nil {
		tokenGenerator = uuid.NewString
	}

	taskID := idGenerator()
	uploadToken := tokenGenerator()
	metadata := NormalizeMetadata(in.Metadata)
	task, err := s.Store.CreateUploadTask(ctx, UploadInitRecord{
		ID:                taskID,
		EnqueueToken:      uploadToken,
		TaskType:          "upload",
		CanonicalURL:      stringPtr("upload:" + taskID),
		SourceArchiveName: stringPtr(fileName),
		UploadTotalBytes:  maxInt64(in.FileSize, 0),
		Author:            metadata.Author,
		SeriesName:        metadata.SeriesName,
		ComicName:         metadata.ComicName,
		Summary:           metadata.Summary,
		TagsRaw:           metadata.TagsRaw,
		TagsNormalized:    metadata.TagsNormalized,
		GenresRaw:         metadata.GenresRaw,
		GenresNormalized:  metadata.GenresNormalized,
	})
	if err != nil {
		return UploadInitResult{}, err
	}

	if err := s.Store.InsertMetadataHistory(ctx, MetadataHistoryRecord{
		TaskType:   "upload",
		Author:     metadata.Author,
		SeriesName: metadata.SeriesName,
		ComicName:  metadata.ComicName,
		Summary:    metadata.Summary,
		Tags:       metadata.TagsNormalized,
		Genres:     metadata.GenresNormalized,
	}); err != nil {
		return UploadInitResult{}, err
	}

	return UploadInitResult{
		TaskID:      task.ID,
		Status:      task.Status,
		UploadToken: uploadToken,
		UploadURL:   "/api/tasks/" + url.PathEscape(strings.TrimSpace(task.ID)) + "/upload-source",
		LogsURL:     "/logs",
	}, nil
}

type UploadSourceStore interface {
	GetUploadTask(ctx context.Context, taskID string) (*UploadTaskRecord, error)
	UpdateUploadProgress(ctx context.Context, taskID string, loadedBytes, totalBytes int64) error
	MarkUploadTaskQueued(ctx context.Context, taskID, sourceArchivePath string, totalBytes int64) error
	MarkTaskFailed(ctx context.Context, taskID, message string) error
}

type UploadSourceInput struct {
	TaskID      string
	UploadToken string
	Body        io.Reader
	ContentSize int64
}

type UploadSourceResult struct {
	TaskID  string
	Status  string
	LogsURL string
}

type UploadSourceService struct {
	Store     UploadSourceStore
	Queue     TaskQueue
	UploadDir string
}

func NewUploadSourceService(store UploadSourceStore, queue TaskQueue, uploadDir string) *UploadSourceService {
	return &UploadSourceService{Store: store, Queue: queue, UploadDir: strings.TrimSpace(uploadDir)}
}

func (s *UploadSourceService) Attach(ctx context.Context, in UploadSourceInput) (UploadSourceResult, error) {
	if s == nil || s.Store == nil || s.Queue == nil {
		return UploadSourceResult{}, errors.New("upload source dependencies are not configured")
	}
	taskID := strings.TrimSpace(in.TaskID)
	if taskID == "" {
		return UploadSourceResult{}, errors.New("task id is required")
	}
	if strings.TrimSpace(in.UploadToken) == "" {
		return UploadSourceResult{}, errors.New("upload token is required")
	}
	task, err := s.Store.GetUploadTask(ctx, taskID)
	if err != nil {
		return UploadSourceResult{}, err
	}
	if task == nil {
		return UploadSourceResult{}, ErrTaskNotFound
	}
	fileName := stringValue(task.SourceArchiveName)
	_, ext := NormalizeUploadArchiveName(fileName)
	if ext == "" {
		return UploadSourceResult{}, ErrInvalidUploadArchive
	}

	targetDir := filepath.Join(s.uploadDirOrDefault(), taskID)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return UploadSourceResult{}, fmt.Errorf("create upload temp dir: %w", err)
	}
	targetPath := filepath.Join(targetDir, "source"+ext)
	file, err := os.Create(targetPath)
	if err != nil {
		return UploadSourceResult{}, fmt.Errorf("create upload target: %w", err)
	}

	var loaded int64
	totalBytes := task.UploadTotalBytes
	if totalBytes <= 0 && in.ContentSize > 0 {
		totalBytes = in.ContentSize
	}
	buffer := make([]byte, 64*1024)
	writeErr := func(err error) {
		_ = file.Close()
		_ = os.Remove(targetPath)
		_ = s.Store.MarkTaskFailed(ctx, taskID, strings.TrimSpace(err.Error()))
	}

	for {
		n, readErr := in.Body.Read(buffer)
		if n > 0 {
			if _, err := file.Write(buffer[:n]); err != nil {
				writeErr(fmt.Errorf("write upload source: %w", err))
				return UploadSourceResult{}, err
			}
			loaded += int64(n)
			if err := s.Store.UpdateUploadProgress(ctx, taskID, loaded, totalBytes); err != nil {
				writeErr(fmt.Errorf("update upload progress: %w", err))
				return UploadSourceResult{}, err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			writeErr(fmt.Errorf("read upload source: %w", readErr))
			return UploadSourceResult{}, readErr
		}
	}
	if err := file.Close(); err != nil {
		writeErr(fmt.Errorf("close upload source: %w", err))
		return UploadSourceResult{}, err
	}
	if err := s.Store.MarkUploadTaskQueued(ctx, taskID, targetPath, loaded); err != nil {
		_ = os.Remove(targetPath)
		return UploadSourceResult{}, err
	}
	if err := s.Queue.Enqueue(ctx, QueueMessage{TaskID: taskID, Token: strings.TrimSpace(in.UploadToken)}); err != nil {
		_ = s.Store.MarkTaskFailed(ctx, taskID, fmt.Sprintf("enqueue failed: %v", err))
		return UploadSourceResult{}, err
	}
	return UploadSourceResult{TaskID: taskID, Status: StatusQueued, LogsURL: "/logs"}, nil
}

func (s *UploadSourceService) uploadDirOrDefault() string {
	if strings.TrimSpace(s.UploadDir) != "" {
		return strings.TrimSpace(s.UploadDir)
	}
	return "temp_uploads"
}

func NormalizeUploadArchiveName(raw string) (string, string) {
	fileName := filepath.Base(strings.TrimSpace(raw))
	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".zip", ".rar", ".7z":
		return fileName, ext
	default:
		return "", ""
	}
}

func maxInt64(value, minimum int64) int64 {
	if value < minimum {
		return minimum
	}
	return value
}

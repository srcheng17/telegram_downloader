package httpapi

import (
	"context"

	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
)

type UploadTaskStore interface {
	CreateUploadTask(ctx context.Context, in postgres.CreateTaskInput) (postgres.TaskRecord, error)
	GetTask(ctx context.Context, taskID string) (*postgres.TaskRecord, error)
	UpdateUploadProgress(ctx context.Context, taskID string, loadedBytes, totalBytes int64) error
	MarkUploadTaskQueued(ctx context.Context, taskID, sourceArchivePath string, totalBytes int64) error
	MarkTaskFailed(ctx context.Context, taskID, message string) error
	RetryUploadTask(ctx context.Context, taskID, enqueueToken string) error
	InsertMetadataHistory(ctx context.Context, entry postgres.MetadataHistoryEntry) error
	ListMetadataHistory(ctx context.Context, limit int) ([]postgres.MetadataHistoryEntry, error)
}

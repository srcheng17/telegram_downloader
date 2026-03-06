package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
	godownloader "github.com/ryancheng/telegram-downloader/go-backend/internal/downloader"
)

type taskDownloaderStore interface {
	GetTask(ctx context.Context, taskID string) (*domain.TaskLog, error)
}

type taskDownloadService interface {
	Download(ctx context.Context, pageURL string) (domain.DownloadResult, error)
	PackageCBZ(images []domain.DownloadedImage, metadata godownloader.TaskMetadata, outputPath string) error
}

type TaskDownloaderConfig struct {
	Store        taskDownloaderStore
	Service      taskDownloadService
	DownloadRoot string
}

type TaskDownloader struct {
	store        taskDownloaderStore
	service      taskDownloadService
	downloadRoot string
}

func NewTaskDownloader(cfg TaskDownloaderConfig) *TaskDownloader {
	return &TaskDownloader{
		store:        cfg.Store,
		service:      cfg.Service,
		downloadRoot: strings.TrimSpace(cfg.DownloadRoot),
	}
}

func (d *TaskDownloader) Execute(ctx context.Context, taskID string) (string, error) {
	if d == nil {
		return "", errors.New("task downloader is required")
	}
	if d.store == nil {
		return "", errors.New("task downloader requires task store")
	}
	if d.service == nil {
		return "", errors.New("task downloader requires download service")
	}

	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", errors.New("task downloader requires task id")
	}

	task, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("load task: %w", err)
	}
	if task == nil {
		return "", fmt.Errorf("task %s not found", taskID)
	}

	pageURL := strings.TrimSpace(task.URL)
	if pageURL == "" {
		return "", fmt.Errorf("task %s has empty url", taskID)
	}

	result, err := d.service.Download(ctx, pageURL)
	if err != nil {
		return "", err
	}

	downloadRoot := d.downloadRoot
	if downloadRoot == "" {
		downloadRoot = "downloaded_images"
	}

	if err := os.MkdirAll(downloadRoot, 0o755); err != nil {
		return "", fmt.Errorf("create download output root: %w", err)
	}

	outputPath := filepath.Join(downloadRoot, taskID+".cbz")
	if err := d.service.PackageCBZ(result.Images, godownloader.TaskMetadataFromTask(*task), outputPath); err != nil {
		return "", err
	}
	return outputPath, nil
}

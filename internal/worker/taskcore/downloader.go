package taskcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	taskarchive "github.com/ryancheng/telegram-downloader/internal/archive"
	"github.com/ryancheng/telegram-downloader/internal/domain"
	domainnaming "github.com/ryancheng/telegram-downloader/internal/domain/naming"
	taskcoredomain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
	godownloader "github.com/ryancheng/telegram-downloader/internal/downloader"
)

const defaultDownloadRoot = "downloaded_images"

type TaskViewService interface {
	GetTask(ctx context.Context, taskID string) (*app.TaskView, error)
}

type DownloadService interface {
	Download(ctx context.Context, pageURL string) (domain.DownloadResult, error)
	PackageCBZ(images []domain.DownloadedImage, metadata godownloader.TaskMetadata, outputPath string) error
}

type ArchiveExtractor interface {
	Extract(ctx context.Context, archivePath string) ([]taskarchive.ExtractedImage, error)
}

type TaskDownloaderConfig struct {
	Tasks        TaskViewService
	Service      DownloadService
	Extractor    ArchiveExtractor
	DownloadRoot string
	Now          func() time.Time
}

type TaskDownloader struct {
	tasks        TaskViewService
	service      DownloadService
	extractor    ArchiveExtractor
	downloadRoot string
	now          func() time.Time
}

func NewTaskDownloader(cfg TaskDownloaderConfig) *TaskDownloader {
	return &TaskDownloader{
		tasks:        cfg.Tasks,
		service:      cfg.Service,
		extractor:    cfg.Extractor,
		downloadRoot: strings.TrimSpace(cfg.DownloadRoot),
		now:          cfg.Now,
	}
}

func (d *TaskDownloader) Execute(ctx context.Context, taskID string) (string, error) {
	if d == nil {
		return "", errors.New("task core downloader is required")
	}
	if d.tasks == nil {
		return "", errors.New("task core downloader requires task service")
	}
	if d.service == nil {
		return "", errors.New("task core downloader requires download service")
	}

	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", errors.New("task core downloader requires task id")
	}

	view, err := d.tasks.GetTask(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("load task core task: %w", err)
	}
	if view == nil {
		return "", fmt.Errorf("task core task %s not found", taskID)
	}

	metadata := metadataFromInput(view.Input)
	images, err := d.loadImages(ctx, *view)
	if err != nil {
		return "", err
	}

	downloadRoot := d.downloadRoot
	if downloadRoot == "" {
		downloadRoot = defaultDownloadRoot
	}
	if err := os.MkdirAll(downloadRoot, 0o755); err != nil {
		return "", fmt.Errorf("create task core download output root: %w", err)
	}

	outputPath := filepath.Join(downloadRoot, buildTaskCoreDownloadFilename(metadata, d.clock().Unix()))
	if err := d.service.PackageCBZ(images, metadata, outputPath); err != nil {
		return "", err
	}
	return outputPath, nil
}

func (d *TaskDownloader) loadImages(ctx context.Context, view app.TaskView) ([]domain.DownloadedImage, error) {
	pageURL := firstNonEmpty(view.Input.CanonicalURL, view.Input.URL)
	if view.Task.Kind == taskcoredomain.KindUpload || pageURL == "" {
		source := strings.TrimSpace(view.Input.SourceArchivePath)
		if source == "" {
			return nil, fmt.Errorf("task core task %s has empty source archive path", view.Task.ID)
		}
		extractor := d.extractor
		if extractor == nil {
			extractor = taskarchive.NewExtractor(taskarchive.ExtractorConfig{})
		}
		extractedImages, err := extractor.Extract(ctx, source)
		if err != nil {
			return nil, err
		}
		images := make([]domain.DownloadedImage, 0, len(extractedImages))
		for _, image := range extractedImages {
			images = append(images, domain.DownloadedImage{
				URL:         image.Name,
				ContentType: image.ContentType,
				Data:        image.Data,
			})
		}
		return images, nil
	}

	result, err := d.service.Download(ctx, pageURL)
	if err != nil {
		return nil, err
	}
	return result.Images, nil
}

func (d *TaskDownloader) clock() time.Time {
	if d != nil && d.now != nil {
		return d.now()
	}
	return time.Now()
}

func metadataFromInput(input app.Input) godownloader.TaskMetadata {
	metadata := input.Metadata
	return godownloader.TaskMetadata{
		Writer:  metadataValue(metadata, "author"),
		Series:  metadataValue(metadata, "series_name"),
		Number:  metadataValue(metadata, "series_number"),
		Title:   metadataValue(metadata, "comic_name"),
		Summary: metadataValue(metadata, "summary"),
		Tags: firstNonEmpty(
			metadataValue(metadata, "tags_normalized"),
			metadataValue(metadata, "tags"),
		),
		Genre: firstNonEmpty(
			metadataValue(metadata, "genres_normalized"),
			metadataValue(metadata, "genres"),
		),
	}
}

func metadataValue(metadata map[string]string, key string) string {
	if metadata == nil {
		return ""
	}
	return strings.TrimSpace(metadata[key])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func buildTaskCoreDownloadFilename(metadata godownloader.TaskMetadata, unixTimestamp int64) string {
	return domainnaming.BuildCBZFileName(domainnaming.CBZFileNameInput{
		Author:    metadata.Writer,
		Series:    metadata.Series,
		Title:     metadata.Title,
		Timestamp: unixTimestamp,
	})
}

package taskcore

import (
	"context"
	"errors"
	"fmt"
	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
	"github.com/ryancheng/telegram-downloader/internal/config"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	ReportProgress(ctx context.Context, taskID string, workerID string, generation int64, progress taskcoredomain.Progress) error
}

type ArchiveExtractor interface {
	Extract(ctx context.Context, archivePath string) ([]taskarchive.ExtractedImage, error)
}

type TaskDownloaderConfig struct {
	Tasks        TaskViewService
	Service      *godownloader.Service
	Extractor    ArchiveExtractor
	DownloadRoot string
	Now          func() time.Time
}

type TaskDownloader struct {
	tasks        TaskViewService
	service      *godownloader.Service
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

func (d *TaskDownloader) Execute(ctx context.Context, task app.Task) (outputPath string, err error) {
	if d == nil || d.tasks == nil || d.service == nil {
		return "", errors.New("task core downloader requires task and download services")
	}
	taskID := strings.TrimSpace(task.ID)
	if taskID == "" || filepath.Base(taskID) != taskID || taskID == "." || taskID == ".." || task.Generation <= 0 || task.LeaseOwner == "" {
		return "", errors.New("task core downloader requires a claimed execution")
	}
	view, err := d.tasks.GetTask(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("load task core task: %w", err)
	}
	if view == nil {
		return "", app.ErrNotFound
	}
	if view.Task.Generation != task.Generation || view.Task.LeaseOwner != task.LeaseOwner || view.Task.Status != taskcoredomain.StatusRunning {
		return "", app.ErrConflict
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var progressMu sync.Mutex
	var progressErr error
	report := func(progress taskcoredomain.Progress) error {
		progressMu.Lock()
		defer progressMu.Unlock()
		if progressErr != nil {
			return progressErr
		}
		progressErr = d.tasks.ReportProgress(runCtx, taskID, task.LeaseOwner, task.Generation, progress)
		if progressErr != nil {
			cancel()
		}
		return progressErr
	}
	service := *d.service
	if view.Input.RuntimeSettings != nil {
		settings := config.NormalizeSettingsSnapshot(*view.Input.RuntimeSettings)
		client := http.Client{}
		if service.HTTPClient != nil {
			client = *service.HTTPClient
		}
		client.Timeout = time.Duration(settings.Timeout) * time.Second
		service.HTTPClient = &client
		service.DownloadRetries = settings.Retries
		service.ImageConcurrency = settings.ImageConcurrency
	}
	service.OnTotalImagesDiscovered = func(total int) {
		_ = report(taskcoredomain.NewProgress(taskcoredomain.PhaseDownloading, 0, int64(total), taskcoredomain.UnitImages, "下载图片"))
	}
	service.OnImageDownloaded = func(current, total int) {
		_ = report(taskcoredomain.NewProgress(taskcoredomain.PhaseDownloading, int64(current), int64(total), taskcoredomain.UnitImages, "下载图片"))
	}
	if err := report(taskcoredomain.NewProgress(taskcoredomain.PhasePreparing, 0, 0, taskcoredomain.UnitNone, "准备处理")); err != nil {
		return "", err
	}
	metadata := metadataFromInput(view.Input)
	images, err := d.loadImages(runCtx, *view, &service)
	progressMu.Lock()
	capturedProgressErr := progressErr
	progressMu.Unlock()
	if capturedProgressErr != nil {
		return "", capturedProgressErr
	}
	if err != nil {
		return "", err
	}
	if err := report(taskcoredomain.NewProgress(taskcoredomain.PhasePackaging, 0, int64(len(images)), taskcoredomain.UnitImages, "打包 CBZ")); err != nil {
		return "", err
	}
	root := d.downloadRoot
	if root == "" {
		root = defaultDownloadRoot
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, taskID, strconv.FormatInt(task.Generation, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create task output directory: %w", err)
	}
	path := filepath.Join(dir, buildTaskCoreDownloadFilename(metadata, d.clock().Unix()))
	defer func() {
		if err != nil {
			_ = os.Remove(path)
			_ = os.Remove(dir)
		}
	}()
	if err := service.PackageCBZContext(runCtx, images, metadata, path); err != nil {
		return "", err
	}
	if err := runCtx.Err(); err != nil {
		return "", err
	}
	return path, nil
}

func (d *TaskDownloader) CleanupSource(ctx context.Context, task app.Task) error {
	view, err := d.tasks.GetTask(ctx, task.ID)
	if err != nil {
		return err
	}
	if view == nil || view.Task.Generation != task.Generation || view.Task.Status != taskcoredomain.StatusSucceeded || view.Task.Kind != taskcoredomain.KindUpload {
		return nil
	}
	root := strings.TrimSpace(os.Getenv("TEMP_PATH"))
	if root == "" {
		root = "temp_downloads"
	}
	opened, err := apptasks.NewArtifactAccess(apptasks.ArtifactAccessConfig{DownloadRoot: root}).Open(view.Input.SourceArchivePath)
	if errors.Is(err, apptasks.ErrArtifactUnavailable) {
		return nil
	}
	if err != nil {
		return err
	}
	path := opened.File.Name()
	if err := opened.Close(); err != nil {
		return err
	}
	return os.Remove(path)
}

func (d *TaskDownloader) loadImages(ctx context.Context, view app.TaskView, service *godownloader.Service) ([]domain.DownloadedImage, error) {
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

	result, err := service.Download(ctx, pageURL)
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

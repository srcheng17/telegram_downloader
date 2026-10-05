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
	"github.com/ryancheng/telegram-downloader/internal/app/telegram"
	taskarchive "github.com/ryancheng/telegram-downloader/internal/archive"
	"github.com/ryancheng/telegram-downloader/internal/domain"
	metadomain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
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

type MetadataRegistry interface {
	Get(context.Context, string) (metadomain.Registry, error)
}
type ExecutionOutput struct {
	MetadataWarnings          []metadomain.Warning
	MetadataProfile           string
	Path                      string
	EffectiveMetadataDocument *metadomain.Document
	RetentionManifest         *taskcoredomain.RetentionManifest
}

type TaskDownloaderConfig struct {
	Registry        MetadataRegistry
	RetentionRoot   string
	RecordRetention func(context.Context, app.Task, taskcoredomain.RetentionManifest) error
	Telegram        TelegramDownloader
	Tasks           TaskViewService
	Service         *godownloader.Service
	Extractor       ArchiveExtractor
	DownloadRoot    string
	Now             func() time.Time
}

type TaskDownloader struct {
	registry        MetadataRegistry
	retentionRoot   string
	recordRetention func(context.Context, app.Task, taskcoredomain.RetentionManifest) error
	telegram        TelegramDownloader
	tasks           TaskViewService
	service         *godownloader.Service
	extractor       ArchiveExtractor
	downloadRoot    string
	now             func() time.Time
}

func NewTaskDownloader(cfg TaskDownloaderConfig) *TaskDownloader {
	return &TaskDownloader{registry: cfg.Registry, retentionRoot: cfg.RetentionRoot, recordRetention: cfg.RecordRetention,
		telegram:     cfg.Telegram,
		tasks:        cfg.Tasks,
		service:      cfg.Service,
		extractor:    cfg.Extractor,
		downloadRoot: strings.TrimSpace(cfg.DownloadRoot),
		now:          cfg.Now,
	}
}

type TelegramDownloader interface {
	Download(context.Context, telegram.Input, string) (telegram.Source, error)
}

func (d *TaskDownloader) Execute(ctx context.Context, task app.Task) (string, error) {
	result, err := d.ExecuteWithMetadata(ctx, task)
	return result.Path, err
}
func (d *TaskDownloader) privateDir(task app.Task) string {
	return filepath.Join(d.retentionRoot, task.ID, strconv.FormatInt(task.Generation, 10))
}
func (d *TaskDownloader) retain(ctx context.Context, task app.Task, bundle *taskarchive.MetadataBundle) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	manifest, err := taskarchive.RetainMetadataBundle(ctx, bundle, d.privateDir(task))
	if err != nil {
		return err
	}
	if d.recordRetention == nil {
		return errors.New("source retention repository unavailable")
	}
	return d.recordRetention(ctx, task, manifest)
}
func (d *TaskDownloader) ExecuteWithMetadata(ctx context.Context, task app.Task) (output ExecutionOutput, err error) {
	if d == nil || d.tasks == nil || d.service == nil {
		return ExecutionOutput{}, errors.New("task core downloader requires task and download services")
	}
	taskID := strings.TrimSpace(task.ID)
	if taskID == "" || filepath.Base(taskID) != taskID || taskID == "." || taskID == ".." || task.Generation <= 0 || task.LeaseOwner == "" {
		return ExecutionOutput{}, errors.New("task core downloader requires a claimed execution")
	}
	view, err := d.tasks.GetTask(ctx, taskID)
	if err != nil {
		return ExecutionOutput{}, fmt.Errorf("load task core task: %w", err)
	}
	if view == nil {
		return ExecutionOutput{}, app.ErrNotFound
	}
	if view.Task.Generation != task.Generation || view.Task.LeaseOwner != task.LeaseOwner || view.Task.Status != taskcoredomain.StatusRunning {
		return ExecutionOutput{}, app.ErrConflict
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
		return ExecutionOutput{}, err
	}
	metadata := metadataFromInput(view.Input)
	images, bundle, err := d.loadImages(runCtx, *view, &service)
	sourceRetained := false
	retainBundle := func() error {
		retainErr := d.retain(runCtx, task, bundle)
		if retainErr == nil {
			sourceRetained = true
		}
		return retainErr
	}
	defer func() {
		if task.Kind == taskcoredomain.KindTelegram && sourceRetained && bundle != nil {
			_ = os.Remove(bundle.SourcePath)
			_ = os.Remove(filepath.Dir(bundle.SourcePath))
		}
	}()
	progressMu.Lock()
	capturedProgressErr := progressErr
	progressMu.Unlock()
	if capturedProgressErr != nil {
		if bundle != nil && d.registry != nil {
			if retainErr := retainBundle(); retainErr != nil {
				return ExecutionOutput{}, retainErr
			}
		}
		return ExecutionOutput{}, capturedProgressErr
	}
	if err != nil {
		if bundle != nil && d.registry != nil {
			if retainErr := retainBundle(); retainErr != nil {
				return ExecutionOutput{}, retainErr
			}
		}
		return ExecutionOutput{}, err
	}
	if bundle != nil && d.registry != nil {
		if err := retainBundle(); err != nil {
			return ExecutionOutput{}, err
		}
	}
	if err := report(taskcoredomain.NewProgress(taskcoredomain.PhasePackaging, 0, int64(len(images)), taskcoredomain.UnitImages, "打包 CBZ")); err != nil {
		return ExecutionOutput{}, err
	}
	root := d.downloadRoot
	if root == "" {
		root = defaultDownloadRoot
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return ExecutionOutput{}, err
	}
	dir := filepath.Join(root, taskID, strconv.FormatInt(task.Generation, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ExecutionOutput{}, fmt.Errorf("create task output directory: %w", err)
	}
	path := filepath.Join(dir, buildTaskCoreDownloadFilename(metadata, d.clock().Unix()))
	publishedByThisExecution := false
	defer func() {
		if err != nil && publishedByThisExecution {
			_ = os.Remove(path)
			_ = os.Remove(dir)
		}
	}()
	if d.registry != nil {
		if view.Input.MetadataDocument == nil {
			return ExecutionOutput{}, app.ErrInvalidInput
		}
		registry, loadErr := d.registry.Get(runCtx, view.Input.MetadataDocument.DefinitionsVersion)
		if loadErr != nil {
			return ExecutionOutput{}, loadErr
		}
		local := godownloader.MetadataLocalImages(images)
		if bundle != nil {
			for i := range local {
				local[i].Name = images[i].URL
			}
		}
		packed, packErr := godownloader.PackageMetadataCBZ(runCtx, godownloader.MetadataPackInput{Images: local, Bundle: bundle, Document: *view.Input.MetadataDocument, Registry: registry, OutputPath: path, PrivateDir: d.privateDir(task)})
		if packErr != nil {
			if bundle != nil {
				if retainErr := retainBundle(); retainErr != nil {
					return ExecutionOutput{}, retainErr
				}
			}
			return ExecutionOutput{}, packErr
		}
		publishedByThisExecution = true
		output.MetadataWarnings = packed.Warnings
		output.MetadataProfile = packed.Profile
		output.EffectiveMetadataDocument = &packed.EffectiveDocument
		output.RetentionManifest = &packed.RetentionManifest
		if d.recordRetention == nil {
			return ExecutionOutput{}, errors.New("source retention repository unavailable")
		}
		if err = d.recordRetention(runCtx, task, packed.RetentionManifest); err != nil {
			return ExecutionOutput{}, err
		}
	} else {
		publishedByThisExecution = true
		if err := service.PackageCBZContext(runCtx, images, metadata, path); err != nil {
			return ExecutionOutput{}, err
		}
	}
	if err := runCtx.Err(); err != nil {
		return ExecutionOutput{}, err
	}
	output.Path = path
	return output, nil
}

func (d *TaskDownloader) CleanupSource(ctx context.Context, task app.Task) error {
	view, err := d.tasks.GetTask(ctx, task.ID)
	if err != nil {
		return err
	}
	if view == nil || view.Task.Generation != task.Generation || view.Task.Status != taskcoredomain.StatusSucceeded || view.Task.Kind != taskcoredomain.KindUpload {
		return nil
	}
	if d.registry != nil && (view.Result == nil || view.Result.RetentionManifest == nil || !view.Result.RetentionManifest.SourceRetained) {
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

func (d *TaskDownloader) loadImages(ctx context.Context, view app.TaskView, service *godownloader.Service) ([]domain.DownloadedImage, *taskarchive.MetadataBundle, error) {
	pageURL := firstNonEmpty(view.Input.CanonicalURL, view.Input.URL)
	if view.Task.Kind == taskcoredomain.KindTelegram {
		if d.telegram == nil || view.Input.Telegram == nil {
			return nil, nil, telegram.Failure("telegram_unavailable")
		}
		root := d.downloadRoot
		if root == "" {
			root = defaultDownloadRoot
		}
		root, err := filepath.Abs(root)
		if err != nil {
			return nil, nil, err
		}
		dir := filepath.Join(root, view.Task.ID, strconv.FormatInt(view.Task.Generation, 10))
		if err = os.MkdirAll(dir, 0700); err != nil {
			return nil, nil, err
		}
		source, err := d.telegram.Download(ctx, *view.Input.Telegram, filepath.Join(dir, "source"))
		if err != nil {
			return nil, nil, err
		}
		view.Input.SourceArchivePath = source.Path
	}
	if view.Task.Kind == taskcoredomain.KindUpload || view.Task.Kind == taskcoredomain.KindTelegram || pageURL == "" {
		source := strings.TrimSpace(view.Input.SourceArchivePath)
		if source == "" {
			return nil, nil, fmt.Errorf("task core task %s has empty source archive path", view.Task.ID)
		}
		extractor := d.extractor
		if extractor == nil {
			extractor = taskarchive.NewExtractor(taskarchive.ExtractorConfig{})
		}
		var extractedImages []taskarchive.ExtractedImage
		var bundle *taskarchive.MetadataBundle
		var err error
		if d.registry != nil {
			full, ok := extractor.(interface {
				ExtractWithMetadata(context.Context, string) (*taskarchive.MetadataBundle, error)
			})
			if !ok {
				return nil, nil, errors.New("metadata archive reader unavailable")
			}
			bundle, err = full.ExtractWithMetadata(ctx, source)
			if bundle != nil {
				extractedImages = bundle.Images
			}
		} else {
			extractedImages, err = extractor.Extract(ctx, source)
		}
		if err != nil {
			return nil, bundle, err
		}
		images := make([]domain.DownloadedImage, 0, len(extractedImages))
		for _, image := range extractedImages {
			images = append(images, domain.DownloadedImage{
				URL:         image.Name,
				ContentType: image.ContentType,
				Data:        image.Data,
			})
		}
		return images, bundle, nil
	}

	result, err := service.Download(ctx, pageURL)
	if err != nil {
		return nil, nil, err
	}
	return result.Images, nil, nil
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

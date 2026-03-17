package worker

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/domain"
	"github.com/ryancheng/telegram-downloader/internal/downloader"
)

func TestTaskDownloaderExecuteDownloadsAndPackagesTask(t *testing.T) {
	store := &fakeTaskDownloadStore{
		task: &domain.TaskLog{
			ID:               "task-1",
			URL:              "https://telegra.ph/demo",
			Author:           stringPtr("author"),
			SeriesName:       stringPtr("series"),
			ComicName:        stringPtr("comic"),
			Summary:          stringPtr("summary"),
			TagsNormalized:   stringPtr("tag1,tag2"),
			GenresNormalized: stringPtr("genre"),
		},
	}
	service := &fakeTaskDownloadService{
		result: domain.DownloadResult{
			Images: []domain.DownloadedImage{
				{URL: "https://cdn.example/1.jpg", Data: []byte("img")},
			},
		},
	}
	outputDir := t.TempDir()
	runner := NewTaskDownloader(TaskDownloaderConfig{
		Store:        store,
		DownloadRoot: outputDir,
		Service:      service,
	})

	outputPath, err := runner.Execute(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("execute task downloader: %v", err)
	}

	if filepath.Dir(outputPath) != outputDir {
		t.Fatalf("expected output directory %q, got %q", outputDir, filepath.Dir(outputPath))
	}
	fileName := filepath.Base(outputPath)
	if matched := regexp.MustCompile(`^author_series_comic_[0-9]+\.cbz$`).MatchString(fileName); !matched {
		t.Fatalf("expected metadata based filename, got %q", fileName)
	}
	if strings.EqualFold(fileName, "task-1.cbz") {
		t.Fatalf("expected filename not to fallback to task id, got %q", fileName)
	}
	if service.downloadURL != "https://telegra.ph/demo" {
		t.Fatalf("expected download URL https://telegra.ph/demo, got %q", service.downloadURL)
	}
	if service.packagePath != outputPath {
		t.Fatalf("expected package output path %q, got %q", outputPath, service.packagePath)
	}
	if service.packageMetadata.Writer != "author" {
		t.Fatalf("expected writer author, got %q", service.packageMetadata.Writer)
	}
	if service.packageMetadata.Series != "series" {
		t.Fatalf("expected series series, got %q", service.packageMetadata.Series)
	}
	if service.packageMetadata.Title != "comic" {
		t.Fatalf("expected title comic, got %q", service.packageMetadata.Title)
	}
	if service.packageMetadata.Summary != "summary" {
		t.Fatalf("expected summary summary, got %q", service.packageMetadata.Summary)
	}
	if service.packageMetadata.Tags != "tag1,tag2" {
		t.Fatalf("expected tags tag1,tag2, got %q", service.packageMetadata.Tags)
	}
	if service.packageMetadata.Genre != "genre" {
		t.Fatalf("expected genre genre, got %q", service.packageMetadata.Genre)
	}
}

func TestTaskDownloaderExecuteReturnsErrorWhenTaskMissing(t *testing.T) {
	runner := NewTaskDownloader(TaskDownloaderConfig{
		Store:        &fakeTaskDownloadStore{},
		DownloadRoot: t.TempDir(),
		Service:      &fakeTaskDownloadService{},
	})

	_, err := runner.Execute(context.Background(), "missing-task")
	if err == nil {
		t.Fatalf("expected missing task error")
	}
}

func TestTaskDownloaderExecuteUsesPlaceholdersForMissingMetadata(t *testing.T) {
	store := &fakeTaskDownloadStore{
		task: &domain.TaskLog{
			ID:  "task-2",
			URL: "https://telegra.ph/demo-2",
		},
	}
	service := &fakeTaskDownloadService{
		result: domain.DownloadResult{
			Images: []domain.DownloadedImage{
				{URL: "https://cdn.example/2.jpg", Data: []byte("img")},
			},
		},
	}
	runner := NewTaskDownloader(TaskDownloaderConfig{
		Store:        store,
		DownloadRoot: t.TempDir(),
		Service:      service,
	})

	outputPath, err := runner.Execute(context.Background(), "task-2")
	if err != nil {
		t.Fatalf("execute task downloader: %v", err)
	}

	fileName := filepath.Base(outputPath)
	if matched := regexp.MustCompile(`^未知作者_未命名漫画_[0-9]+\.cbz$`).MatchString(fileName); !matched {
		t.Fatalf("expected placeholder based filename, got %q", fileName)
	}
}

type fakeTaskDownloadStore struct {
	task *domain.TaskLog
	err  error
}

func (f *fakeTaskDownloadStore) GetTask(_ context.Context, _ string) (*domain.TaskLog, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.task, nil
}

type fakeTaskDownloadService struct {
	result domain.DownloadResult
	err    error

	downloadURL      string
	packagePath      string
	packageMetadata  downloader.TaskMetadata
	packageErr       error
	packageImageList []domain.DownloadedImage
}

func (f *fakeTaskDownloadService) Download(_ context.Context, pageURL string) (domain.DownloadResult, error) {
	f.downloadURL = pageURL
	if f.err != nil {
		return domain.DownloadResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeTaskDownloadService) PackageCBZ(
	images []domain.DownloadedImage,
	metadata downloader.TaskMetadata,
	outputPath string,
) error {
	f.packageImageList = images
	f.packageMetadata = metadata
	f.packagePath = outputPath
	if f.packageErr != nil {
		return f.packageErr
	}
	return nil
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	copied := value
	return &copied
}

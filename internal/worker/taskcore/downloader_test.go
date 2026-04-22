package taskcore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/domain"
	godownloader "github.com/ryancheng/telegram-downloader/internal/downloader"
)

func TestTaskDownloaderUsesTaskCoreInputAndMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	tasks := &fakeTaskViewService{
		view: &app.TaskView{
			Task: app.Task{ID: "task-1"},
			Input: app.Input{
				TaskID:       "task-1",
				URL:          "https://example.invalid/raw",
				CanonicalURL: "https://example.invalid/canonical",
				Metadata: map[string]string{
					"author":            "Author",
					"series_name":       "Series",
					"comic_name":        "Title",
					"summary":           "Summary",
					"tags":              "raw-tag",
					"tags_normalized":   "normalized-tag",
					"genres":            "raw-genre",
					"genres_normalized": "normalized-genre",
				},
			},
		},
	}
	downloads := &fakeTaskCoreDownloadService{
		result: domain.DownloadResult{Images: []domain.DownloadedImage{{URL: "https://example.invalid/1.jpg", ContentType: "image/jpeg", Data: []byte("image")}}},
	}
	downloader := NewTaskDownloader(TaskDownloaderConfig{
		Tasks:        tasks,
		Service:      downloads,
		DownloadRoot: tmpDir,
		Now:          func() time.Time { return time.Unix(1700000000, 0) },
	})

	path, err := downloader.Execute(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if tasks.taskID != "task-1" {
		t.Fatalf("GetTask taskID = %q", tasks.taskID)
	}
	if downloads.downloadURL != "https://example.invalid/canonical" {
		t.Fatalf("Download URL = %q", downloads.downloadURL)
	}
	wantMetadata := godownloader.TaskMetadata{
		Writer:  "Author",
		Series:  "Series",
		Title:   "Title",
		Summary: "Summary",
		Tags:    "normalized-tag",
		Genre:   "normalized-genre",
	}
	if downloads.metadata != wantMetadata {
		t.Fatalf("metadata = %#v, want %#v", downloads.metadata, wantMetadata)
	}
	if filepath.Dir(path) != tmpDir {
		t.Fatalf("output dir = %q, want %q", filepath.Dir(path), tmpDir)
	}
	if filepath.Base(path) != "Author_Series_Title_1700000000.cbz" {
		t.Fatalf("output filename = %q", filepath.Base(path))
	}
	if downloads.outputPath != path {
		t.Fatalf("PackageCBZ output path = %q, want %q", downloads.outputPath, path)
	}
}

type fakeTaskViewService struct {
	view   *app.TaskView
	taskID string
}

func (s *fakeTaskViewService) GetTask(ctx context.Context, taskID string) (*app.TaskView, error) {
	s.taskID = taskID
	return s.view, nil
}

type fakeTaskCoreDownloadService struct {
	result      domain.DownloadResult
	downloadURL string
	metadata    godownloader.TaskMetadata
	outputPath  string
}

func (s *fakeTaskCoreDownloadService) Download(ctx context.Context, pageURL string) (domain.DownloadResult, error) {
	s.downloadURL = pageURL
	return s.result, nil
}

func (s *fakeTaskCoreDownloadService) PackageCBZ(images []domain.DownloadedImage, metadata godownloader.TaskMetadata, outputPath string) error {
	s.metadata = metadata
	s.outputPath = outputPath
	return nil
}

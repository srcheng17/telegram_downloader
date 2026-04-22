package taskcore

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/domain"
	taskcoredomain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
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

func TestTaskDownloaderExtractsUploadSourceArchive(t *testing.T) {
	tmpDir := t.TempDir()
	sourceArchive := filepath.Join(tmpDir, "source.zip")
	writeTestZip(t, sourceArchive, map[string]string{
		"002.jpg":      "second",
		"001.png":      "first",
		"notes.txt":    "ignore",
		"nested/":      "",
		"nested/3.gif": "third",
	})
	tasks := &fakeTaskViewService{
		view: &app.TaskView{
			Task: app.Task{ID: "task-upload-1", Kind: taskcoredomain.KindUpload},
			Input: app.Input{
				TaskID:            "task-upload-1",
				SourceArchivePath: sourceArchive,
				Metadata: map[string]string{
					"author":      "Upload Author",
					"series_name": "Upload Series",
					"comic_name":  "Upload Title",
				},
			},
		},
	}
	downloads := &fakeTaskCoreDownloadService{}
	downloader := NewTaskDownloader(TaskDownloaderConfig{
		Tasks:        tasks,
		Service:      downloads,
		DownloadRoot: tmpDir,
		Now:          func() time.Time { return time.Unix(1700000001, 0) },
	})

	path, err := downloader.Execute(context.Background(), "task-upload-1")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if downloads.downloadURL != "" {
		t.Fatalf("Download should not be called for upload task, got URL %q", downloads.downloadURL)
	}
	if len(downloads.images) != 3 {
		t.Fatalf("expected 3 extracted images, got %#v", downloads.images)
	}
	if downloads.images[0].URL != "001.png" || downloads.images[0].ContentType != "image/png" || string(downloads.images[0].Data) != "first" {
		t.Fatalf("unexpected first image: %#v", downloads.images[0])
	}
	if downloads.images[1].URL != "002.jpg" || downloads.images[1].ContentType != "image/jpeg" || string(downloads.images[1].Data) != "second" {
		t.Fatalf("unexpected second image: %#v", downloads.images[1])
	}
	if downloads.images[2].URL != "nested/3.gif" || downloads.images[2].ContentType != "image/gif" || string(downloads.images[2].Data) != "third" {
		t.Fatalf("unexpected third image: %#v", downloads.images[2])
	}
	if filepath.Base(path) != "Upload Author_Upload Series_Upload Title_1700000001.cbz" {
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
	images      []domain.DownloadedImage
}

func (s *fakeTaskCoreDownloadService) Download(ctx context.Context, pageURL string) (domain.DownloadResult, error) {
	s.downloadURL = pageURL
	return s.result, nil
}

func (s *fakeTaskCoreDownloadService) PackageCBZ(images []domain.DownloadedImage, metadata godownloader.TaskMetadata, outputPath string) error {
	s.metadata = metadata
	s.outputPath = outputPath
	s.images = append([]domain.DownloadedImage(nil), images...)
	return nil
}

func writeTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(file)
	for name, content := range files {
		if filepath.Base(name) == "." || name[len(name)-1:] == "/" {
			_, err = writer.Create(name)
			if err != nil {
				t.Fatalf("create zip dir %s: %v", name, err)
			}
			continue
		}
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
}

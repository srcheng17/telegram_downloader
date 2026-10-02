package taskcore

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/config"
	taskcoredomain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
	godownloader "github.com/ryancheng/telegram-downloader/internal/downloader"
)

func TestTaskDownloaderUsesSnapshotProgressAndIsolatedArtifacts(t *testing.T) {
	var active, maxActive, requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/page" {
			_, _ = io.WriteString(w, `<img src="/1.jpg"><img src="/2.jpg"><img src="/3.jpg">`)
			return
		}
		n := active.Add(1)
		defer active.Add(-1)
		for old := maxActive.Load(); n > old; old = maxActive.Load() {
			if maxActive.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		if r.URL.Path == "/1.jpg" && requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	defer server.Close()
	tasks := &fakeTaskViewService{view: &app.TaskView{
		Task:  app.Task{ID: "task-a", Kind: taskcoredomain.KindURL, Status: taskcoredomain.StatusRunning, Attempt: 1, Generation: 1, LeaseOwner: "worker-a"},
		Input: app.Input{URL: server.URL + "/page", Metadata: map[string]string{"author": "Author", "series_name": "Series", "series_number": "3", "comic_name": "Title", "tags_normalized": "tag"}, RuntimeSettings: &config.SettingsSnapshot{Timeout: 1, Retries: 1, ImageConcurrency: 1}},
	}}
	service := &godownloader.Service{HTTPClient: &http.Client{Timeout: time.Millisecond}, DownloadRetries: 0, ImageConcurrency: 4}
	root := t.TempDir()
	d := NewTaskDownloader(TaskDownloaderConfig{Tasks: tasks, Service: service, DownloadRoot: root, Now: func() time.Time { return time.Unix(1700000000, 0) }})
	first, err := d.Execute(context.Background(), tasks.view.Task)
	if err != nil {
		t.Fatal(err)
	}
	if maxActive.Load() != 1 || requests.Load() != 2 {
		t.Fatalf("snapshot concurrency=%d retry requests=%d", maxActive.Load(), requests.Load())
	}
	if service.HTTPClient.Timeout != time.Millisecond || service.ImageConcurrency != 4 || service.DownloadRetries != 0 {
		t.Fatal("shared downloader settings mutated")
	}
	if filepath.Base(first) != "Author_Series_Title_1700000000.cbz" || filepath.Dir(first) != filepath.Join(root, "task-a", "1") {
		t.Fatalf("artifact path = %s", first)
	}
	var info struct{ Writer, Series, Number, Title, Tags string }
	if err := xml.Unmarshal(readCBZEntry(t, first, "ComicInfo.xml"), &info); err != nil {
		t.Fatal(err)
	}
	if info.Writer != "Author" || info.Series != "Series" || info.Number != "3" || info.Title != "Title" || info.Tags != "tag" {
		t.Fatalf("metadata=%+v", info)
	}
	foundDownloading, foundPackaging := false, false
	for _, p := range tasks.progress {
		if p.Phase == taskcoredomain.PhaseDownloading && p.Current == 3 && p.Total == 3 {
			foundDownloading = true
		}
		if p.Phase == taskcoredomain.PhasePackaging {
			foundPackaging = true
		}
	}
	if !foundDownloading || !foundPackaging {
		t.Fatalf("missing real progress: %+v", tasks.progress)
	}
	tasks.view.Task.ID = "task-b"
	second, err := d.Execute(context.Background(), tasks.view.Task)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("separate tasks share artifact")
	}
	tasks.view.Task.Generation = 2
	third, err := d.Execute(context.Background(), tasks.view.Task)
	if err != nil {
		t.Fatal(err)
	}
	if second == third {
		t.Fatal("separate generations share artifact")
	}
}

func TestTaskDownloaderExtractsUploadSourceArchive(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.zip")
	writeTestZip(t, source, map[string]string{"002.jpg": "second", "001.png": "first", "notes.txt": "ignore", "nested/3.gif": "third"})
	tasks := &fakeTaskViewService{view: &app.TaskView{Task: app.Task{ID: "upload-a", Kind: taskcoredomain.KindUpload, Status: taskcoredomain.StatusRunning, Attempt: 1, Generation: 1, LeaseOwner: "worker-a"}, Input: app.Input{SourceArchivePath: source}}}
	d := NewTaskDownloader(TaskDownloaderConfig{Tasks: tasks, Service: &godownloader.Service{}, DownloadRoot: root})
	path, err := d.Execute(context.Background(), tasks.view.Task)
	if err != nil {
		t.Fatal(err)
	}
	if string(readCBZEntry(t, path, "1.png")) != "first" || string(readCBZEntry(t, path, "2.jpg")) != "second" || string(readCBZEntry(t, path, "3.gif")) != "third" {
		t.Fatal("upload images or order changed")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("source removed before success committed")
	}
}

func TestTaskDownloaderRejectsStaleGenerationAndCleansCanceledOutput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.zip")
	writeTestZip(t, source, map[string]string{"1.jpg": "image"})
	tasks := &fakeTaskViewService{view: &app.TaskView{Task: app.Task{ID: "upload-a", Kind: taskcoredomain.KindUpload, Status: taskcoredomain.StatusRunning, Attempt: 1, Generation: 2, LeaseOwner: "worker-a"}, Input: app.Input{SourceArchivePath: source}}}
	d := NewTaskDownloader(TaskDownloaderConfig{Tasks: tasks, Service: &godownloader.Service{}, DownloadRoot: root})
	stale := tasks.view.Task
	stale.Generation = 1
	if _, err := d.Execute(context.Background(), stale); err != app.ErrConflict {
		t.Fatalf("stale err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	tasks.onProgress = func(p taskcoredomain.Progress) {
		if p.Phase == taskcoredomain.PhasePackaging {
			cancel()
		}
	}
	if _, err := d.Execute(ctx, tasks.view.Task); err != context.Canceled {
		t.Fatalf("cancel err=%v", err)
	}
	matches, err := filepath.Glob(filepath.Join(root, "upload-a", "2", "*"))
	if err != nil || len(matches) > 0 {
		t.Fatalf("canceled outputs=%v err=%v", matches, err)
	}
}

type fakeTaskViewService struct {
	view       *app.TaskView
	taskID     string
	mu         sync.Mutex
	progress   []taskcoredomain.Progress
	onProgress func(taskcoredomain.Progress)
}

func (s *fakeTaskViewService) GetTask(context context.Context, taskID string) (*app.TaskView, error) {
	s.taskID = taskID
	return s.view, nil
}
func (s *fakeTaskViewService) ReportProgress(_ context.Context, _ string, _ string, _ int64, p taskcoredomain.Progress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progress = append(s.progress, p)
	if s.onProgress != nil {
		s.onProgress(p)
	}
	return nil
}
func writeTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(file)
	for name, content := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
func readCBZEntry(t *testing.T, path, name string) []byte {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	for _, f := range z.File {
		if strings.EqualFold(f.Name, name) {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			data, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
	}
	t.Fatalf("missing %s", name)
	return nil
}

func TestCleanupSourceOnlyAfterSuccessfulCompletion(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TEMP_PATH", root)
	source := filepath.Join(root, "source.zip")
	writeTestZip(t, source, map[string]string{"1.jpg": "image"})
	task := app.Task{ID: "upload-a", Kind: taskcoredomain.KindUpload, Status: taskcoredomain.StatusCanceled, Generation: 1}
	tasks := &fakeTaskViewService{view: &app.TaskView{Task: task, Input: app.Input{SourceArchivePath: source}}}
	d := NewTaskDownloader(TaskDownloaderConfig{Tasks: tasks, Service: &godownloader.Service{}})
	if err := d.CleanupSource(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("canceled task source removed")
	}
	tasks.view.Task.Status = taskcoredomain.StatusSucceeded
	if err := d.CleanupSource(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("successful task source remains: %v", err)
	}
}

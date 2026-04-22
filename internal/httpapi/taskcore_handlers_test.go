package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

func TestTaskCoreHandlersCreateURLTask(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{createURLStatus: domain.StatusReady}
	router := newTaskCoreTestRouter(t, svc)
	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fdemo"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if svc.createdURL.URL != "https://telegra.ph/demo" {
		t.Fatalf("created URL = %#v", svc.createdURL)
	}
	if svc.createdURL.ID == "" {
		t.Fatalf("created task ID is empty")
	}
	if svc.createdTask.Status != domain.StatusReady {
		t.Fatalf("created status = %q, want READY", svc.createdTask.Status)
	}
}

func TestTaskCoreHandlersCancelTask(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{}
	router := newTaskCoreTestRouter(t, svc)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-1/cancel", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if svc.canceledID != "task-1" {
		t.Fatalf("canceled id = %q", svc.canceledID)
	}
}

func TestTaskCoreHandlersListTasks(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{views: []app.TaskView{{Task: app.Task{ID: "task-1", Status: domain.StatusFailed}}}}
	router := newTaskCoreTestRouter(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "retry") {
		t.Fatalf("body missing retry action: %s", rec.Body.String())
	}
}

func TestTaskCoreHandlersDownloadHeadRejectsUnavailableResult(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{view: &app.TaskView{Task: app.Task{ID: "task-1", Status: domain.StatusFailed}}}
	router := newTaskCoreTestRouter(t, svc)
	req := httptest.NewRequest(http.MethodHead, "/api/tasks/task-1/download", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTaskCoreHandlersUploadSourceRejectsInvalidTaskBeforeCreatingTempFile(t *testing.T) {
	uploadTempDir := t.TempDir()
	svc := &fakeTaskCoreHTTPService{
		view: &app.TaskView{Task: app.Task{ID: "task-1", Kind: domain.KindURL, Status: domain.StatusReady}},
	}
	router := newTaskCoreTestRouterWithOptions(t, svc, RouterOptions{UploadTempDir: uploadTempDir})
	req := httptest.NewRequest(http.MethodPut, "/api/tasks/task-1/upload-source?file_name=demo.zip", strings.NewReader("zip-data"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if svc.attachCalls != 0 {
		t.Fatalf("AttachUploadSource called %d times, want 0", svc.attachCalls)
	}
	assertDirEmpty(t, uploadTempDir)
}

func TestTaskCoreHandlersUploadSourceCleansTempFileWhenAttachFails(t *testing.T) {
	uploadTempDir := t.TempDir()
	svc := &fakeTaskCoreHTTPService{
		view:      &app.TaskView{Task: app.Task{ID: "task-1", Kind: domain.KindUpload, Status: domain.StatusCreated}},
		attachErr: app.ErrConflict,
	}
	router := newTaskCoreTestRouterWithOptions(t, svc, RouterOptions{UploadTempDir: uploadTempDir})
	req := httptest.NewRequest(http.MethodPut, "/api/tasks/task-1/upload-source?file_name=demo.zip", strings.NewReader("zip-data"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if svc.attachCalls != 1 {
		t.Fatalf("AttachUploadSource called %d times, want 1", svc.attachCalls)
	}
	assertDirEmpty(t, uploadTempDir)
}

func TestTaskCoreHandlersDownloadHeadRejectsMissingArtifact(t *testing.T) {
	downloadRoot := t.TempDir()
	t.Setenv("DOWNLOAD_PATH", downloadRoot)
	missingPath := filepath.Join(downloadRoot, "missing.cbz")
	svc := &fakeTaskCoreHTTPService{view: &app.TaskView{
		Task:   app.Task{ID: "task-1", Status: domain.StatusSucceeded},
		Result: &app.Result{TaskID: "task-1", ArtifactPath: missingPath, ArtifactName: "missing.cbz"},
	}}
	router := newTaskCoreTestRouter(t, svc)
	req := httptest.NewRequest(http.MethodHead, "/api/tasks/task-1/download", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTaskCoreHandlersDownloadHeadRejectsUnsafeArtifactPath(t *testing.T) {
	downloadRoot := t.TempDir()
	t.Setenv("DOWNLOAD_PATH", downloadRoot)
	outsideRoot := t.TempDir()
	unsafePath := filepath.Join(outsideRoot, "unsafe.cbz")
	if err := os.WriteFile(unsafePath, []byte("cbz-data"), 0o644); err != nil {
		t.Fatalf("write unsafe artifact: %v", err)
	}
	svc := &fakeTaskCoreHTTPService{view: &app.TaskView{
		Task:   app.Task{ID: "task-1", Status: domain.StatusSucceeded},
		Result: &app.Result{TaskID: "task-1", ArtifactPath: unsafePath, ArtifactName: "unsafe.cbz"},
	}}
	router := newTaskCoreTestRouter(t, svc)
	req := httptest.NewRequest(http.MethodHead, "/api/tasks/task-1/download", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTaskCoreHandlersCopyToKomgaCopiesSafeArtifact(t *testing.T) {
	downloadRoot := t.TempDir()
	t.Setenv("DOWNLOAD_PATH", downloadRoot)
	komgaRoot := t.TempDir()
	artifactPath := filepath.Join(downloadRoot, "demo.cbz")
	if err := os.WriteFile(artifactPath, []byte("cbz-data"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	svc := &fakeTaskCoreHTTPService{view: &app.TaskView{
		Task: app.Task{ID: "task-1", Status: domain.StatusSucceeded},
		Input: app.Input{Metadata: map[string]string{
			"series_name": "系列A",
		}},
		Result: &app.Result{TaskID: "task-1", ArtifactPath: artifactPath, ArtifactName: "demo.cbz"},
	}}
	router := newTaskCoreTestRouterWithOptions(t, svc, RouterOptions{KomgaRootDir: komgaRoot})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-1/copy-to-komga", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	targetPath := filepath.Join(komgaRoot, "系列A", "demo.cbz")
	if content, err := os.ReadFile(targetPath); err != nil {
		t.Fatalf("read copied artifact: %v", err)
	} else if string(content) != "cbz-data" {
		t.Fatalf("copied content = %q, want cbz-data", string(content))
	}
	if !strings.Contains(rec.Body.String(), `"target_path"`) {
		t.Fatalf("response missing target_path: %s", rec.Body.String())
	}
}

func newTaskCoreTestRouter(t *testing.T, svc *fakeTaskCoreHTTPService) http.Handler {
	t.Helper()
	return newTaskCoreTestRouterWithOptions(t, svc, RouterOptions{})
}

func newTaskCoreTestRouterWithOptions(t *testing.T, svc *fakeTaskCoreHTTPService, options RouterOptions) http.Handler {
	t.Helper()
	if options.KomgaRootDir == "" {
		options.KomgaRootDir = t.TempDir()
	}
	if options.UploadTempDir == "" {
		options.UploadTempDir = t.TempDir()
	}
	options.TaskCoreService = svc
	return NewRouterWithOptions(&fakeTaskReader{}, RouterOptions{
		TaskCoreService: options.TaskCoreService,
		KomgaRootDir:    options.KomgaRootDir,
		UploadTempDir:   options.UploadTempDir,
	})
}

type fakeTaskCoreHTTPService struct {
	createURLStatus domain.Status
	createdURL      app.CreateURLInput
	createdTask     app.Task
	canceledID      string
	retriedID       string
	views           []app.TaskView
	view            *app.TaskView
	attachErr       error
	attachCalls     int
}

func (f *fakeTaskCoreHTTPService) CreateURLTask(_ context.Context, in app.CreateURLInput) (app.Task, error) {
	f.createdURL = in
	status := f.createURLStatus
	if status == "" {
		status = domain.StatusReady
	}
	f.createdTask = app.Task{ID: in.ID, Kind: domain.KindURL, Status: status}
	f.view = &app.TaskView{
		Task:     f.createdTask,
		Input:    app.Input{TaskID: in.ID, URL: in.URL, CanonicalURL: in.CanonicalURL, Metadata: in.Metadata},
		Progress: domain.NewProgress(domain.PhasePreparing, 0, 0, domain.UnitNone, ""),
	}
	return f.createdTask, nil
}

func (f *fakeTaskCoreHTTPService) InitUploadTask(_ context.Context, in app.InitUploadInput) (app.Task, error) {
	task := app.Task{ID: in.ID, Kind: domain.KindUpload, Status: domain.StatusCreated}
	f.view = &app.TaskView{Task: task, Input: app.Input{TaskID: in.ID, Metadata: in.Metadata}}
	return task, nil
}

func (f *fakeTaskCoreHTTPService) AttachUploadSource(_ context.Context, in app.AttachUploadSourceInput) (app.Task, error) {
	f.attachCalls++
	if f.attachErr != nil {
		return app.Task{}, f.attachErr
	}
	task := app.Task{ID: in.TaskID, Kind: domain.KindUpload, Status: domain.StatusReady}
	f.view = &app.TaskView{Task: task, Input: app.Input{TaskID: in.TaskID, SourceArchiveName: in.Name, SourceArchivePath: in.Path, SourceArchiveSize: in.Size}}
	return task, nil
}

func (f *fakeTaskCoreHTTPService) RequestCancel(_ context.Context, taskID string) (app.Task, error) {
	f.canceledID = taskID
	task := app.Task{ID: taskID, Status: domain.StatusCanceling}
	f.view = &app.TaskView{Task: task}
	return task, nil
}

func (f *fakeTaskCoreHTTPService) Retry(_ context.Context, taskID string) (app.Task, error) {
	f.retriedID = taskID
	task := app.Task{ID: taskID, Status: domain.StatusReady}
	f.view = &app.TaskView{Task: task}
	return task, nil
}

func (f *fakeTaskCoreHTTPService) ListTasks(_ context.Context, _ int, _ int) ([]app.TaskView, error) {
	return append([]app.TaskView(nil), f.views...), nil
}

func (f *fakeTaskCoreHTTPService) GetTask(_ context.Context, taskID string) (*app.TaskView, error) {
	if f.view == nil && f.attachErr != nil && errors.Is(f.attachErr, app.ErrNotFound) {
		return nil, app.ErrNotFound
	}
	if f.view == nil {
		return &app.TaskView{Task: app.Task{ID: taskID}}, nil
	}
	copied := *f.view
	return &copied, nil
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("temp dir contains files: %v", names)
	}
}

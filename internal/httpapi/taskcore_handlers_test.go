package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func newTaskCoreTestRouter(t *testing.T, svc *fakeTaskCoreHTTPService) http.Handler {
	t.Helper()
	return NewRouterWithOptions(&fakeTaskReader{}, RouterOptions{
		TaskCoreService: svc,
		KomgaRootDir:    t.TempDir(),
		UploadTempDir:   t.TempDir(),
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
	if f.view == nil {
		return &app.TaskView{Task: app.Task{ID: taskID}}, nil
	}
	copied := *f.view
	return &copied, nil
}

package httpapi

import (
	"context"
	"encoding/json"
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

	if rec.Code != http.StatusAccepted {
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

func TestTaskCoreHandlersLogsUseTaskCoreListTasks(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{}
	router := newTaskCoreTestRouter(t, svc)
	createReq := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fdemo"))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}

	logsReq := httptest.NewRequest(http.MethodGet, "/api/logs?page=1&per_page=25", nil)
	logsRec := httptest.NewRecorder()
	router.ServeHTTP(logsRec, logsReq)

	if logsRec.Code != http.StatusOK {
		t.Fatalf("logs status = %d body=%s", logsRec.Code, logsRec.Body.String())
	}
	var payload struct {
		Logs []struct {
			ID               string   `json:"id"`
			TaskType         string   `json:"task_type"`
			Status           string   `json:"status"`
			StatusLabel      string   `json:"status_label"`
			PhaseLabel       string   `json:"phase_label"`
			AvailableActions []string `json:"available_actions"`
			URL              string   `json:"url"`
			CanonicalURL     string   `json:"canonical_url"`
		} `json:"logs"`
		Total          int  `json:"total"`
		Page           int  `json:"page"`
		PerPage        int  `json:"per_page"`
		TotalPages     int  `json:"total_pages"`
		HasActiveTasks bool `json:"has_active_tasks"`
		Summary        struct {
			TotalTasks      int `json:"total_tasks"`
			PendingTasks    int `json:"pending_tasks"`
			ActiveTasks     int `json:"active_tasks"`
			FinishedTasks   int `json:"finished_tasks"`
			StartupRecovery struct {
				Happened bool `json:"happened"`
			} `json:"startup_recovery"`
		} `json:"summary"`
		StatusCatalog map[string]any `json:"status_catalog"`
		Filters       map[string]any `json:"filters"`
	}
	if err := json.NewDecoder(logsRec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode logs payload: %v body=%s", err, logsRec.Body.String())
	}
	if payload.Total != 1 || payload.Page != 1 || payload.PerPage != 25 || payload.TotalPages != 1 {
		t.Fatalf("unexpected pagination payload: %#v", payload)
	}
	if !payload.HasActiveTasks {
		t.Fatalf("expected taskcore active task in logs payload")
	}
	if len(payload.Logs) != 1 {
		t.Fatalf("expected one log entry, got %#v", payload.Logs)
	}
	log := payload.Logs[0]
	if log.ID == "" || log.TaskType != "url" || log.Status != "READY" || log.StatusLabel == "" || log.PhaseLabel == "" {
		t.Fatalf("unexpected taskcore log entry: %#v", log)
	}
	if log.URL != "https://telegra.ph/demo" || log.CanonicalURL != "https://telegra.ph/demo" {
		t.Fatalf("unexpected taskcore log URLs: %#v", log)
	}
	if !containsAction(log.AvailableActions, "cancel") {
		t.Fatalf("expected cancel in available_actions, got %#v", log.AvailableActions)
	}
	if payload.Summary.TotalTasks != 1 || payload.Summary.PendingTasks != 1 || payload.Summary.ActiveTasks != 1 || payload.Summary.FinishedTasks != 0 {
		t.Fatalf("unexpected taskcore summary in logs payload: %#v", payload.Summary)
	}
	if payload.Summary.StartupRecovery.Happened {
		t.Fatalf("expected startup recovery default false")
	}
	if _, ok := payload.StatusCatalog["READY"]; !ok {
		t.Fatalf("expected READY in taskcore status catalog: %#v", payload.StatusCatalog)
	}
	if payload.Filters == nil {
		t.Fatalf("expected filters payload")
	}
}

func TestTaskCoreHandlersSummaryUsesTaskCoreViews(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{views: []app.TaskView{
		{Task: app.Task{ID: "task-ready", Kind: domain.KindURL, Status: domain.StatusReady}},
		{Task: app.Task{ID: "task-success", Kind: domain.KindURL, Status: domain.StatusSucceeded}, Result: &app.Result{ArtifactPath: "/tmp/out.cbz"}},
		{Task: app.Task{ID: "task-failed", Kind: domain.KindURL, Status: domain.StatusFailed}},
	}}
	router := newTaskCoreTestRouter(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("summary status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		TotalTasks      int      `json:"total_tasks"`
		PendingTasks    int      `json:"pending_tasks"`
		SuccessTasks    int      `json:"success_tasks"`
		FailedTasks     int      `json:"failed_tasks"`
		ActiveTasks     int      `json:"active_tasks"`
		FinishedTasks   int      `json:"finished_tasks"`
		SuccessRate     *float64 `json:"success_rate"`
		StartupRecovery struct {
			Happened bool `json:"happened"`
		} `json:"startup_recovery"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode summary payload: %v", err)
	}
	if payload.TotalTasks != 3 || payload.PendingTasks != 1 || payload.SuccessTasks != 1 || payload.FailedTasks != 1 || payload.ActiveTasks != 1 || payload.FinishedTasks != 2 {
		t.Fatalf("unexpected summary payload: %#v", payload)
	}
	if payload.SuccessRate == nil || *payload.SuccessRate != 50 {
		t.Fatalf("expected success_rate=50, got %#v", payload.SuccessRate)
	}
	if payload.StartupRecovery.Happened {
		t.Fatalf("expected startup recovery default false")
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

func TestTaskCoreHandlersUploadInitURLPreservesFrontendFileNameForRawPut(t *testing.T) {
	uploadTempDir := t.TempDir()
	svc := &fakeTaskCoreHTTPService{}
	router := newTaskCoreTestRouterWithOptions(t, svc, RouterOptions{UploadTempDir: uploadTempDir})

	initReq := httptest.NewRequest(
		http.MethodPost,
		"/api/tasks/upload/init",
		strings.NewReader(`{"file_name":"demo.zip","file_size":8}`),
	)
	initReq.Header.Set("Content-Type", "application/json")
	initRec := httptest.NewRecorder()
	router.ServeHTTP(initRec, initReq)

	if initRec.Code != http.StatusAccepted {
		t.Fatalf("init status = %d body=%s", initRec.Code, initRec.Body.String())
	}
	var initPayload struct {
		UploadURL string `json:"upload_url"`
	}
	if err := json.NewDecoder(initRec.Body).Decode(&initPayload); err != nil {
		t.Fatalf("decode init payload: %v", err)
	}
	if !strings.Contains(initPayload.UploadURL, "file_name=demo.zip") {
		t.Fatalf("upload_url = %q, want file_name=demo.zip query", initPayload.UploadURL)
	}

	putReq := httptest.NewRequest(http.MethodPut, initPayload.UploadURL, strings.NewReader("zip-data"))
	putRec := httptest.NewRecorder()
	router.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusAccepted {
		t.Fatalf("put status = %d body=%s", putRec.Code, putRec.Body.String())
	}
	if svc.attached.Name != "demo.zip" {
		t.Fatalf("attached name = %q, want demo.zip", svc.attached.Name)
	}
	if filepath.Ext(svc.attached.Path) != ".zip" {
		t.Fatalf("attached path = %q, want .zip extension", svc.attached.Path)
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
	attached        app.AttachUploadSourceInput
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
	f.attached = in
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
	if len(f.views) == 0 && f.view != nil {
		return []app.TaskView{*f.view}, nil
	}
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

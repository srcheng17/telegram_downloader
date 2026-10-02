package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ryancheng/telegram-downloader/internal/config"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
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

func TestTaskCoreHandlersCreateURLTaskRecordsMetadataHistory(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{createURLStatus: domain.StatusReady}
	historyStore := &recordingTaskCoreMetadataHistoryStore{}
	router := newTaskCoreTestRouterWithOptions(t, svc, RouterOptions{UploadTaskStore: historyStore})
	req := httptest.NewRequest(
		http.MethodPost,
		"/download",
		strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fdemo&author=%E4%BD%9C%E8%80%85A&series_name=%E7%B3%BB%E5%88%97B&series_number=3&comic_name=%E6%BC%AB%E7%94%BBC&summary=%E7%AE%80%E4%BB%8BD&tags=tag1&genres=genre1"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(historyStore.inserted) != 1 {
		t.Fatalf("expected one metadata history entry, got %#v", historyStore.inserted)
	}
	entry := historyStore.inserted[0]
	if entry.TaskType != "url" {
		t.Fatalf("task_type = %q, want url", entry.TaskType)
	}
	assertOptionalHistoryValue(t, entry.URL, "https://telegra.ph/demo", "url")
	assertOptionalHistoryValue(t, entry.Author, "作者A", "author")
	assertOptionalHistoryValue(t, entry.SeriesName, "系列B", "series_name")
	assertOptionalHistoryValue(t, entry.SeriesNumber, "3", "series_number")
	assertOptionalHistoryValue(t, entry.ComicName, "漫画C", "comic_name")
	assertOptionalHistoryValue(t, entry.Summary, "简介D", "summary")
	assertOptionalHistoryValue(t, entry.Tags, "tag1", "tags")
	assertOptionalHistoryValue(t, entry.Genres, "genre1", "genres")
	if svc.createdURL.Metadata["series_number"] != "3" {
		t.Fatalf("created metadata series_number = %q, want 3", svc.createdURL.Metadata["series_number"])
	}
}

func TestTaskCoreHandlersCreateURLTaskSkipsDuplicateMetadataHistory(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{createURLStatus: domain.StatusReady}
	historyStore := &recordingTaskCoreMetadataHistoryStore{
		entries: []postgres.MetadataHistoryEntry{
			{
				TaskType:     "url",
				URL:          stringPtr("https://telegra.ph/demo"),
				Author:       stringPtr("作者A"),
				SeriesName:   stringPtr("系列B"),
				SeriesNumber: stringPtr("3"),
				ComicName:    stringPtr("漫画C"),
			},
		},
	}
	router := newTaskCoreTestRouterWithOptions(t, svc, RouterOptions{UploadTaskStore: historyStore})
	req := httptest.NewRequest(
		http.MethodPost,
		"/download",
		strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fdemo&author=%E4%BD%9C%E8%80%85A&series_name=%E7%B3%BB%E5%88%97B&series_number=3&comic_name=%E6%BC%AB%E7%94%BBC"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(historyStore.inserted) != 0 {
		t.Fatalf("expected duplicate metadata history to be skipped, inserted %#v", historyStore.inserted)
	}
}

func TestTaskCoreHandlersUploadInitRecordsMetadataHistory(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{}
	historyStore := &recordingTaskCoreMetadataHistoryStore{}
	router := newTaskCoreTestRouterWithOptions(t, svc, RouterOptions{UploadTaskStore: historyStore})
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/tasks/upload/init",
		strings.NewReader(`{"file_name":"demo.zip","author":"上传作者","series_name":"上传系列","series_number":"4","comic_name":"上传漫画","summary":"上传简介","tags":"上传标签","genres":"上传类型"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(historyStore.inserted) != 1 {
		t.Fatalf("expected one metadata history entry, got %#v", historyStore.inserted)
	}
	entry := historyStore.inserted[0]
	if entry.TaskType != "upload" {
		t.Fatalf("task_type = %q, want upload", entry.TaskType)
	}
	if entry.URL != nil {
		t.Fatalf("upload metadata history URL = %#v, want nil", entry.URL)
	}
	assertOptionalHistoryValue(t, entry.Author, "上传作者", "author")
	assertOptionalHistoryValue(t, entry.SeriesName, "上传系列", "series_name")
	assertOptionalHistoryValue(t, entry.SeriesNumber, "4", "series_number")
	assertOptionalHistoryValue(t, entry.ComicName, "上传漫画", "comic_name")
	assertOptionalHistoryValue(t, entry.Summary, "上传简介", "summary")
	assertOptionalHistoryValue(t, entry.Tags, "上传标签", "tags")
	assertOptionalHistoryValue(t, entry.Genres, "上传类型", "genres")
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
	svc := &fakeTaskCoreHTTPService{views: []app.TaskView{{Task: app.Task{ID: "task-1", Kind: domain.KindURL, Status: domain.StatusFailed}, Input: app.Input{URL: "https://telegra.ph/retry"}}}}
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
		TaskCoreService:  options.TaskCoreService,
		UploadTaskStore:  options.UploadTaskStore,
		KomgaRootDir:     options.KomgaRootDir,
		UploadTempDir:    options.UploadTempDir,
		SettingsProvider: options.SettingsProvider,
	})
}

type fakeTaskCoreHTTPService struct {
	createResult    *app.CreateURLResult
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

func (f *fakeTaskCoreHTTPService) CreateURLTask(_ context.Context, in app.CreateURLInput) (app.CreateURLResult, error) {
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
	if f.createResult != nil {
		f.view.Task = f.createResult.Task
		f.view.Result = f.createResult.Result
		return *f.createResult, nil
	}
	return app.CreateURLResult{Task: f.createdTask}, nil
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

type recordingTaskCoreMetadataHistoryStore struct {
	entries  []postgres.MetadataHistoryEntry
	inserted []postgres.MetadataHistoryEntry
}

func (f *recordingTaskCoreMetadataHistoryStore) CreateUploadTask(context.Context, postgres.CreateTaskInput) (postgres.TaskRecord, error) {
	return postgres.TaskRecord{}, nil
}

func (f *recordingTaskCoreMetadataHistoryStore) GetTask(context.Context, string) (*postgres.TaskRecord, error) {
	return nil, nil
}

func (f *recordingTaskCoreMetadataHistoryStore) UpdateUploadProgress(context.Context, string, int64, int64) error {
	return nil
}

func (f *recordingTaskCoreMetadataHistoryStore) MarkUploadTaskQueued(context.Context, string, string, int64) error {
	return nil
}

func (f *recordingTaskCoreMetadataHistoryStore) MarkTaskFailed(context.Context, string, string) error {
	return nil
}

func (f *recordingTaskCoreMetadataHistoryStore) RetryUploadTask(context.Context, string, string) error {
	return nil
}

func (f *recordingTaskCoreMetadataHistoryStore) InsertMetadataHistory(_ context.Context, entry postgres.MetadataHistoryEntry) error {
	f.inserted = append(f.inserted, entry)
	return nil
}

func (f *recordingTaskCoreMetadataHistoryStore) ListMetadataHistory(context.Context, int) ([]postgres.MetadataHistoryEntry, error) {
	return append([]postgres.MetadataHistoryEntry(nil), f.entries...), nil
}

func assertOptionalHistoryValue(t *testing.T, actual *string, want string, field string) {
	t.Helper()
	if actual == nil || *actual != want {
		t.Fatalf("%s = %#v, want %q", field, actual, want)
	}
}

func (f *fakeTaskCoreHTTPService) QueryTasks(ctx context.Context, q app.TaskQuery) (app.TaskPage, error) {
	views, err := f.ListTasks(ctx, 0, 0)
	if err != nil {
		return app.TaskPage{}, err
	}
	selected := make([]app.TaskView, 0)
	for _, v := range views {
		if q.Status != "" && v.Task.Status != q.Status {
			continue
		}
		if q.Keyword != "" && !taskCoreViewMatchesKeyword(v, q.Keyword) {
			continue
		}
		selected = append(selected, v)
	}
	total := len(selected)
	start := q.Offset
	if start > total {
		start = total
	}
	end := start + q.Limit
	if end > total {
		end = total
	}
	return app.TaskPage{Tasks: selected[start:end], Total: total}, nil
}
func (f *fakeTaskCoreHTTPService) StatusCounts(ctx context.Context) (map[domain.Status]int, error) {
	views, err := f.ListTasks(ctx, 0, 0)
	counts := map[domain.Status]int{}
	for _, v := range views {
		counts[v.Task.Status]++
	}
	return counts, err
}

func taskCoreViewMatchesKeyword(view app.TaskView, keyword string) bool {
	needle := strings.ToLower(strings.TrimSpace(keyword))
	if needle == "" {
		return true
	}
	values := []string{
		view.Task.ID,
		string(view.Task.Kind),
		string(view.Task.Status),
		view.Task.LastError,
		view.Input.URL,
		view.Input.CanonicalURL,
		view.Input.SourceArchiveName,
		view.Input.SourceArchivePath,
	}
	if view.Result != nil {
		values = append(values, view.Result.ArtifactName, view.Result.ArtifactPath, view.Result.KomgaTargetPath)
	}
	for _, value := range view.Input.Metadata {
		values = append(values, value)
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

type fakeTaskSettings struct{ snapshot config.SettingsSnapshot }

func (f fakeTaskSettings) GetSettings(context.Context) (config.SettingsSnapshot, error) {
	return f.snapshot, nil
}
func TestTaskCoreURLSettingsForceAndConfirmation(t *testing.T) {
	settings := config.SettingsSnapshot{Timeout: 89, Retries: 4, ImageConcurrency: 7, DownloadActionMode: "browser"}
	svc := &fakeTaskCoreHTTPService{createResult: &app.CreateURLResult{Task: app.Task{ID: "existing", Kind: domain.KindURL, Status: domain.StatusSucceeded}, Reused: true, NeedsConfirmation: true, Result: &app.Result{ArtifactPath: "result.cbz"}}}
	router := newTaskCoreTestRouterWithOptions(t, svc, RouterOptions{SettingsProvider: fakeTaskSettings{settings}})
	r := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader(`{"url":"https://telegra.ph/demo","force":true}`))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, r)
	if !svc.createdURL.Force || svc.createdURL.RuntimeSettings == nil || *svc.createdURL.RuntimeSettings != settings {
		t.Fatalf("request=%#v", svc.createdURL)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["duplicate"] != true || response["needs_confirmation"] != true || response["download_url"] != "/api/tasks/existing/download" {
		t.Fatalf("response=%v", response)
	}
}

type uploadZeroReader struct{}

func (uploadZeroReader) Read(p []byte) (int, error) { return len(p), nil }
func TestTaskCoreUploadBoundaryAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		status int
	}{
		{"unsupported", `{"file_name":"demo.exe","file_size":1}`, http.StatusBadRequest},
		{"too large", `{"file_name":"demo.zip","file_size":67108865}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeTaskCoreHTTPService{}
			router := newTaskCoreTestRouter(t, svc)
			r := httptest.NewRequest(http.MethodPost, "/api/tasks/upload/init", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, r)
			if rec.Code != tc.status || svc.view != nil {
				t.Fatalf("status=%d view=%v", rec.Code, svc.view)
			}
		})
	}
	t.Run("body cap", func(t *testing.T) {
		dir := t.TempDir()
		svc := &fakeTaskCoreHTTPService{view: &app.TaskView{Task: app.Task{ID: "upload", Kind: domain.KindUpload, Status: domain.StatusCreated}}}
		router := newTaskCoreTestRouterWithOptions(t, svc, RouterOptions{UploadTempDir: dir})
		r := httptest.NewRequest(http.MethodPut, "/api/tasks/upload/upload-source?file_name=demo.zip", io.LimitReader(uploadZeroReader{}, config.MaxUploadBytes+1))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		if rec.Code != http.StatusRequestEntityTooLarge || svc.attachCalls != 0 {
			t.Fatalf("status=%d calls=%d body=%s", rec.Code, svc.attachCalls, rec.Body.String())
		}
		assertDirEmpty(t, dir)
	})
}
func TestTaskCoreArtifactRejectsSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DOWNLOAD_PATH", root)
	outside := filepath.Join(t.TempDir(), "private.cbz")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "result.cbz")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	svc := &fakeTaskCoreHTTPService{view: &app.TaskView{Task: app.Task{ID: "task", Status: domain.StatusSucceeded}, Result: &app.Result{ArtifactPath: link}}}
	router := newTaskCoreTestRouter(t, svc)
	r := httptest.NewRequest(http.MethodHead, "/api/tasks/task/download", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rec.Code)
	}
}

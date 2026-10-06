package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

type submissionHTTPFake struct {
	*fakeTaskCoreHTTPService
	key    string
	upload app.InitUploadInput
}

func (f *submissionHTTPFake) GetSubmission(_ context.Context, key string) (*app.TaskView, error) {
	f.key = key
	return f.view, nil
}
func (f *submissionHTTPFake) InitUploadTask(_ context.Context, in app.InitUploadInput) (app.Task, error) {
	f.upload = in
	return f.view.Task, nil
}
func TestSubmissionRecoveryReturnsUploadURLWithoutCreatingTask(t *testing.T) {
	for _, status := range []domain.Status{domain.StatusCreated, domain.StatusReady} {
		svc := &submissionHTTPFake{fakeTaskCoreHTTPService: &fakeTaskCoreHTTPService{view: &app.TaskView{Task: app.Task{ID: "original-task", Kind: domain.KindUpload, Status: status}, Input: app.Input{SourceArchiveName: "my archive.zip"}}}}
		router := NewRouterWithOptions(&fakeTaskReader{}, RouterOptions{TaskCoreService: svc})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks/submissions/client-stable-key-1234", nil))
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || body["task_id"] != "original-task" || svc.key != "client-stable-key-1234" || svc.createdTask.ID != "" {
			t.Fatalf("recovery=%s", rec.Body.String())
		}
		_, hasUpload := body["upload_url"]
		if hasUpload != (status == domain.StatusCreated) || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("bad replay upload eligibility/cache")
		}
	}
}
func TestSubmissionInputsReachAppWithoutDroppingFileIdentity(t *testing.T) {
	svc := &submissionHTTPFake{fakeTaskCoreHTTPService: &fakeTaskCoreHTTPService{view: &app.TaskView{Task: app.Task{ID: "original-task", Kind: domain.KindUpload, Status: domain.StatusCreated}}}}
	router := NewRouterWithOptions(&fakeTaskReader{}, RouterOptions{TaskCoreService: svc})
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/upload/init", strings.NewReader(`{"idempotency_key":"client-stable-key-1234","delivery_target":"komga","file_name":"archive.zip","file_size":12,"file_sha256":"`+strings.Repeat("a", 64)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 202 || svc.upload.IdempotencyKey != "client-stable-key-1234" || svc.upload.DeliveryTarget != "komga" || svc.upload.FileName != "archive.zip" || svc.upload.FileSize != 12 || svc.upload.FileSHA256 != strings.Repeat("a", 64) {
		t.Fatalf("upload fingerprint=%+v response=%s", svc.upload, rec.Body.String())
	}
}
func TestSubmissionConflictHasSpecificHTTPCode(t *testing.T) {
	rec := httptest.NewRecorder()
	(&taskCoreHandlers{}).writeServiceError(rec, app.ErrIdempotencyConflict)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "idempotency_conflict") {
		t.Fatal(rec.Body.String())
	}
}

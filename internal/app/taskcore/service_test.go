package taskcore

import (
	"context"
	"errors"
	"github.com/ryancheng/telegram-downloader/internal/config"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

func TestCreateURLTaskStartsReady(t *testing.T) {
	t.Parallel()

	repo := newMemoryRepo()
	svc := NewService(repo, Config{LeaseTTL: time.Minute, MaxAttempts: 3})

	got, err := svc.CreateURLTask(context.Background(), CreateURLInput{
		ID:           "11111111-1111-1111-1111-111111111111",
		URL:          "https://telegra.ph/demo",
		CanonicalURL: "https://telegra.ph/demo",
		Metadata:     map[string]string{"comic_name": "Demo"},
	})
	if err != nil {
		t.Fatalf("CreateURLTask: %v", err)
	}
	if got.Status != domain.StatusReady {
		t.Fatalf("status = %s, want READY", got.Status)
	}
	if got.Kind != domain.KindURL {
		t.Fatalf("kind = %s, want url", got.Kind)
	}
	if repo.progress[got.ID].Phase != domain.PhasePreparing {
		t.Fatalf("initial phase = %s, want preparing", repo.progress[got.ID].Phase)
	}
	if repo.progress[got.ID].Message != "准备下载" {
		t.Fatalf("initial message = %q, want 准备下载", repo.progress[got.ID].Message)
	}
	if repo.inputs[got.ID].CanonicalURL != "https://telegra.ph/demo" {
		t.Fatalf("canonical URL = %q", repo.inputs[got.ID].CanonicalURL)
	}
}

func TestUploadTaskBecomesReadyAfterSourceAttached(t *testing.T) {
	t.Parallel()

	repo := newMemoryRepo()
	svc := NewService(repo, Config{LeaseTTL: time.Minute, MaxAttempts: 3})

	created, err := svc.InitUploadTask(context.Background(), InitUploadInput{ID: "22222222-2222-2222-2222-222222222222"})
	if err != nil {
		t.Fatalf("InitUploadTask: %v", err)
	}
	if created.Status != domain.StatusCreated {
		t.Fatalf("status = %s, want CREATED", created.Status)
	}
	if repo.progress[created.ID].Phase != domain.PhaseUploading || repo.progress[created.ID].Message != "等待上传" {
		t.Fatalf("initial upload progress = %#v", repo.progress[created.ID])
	}

	ready, err := svc.AttachUploadSource(context.Background(), AttachUploadSourceInput{
		TaskID: "22222222-2222-2222-2222-222222222222",
		Name:   "source.zip",
		Path:   "/tmp/source.zip",
		Size:   123,
	})
	if err != nil {
		t.Fatalf("AttachUploadSource: %v", err)
	}
	if ready.Status != domain.StatusReady {
		t.Fatalf("status = %s, want READY", ready.Status)
	}
	input := repo.inputs[ready.ID]
	if input.SourceArchiveName != "source.zip" || input.SourceArchivePath != "/tmp/source.zip" || input.SourceArchiveSize != 123 {
		t.Fatalf("source input = %#v", input)
	}
	progress := repo.progress[ready.ID]
	if progress.Phase != domain.PhasePreparing || progress.Current != 123 || progress.Total != 123 || progress.Unit != domain.UnitBytes {
		t.Fatalf("attach progress = %#v", progress)
	}
}

func TestClaimHeartbeatAndCompleteRequireLeaseAttempt(t *testing.T) {
	t.Parallel()

	repo := newMemoryRepo()
	svc := NewService(repo, Config{LeaseTTL: time.Minute, MaxAttempts: 3})
	_, _ = svc.CreateURLTask(context.Background(), CreateURLInput{ID: "33333333-3333-3333-3333-333333333333", URL: "https://telegra.ph/demo"})

	claimed, err := svc.ClaimNext(context.Background(), "worker-a")
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed.Task == nil || claimed.Task.Status != domain.StatusRunning || claimed.Task.Attempt != 1 {
		t.Fatalf("claim = %#v, want running attempt 1", claimed.Task)
	}

	if _, err := svc.Heartbeat(context.Background(), claimed.Task.ID, "worker-a", claimed.Task.Generation); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if _, err := svc.Heartbeat(context.Background(), claimed.Task.ID, "worker-b", claimed.Task.Generation); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale worker heartbeat err = %v, want ErrConflict", err)
	}

	complete := CompleteInput{TaskID: claimed.Task.ID, WorkerID: "worker-a", Attempt: 1, Generation: 1, ArtifactPath: "/tmp/out.cbz", ArtifactName: "out.cbz", ArtifactSize: 7}
	staleWorker := complete
	staleWorker.WorkerID = "worker-b"
	if err := svc.Complete(context.Background(), staleWorker); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale worker complete err = %v, want ErrConflict", err)
	}
	staleAttempt := complete
	staleAttempt.Attempt = 2
	if err := svc.Complete(context.Background(), staleAttempt); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale attempt complete err = %v, want ErrConflict", err)
	}

	if err := svc.Complete(context.Background(), complete); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if repo.tasks[claimed.Task.ID].Status != domain.StatusSucceeded {
		t.Fatalf("status = %s, want SUCCEEDED", repo.tasks[claimed.Task.ID].Status)
	}
	if repo.results[claimed.Task.ID] == nil || repo.results[claimed.Task.ID].ArtifactName != "out.cbz" {
		t.Fatalf("result = %#v, want artifact", repo.results[claimed.Task.ID])
	}
}

func TestRequestCancelCreatedReadyAndRunning(t *testing.T) {
	t.Parallel()

	repo := newMemoryRepo()
	svc := NewService(repo, Config{LeaseTTL: time.Minute, MaxAttempts: 3})

	created, err := svc.InitUploadTask(context.Background(), InitUploadInput{ID: "44444444-4444-4444-4444-444444444441"})
	if err != nil {
		t.Fatalf("InitUploadTask: %v", err)
	}
	created, err = svc.RequestCancel(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("RequestCancel(created): %v", err)
	}
	if created.Status != domain.StatusCanceling {
		t.Fatalf("created cancel status = %s, want CANCELING", created.Status)
	}

	ready, err := svc.CreateURLTask(context.Background(), CreateURLInput{ID: "44444444-4444-4444-4444-444444444442", URL: "https://telegra.ph/ready"})
	if err != nil {
		t.Fatalf("CreateURLTask: %v", err)
	}
	ready.Task, err = svc.RequestCancel(context.Background(), ready.ID)
	if err != nil {
		t.Fatalf("RequestCancel(ready): %v", err)
	}
	if ready.Status != domain.StatusCanceling {
		t.Fatalf("ready cancel status = %s, want CANCELING", ready.Status)
	}

	_, _ = svc.CreateURLTask(context.Background(), CreateURLInput{ID: "44444444-4444-4444-4444-444444444443", URL: "https://telegra.ph/running"})
	claimed, err := svc.ClaimNext(context.Background(), "worker-c")
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	running, err := svc.RequestCancel(context.Background(), claimed.Task.ID)
	if err != nil {
		t.Fatalf("RequestCancel(running): %v", err)
	}
	if running.Status != domain.StatusCanceling {
		t.Fatalf("running cancel status = %s, want CANCELING", running.Status)
	}
	heartbeat, err := svc.Heartbeat(context.Background(), claimed.Task.ID, "worker-c", claimed.Task.Generation)
	if err != nil {
		t.Fatalf("Heartbeat after cancel request: %v", err)
	}
	if !heartbeat.CancelRequested {
		t.Fatalf("heartbeat CancelRequested = false, want true")
	}
	if err := svc.AcknowledgeCancel(context.Background(), claimed.Task.ID, "worker-c", claimed.Task.Attempt, claimed.Task.Generation); err != nil {
		t.Fatalf("AcknowledgeCancel: %v", err)
	}
	if repo.tasks[claimed.Task.ID].Status != domain.StatusCanceled {
		t.Fatalf("ack status = %s, want CANCELED", repo.tasks[claimed.Task.ID].Status)
	}
}

func TestRetryResetsFailedAndCanceledTasksToReady(t *testing.T) {
	t.Parallel()

	repo := newMemoryRepo()
	svc := NewService(repo, Config{LeaseTTL: time.Minute, MaxAttempts: 3})

	_, _ = svc.CreateURLTask(context.Background(), CreateURLInput{ID: "55555555-5555-5555-5555-555555555551", URL: "https://telegra.ph/failed"})
	failedClaim, err := svc.ClaimNext(context.Background(), "worker-f")
	if err != nil {
		t.Fatalf("ClaimNext failed task: %v", err)
	}
	if err := svc.Fail(context.Background(), FailInput{TaskID: failedClaim.Task.ID, WorkerID: "worker-f", Attempt: failedClaim.Task.Attempt, Generation: failedClaim.Task.Generation}); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	failedRetry, err := svc.Retry(context.Background(), failedClaim.Task.ID)
	if err != nil {
		t.Fatalf("Retry(failed): %v", err)
	}
	if failedRetry.Status != domain.StatusReady || failedRetry.Attempt != 0 || failedRetry.LastError != "" {
		t.Fatalf("failed retry task = %#v, want READY attempt 0 no error", failedRetry)
	}
	if repo.progress[failedRetry.ID].Message != "等待重试" {
		t.Fatalf("retry progress = %#v", repo.progress[failedRetry.ID])
	}

	_, _ = svc.CreateURLTask(context.Background(), CreateURLInput{ID: "55555555-5555-5555-5555-555555555552", URL: "https://telegra.ph/canceled"})
	canceledClaim, err := svc.ClaimNext(context.Background(), "worker-x")
	if err != nil {
		t.Fatalf("ClaimNext canceled task: %v", err)
	}
	if _, err := svc.RequestCancel(context.Background(), canceledClaim.Task.ID); err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if err := svc.AcknowledgeCancel(context.Background(), canceledClaim.Task.ID, "worker-x", canceledClaim.Task.Attempt, canceledClaim.Task.Generation); err != nil {
		t.Fatalf("AcknowledgeCancel: %v", err)
	}
	canceledRetry, err := svc.Retry(context.Background(), canceledClaim.Task.ID)
	if err != nil {
		t.Fatalf("Retry(canceled): %v", err)
	}
	if canceledRetry.Status != domain.StatusReady || canceledRetry.Attempt != 0 || canceledRetry.LeaseOwner != "" || canceledRetry.LeaseExpiresAt != nil {
		t.Fatalf("canceled retry task = %#v, want READY with lease cleared", canceledRetry)
	}
}

func TestRetryRejectsTasksWithoutExecutableSource(t *testing.T) {
	t.Parallel()
	for _, kind := range []domain.Kind{domain.KindUpload, domain.KindURL} {
		for _, status := range []domain.Status{domain.StatusFailed, domain.StatusCanceled} {
			t.Run(string(kind)+"/"+string(status), func(t *testing.T) {
				repo := newMemoryRepo()
				const taskID = "missing-source"
				repo.tasks[taskID] = Task{ID: taskID, Kind: kind, Status: status}
				repo.inputs[taskID] = Input{TaskID: taskID, URL: " ", CanonicalURL: "\t", SourceArchivePath: " "}
				_, err := NewService(repo, Config{}).Retry(context.Background(), taskID)
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("Retry without source err = %v, want ErrConflict", err)
				}
				if repo.tasks[taskID].Status != status {
					t.Fatalf("Retry changed status to %s", repo.tasks[taskID].Status)
				}
			})
		}
	}
}

func TestRetryPreservesAttachedUploadSource(t *testing.T) {
	t.Parallel()
	for _, status := range []domain.Status{domain.StatusFailed, domain.StatusCanceled} {
		t.Run(string(status), func(t *testing.T) {
			repo := newMemoryRepo()
			const taskID = "attached-source"
			repo.tasks[taskID] = Task{ID: taskID, Kind: domain.KindUpload, Status: status}
			repo.inputs[taskID] = Input{TaskID: taskID, SourceArchivePath: "/tmp/source.zip"}
			got, err := NewService(repo, Config{}).Retry(context.Background(), taskID)
			if err != nil || got.Status != domain.StatusReady {
				t.Fatalf("Retry with source = %#v, %v, want READY", got, err)
			}
			if repo.inputs[taskID].SourceArchivePath != "/tmp/source.zip" {
				t.Fatal("Retry removed attached source")
			}
		})
	}
}

func TestServiceRejectsMalformedInputs(t *testing.T) {
	t.Parallel()

	repo := newMemoryRepo()
	svc := NewService(repo, Config{LeaseTTL: time.Minute, MaxAttempts: 3})
	ctx := context.Background()

	if _, err := svc.InitUploadTask(ctx, InitUploadInput{ID: "66666666-6666-6666-6666-666666666661"}); err != nil {
		t.Fatalf("InitUploadTask: %v", err)
	}
	_, _ = svc.CreateURLTask(ctx, CreateURLInput{ID: "66666666-6666-6666-6666-666666666662", URL: "https://telegra.ph/validation"})
	claimed, err := svc.ClaimNext(ctx, "worker-v")
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	cases := []struct {
		name string
		run  func() error
	}{
		{name: "negative upload size", run: func() error {
			_, err := svc.AttachUploadSource(ctx, AttachUploadSourceInput{TaskID: "66666666-6666-6666-6666-666666666661", Name: "source.zip", Path: "/tmp/source.zip", Size: -1})
			return err
		}},
		{name: "blank cancel task id", run: func() error {
			_, err := svc.RequestCancel(ctx, " \t")
			return err
		}},
		{name: "blank retry task id", run: func() error {
			_, err := svc.Retry(ctx, "\n")
			return err
		}},
		{name: "blank heartbeat worker id", run: func() error {
			_, err := svc.Heartbeat(ctx, claimed.Task.ID, " ", claimed.Task.Generation)
			return err
		}},
		{name: "blank progress task id", run: func() error {
			return svc.ReportProgress(ctx, " ", "worker-v", 1, domain.NewProgress(domain.PhaseDownloading, 1, 2, domain.UnitImages, "downloading"))
		}},
		{name: "complete invalid attempt", run: func() error {
			return svc.Complete(ctx, CompleteInput{TaskID: claimed.Task.ID, WorkerID: "worker-v", Attempt: 0, ArtifactPath: "/tmp/out.cbz", ArtifactName: "out.cbz"})
		}},
		{name: "complete blank worker", run: func() error {
			return svc.Complete(ctx, CompleteInput{TaskID: claimed.Task.ID, WorkerID: " ", Attempt: 1, Generation: 1, ArtifactPath: "/tmp/out.cbz", ArtifactName: "out.cbz"})
		}},
		{name: "fail blank task", run: func() error {
			return svc.Fail(ctx, FailInput{TaskID: " ", WorkerID: "worker-v", Attempt: 1})
		}},
		{name: "fail invalid attempt", run: func() error {
			return svc.Fail(ctx, FailInput{TaskID: claimed.Task.ID, WorkerID: "worker-v", Attempt: 0})
		}},
		{name: "ack blank worker", run: func() error {
			return svc.AcknowledgeCancel(ctx, claimed.Task.ID, " ", 1, 1)
		}},
		{name: "ack invalid attempt", run: func() error {
			return svc.AcknowledgeCancel(ctx, claimed.Task.ID, "worker-v", 0, 1)
		}},
		{name: "get blank task", run: func() error {
			_, err := svc.GetTask(ctx, " ")
			return err
		}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.run(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestGetTaskAndListTasksDelegateWithValidationAndDefaults(t *testing.T) {
	t.Parallel()

	repo := newMemoryRepo()
	svc := NewService(repo, Config{LeaseTTL: time.Minute, MaxAttempts: 3})
	ctx := context.Background()

	first, err := svc.CreateURLTask(ctx, CreateURLInput{ID: "77777777-7777-7777-7777-777777777771", URL: "https://telegra.ph/first"})
	if err != nil {
		t.Fatalf("CreateURLTask first: %v", err)
	}
	if _, err := svc.CreateURLTask(ctx, CreateURLInput{ID: "77777777-7777-7777-7777-777777777772", URL: "https://telegra.ph/second"}); err != nil {
		t.Fatalf("CreateURLTask second: %v", err)
	}

	view, err := svc.GetTask(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if view.Task.ID != first.ID || view.Input.URL != "https://telegra.ph/first" {
		t.Fatalf("view = %#v", view)
	}

	views, err := svc.ListTasks(ctx, 0, -10)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("len(ListTasks defaulted) = %d, want 2", len(views))
	}
	if repo.lastListLimit != 20 || repo.lastListOffset != 0 {
		t.Fatalf("ListTasks delegated limit/offset = %d/%d, want 20/0", repo.lastListLimit, repo.lastListOffset)
	}
}

func TestRequestCancelDoesNotPopulateLastError(t *testing.T) {
	t.Parallel()

	repo := newMemoryRepo()
	svc := NewService(repo, Config{LeaseTTL: time.Minute, MaxAttempts: 3})

	created, err := svc.InitUploadTask(context.Background(), InitUploadInput{ID: "88888888-8888-8888-8888-888888888888"})
	if err != nil {
		t.Fatalf("InitUploadTask: %v", err)
	}
	canceled, err := svc.RequestCancel(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if canceled.LastError != "" {
		t.Fatalf("LastError = %q, want empty for normal cancellation", canceled.LastError)
	}
}

type memoryRepo struct {
	tasks          map[string]Task
	inputs         map[string]Input
	progress       map[string]domain.Progress
	results        map[string]*Result
	lastListLimit  int
	lastListOffset int
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		tasks:    map[string]Task{},
		inputs:   map[string]Input{},
		progress: map[string]domain.Progress{},
		results:  map[string]*Result{},
	}
}

func (r *memoryRepo) CreateTask(ctx context.Context, task Task, input Input, progress domain.Progress) (Task, error) {
	if _, exists := r.tasks[task.ID]; exists {
		return Task{}, ErrConflict
	}
	now := time.Now()
	task.CreatedAt = now
	task.UpdatedAt = now
	input.TaskID = task.ID
	r.tasks[task.ID] = task
	r.inputs[task.ID] = input
	r.progress[task.ID] = progress
	return task, nil
}

func (r *memoryRepo) GetTask(ctx context.Context, taskID string) (*TaskView, error) {
	task, ok := r.tasks[taskID]
	if !ok {
		return nil, ErrNotFound
	}
	view := &TaskView{Task: task, Input: r.inputs[taskID], Progress: r.progress[taskID], Result: r.results[taskID]}
	return view, nil
}

func (r *memoryRepo) Transition(ctx context.Context, taskID string, to domain.Status, actor domain.Actor, message string) (Task, error) {
	task, ok := r.tasks[taskID]
	if !ok {
		return Task{}, ErrNotFound
	}
	if err := domain.ValidateTransition(task.Status, to, actor); err != nil {
		return Task{}, ErrConflict
	}
	task.Status = to
	task.UpdatedAt = time.Now()
	if to == domain.StatusFailed && message != "" {
		task.LastError = message
	}
	r.tasks[taskID] = task
	return task, nil
}

func (r *memoryRepo) AttachUploadSource(ctx context.Context, input AttachUploadSourceInput, progress domain.Progress) (Task, error) {
	task, ok := r.tasks[input.TaskID]
	if !ok {
		return Task{}, ErrNotFound
	}
	if task.Kind != domain.KindUpload {
		return Task{}, ErrConflict
	}
	if err := domain.ValidateTransition(task.Status, domain.StatusReady, domain.ActorAPI); err != nil {
		return Task{}, ErrConflict
	}
	task.Status = domain.StatusReady
	task.UpdatedAt = time.Now()
	existing := r.inputs[input.TaskID]
	existing.TaskID = input.TaskID
	existing.SourceArchiveName = input.Name
	existing.SourceArchivePath = input.Path
	existing.SourceArchiveSize = input.Size
	r.tasks[input.TaskID] = task
	r.inputs[input.TaskID] = existing
	r.progress[input.TaskID] = progress
	return task, nil
}

func (r *memoryRepo) Retry(ctx context.Context, taskID string, progress domain.Progress) (Task, error) {
	task, ok := r.tasks[taskID]
	if !ok {
		return Task{}, ErrNotFound
	}
	if err := domain.ValidateTransition(task.Status, domain.StatusReady, domain.ActorAPI); err != nil {
		return Task{}, ErrConflict
	}
	task.Status = domain.StatusReady
	task.Attempt = 0
	task.LastError = ""
	task.LeaseOwner = ""
	task.LeaseExpiresAt = nil
	task.UpdatedAt = time.Now()
	r.tasks[taskID] = task
	r.progress[taskID] = progress
	return task, nil
}

func (r *memoryRepo) ClaimNext(ctx context.Context, workerID string, leaseTTL time.Duration) (*Task, error) {
	ids := make([]string, 0, len(r.tasks))
	for id := range r.tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		task := r.tasks[id]
		if task.Status != domain.StatusReady {
			continue
		}
		if err := domain.ValidateTransition(task.Status, domain.StatusRunning, domain.ActorWorker); err != nil {
			return nil, ErrConflict
		}
		expires := time.Now().Add(leaseTTL)
		task.Status = domain.StatusRunning
		task.Attempt++
		task.Generation++
		task.LeaseOwner = workerID
		task.LeaseExpiresAt = &expires
		task.UpdatedAt = time.Now()
		r.tasks[id] = task
		claimed := task
		return &claimed, nil
	}
	return nil, nil
}

func (r *memoryRepo) Heartbeat(ctx context.Context, taskID string, workerID string, generation int64, leaseTTL time.Duration) (HeartbeatResult, error) {
	task, ok := r.tasks[taskID]
	if !ok {
		return HeartbeatResult{}, ErrNotFound
	}
	if task.LeaseOwner != workerID || task.Generation != generation {
		return HeartbeatResult{}, ErrConflict
	}
	if task.Status == domain.StatusCanceling {
		return HeartbeatResult{CancelRequested: true}, nil
	}
	if task.Status != domain.StatusRunning {
		return HeartbeatResult{}, ErrConflict
	}
	expires := time.Now().Add(leaseTTL)
	task.LeaseExpiresAt = &expires
	task.UpdatedAt = time.Now()
	r.tasks[taskID] = task
	return HeartbeatResult{}, nil
}

func (r *memoryRepo) UpdateProgress(ctx context.Context, taskID string, workerID string, generation int64, progress domain.Progress) error {
	if _, ok := r.tasks[taskID]; !ok {
		return ErrNotFound
	}
	task := r.tasks[taskID]
	if task.LeaseOwner != workerID || task.Generation != generation || task.Status != domain.StatusRunning {
		return ErrConflict
	}
	r.progress[taskID] = progress
	return nil
}

func (r *memoryRepo) Complete(ctx context.Context, in CompleteInput) error {
	task, ok := r.tasks[in.TaskID]
	if !ok {
		return ErrNotFound
	}
	if task.LeaseOwner != in.WorkerID || task.Attempt != in.Attempt || task.Generation != in.Generation {
		return ErrConflict
	}
	if err := domain.ValidateTransition(task.Status, domain.StatusSucceeded, domain.ActorWorker); err != nil {
		return ErrConflict
	}
	task.Status = domain.StatusSucceeded
	task.LeaseOwner = ""
	task.LeaseExpiresAt = nil
	task.UpdatedAt = time.Now()
	r.tasks[in.TaskID] = task
	r.results[in.TaskID] = &Result{TaskID: in.TaskID, ArtifactPath: in.ArtifactPath, ArtifactName: in.ArtifactName, ArtifactSize: in.ArtifactSize, ArtifactKind: "cbz"}
	r.progress[in.TaskID] = domain.NewProgress(domain.PhaseDone, 1, 1, domain.UnitSteps, "完成")
	return nil
}

func (r *memoryRepo) Fail(ctx context.Context, in FailInput) error {
	task, ok := r.tasks[in.TaskID]
	if !ok {
		return ErrNotFound
	}
	if task.LeaseOwner != in.WorkerID || task.Attempt != in.Attempt || task.Generation != in.Generation {
		return ErrConflict
	}
	if err := domain.ValidateTransition(task.Status, domain.StatusFailed, domain.ActorWorker); err != nil {
		return ErrConflict
	}
	task.Status = domain.StatusFailed
	task.LastError = in.Message
	task.LeaseOwner = ""
	task.LeaseExpiresAt = nil
	task.UpdatedAt = time.Now()
	r.tasks[in.TaskID] = task
	return nil
}

func (r *memoryRepo) AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int, generation int64) error {
	task, ok := r.tasks[taskID]
	if !ok {
		return ErrNotFound
	}
	if task.LeaseOwner != workerID || task.Attempt != attempt || task.Generation != generation {
		return ErrConflict
	}
	if err := domain.ValidateTransition(task.Status, domain.StatusCanceled, domain.ActorWorker); err != nil {
		return ErrConflict
	}
	task.Status = domain.StatusCanceled
	task.LeaseOwner = ""
	task.LeaseExpiresAt = nil
	task.UpdatedAt = time.Now()
	r.tasks[taskID] = task
	return nil
}

func (r *memoryRepo) RecoverExpired(ctx context.Context, maxAttempts int) (RecoveryResult, error) {
	now := time.Now()
	var result RecoveryResult
	for id, task := range r.tasks {
		if task.Status != domain.StatusRunning || task.LeaseExpiresAt == nil || task.LeaseExpiresAt.After(now) {
			continue
		}
		if task.Attempt >= maxAttempts {
			if err := domain.ValidateTransition(task.Status, domain.StatusFailed, domain.ActorRecovery); err != nil {
				return RecoveryResult{}, ErrConflict
			}
			task.Status = domain.StatusFailed
			task.LastError = "lease expired"
			result.Failed++
		} else {
			if err := domain.ValidateTransition(task.Status, domain.StatusReady, domain.ActorRecovery); err != nil {
				return RecoveryResult{}, ErrConflict
			}
			task.Status = domain.StatusReady
			result.Requeued++
		}
		task.LeaseOwner = ""
		task.LeaseExpiresAt = nil
		task.UpdatedAt = now
		r.tasks[id] = task
	}
	return result, nil
}

func (r *memoryRepo) ListTasks(ctx context.Context, limit int, offset int) ([]TaskView, error) {
	r.lastListLimit = limit
	r.lastListOffset = offset
	ids := make([]string, 0, len(r.tasks))
	for id := range r.tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if offset > len(ids) {
		return nil, nil
	}
	end := len(ids)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	views := make([]TaskView, 0, end-offset)
	for _, id := range ids[offset:end] {
		views = append(views, TaskView{Task: r.tasks[id], Input: r.inputs[id], Progress: r.progress[id], Result: r.results[id]})
	}
	return views, nil
}

func (r *memoryRepo) CreateURLTask(ctx context.Context, task Task, input Input, progress domain.Progress, force bool) (CreateURLResult, error) {
	for id, existing := range r.tasks {
		if existing.Kind != domain.KindURL || r.inputs[id].CanonicalURL != input.CanonicalURL {
			continue
		}
		if !domain.IsTerminal(existing.Status) {
			return CreateURLResult{Task: existing, Reused: true}, nil
		}
		if existing.Status == domain.StatusSucceeded && !force && r.results[id] != nil && (input.CanReuseResult == nil || input.CanReuseResult(r.results[id])) {
			return CreateURLResult{Task: existing, Reused: true, NeedsConfirmation: true, Result: r.results[id]}, nil
		}
	}
	created, err := r.CreateTask(ctx, task, input, progress)
	return CreateURLResult{Task: created}, err
}
func (r *memoryRepo) QueryTasks(ctx context.Context, query TaskQuery) (TaskPage, error) {
	views, err := r.ListTasks(ctx, len(r.tasks), 0)
	if err != nil {
		return TaskPage{}, err
	}
	filtered := []TaskView{}
	for _, view := range views {
		if query.Status != "" && view.Task.Status != query.Status {
			continue
		}
		if query.Keyword != "" && !strings.Contains(view.Input.URL, query.Keyword) {
			continue
		}
		filtered = append(filtered, view)
	}
	page := TaskPage{Total: len(filtered), Tasks: []TaskView{}}
	if query.Offset < len(filtered) {
		end := query.Offset + query.Limit
		if end > len(filtered) {
			end = len(filtered)
		}
		page.Tasks = filtered[query.Offset:end]
	}
	return page, nil
}
func (r *memoryRepo) StatusCounts(context.Context) (map[domain.Status]int, error) {
	counts := map[domain.Status]int{}
	for _, task := range r.tasks {
		counts[task.Status]++
	}
	return counts, nil
}

func TestURLConfirmationRequiresSafeExistingArtifactAndSnapshotsAreCopied(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DOWNLOAD_PATH", root)
	path := filepath.Join(root, "existing.cbz")
	if err := os.WriteFile(path, []byte("artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	repo := newMemoryRepo()
	svc := NewService(repo, Config{})
	snapshot := config.SettingsSnapshot{Timeout: 13, Retries: 0, ImageConcurrency: 2}
	created, err := svc.CreateURLTask(context.Background(), CreateURLInput{ID: "first", URL: "https://telegra.ph/snapshot", RuntimeSettings: &snapshot})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Timeout = 100
	if repo.inputs[created.ID].RuntimeSettings.Timeout != 13 {
		t.Fatal("task snapshot aliases caller settings")
	}
	claimed, err := svc.ClaimNext(context.Background(), "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Complete(context.Background(), CompleteInput{TaskID: created.ID, WorkerID: "worker-a", Attempt: claimed.Task.Attempt, Generation: claimed.Task.Generation, ArtifactPath: path, ArtifactName: "existing.cbz", ArtifactSize: 8}); err != nil {
		t.Fatal(err)
	}
	confirmed, err := svc.CreateURLTask(context.Background(), CreateURLInput{ID: "second", URL: "https://telegra.ph/snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	if !confirmed.Reused || !confirmed.NeedsConfirmation || confirmed.ID != created.ID {
		t.Fatalf("confirmation=%+v", confirmed)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	replacement, err := svc.CreateURLTask(context.Background(), CreateURLInput{ID: "third", URL: "https://telegra.ph/snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Reused || replacement.NeedsConfirmation || replacement.ID != "third" {
		t.Fatalf("missing artifact not replaced: %+v", replacement)
	}
	forced, err := svc.CreateURLTask(context.Background(), CreateURLInput{ID: "fourth", URL: "https://telegra.ph/snapshot", Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if !forced.Reused || forced.ID != replacement.ID {
		t.Fatalf("force duplicated active task: %+v", forced)
	}
}

func TestInputSourceMatchesTaskKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		kind  domain.Kind
		input Input
		want  bool
	}{
		{name: "upload source", kind: domain.KindUpload, input: Input{SourceArchivePath: "/tmp/a.zip"}, want: true},
		{name: "upload without source", kind: domain.KindUpload, input: Input{URL: "https://telegra.ph/a"}},
		{name: "url original", kind: domain.KindURL, input: Input{URL: "https://telegra.ph/a"}, want: true},
		{name: "url canonical", kind: domain.KindURL, input: Input{CanonicalURL: "https://telegra.ph/a"}, want: true},
		{name: "url without url", kind: domain.KindURL, input: Input{SourceArchivePath: "/tmp/a.zip"}},
		{name: "unknown kind", kind: "unknown", input: Input{URL: "https://telegra.ph/a", SourceArchivePath: "/tmp/a.zip"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.input.HasSource(tc.kind); got != tc.want {
				t.Fatalf("HasSource(%s) = %v, want %v", tc.kind, got, tc.want)
			}
		})
	}
}

func TestRetryCannotPromoteCreatedUpload(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	const taskID = "created-upload"
	repo.tasks[taskID] = Task{ID: taskID, Kind: domain.KindUpload, Status: domain.StatusCreated}
	repo.inputs[taskID] = Input{TaskID: taskID, SourceArchivePath: "/tmp/source.zip"}
	_, err := NewService(repo, Config{}).Retry(context.Background(), taskID)
	if !errors.Is(err, ErrConflict) || repo.tasks[taskID].Status != domain.StatusCreated {
		t.Fatalf("Retry(created) err = %v, status = %s, want ErrConflict and CREATED", err, repo.tasks[taskID].Status)
	}
}

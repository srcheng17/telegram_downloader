# Task Core Rebuild Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the scattered task lifecycle implementation with a new Task Core that has one state machine, lease-based worker recovery, task-core tables, stable HTTP views, and tests around the critical lifecycle paths.

**Architecture:** Build a new vertical slice beside the existing v2/legacy task code, then switch HTTP and worker wiring to it. Domain owns states and action eligibility, app owns lifecycle use cases, Postgres store owns transactional persistence, worker owns execution coordination, and HTTP/frontend only present task-core views.

**Tech Stack:** Go 1.x, pgx/PostgreSQL migrations, Redis Streams where existing worker wiring still needs stream polling, chi HTTP handlers, Vite/native JS frontend tests, Playwright E2E.

---

## Scope and sequencing

This plan implements the approved spec `docs/superpowers/specs/2026-04-22-task-core-rebuild-design.md`.

Work in small commits. Do not delete old `internal/app/tasks`, `internal/httpv2`, or `internal/store/postgres/v2_repo.go` during this plan. New code uses `taskcore` packages and `task_core_*` tables. Once the new path is verified, a separate cleanup plan can remove old task code.

## File structure

Create or modify these files:

- Create `internal/domain/taskcore/status.go`: task kinds, statuses, phases, action names, transition validation, action eligibility.
- Create `internal/domain/taskcore/status_test.go`: legal/illegal transition and action tests.
- Create `internal/domain/taskcore/progress.go`: progress snapshot and phase label helpers.
- Create `internal/domain/taskcore/progress_test.go`: progress validation and label tests.
- Create `internal/store/postgres/migrations/011_task_core_schema.sql`: new `task_core_*` tables and indexes.
- Create `internal/store/postgres/taskcore/store.go`: Postgres repository, row types, command/query methods.
- Create `internal/store/postgres/taskcore/store_test.go`: transactional lifecycle tests using `TEST_DATABASE_URL` and deterministic setup/cleanup helpers.
- Create `internal/app/taskcore/types.go`: app DTOs and interfaces.
- Create `internal/app/taskcore/service.go`: create URL, init upload, attach source, cancel, retry, claim, heartbeat, progress, complete, fail, acknowledge cancel, recovery.
- Create `internal/app/taskcore/service_test.go`: app use-case tests using an in-memory repository.
- Create `internal/worker/taskcore/executor.go`: claim loop handler and lease-aware execution wrapper.
- Create `internal/worker/taskcore/executor_test.go`: heartbeat, cancel, stale attempt, and terminal reporting tests.
- Create `internal/httpapi/taskcore_presenter.go`: stable frontend view model and action list.
- Create `internal/httpapi/taskcore_handlers.go`: handlers for existing user-facing endpoints backed by app task core.
- Modify `internal/httpapi/api.go`: route the existing task endpoints through task-core handlers when configured.
- Modify `cmd/server/main.go`: construct task-core store/service and pass handlers into the legacy router options.
- Modify `cmd/worker/main.go`: construct task-core worker executor and run the new claim loop.
- Modify `frontend/src/logs/view_model.js`: read backend `available_actions`, `status_label`, and `phase_label` first.
- Modify `frontend/src/logs/task_actions.js`: button eligibility comes from `available_actions`.
- Modify or add frontend tests under `frontend/src/tests/` for action and view model changes.
- Modify `tests/e2e/specs/logs-flow.spec.js` and `tests/e2e/specs/upload-flow.spec.js`: cover cancel/retry/download/Komga paths against task-core views.
- Modify `README.md` and add `docs/runbooks/2026-04-22-task-core-rebuild.md`: document breaking task log reset and verification.

---

### Task 1: Domain task-core state machine

**Files:**
- Create: `internal/domain/taskcore/status.go`
- Create: `internal/domain/taskcore/status_test.go`
- Create: `internal/domain/taskcore/progress.go`
- Create: `internal/domain/taskcore/progress_test.go`

- [ ] **Step 1: Write failing state tests**

Create `internal/domain/taskcore/status_test.go` with these tests:

```go
package taskcore

import "testing"

func TestTransitionAllowsApprovedLifecycle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		from  Status
		to    Status
		actor Actor
	}{
		{name: "upload source ready", from: StatusCreated, to: StatusReady, actor: ActorAPI},
		{name: "created cancel requested", from: StatusCreated, to: StatusCanceling, actor: ActorAPI},
		{name: "ready claimed", from: StatusReady, to: StatusRunning, actor: ActorWorker},
		{name: "ready failed by app", from: StatusReady, to: StatusFailed, actor: ActorAPI},
		{name: "running succeeded", from: StatusRunning, to: StatusSucceeded, actor: ActorWorker},
		{name: "running failed", from: StatusRunning, to: StatusFailed, actor: ActorWorker},
		{name: "running cancel requested", from: StatusRunning, to: StatusCanceling, actor: ActorAPI},
		{name: "cancel acknowledged", from: StatusCanceling, to: StatusCanceled, actor: ActorWorker},
		{name: "failed retry", from: StatusFailed, to: StatusReady, actor: ActorAPI},
		{name: "canceled retry", from: StatusCanceled, to: StatusReady, actor: ActorAPI},
		{name: "expired lease requeue", from: StatusRunning, to: StatusReady, actor: ActorRecovery},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateTransition(tc.from, tc.to, tc.actor); err != nil {
				t.Fatalf("ValidateTransition(%s -> %s by %s) returned error: %v", tc.from, tc.to, tc.actor, err)
			}
		})
	}
}

func TestTransitionRejectsAmbiguousLifecycle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		from  Status
		to    Status
		actor Actor
	}{
		{name: "succeeded cannot retry", from: StatusSucceeded, to: StatusReady, actor: ActorAPI},
		{name: "failed cannot complete", from: StatusFailed, to: StatusSucceeded, actor: ActorWorker},
		{name: "worker cannot requeue running without recovery", from: StatusRunning, to: StatusReady, actor: ActorWorker},
		{name: "ready cannot directly succeed", from: StatusReady, to: StatusSucceeded, actor: ActorWorker},
		{name: "created cannot run", from: StatusCreated, to: StatusRunning, actor: ActorWorker},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateTransition(tc.from, tc.to, tc.actor); err == nil {
				t.Fatalf("ValidateTransition(%s -> %s by %s) succeeded, want error", tc.from, tc.to, tc.actor)
			}
		})
	}
}

func TestActionsComeFromDomainStateAndArtifacts(t *testing.T) {
	t.Parallel()

	if !CanCancel(StatusReady) || !CanCancel(StatusRunning) || !CanCancel(StatusCreated) {
		t.Fatalf("created, ready, and running tasks must be cancelable")
	}
	if CanCancel(StatusSucceeded) || CanCancel(StatusFailed) || CanCancel(StatusCanceled) {
		t.Fatalf("terminal tasks must not be cancelable")
	}
	if !CanRetry(StatusFailed) || !CanRetry(StatusCanceled) {
		t.Fatalf("failed and canceled tasks must be retryable")
	}
	if CanRetry(StatusSucceeded) || CanRetry(StatusReady) {
		t.Fatalf("succeeded and active tasks must not be retryable")
	}
	if !CanAccessResult(StatusSucceeded, true) {
		t.Fatalf("succeeded task with result must allow result access")
	}
	if CanAccessResult(StatusSucceeded, false) || CanAccessResult(StatusFailed, true) {
		t.Fatalf("result access requires succeeded status and result")
	}
}
```

- [ ] **Step 2: Run state tests to verify they fail**

Run:

```bash
go test ./internal/domain/taskcore
```

Expected: FAIL because package `internal/domain/taskcore` or symbols such as `StatusCreated` and `ValidateTransition` do not exist.

- [ ] **Step 3: Implement state machine**

Create `internal/domain/taskcore/status.go`:

```go
package taskcore

import (
	"fmt"
	"strings"
)

type Kind string

type Status string

type Actor string

type Action string

const (
	KindURL    Kind = "url"
	KindUpload Kind = "upload"
)

const (
	StatusCreated   Status = "CREATED"
	StatusReady     Status = "READY"
	StatusRunning   Status = "RUNNING"
	StatusCanceling Status = "CANCELING"
	StatusSucceeded Status = "SUCCEEDED"
	StatusFailed    Status = "FAILED"
	StatusCanceled  Status = "CANCELED"
)

const (
	ActorAPI      Actor = "api"
	ActorWorker   Actor = "worker"
	ActorRecovery Actor = "recovery"
)

const (
	ActionCancel      Action = "cancel"
	ActionRetry       Action = "retry"
	ActionDownload    Action = "download"
	ActionCopyToKomga Action = "copy_to_komga"
)

func NormalizeStatus(value string) Status {
	return Status(strings.ToUpper(strings.TrimSpace(value)))
}

func NormalizeKind(value string) Kind {
	return Kind(strings.ToLower(strings.TrimSpace(value)))
}

func IsTerminal(status Status) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

func CanCancel(status Status) bool {
	switch status {
	case StatusCreated, StatusReady, StatusRunning:
		return true
	default:
		return false
	}
}

func CanRetry(status Status) bool {
	return status == StatusFailed || status == StatusCanceled
}

func CanAccessResult(status Status, hasResult bool) bool {
	return status == StatusSucceeded && hasResult
}

func AvailableActions(status Status, hasResult bool, komgaConfigured bool) []Action {
	actions := make([]Action, 0, 4)
	if CanCancel(status) {
		actions = append(actions, ActionCancel)
	}
	if CanRetry(status) {
		actions = append(actions, ActionRetry)
	}
	if CanAccessResult(status, hasResult) {
		actions = append(actions, ActionDownload)
		if komgaConfigured {
			actions = append(actions, ActionCopyToKomga)
		}
	}
	return actions
}

func ValidateTransition(from Status, to Status, actor Actor) error {
	if transitionAllowed(from, to, actor) {
		return nil
	}
	return fmt.Errorf("invalid task transition %s -> %s by %s", from, to, actor)
}

func transitionAllowed(from Status, to Status, actor Actor) bool {
	switch from {
	case StatusCreated:
		return (to == StatusReady && actor == ActorAPI) || (to == StatusCanceling && actor == ActorAPI)
	case StatusReady:
		return (to == StatusRunning && actor == ActorWorker) ||
			(to == StatusCanceling && actor == ActorAPI) ||
			(to == StatusFailed && actor == ActorAPI)
	case StatusRunning:
		return (to == StatusSucceeded && actor == ActorWorker) ||
			(to == StatusFailed && actor == ActorWorker) ||
			(to == StatusCanceling && actor == ActorAPI) ||
			(to == StatusReady && actor == ActorRecovery)
	case StatusCanceling:
		return to == StatusCanceled && (actor == ActorWorker || actor == ActorRecovery || actor == ActorAPI)
	case StatusFailed, StatusCanceled:
		return to == StatusReady && actor == ActorAPI
	case StatusSucceeded:
		return false
	default:
		return false
	}
}
```

- [ ] **Step 4: Write failing progress tests**

Create `internal/domain/taskcore/progress_test.go`:

```go
package taskcore

import "testing"

func TestProgressLabelsAreStableForFrontend(t *testing.T) {
	t.Parallel()

	cases := []struct {
		phase Phase
		want  string
	}{
		{phase: PhaseUploading, want: "上传中"},
		{phase: PhasePreparing, want: "准备中"},
		{phase: PhaseDownloading, want: "下载中"},
		{phase: PhasePackaging, want: "打包中"},
		{phase: PhaseCopying, want: "复制中"},
		{phase: PhaseDone, want: "已完成"},
	}

	for _, tc := range cases {
		if got := tc.phase.Label(); got != tc.want {
			t.Fatalf("phase %s label = %q, want %q", tc.phase, got, tc.want)
		}
	}
}

func TestProgressSnapshotNormalizesNegativeNumbers(t *testing.T) {
	t.Parallel()

	snapshot := NewProgress(PhaseDownloading, -1, -9, UnitImages, "")
	if snapshot.Current != 0 || snapshot.Total != 0 {
		t.Fatalf("negative progress should normalize to zero, got %d/%d", snapshot.Current, snapshot.Total)
	}
}
```

- [ ] **Step 5: Implement progress model**

Create `internal/domain/taskcore/progress.go`:

```go
package taskcore

import "strings"

type Phase string

type Unit string

const (
	PhaseUploading   Phase = "uploading"
	PhasePreparing   Phase = "preparing"
	PhaseDownloading Phase = "downloading"
	PhasePackaging   Phase = "packaging"
	PhaseCopying     Phase = "copying"
	PhaseDone        Phase = "done"
)

const (
	UnitNone   Unit = "none"
	UnitBytes  Unit = "bytes"
	UnitImages Unit = "images"
	UnitFiles  Unit = "files"
	UnitSteps  Unit = "steps"
)

type Progress struct {
	Phase   Phase
	Current int64
	Total   int64
	Unit    Unit
	Message string
}

func NewProgress(phase Phase, current int64, total int64, unit Unit, message string) Progress {
	if current < 0 {
		current = 0
	}
	if total < 0 {
		total = 0
	}
	return Progress{
		Phase:   phase,
		Current: current,
		Total:   total,
		Unit:    unit,
		Message: strings.TrimSpace(message),
	}
}

func (p Phase) Label() string {
	switch p {
	case PhaseUploading:
		return "上传中"
	case PhasePreparing:
		return "准备中"
	case PhaseDownloading:
		return "下载中"
	case PhasePackaging:
		return "打包中"
	case PhaseCopying:
		return "复制中"
	case PhaseDone:
		return "已完成"
	default:
		return "处理中"
	}
}
```

- [ ] **Step 6: Run domain tests**

Run:

```bash
go test ./internal/domain/taskcore
```

Expected: PASS.

- [ ] **Step 7: Commit domain model**

```bash
git add internal/domain/taskcore/status.go internal/domain/taskcore/status_test.go internal/domain/taskcore/progress.go internal/domain/taskcore/progress_test.go
git commit -m "feat: add task core domain model"
```

---

### Task 2: Task-core schema migration

**Files:**
- Create: `internal/store/postgres/migrations/011_task_core_schema.sql`
- Modify: `internal/store/postgres/migrations/runner_test.go`

- [ ] **Step 1: Write migration listing test**

Add this test to `internal/store/postgres/migrations/runner_test.go`:

```go
func TestListMigrationVersionsIncludesTaskCoreSchema(t *testing.T) {
	versions, err := listMigrationVersions()
	if err != nil {
		t.Fatalf("list migration versions: %v", err)
	}

	found := false
	for _, version := range versions {
		if version == "011_task_core_schema.sql" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task core schema migration to be listed, got %#v", versions)
	}
}
```

- [ ] **Step 2: Run migration test to verify it fails**

Run:

```bash
go test ./internal/store/postgres/migrations -run TestListMigrationVersionsIncludesTaskCoreSchema -count=1
```

Expected: FAIL because `011_task_core_schema.sql` does not exist.

- [ ] **Step 3: Create task-core schema migration**

Create `internal/store/postgres/migrations/011_task_core_schema.sql`:

```sql
BEGIN;

CREATE TABLE IF NOT EXISTS task_core_tasks (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('url', 'upload')),
    status TEXT NOT NULL CHECK (status IN ('CREATED', 'READY', 'RUNNING', 'CANCELING', 'SUCCEEDED', 'FAILED', 'CANCELED')),
    attempt INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ready_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    cancel_requested_at TIMESTAMPTZ,
    last_error TEXT,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    heartbeat_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_task_core_tasks_status_ready_at
    ON task_core_tasks (status, ready_at ASC NULLS LAST, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_task_core_tasks_running_lease
    ON task_core_tasks (status, lease_expires_at)
    WHERE status = 'RUNNING';

CREATE INDEX IF NOT EXISTS idx_task_core_tasks_canceling_updated
    ON task_core_tasks (status, updated_at)
    WHERE status = 'CANCELING';

CREATE TABLE IF NOT EXISTS task_core_inputs (
    task_id UUID PRIMARY KEY REFERENCES task_core_tasks(id) ON DELETE CASCADE,
    url TEXT,
    canonical_url TEXT,
    source_archive_name TEXT,
    source_archive_path TEXT,
    source_archive_size BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_task_core_inputs_canonical_url
    ON task_core_inputs (canonical_url);

CREATE TABLE IF NOT EXISTS task_core_progress (
    task_id UUID PRIMARY KEY REFERENCES task_core_tasks(id) ON DELETE CASCADE,
    phase TEXT NOT NULL,
    current BIGINT NOT NULL DEFAULT 0,
    total BIGINT NOT NULL DEFAULT 0,
    unit TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS task_core_results (
    task_id UUID PRIMARY KEY REFERENCES task_core_tasks(id) ON DELETE CASCADE,
    artifact_path TEXT NOT NULL,
    artifact_name TEXT NOT NULL,
    artifact_size BIGINT NOT NULL,
    artifact_kind TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    komga_copied_at TIMESTAMPTZ,
    komga_target_path TEXT
);

CREATE TABLE IF NOT EXISTS task_core_events (
    id BIGSERIAL PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES task_core_tasks(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    from_status TEXT,
    to_status TEXT,
    actor TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_core_events_task_created_at
    ON task_core_events (task_id, created_at DESC);

COMMIT;
```

- [ ] **Step 4: Run migration tests**

Run:

```bash
go test ./internal/store/postgres/migrations -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit migration**

```bash
git add internal/store/postgres/migrations/011_task_core_schema.sql internal/store/postgres/migrations/runner_test.go
git commit -m "feat: add task core schema migration"
```

---

### Task 3: App service contract with in-memory tests

**Files:**
- Create: `internal/app/taskcore/types.go`
- Create: `internal/app/taskcore/service.go`
- Create: `internal/app/taskcore/service_test.go`

- [ ] **Step 1: Write app service tests first**

Create `internal/app/taskcore/service_test.go` with an in-memory repository. Start with these tests:

```go
package taskcore

import (
	"context"
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
	if repo.progress[got.ID].Phase != domain.PhasePreparing {
		t.Fatalf("initial phase = %s, want preparing", repo.progress[got.ID].Phase)
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

	if err := svc.Complete(context.Background(), CompleteInput{TaskID: claimed.Task.ID, WorkerID: "worker-b", Attempt: 1, ArtifactPath: "/tmp/out.cbz", ArtifactName: "out.cbz", ArtifactSize: 7}); err == nil {
		t.Fatalf("stale worker complete succeeded, want error")
	}

	if err := svc.Complete(context.Background(), CompleteInput{TaskID: claimed.Task.ID, WorkerID: "worker-a", Attempt: 1, ArtifactPath: "/tmp/out.cbz", ArtifactName: "out.cbz", ArtifactSize: 7}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if repo.tasks[claimed.Task.ID].Status != domain.StatusSucceeded {
		t.Fatalf("status = %s, want SUCCEEDED", repo.tasks[claimed.Task.ID].Status)
	}
}
```

The same file must include a small `memoryRepo` implementing the repository interface from Step 3. Keep it private to tests.

- [ ] **Step 2: Run app tests to verify they fail**

Run:

```bash
go test ./internal/app/taskcore
```

Expected: FAIL because `NewService`, DTOs, and repository contracts do not exist.

- [ ] **Step 3: Define app DTOs and repository interface**

Create `internal/app/taskcore/types.go`:

```go
package taskcore

import (
	"context"
	"time"

	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

type Config struct {
	LeaseTTL    time.Duration
	MaxAttempts int
}

type Task struct {
	ID             string
	Kind           domain.Kind
	Status         domain.Status
	Attempt        int
	LastError      string
	LeaseOwner     string
	LeaseExpiresAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Input struct {
	TaskID            string
	URL               string
	CanonicalURL      string
	SourceArchiveName string
	SourceArchivePath string
	SourceArchiveSize int64
	Metadata          map[string]string
}

type Result struct {
	TaskID          string
	ArtifactPath    string
	ArtifactName    string
	ArtifactSize    int64
	ArtifactKind    string
	KomgaTargetPath string
}

type TaskView struct {
	Task     Task
	Input    Input
	Progress domain.Progress
	Result   *Result
}

type CreateURLInput struct {
	ID           string
	URL          string
	CanonicalURL string
	Metadata     map[string]string
}

type InitUploadInput struct {
	ID       string
	Metadata map[string]string
}

type AttachUploadSourceInput struct {
	TaskID string
	Name   string
	Path   string
	Size   int64
}

type ClaimResult struct {
	Task *Task
}

type HeartbeatResult struct {
	CancelRequested bool
}

type CompleteInput struct {
	TaskID       string
	WorkerID     string
	Attempt      int
	ArtifactPath string
	ArtifactName string
	ArtifactSize int64
}

type FailInput struct {
	TaskID   string
	WorkerID string
	Attempt  int
	Message  string
}

type RecoveryResult struct {
	Requeued int
	Failed   int
	Canceled int
}

type Repository interface {
	CreateTask(ctx context.Context, task Task, input Input, progress domain.Progress) (Task, error)
	GetTask(ctx context.Context, taskID string) (*TaskView, error)
	Transition(ctx context.Context, taskID string, to domain.Status, actor domain.Actor, message string) (Task, error)
	AttachUploadSource(ctx context.Context, input AttachUploadSourceInput, progress domain.Progress) (Task, error)
	Retry(ctx context.Context, taskID string, progress domain.Progress) (Task, error)
	ClaimNext(ctx context.Context, workerID string, leaseTTL time.Duration) (*Task, error)
	Heartbeat(ctx context.Context, taskID string, workerID string, leaseTTL time.Duration) (HeartbeatResult, error)
	UpdateProgress(ctx context.Context, taskID string, progress domain.Progress) error
	Complete(ctx context.Context, in CompleteInput) error
	Fail(ctx context.Context, in FailInput) error
	AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int) error
	RecoverExpired(ctx context.Context, maxAttempts int) (RecoveryResult, error)
	ListTasks(ctx context.Context, limit int, offset int) ([]TaskView, error)
}
```

- [ ] **Step 4: Implement service orchestration**

Create `internal/app/taskcore/service.go`:

```go
package taskcore

import (
	"context"
	"errors"
	"strings"
	"time"

	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

var (
	ErrInvalidInput = errors.New("invalid task core input")
	ErrNotFound     = errors.New("task not found")
	ErrConflict     = errors.New("task state conflict")
)

type Service struct {
	repo Repository
	cfg  Config
}

func NewService(repo Repository, cfg Config) *Service {
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 30 * time.Second
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	return &Service{repo: repo, cfg: cfg}
}

func (s *Service) CreateURLTask(ctx context.Context, in CreateURLInput) (Task, error) {
	if strings.TrimSpace(in.ID) == "" || strings.TrimSpace(in.URL) == "" {
		return Task{}, ErrInvalidInput
	}
	canonical := strings.TrimSpace(in.CanonicalURL)
	if canonical == "" {
		canonical = strings.TrimSpace(in.URL)
	}
	return s.repo.CreateTask(ctx, Task{ID: in.ID, Kind: domain.KindURL, Status: domain.StatusReady}, Input{TaskID: in.ID, URL: strings.TrimSpace(in.URL), CanonicalURL: canonical, Metadata: in.Metadata}, domain.NewProgress(domain.PhasePreparing, 0, 0, domain.UnitNone, "准备下载"))
}

func (s *Service) InitUploadTask(ctx context.Context, in InitUploadInput) (Task, error) {
	if strings.TrimSpace(in.ID) == "" {
		return Task{}, ErrInvalidInput
	}
	return s.repo.CreateTask(ctx, Task{ID: in.ID, Kind: domain.KindUpload, Status: domain.StatusCreated}, Input{TaskID: in.ID, Metadata: in.Metadata}, domain.NewProgress(domain.PhaseUploading, 0, 0, domain.UnitBytes, "等待上传"))
}

func (s *Service) AttachUploadSource(ctx context.Context, in AttachUploadSourceInput) (Task, error) {
	if strings.TrimSpace(in.TaskID) == "" || strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Name) == "" {
		return Task{}, ErrInvalidInput
	}
	return s.repo.AttachUploadSource(ctx, in, domain.NewProgress(domain.PhasePreparing, in.Size, in.Size, domain.UnitBytes, "上传完成，等待处理"))
}

func (s *Service) RequestCancel(ctx context.Context, taskID string) (Task, error) {
	return s.repo.Transition(ctx, taskID, domain.StatusCanceling, domain.ActorAPI, "cancel requested")
}

func (s *Service) Retry(ctx context.Context, taskID string) (Task, error) {
	return s.repo.Retry(ctx, taskID, domain.NewProgress(domain.PhasePreparing, 0, 0, domain.UnitNone, "等待重试"))
}

func (s *Service) ClaimNext(ctx context.Context, workerID string) (ClaimResult, error) {
	if strings.TrimSpace(workerID) == "" {
		return ClaimResult{}, ErrInvalidInput
	}
	task, err := s.repo.ClaimNext(ctx, workerID, s.cfg.LeaseTTL)
	if err != nil || task == nil {
		return ClaimResult{Task: task}, err
	}
	return ClaimResult{Task: task}, nil
}

func (s *Service) Heartbeat(ctx context.Context, taskID string, workerID string) (HeartbeatResult, error) {
	return s.repo.Heartbeat(ctx, taskID, workerID, s.cfg.LeaseTTL)
}

func (s *Service) ReportProgress(ctx context.Context, taskID string, progress domain.Progress) error {
	return s.repo.UpdateProgress(ctx, taskID, progress)
}

func (s *Service) Complete(ctx context.Context, in CompleteInput) error {
	if strings.TrimSpace(in.ArtifactPath) == "" || strings.TrimSpace(in.ArtifactName) == "" || in.ArtifactSize < 0 {
		return ErrInvalidInput
	}
	return s.repo.Complete(ctx, in)
}

func (s *Service) Fail(ctx context.Context, in FailInput) error {
	if strings.TrimSpace(in.Message) == "" {
		in.Message = "task failed"
	}
	return s.repo.Fail(ctx, in)
}

func (s *Service) AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int) error {
	return s.repo.AcknowledgeCancel(ctx, taskID, workerID, attempt)
}

func (s *Service) RecoverExpired(ctx context.Context) (RecoveryResult, error) {
	return s.repo.RecoverExpired(ctx, s.cfg.MaxAttempts)
}
```

- [ ] **Step 5: Complete in-memory test repository**

In `internal/app/taskcore/service_test.go`, add `memoryRepo` methods matching `Repository`. The key checks must be:

```go
func (r *memoryRepo) Complete(ctx context.Context, in CompleteInput) error {
	task := r.tasks[in.TaskID]
	if task.LeaseOwner != in.WorkerID || task.Attempt != in.Attempt {
		return ErrConflict
	}
	if err := domain.ValidateTransition(task.Status, domain.StatusSucceeded, domain.ActorWorker); err != nil {
		return err
	}
	task.Status = domain.StatusSucceeded
	r.tasks[in.TaskID] = task
	r.results[in.TaskID] = &Result{TaskID: in.TaskID, ArtifactPath: in.ArtifactPath, ArtifactName: in.ArtifactName, ArtifactSize: in.ArtifactSize, ArtifactKind: "cbz"}
	return nil
}
```

Also enforce `ValidateTransition` in `Transition`, `Retry`, `ClaimNext`, `Fail`, and `AcknowledgeCancel`.

- [ ] **Step 6: Run app tests**

Run:

```bash
go test ./internal/app/taskcore
```

Expected: PASS.

- [ ] **Step 7: Commit app service contract**

```bash
git add internal/app/taskcore/types.go internal/app/taskcore/service.go internal/app/taskcore/service_test.go
git commit -m "feat: add task core app service"
```

---

### Task 4: Postgres task-core repository

**Files:**
- Create: `internal/store/postgres/taskcore/store.go`
- Create: `internal/store/postgres/taskcore/store_test.go`

- [ ] **Step 1: Write repository tests for lease and stale completion**

Create `internal/store/postgres/taskcore/store_test.go`. If the project already has a Postgres integration helper, reuse it. Otherwise write tests against a fake executor for generated SQL shape and keep full DB validation in E2E. Required test names:

```go
func TestClaimNextOnlyClaimsReadyTasks(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	created := insertTaskCoreReadyTask(t, ctx, store, "44444444-4444-4444-4444-444444444444")
	claimed, err := store.ClaimNext(ctx, "worker-a", time.Minute)
	if err != nil { t.Fatalf("ClaimNext: %v", err) }
	if claimed == nil { t.Fatalf("expected claimed task") }
	if claimed.ID != created.ID || claimed.Status != domain.StatusRunning || claimed.Attempt != 1 { t.Fatalf("claimed = %#v", claimed) }
}

func TestHeartbeatRequiresLeaseOwner(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	insertTaskCoreReadyTask(t, ctx, store, "55555555-5555-5555-5555-555555555555")
	claimed, err := store.ClaimNext(ctx, "worker-a", time.Minute)
	if err != nil { t.Fatalf("ClaimNext: %v", err) }
	if _, err := store.Heartbeat(ctx, claimed.ID, "worker-b", time.Minute); err == nil { t.Fatalf("heartbeat by stale worker succeeded") }
}

func TestCompleteRequiresMatchingWorkerAndAttempt(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	insertTaskCoreReadyTask(t, ctx, store, "66666666-6666-6666-6666-666666666666")
	claimed, err := store.ClaimNext(ctx, "worker-a", time.Minute)
	if err != nil { t.Fatalf("ClaimNext: %v", err) }
	bad := app.CompleteInput{TaskID: claimed.ID, WorkerID: "worker-a", Attempt: claimed.Attempt + 1, ArtifactPath: "/tmp/out.cbz", ArtifactName: "out.cbz", ArtifactSize: 1}
	if err := store.Complete(ctx, bad); err == nil { t.Fatalf("complete with stale attempt succeeded") }
}

func TestRecoverExpiredRequeuesBelowMaxAttempts(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	insertExpiredRunningTask(t, ctx, store, "77777777-7777-7777-7777-777777777777", 1)
	result, err := store.RecoverExpired(ctx, 3)
	if err != nil { t.Fatalf("RecoverExpired: %v", err) }
	if result.Requeued != 1 || result.Failed != 0 { t.Fatalf("result = %#v", result) }
}

func TestRecoverExpiredFailsAtMaxAttempts(t *testing.T) {
	ctx, store := openTaskCoreTestStore(t)
	insertExpiredRunningTask(t, ctx, store, "88888888-8888-8888-8888-888888888888", 3)
	result, err := store.RecoverExpired(ctx, 3)
	if err != nil { t.Fatalf("RecoverExpired: %v", err) }
	if result.Requeued != 0 || result.Failed != 1 { t.Fatalf("result = %#v", result) }
}
```

Use `t.Skip("requires TEST_DATABASE_URL")` only when the environment variable is absent:

```go
dsn := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
if dsn == "" {
	t.Skip("requires TEST_DATABASE_URL")
}
```

- [ ] **Step 2: Run repository tests to verify they fail**

Run:

```bash
go test ./internal/store/postgres/taskcore -run TestClaimNextOnlyClaimsReadyTasks -count=1
```

Expected: FAIL because the repository does not exist, or SKIP if no `TEST_DATABASE_URL` is configured. If it skips locally, continue and rely on code review plus Compose/E2E later.

- [ ] **Step 3: Implement repository constructor and row mapping**

Create `internal/store/postgres/taskcore/store.go` with package imports and constructor:

```go
package taskcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

type Store struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, now: time.Now}
}

type executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var errConflict = app.ErrConflict
```

- [ ] **Step 4: Implement create and attach source transaction**

Add `CreateTask` and `AttachUploadSource`. `CreateTask` must insert `task_core_tasks`, `task_core_inputs`, `task_core_progress`, and `task_core_events` in one transaction. `AttachUploadSource` must only update a `CREATED` upload task and transition it to `READY`.

Use this transition guard before SQL updates:

```go
if err := domain.ValidateTransition(domain.StatusCreated, domain.StatusReady, domain.ActorAPI); err != nil {
	return app.Task{}, err
}
```

- [ ] **Step 5: Implement ClaimNext with row locking**

Add `ClaimNext` using this SQL shape:

```sql
WITH candidate AS (
    SELECT id, attempt
    FROM task_core_tasks
    WHERE status = 'READY'
    ORDER BY ready_at ASC NULLS LAST, created_at ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE task_core_tasks t
SET status = 'RUNNING',
    attempt = candidate.attempt + 1,
    started_at = NOW(),
    updated_at = NOW(),
    lease_owner = $1,
    lease_expires_at = NOW() + $2::interval,
    heartbeat_at = NOW()
FROM candidate
WHERE t.id = candidate.id
RETURNING t.id, t.kind, t.status, t.attempt, t.last_error, t.lease_owner, t.lease_expires_at, t.created_at, t.updated_at;
```

Pass lease TTL as a PostgreSQL interval string, for example `fmt.Sprintf("%f seconds", leaseTTL.Seconds())`.

- [ ] **Step 6: Implement heartbeat, complete, fail, acknowledge cancel, recovery**

Required SQL guards:

- Heartbeat updates only where `id=$1 AND lease_owner=$2 AND status IN ('RUNNING','CANCELING')`.
- Complete updates only where `id=$1 AND lease_owner=$2 AND attempt=$3 AND status='RUNNING'`.
- Fail updates only where `id=$1 AND lease_owner=$2 AND attempt=$3 AND status='RUNNING'`.
- Acknowledge cancel updates only where `id=$1 AND lease_owner=$2 AND attempt=$3 AND status='CANCELING'`.
- Recovery requeues only `RUNNING` rows where `lease_expires_at < NOW()` and `attempt < maxAttempts`.
- Recovery fails only `RUNNING` rows where `lease_expires_at < NOW()` and `attempt >= maxAttempts`.
- Recovery cancels `CANCELING` rows where `updated_at < NOW() - interval '2 minutes'`.

Every successful state update must insert a `task_core_events` row in the same transaction.

- [ ] **Step 7: Implement list and get methods**

`GetTask` and `ListTasks` must join tasks, inputs, progress, and optional results. Order `ListTasks` by `created_at DESC`, with limit/offset arguments. Map missing progress to `PhasePreparing` and `UnitNone`.

- [ ] **Step 8: Run repository tests**

Run:

```bash
go test ./internal/store/postgres/taskcore -count=1
```

Expected: PASS or SKIP only when `TEST_DATABASE_URL` is absent.

- [ ] **Step 9: Commit repository**

```bash
git add internal/store/postgres/taskcore/store.go internal/store/postgres/taskcore/store_test.go
git commit -m "feat: add postgres task core store"
```

---

### Task 5: Worker task-core executor and recovery loop

**Files:**
- Create: `internal/worker/taskcore/executor.go`
- Create: `internal/worker/taskcore/executor_test.go`
- Modify: `cmd/worker/main.go`

- [ ] **Step 1: Write executor tests**

Create `internal/worker/taskcore/executor_test.go` with fake service and downloader. Required test names and assertions:

```go
func TestExecutorCompletesClaimedTask(t *testing.T) {
	svc := &fakeService{claim: &app.Task{ID: "task-1", Attempt: 1}}
	downloader := &fakeDownloader{path: "/tmp/out.cbz"}
	executor := Executor{Service: svc, Downloader: downloader, WorkerID: "worker-a", HeartbeatInterval: time.Hour}
	processed, err := executor.ProcessOne(context.Background())
	if err != nil || !processed { t.Fatalf("ProcessOne processed=%v err=%v", processed, err) }
	if svc.completed.WorkerID != "worker-a" || svc.completed.Attempt != 1 { t.Fatalf("complete = %#v", svc.completed) }
}

func TestExecutorFailsClaimedTaskOnDownloaderError(t *testing.T) {
	svc := &fakeService{claim: &app.Task{ID: "task-1", Attempt: 1}}
	downloader := &fakeDownloader{err: errors.New("download failed")}
	executor := Executor{Service: svc, Downloader: downloader, WorkerID: "worker-a", HeartbeatInterval: time.Hour}
	processed, err := executor.ProcessOne(context.Background())
	if err != nil || !processed { t.Fatalf("ProcessOne processed=%v err=%v", processed, err) }
	if svc.failed.Message != "download failed" { t.Fatalf("fail = %#v", svc.failed) }
}

func TestExecutorAcknowledgesCancelOnHeartbeatSignal(t *testing.T) {
	svc := &fakeService{claim: &app.Task{ID: "task-1", Attempt: 1}, heartbeat: app.HeartbeatResult{CancelRequested: true}}
	downloader := &blockingDownloader{}
	executor := Executor{Service: svc, Downloader: downloader, WorkerID: "worker-a", HeartbeatInterval: time.Millisecond}
	processed, err := executor.ProcessOne(context.Background())
	if err != nil || !processed { t.Fatalf("ProcessOne processed=%v err=%v", processed, err) }
	if !svc.cancelAcknowledged { t.Fatalf("expected cancel acknowledgement") }
}

func TestExecutorDoesNothingWhenNoTaskClaimed(t *testing.T) {
	svc := &fakeService{}
	downloader := &fakeDownloader{path: "/tmp/out.cbz"}
	executor := Executor{Service: svc, Downloader: downloader, WorkerID: "worker-a"}
	processed, err := executor.ProcessOne(context.Background())
	if err != nil || processed { t.Fatalf("ProcessOne processed=%v err=%v", processed, err) }
	if downloader.called { t.Fatalf("downloader should not be called") }
}
```

- [ ] **Step 2: Run executor tests to verify they fail**

Run:

```bash
go test ./internal/worker/taskcore
```

Expected: FAIL because executor does not exist.

- [ ] **Step 3: Implement executor interfaces and one-shot processing**

Create `internal/worker/taskcore/executor.go`:

```go
package taskcore

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
)

type Service interface {
	ClaimNext(ctx context.Context, workerID string) (app.ClaimResult, error)
	Heartbeat(ctx context.Context, taskID string, workerID string) (app.HeartbeatResult, error)
	Complete(ctx context.Context, in app.CompleteInput) error
	Fail(ctx context.Context, in app.FailInput) error
	AcknowledgeCancel(ctx context.Context, taskID string, workerID string, attempt int) error
	RecoverExpired(ctx context.Context) (app.RecoveryResult, error)
}

type Downloader interface {
	Execute(ctx context.Context, taskID string) (string, error)
}

type Executor struct {
	Service           Service
	Downloader        Downloader
	WorkerID          string
	HeartbeatInterval time.Duration
}

func (e *Executor) ProcessOne(ctx context.Context) (bool, error) {
	if e.Service == nil || e.Downloader == nil || e.WorkerID == "" {
		return false, errors.New("task core executor requires service, downloader, and worker id")
	}
	claim, err := e.Service.ClaimNext(ctx, e.WorkerID)
	if err != nil || claim.Task == nil {
		return false, err
	}
	task := claim.Task
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCh := make(chan struct {
		path string
		err  error
	}, 1)
	go func() {
		path, runErr := e.Downloader.Execute(runCtx, task.ID)
		resultCh <- struct {
			path string
			err  error
		}{path: path, err: runErr}
	}()

	ticker := time.NewTicker(e.heartbeatEvery())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			cancel()
			return true, ctx.Err()
		case result := <-resultCh:
			if result.err != nil {
				return true, e.Service.Fail(ctx, app.FailInput{TaskID: task.ID, WorkerID: e.WorkerID, Attempt: task.Attempt, Message: result.err.Error()})
			}
			name := filepath.Base(result.path)
			return true, e.Service.Complete(ctx, app.CompleteInput{TaskID: task.ID, WorkerID: e.WorkerID, Attempt: task.Attempt, ArtifactPath: result.path, ArtifactName: name, ArtifactSize: 0})
		case <-ticker.C:
			heartbeat, err := e.Service.Heartbeat(ctx, task.ID, e.WorkerID)
			if err != nil {
				cancel()
				return true, err
			}
			if heartbeat.CancelRequested {
				cancel()
				return true, e.Service.AcknowledgeCancel(ctx, task.ID, e.WorkerID, task.Attempt)
			}
		}
	}
}

func (e *Executor) RecoverExpired(ctx context.Context) (app.RecoveryResult, error) {
	if e.Service == nil {
		return app.RecoveryResult{}, errors.New("task core executor requires service")
	}
	return e.Service.RecoverExpired(ctx)
}

func (e *Executor) heartbeatEvery() time.Duration {
	if e.HeartbeatInterval <= 0 {
		return 5 * time.Second
	}
	return e.HeartbeatInterval
}
```

- [ ] **Step 4: Run worker tests**

Run:

```bash
go test ./internal/worker/taskcore
```

Expected: PASS.

- [ ] **Step 5: Wire worker main to task-core executor**

Modify `cmd/worker/main.go` so it constructs:

```go
taskCoreStore := pgtaskcore.NewStore(pool)
taskCoreService := apptaskcore.NewService(taskCoreStore, apptaskcore.Config{LeaseTTL: 30 * time.Second, MaxAttempts: 3})
taskCoreExecutor := workertaskcore.Executor{
	Service: taskCoreService,
	Downloader: taskDownloader,
	WorkerID: workerName,
	HeartbeatInterval: 5 * time.Second,
}
```

Run a loop that calls `ProcessOne(ctx)`. When it returns `processed=false`, sleep for one second. Run `RecoverExpired(ctx)` every 30 seconds in the same process or a companion goroutine.

- [ ] **Step 6: Run worker package tests**

Run:

```bash
go test ./cmd/worker ./internal/worker/taskcore ./internal/worker
```

Expected: PASS.

- [ ] **Step 7: Commit worker executor**

```bash
git add internal/worker/taskcore/executor.go internal/worker/taskcore/executor_test.go cmd/worker/main.go
git commit -m "feat: add task core worker executor"
```

---

### Task 6: HTTP task-core presenter and handlers

**Files:**
- Create: `internal/httpapi/taskcore_presenter.go`
- Create: `internal/httpapi/taskcore_presenter_test.go`
- Create: `internal/httpapi/taskcore_handlers.go`
- Create: `internal/httpapi/taskcore_handlers_test.go`
- Modify: `internal/httpapi/api.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Write presenter tests**

Create `internal/httpapi/taskcore_presenter_test.go`:

```go
package httpapi

import (
	"testing"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

func TestPresentTaskCoreViewUsesDomainActions(t *testing.T) {
	t.Parallel()

	view := presentTaskCoreView(app.TaskView{
		Task: app.Task{ID: "task-1", Status: domain.StatusSucceeded},
		Progress: domain.NewProgress(domain.PhaseDone, 1, 1, domain.UnitSteps, ""),
		Result: &app.Result{TaskID: "task-1", ArtifactPath: "/tmp/a.cbz", ArtifactName: "a.cbz", ArtifactSize: 1},
	}, true)

	if view.StatusLabel != "成功" {
		t.Fatalf("status label = %q, want 成功", view.StatusLabel)
	}
	if view.PhaseLabel != "已完成" {
		t.Fatalf("phase label = %q, want 已完成", view.PhaseLabel)
	}
	if !containsAction(view.AvailableActions, "download") || !containsAction(view.AvailableActions, "copy_to_komga") {
		t.Fatalf("actions = %#v, want download and copy_to_komga", view.AvailableActions)
	}
}
```

- [ ] **Step 2: Implement presenter**

Create `internal/httpapi/taskcore_presenter.go`:

```go
package httpapi

import (
	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

type taskCoreProgressView struct {
	Phase   string `json:"phase"`
	Current int64  `json:"current"`
	Total   int64  `json:"total"`
	Unit    string `json:"unit"`
	Message string `json:"message"`
}

type taskCoreView struct {
	ID               string               `json:"id"`
	Kind             string               `json:"task_type"`
	Status           string               `json:"status"`
	StatusLabel      string               `json:"status_label"`
	PhaseLabel       string               `json:"phase_label"`
	AvailableActions []string             `json:"available_actions"`
	Progress         taskCoreProgressView `json:"progress"`
	URL              string               `json:"url,omitempty"`
	CanonicalURL     string               `json:"canonical_url,omitempty"`
	ArtifactName     string               `json:"artifact_name,omitempty"`
	Error            string               `json:"error,omitempty"`
}

func presentTaskCoreView(view app.TaskView, komgaConfigured bool) taskCoreView {
	hasResult := view.Result != nil && view.Result.ArtifactPath != ""
	actions := domain.AvailableActions(view.Task.Status, hasResult, komgaConfigured)
	outActions := make([]string, 0, len(actions))
	for _, action := range actions {
		outActions = append(outActions, string(action))
	}
	out := taskCoreView{
		ID:               view.Task.ID,
		Kind:             string(view.Task.Kind),
		Status:           string(view.Task.Status),
		StatusLabel:      taskCoreStatusLabel(view.Task.Status),
		PhaseLabel:       view.Progress.Phase.Label(),
		AvailableActions: outActions,
		Progress: taskCoreProgressView{
			Phase:   string(view.Progress.Phase),
			Current: view.Progress.Current,
			Total:   view.Progress.Total,
			Unit:    string(view.Progress.Unit),
			Message: view.Progress.Message,
		},
		URL:          view.Input.URL,
		CanonicalURL: view.Input.CanonicalURL,
		Error:        view.Task.LastError,
	}
	if view.Result != nil {
		out.ArtifactName = view.Result.ArtifactName
	}
	return out
}

func taskCoreStatusLabel(status domain.Status) string {
	switch status {
	case domain.StatusCreated:
		return "待上传"
	case domain.StatusReady:
		return "排队中"
	case domain.StatusRunning:
		return "运行中"
	case domain.StatusCanceling:
		return "取消中"
	case domain.StatusSucceeded:
		return "成功"
	case domain.StatusFailed:
		return "失败"
	case domain.StatusCanceled:
		return "已取消"
	default:
		return "未知"
	}
}

func containsAction(actions []string, want string) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Write handler tests**

Create `internal/httpapi/taskcore_handlers_test.go` with tests for:

```go
func TestTaskCoreHandlersCreateURLTask(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{createURLStatus: domain.StatusReady}
	router := newTaskCoreTestRouter(svc)
	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("url=https%3A%2F%2Ftelegra.ph%2Fdemo"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String()) }
	if svc.createdURL.URL != "https://telegra.ph/demo" { t.Fatalf("created URL = %#v", svc.createdURL) }
}

func TestTaskCoreHandlersCancelTask(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{}
	router := newTaskCoreTestRouter(svc)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-1/cancel", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String()) }
	if svc.canceledID != "task-1" { t.Fatalf("canceled id = %q", svc.canceledID) }
}

func TestTaskCoreHandlersListTasks(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{views: []app.TaskView{{Task: app.Task{ID: "task-1", Status: domain.StatusFailed}}}}
	router := newTaskCoreTestRouter(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String()) }
	if !strings.Contains(rec.Body.String(), "retry") { t.Fatalf("body missing retry action: %s", rec.Body.String()) }
}

func TestTaskCoreHandlersDownloadHeadRejectsUnavailableResult(t *testing.T) {
	svc := &fakeTaskCoreHTTPService{view: &app.TaskView{Task: app.Task{ID: "task-1", Status: domain.StatusFailed}}}
	router := newTaskCoreTestRouter(svc)
	req := httptest.NewRequest(http.MethodHead, "/api/tasks/task-1/download", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict { t.Fatalf("status = %d", rec.Code) }
}
```

Use a fake app service that records calls and returns deterministic task views.

- [ ] **Step 4: Implement handlers**

Create `internal/httpapi/taskcore_handlers.go` with a handler struct that depends on an interface:

```go
type TaskCoreService interface {
	CreateURLTask(ctx context.Context, in app.CreateURLInput) (app.Task, error)
	InitUploadTask(ctx context.Context, in app.InitUploadInput) (app.Task, error)
	AttachUploadSource(ctx context.Context, in app.AttachUploadSourceInput) (app.Task, error)
	RequestCancel(ctx context.Context, taskID string) (app.Task, error)
	Retry(ctx context.Context, taskID string) (app.Task, error)
	ListTasks(ctx context.Context, limit int, offset int) ([]app.TaskView, error)
	GetTask(ctx context.Context, taskID string) (*app.TaskView, error)
}
```

Handlers must:

- Generate UUIDs in HTTP layer if request has no ID.
- Parse metadata by calling the existing `parseMetadataInput(r)` helper from `download_request.go`; if the helper is not exported, keep the new handler in package `httpapi` and call it directly.
- Use `presentTaskCoreView` for every task response.
- Return JSON error responses with existing `writeJSONError` helper style.
- Keep endpoint paths listed in the spec.

- [ ] **Step 5: Wire router options**

Modify `internal/httpapi/api.go`:

- Add `TaskCoreService TaskCoreService` to `RouterOptions`.
- If `TaskCoreService != nil`, register task-core handlers for `/download`, `/api/tasks`, `/api/tasks/{id}/cancel`, `/api/tasks/{id}/retry`, `/api/tasks/{id}/download`, `/api/tasks/{id}/copy-to-komga`, `/api/tasks/upload/init`, and `/api/tasks/{id}/upload-source`.
- Keep old route registration only when `TaskCoreService == nil`.

- [ ] **Step 6: Wire server main**

Modify `cmd/server/main.go`:

```go
taskCoreStore := pgtaskcore.NewStore(pool)
taskCoreService := apptaskcore.NewService(taskCoreStore, apptaskcore.Config{LeaseTTL: 30 * time.Second, MaxAttempts: 3})
legacyRouterOptions := buildLegacyRouterOptions(cfg, downloadQueue, v2Store, v2Queue)
legacyRouterOptions.TaskCoreService = taskCoreService
```

- [ ] **Step 7: Run HTTP tests**

Run:

```bash
go test ./internal/httpapi ./cmd/server
```

Expected: PASS.

- [ ] **Step 8: Commit HTTP integration**

```bash
git add internal/httpapi/taskcore_presenter.go internal/httpapi/taskcore_presenter_test.go internal/httpapi/taskcore_handlers.go internal/httpapi/taskcore_handlers_test.go internal/httpapi/api.go cmd/server/main.go
git commit -m "feat: route task api through task core"
```

---

### Task 7: Frontend consumes backend action and progress view

**Files:**
- Modify: `frontend/src/logs/view_model.js`
- Modify: `frontend/src/logs/task_actions.js`
- Modify: `frontend/src/tests/logs_view_model.test.mjs`
- Modify: `frontend/src/tests/logs_task_actions.test.mjs`
- Modify: `web/static/dist/logs.bundle.js`

- [ ] **Step 1: Write frontend view tests**

Add tests to `frontend/src/tests/logs_view_model.test.mjs`:

```js
test('uses backend task core labels when present', () => {
  const task = buildLogTaskView({
    id: 'task-1',
    status: 'RUNNING',
    status_label: '运行中',
    phase_label: '下载中',
    progress: { phase: 'downloading', current: 2, total: 5, unit: 'images', message: '下载中' },
    available_actions: ['cancel'],
  });

  assert.equal(task.statusLabel, '运行中');
  assert.equal(task.progressLabel, '下载中 2/5');
  assert.deepEqual(task.availableActions, ['cancel']);
});
```

Add tests to `frontend/src/tests/logs_task_actions.test.mjs`:

```js
test('task actions prefer backend available_actions', () => {
  const task = { status: 'SUCCEEDED', available_actions: ['download', 'copy_to_komga'] };
  assert.equal(canDownloadTask(task), true);
  assert.equal(canCopyToKomga(task), true);
  assert.equal(canCancelTask(task), false);
});
```

- [ ] **Step 2: Run frontend tests to verify they fail**

Run:

```bash
npm run test:frontend -- logs_view_model logs_task_actions
```

Expected: FAIL because current frontend logic does not expose or prefer `available_actions` and task-core labels.

- [ ] **Step 3: Update view model**

Modify `frontend/src/logs/view_model.js` so it:

- Uses `task.status_label` before local status mapping.
- Uses `task.phase_label` and `task.progress` before legacy `progress/total_images/upload_loaded_bytes` inference.
- Exposes `availableActions: Array.isArray(task.available_actions) ? task.available_actions : []`.

Required helper shape:

```js
function buildTaskCoreProgressLabel(task) {
  const progress = task && task.progress && typeof task.progress === 'object' ? task.progress : null;
  if (!progress) return '';
  const label = String(task.phase_label || progress.message || '').trim();
  const current = Number(progress.current || 0);
  const total = Number(progress.total || 0);
  if (total > 0) return `${label} ${current}/${total}`.trim();
  return label;
}
```

- [ ] **Step 4: Update task action helpers**

Modify `frontend/src/logs/task_actions.js` so helpers prefer backend actions:

```js
function hasBackendAction(task, action) {
  return Array.isArray(task?.available_actions)
    ? task.available_actions.includes(action)
    : Array.isArray(task?.availableActions) && task.availableActions.includes(action);
}

export function canCancelTask(task) {
  if (Array.isArray(task?.available_actions) || Array.isArray(task?.availableActions)) {
    return hasBackendAction(task, 'cancel');
  }
  return legacyCanCancelTask(task);
}
```

Apply the same pattern for `retry`, `download`, and `copy_to_komga`.

- [ ] **Step 5: Run frontend tests**

Run:

```bash
npm run test:frontend
```

Expected: PASS.

- [ ] **Step 6: Build frontend bundle**

Run:

```bash
npm run build
```

Expected: PASS and `web/static/dist/logs.bundle.js` changes if logs bundle is affected.

- [ ] **Step 7: Commit frontend task-core view support**

```bash
git add frontend/src/logs/view_model.js frontend/src/logs/task_actions.js frontend/src/tests/logs_view_model.test.mjs frontend/src/tests/logs_task_actions.test.mjs web/static/dist/logs.bundle.js
git commit -m "feat: consume task core actions in logs UI"
```

---

### Task 8: E2E coverage and docs for breaking task-core cutover

**Files:**
- Modify: `tests/e2e/specs/logs-flow.spec.js`
- Modify: `tests/e2e/specs/upload-flow.spec.js`
- Add: `docs/runbooks/2026-04-22-task-core-rebuild.md`
- Modify: `README.md`

- [ ] **Step 1: Add E2E assertions for backend-driven actions**

In `tests/e2e/specs/logs-flow.spec.js`, add assertions in the successful task section that:

```js
await expect(page.getByRole('button', { name: /下载|复制到 Komga/ })).toBeVisible();
await expect(page.locator('[data-task-status-label]').first()).toContainText(/成功|运行中|排队中/);
```

If the markup lacks `data-task-status-label`, add it in the rendering path during this task and cover it in frontend tests.

- [ ] **Step 2: Add E2E cancel/retry assertion**

In `tests/e2e/specs/logs-flow.spec.js`, add a test that creates or locates a cancellable task, clicks cancel, waits for `取消中` or `已取消`, then verifies a retry button appears after terminal cancellation.

Use existing helpers in the file for task creation and polling. The test must not use fixed sleeps longer than the existing polling helper interval.

- [ ] **Step 3: Add upload task-core assertion**

In `tests/e2e/specs/upload-flow.spec.js`, assert upload flow shows:

```js
await expect(page.getByText(/上传中|准备中|运行中|成功/)).toBeVisible();
```

After success, assert the available action button is visible.

- [ ] **Step 4: Document breaking cutover**

Create `docs/runbooks/2026-04-22-task-core-rebuild.md`:

```markdown
# Task Core Rebuild Runbook

## What changes

The task lifecycle now uses `task_core_*` tables. Existing legacy task log rows are not migrated into the new logs view.

## Before deploy

1. Back up Postgres.
2. Confirm `bash scripts/verify_release_gates.sh` passes.
3. Confirm `docker compose up -d --build` starts `go-api`, `go-worker`, Postgres, Redis, and gateway.

## After deploy

1. Open the home page.
2. Submit one Telegraph URL task.
3. Upload one ZIP/RAR/7Z source task.
4. Confirm logs show status labels and backend-driven actions.
5. Confirm a successful task can be downloaded or copied to Komga depending on settings.

## Rollback

Rollback code to the previous release. Old tables are preserved. `task_core_*` data can be ignored during rollback.
```

- [ ] **Step 5: Update README**

Add a short note near the architecture section:

```markdown
### Task Core lifecycle

The current task lifecycle uses the `task_core_*` tables. This was a breaking task-log reset: legacy task rows are retained in the database for rollback, but the active logs view is backed by Task Core. See `docs/runbooks/2026-04-22-task-core-rebuild.md` for deploy and rollback checks.
```

- [ ] **Step 6: Run E2E tests**

Run:

```bash
npm run e2e:test
```

Expected: PASS. If browser automation is blocked by macOS permissions, capture the exact failure and run Compose smoke from Task 9.

- [ ] **Step 7: Commit E2E and docs**

```bash
git add tests/e2e/specs/logs-flow.spec.js tests/e2e/specs/upload-flow.spec.js docs/runbooks/2026-04-22-task-core-rebuild.md README.md
git commit -m "test: cover task core lifecycle flows"
```

---

### Task 9: Release gate and final verification

**Files:**
- Modify only files required by failures found in this task.

- [ ] **Step 1: Run Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run Go race tests**

Run:

```bash
go test -race ./...
```

Expected: PASS.

- [ ] **Step 3: Run frontend tests and lint**

Run:

```bash
npm run test:frontend
npm run lint
npm run build
```

Expected: all PASS.

- [ ] **Step 4: Run release gate script**

Run:

```bash
bash scripts/verify_release_gates.sh
```

Expected: PASS. If E2E cannot launch a browser due to local macOS automation permissions, record the exact command output and run Step 5.

- [ ] **Step 5: Run Compose smoke when E2E browser is blocked**

Run:

```bash
INTERNAL_ENQUEUE_TOKEN=test-token docker compose up -d --build
curl -fsS http://localhost:5002/healthz
curl -fsS http://localhost:5002/readyz
docker compose logs --tail=100 go-api go-worker
docker compose down
```

Expected: health and ready endpoints return success, and logs show no task-core migration or startup errors.

- [ ] **Step 6: Inspect git status**

Run:

```bash
git status --short
```

Expected: only intentional files are modified. If verification changes generated bundles or docs, commit them.

- [ ] **Step 7: Commit verification fixes if any**

If changes were required during verification:

```bash
git add <changed-files>
git commit -m "fix: stabilize task core release gate"
```

If no changes were required, do not create an empty commit.

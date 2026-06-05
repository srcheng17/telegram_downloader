# Runtime Mainline Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the repository's runnable contract match the actual Task Core mainline: `gateway + go-api + go-worker + postgres`, with no Redis Streams or Python/Flask runtime residue in current configuration, scripts, or baseline docs.

**Architecture:** The current runtime already creates and executes work through `task_core_*` tables, PostgreSQL leases, worker heartbeats, and recovery. This plan first adds static contract tests that fail on the remaining Redis/Python runtime residue, then updates Compose, Go config, local startup, Docker runtime dependencies, docs, and module metadata until those contracts pass.

**Tech Stack:** Go 1.23, PostgreSQL, Docker Compose, Alpine Docker image, Bash, Vite frontend checks

---

## File Structure

- Modify `internal/config/compose_contract_test.go`: keep Compose and nginx topology contracts, remove Redis from the expected service set, assert Go services do not receive retired stream env vars.
- Create `internal/config/runtime_contract_test.go`: repository-level runtime contracts for retired Config fields, `scripts/start_local.sh`, Docker archive extractor dependency, and current runtime docs.
- Modify `internal/config/config.go`: delete Redis/stream config fields and env parsing; retain `ConsumerName` as the worker identity source.
- Modify `internal/config/config_test.go`: replace Redis config tests with worker identity and production-required setting tests.
- Modify `docker-compose.yml`: delete `redis`, Redis env vars, Redis dependencies, and Redis data volume usage.
- Modify `.env.example`: delete `STREAM_NAME` and `CONSUMER_GROUP`; keep `GO_WORKER_CONSUMER_NAME`.
- Modify `Dockerfile`: install `libarchive-tools` in the runtime image so `bsdtar` exists for RAR/7Z upload extraction.
- Modify `scripts/start_local.sh`: replace Python/Flask local startup with Compose mainline startup.
- Modify `scripts/verify_no_legacy_runtime.sh`: extend static checks to cover `scripts/start_local.sh` and the retired env vars.
- Modify `README.md`, `docs/architecture/current-system-overview.md`, `docs/architecture/config-inventory.md`, and `docs/architecture/task-lifecycle-baseline.md`: current facts describe Task Core over PostgreSQL, not Redis Streams.
- Modify `go.mod` and `go.sum`: run `go mod tidy` after Go references to Redis are gone.

---

### Task 1: Add Failing Runtime Mainline Contracts

**Files:**
- Modify: `internal/config/compose_contract_test.go`
- Create: `internal/config/runtime_contract_test.go`

- [ ] **Step 1: Update the Compose service-set contract**

In `internal/config/compose_contract_test.go`, change the expected service set in `TestComposeTopologyMatchesGoBackendOnly` to:

```go
expectedServices := map[string]struct{}{
	"go-api":    {},
	"go-worker": {},
	"postgres":  {},
	"gateway":   {},
}
```

Then add this helper near the other private helpers:

```go
func assertComposeBlockOmitsRetiredRuntimeEnv(t *testing.T, block, serviceName string) {
	t.Helper()

	retiredEnvNames := []string{
		strings.Join([]string{"REDIS", "URL:"}, "_"),
		strings.Join([]string{"STREAM", "NAME:"}, "_"),
		strings.Join([]string{"V2", "STREAM", "NAME:"}, "_"),
		strings.Join([]string{"CONSUMER", "GROUP:"}, "_"),
	}
	for _, name := range retiredEnvNames {
		if strings.Contains(block, name) {
			t.Fatalf("%s must not declare retired Redis Streams env %s", serviceName, name)
		}
	}
	if strings.Contains(block, "redis:") {
		t.Fatalf("%s must not depend on redis in the Task Core PostgreSQL mainline", serviceName)
	}
}
```

Call it after extracting the service blocks:

```go
goAPIBlock := extractComposeServiceBlock(t, composeText, "go-api")
assertComposeBlockOmitsRetiredRuntimeEnv(t, goAPIBlock, "go-api")

goWorkerBlock := extractComposeServiceBlock(t, composeText, "go-worker")
assertComposeBlockOmitsRetiredRuntimeEnv(t, goWorkerBlock, "go-worker")
```

Keep retired env and Config field names assembled from fragments inside `internal/config` tests. The final verifier intentionally greps `internal/config` for retired runtime tokens, so test code must not reintroduce those contiguous strings.

- [ ] **Step 2: Add repository runtime contracts**

Create `internal/config/runtime_contract_test.go` with:

```go
package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigDoesNotExposeRetiredRedisStreamSettings(t *testing.T) {
	configType := reflect.TypeOf(Config{})
	retiredFields := []string{
		"Redis" + "URL",
		"Stream" + "Name",
		"V2" + "Stream" + "Name",
		"Consumer" + "Group",
	}

	for _, fieldName := range retiredFields {
		if _, ok := configType.FieldByName(fieldName); ok {
			t.Fatalf("Config must not expose retired Redis Streams field %s", fieldName)
		}
	}
}

func TestStartLocalScriptUsesComposeMainline(t *testing.T) {
	script := readRepoFile(t, "scripts", "start_local.sh")

	forbidden := []string{
		"python3",
		"pip install",
		"requirements.txt",
		"FLASK_APP",
		"app.py",
		"flask run",
	}
	for _, token := range forbidden {
		if strings.Contains(script, token) {
			t.Fatalf("scripts/start_local.sh must not contain legacy Python runtime token %q", token)
		}
	}
	if !strings.Contains(script, "docker compose up -d --build") {
		t.Fatalf("scripts/start_local.sh must start the Docker Compose mainline")
	}
	if !strings.Contains(script, "INTERNAL_ENQUEUE_TOKEN") {
		t.Fatalf("scripts/start_local.sh must provide or require INTERNAL_ENQUEUE_TOKEN")
	}
}

func TestDockerfileDeclaresArchiveExtractorRuntimePackage(t *testing.T) {
	dockerfile := readRepoFile(t, "Dockerfile")

	if !strings.Contains(dockerfile, "libarchive-tools") {
		t.Fatalf("Dockerfile runtime image must install libarchive-tools so bsdtar is available for RAR/7Z uploads")
	}
}

func TestRuntimeDocsDescribeTaskCorePostgresMainline(t *testing.T) {
	targets := []string{
		readRepoFile(t, "README.md"),
		readRepoFile(t, "docs", "architecture", "current-system-overview.md"),
		readRepoFile(t, "docs", "architecture", "config-inventory.md"),
		readRepoFile(t, "docs", "architecture", "task-lifecycle-baseline.md"),
	}

	for _, content := range targets {
		forbidden := []string{
			"Redis Streams",
			"redis stream",
			"redis streams",
			"postgres/redis",
			"postgres + redis",
			"internal/queue/v2",
			"internal/queue/redisstream",
		}
		for _, token := range forbidden {
			if strings.Contains(content, token) {
				t.Fatalf("current runtime docs must not describe retired Redis Streams mainline token %q", token)
			}
		}
	}
}

func readRepoFile(t *testing.T, pathParts ...string) string {
	t.Helper()

	candidates := [][]string{
		append([]string{"..", ".."}, pathParts...),
		append([]string{"..", "..", ".."}, pathParts...),
		append([]string{".."}, pathParts...),
		pathParts,
	}
	for _, parts := range candidates {
		path := filepath.Join(parts...)
		content, err := os.ReadFile(path)
		if err == nil {
			return string(content)
		}
	}
	t.Fatalf("repo file not found: %s", filepath.Join(pathParts...))
	return ""
}
```

- [ ] **Step 3: Run the new contracts and verify RED**

Run:

```bash
go test ./internal/config -run 'TestComposeTopologyMatchesGoBackendOnly|TestConfigDoesNotExposeRetiredRedisStreamSettings|TestStartLocalScriptUsesComposeMainline|TestDockerfileDeclaresArchiveExtractorRuntimePackage|TestRuntimeDocsDescribeTaskCorePostgresMainline' -count=1
```

Expected: FAIL for the current Redis service/env vars, retired Config fields, Python `start_local.sh`, missing `libarchive-tools`, and Redis Streams runtime docs.

---

### Task 2: Remove Redis Streams Runtime Configuration

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `docker-compose.yml`
- Modify: `.env.example`
- Test: `internal/config/compose_contract_test.go`
- Test: `internal/config/runtime_contract_test.go`

- [ ] **Step 1: Delete retired Config fields and parsing**

In `internal/config/config.go`, make the `Config` struct's runtime fields:

```go
type Config struct {
	Addr             string
	DatabaseURL      string
	ConsumerName     string
	UpstreamBaseURL  string
	InternalToken    string
	DownloadTimeout  int
	DownloadRetries  int
	ImageConcurrency int
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	ShutdownTimeout  time.Duration
}
```

Delete the `redisURL`, `streamName`, `v2StreamName`, and `consumerGroup` parsing blocks from `LoadFromEnv`, and delete the corresponding assignments from the returned `Config`.

- [ ] **Step 2: Replace Redis config tests with worker identity tests**

In `internal/config/config_test.go`, delete `TestLoadFromEnvIncludesRedisWorkerConfig` and `TestLoadFromEnvReadsV2StreamOverride`.

Add:

```go
func TestLoadFromEnvUsesConsumerNameOverride(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://telegraph:telegraph@postgres:5432/telegraph?sslmode=disable")
	t.Setenv("CONSUMER_NAME", "worker-explicit")
	t.Setenv("HOSTNAME", "worker-host")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.ConsumerName != "worker-explicit" {
		t.Fatalf("expected explicit consumer name, got %q", cfg.ConsumerName)
	}
}

func TestLoadFromEnvFallsBackToHostnameForConsumerName(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://telegraph:telegraph@postgres:5432/telegraph?sslmode=disable")
	t.Setenv("CONSUMER_NAME", "")
	t.Setenv("HOSTNAME", "go-worker-1")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.ConsumerName != "go-worker-1" {
		t.Fatalf("expected consumer name from HOSTNAME, got %q", cfg.ConsumerName)
	}
}
```

- [ ] **Step 3: Remove Redis from Compose**

In `docker-compose.yml`:

- Delete the entire `redis:` service block.
- Delete `REDIS_URL` and `STREAM_NAME` from `go-api.environment`.
- Delete `REDIS_URL`, `STREAM_NAME`, and `CONSUMER_GROUP` from `go-worker.environment`.
- Delete the `redis:` dependency from both `go-api.depends_on` and `go-worker.depends_on`.
- Keep `CONSUMER_NAME: ${GO_WORKER_CONSUMER_NAME:-}` for worker identity.

The resulting service set must be exactly `postgres`, `go-api`, `go-worker`, and `gateway`.

- [ ] **Step 4: Remove retired env vars from `.env.example`**

Delete these lines:

```dotenv
STREAM_NAME=download_tasks
CONSUMER_GROUP=go-workers
```

Keep:

```dotenv
GO_WORKER_CONSUMER_NAME=
```

- [ ] **Step 5: Verify GREEN for config and Compose contracts**

Run:

```bash
go test ./internal/config -count=1
```

Expected: PASS.

---

### Task 3: Fix Local Startup and Runtime Archive Dependency

**Files:**
- Modify: `scripts/start_local.sh`
- Modify: `Dockerfile`
- Test: `internal/config/runtime_contract_test.go`

- [ ] **Step 1: Replace `scripts/start_local.sh` with Compose startup**

Use this full script:

```bash
#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_PORT="${APP_PORT:-5002}"

cd "$ROOT_DIR"
mkdir -p "$ROOT_DIR/downloaded_images" "$ROOT_DIR/temp_downloads" "$ROOT_DIR/data/postgres"

export INTERNAL_ENQUEUE_TOKEN="${INTERNAL_ENQUEUE_TOKEN:-local-dev-token}"
export APP_PORT

docker compose up -d --build
docker compose ps

printf 'Telegraph Downloader is available at http://localhost:%s\n' "$APP_PORT"
```

- [ ] **Step 2: Install `libarchive-tools` in the Docker runtime image**

In `Dockerfile`, replace the runtime package line with:

```dockerfile
RUN apk add --no-cache ca-certificates tzdata libarchive-tools \
    && adduser -D -u 10001 app
```

- [ ] **Step 3: Verify startup and Dockerfile contracts**

Run:

```bash
go test ./internal/config -run 'TestStartLocalScriptUsesComposeMainline|TestDockerfileDeclaresArchiveExtractorRuntimePackage' -count=1
```

Expected: PASS.

---

### Task 4: Align Current Runtime Docs and Legacy Runtime Verifier

**Files:**
- Modify: `README.md`
- Modify: `docs/architecture/current-system-overview.md`
- Modify: `docs/architecture/config-inventory.md`
- Modify: `docs/architecture/task-lifecycle-baseline.md`
- Modify: `scripts/verify_no_legacy_runtime.sh`
- Test: `internal/config/runtime_contract_test.go`

- [ ] **Step 1: Update README current runtime facts**

In `README.md`, make these current-fact replacements:

```markdown
*   **部署**: Docker Compose, Nginx, PostgreSQL
```

```markdown
默认生产拓扑：

`gateway -> go-api -> postgres`

`go-worker -> postgres`

`go-worker` 通过 PostgreSQL 中的 Task Core 任务、lease、heartbeat 与 recovery 推进任务生命周期。
```

Replace the core directory list's queue entry with:

```text
internal/app/taskcore/     # Task Core 应用服务与任务生命周期用例
internal/worker/taskcore/  # PostgreSQL lease 驱动的 worker 执行链路
internal/store/postgres/   # Task Core / 上传 / 设置仓储 + migrations runner
```

Replace the Compose topology line with:

```markdown
默认服务拓扑为：`gateway + go-api + go-worker + postgres`。
```

- [ ] **Step 2: Update `current-system-overview.md` current topology**

Replace the runtime topology block with this content:

```markdown
当前默认单主线部署拓扑为：

    gateway (nginx)
      -> go-api
           -> postgres
    go-worker
      -> postgres

补充说明：

- `gateway` 是统一对外入口，默认暴露 `APP_PORT=5002`。
- `go-api` 同时负责页面渲染（首页/日志/设置）与 `/download`、`/api/*` 接口。
- `go-worker` 通过 PostgreSQL `task_core_*` 表中的 ready 任务、lease、heartbeat 与 recovery 推进任务状态。
- `postgres` 存储 Task Core 任务、上传任务、元数据历史、设置和状态变化相关数据。
```

Remove current-runtime references to `internal/queue/v2/`, `internal/queue/redisstream/`, and Redis Streams.

- [ ] **Step 3: Update `config-inventory.md`**

Remove the `redis` service section. In the `go-api` section, list:

```markdown
- `PORT`（默认 `5000`）
- `DATABASE_URL`
- `INTERNAL_ENQUEUE_TOKEN`（必填）
- `GO_DOWNLOAD_TIMEOUT`
- `GO_DOWNLOAD_RETRIES`
- `GO_IMAGE_CONCURRENCY`
- `DOWNLOAD_PATH`（默认 `/app/downloaded_images`）
- `TEMP_PATH`（默认 `/app/temp_downloads`）
- `KOMGA_LIBRARY_ROOT`（默认 `/app/komga/myReadingManga`）
```

In the `go-worker` section, list:

```markdown
- `DATABASE_URL`
- `INTERNAL_ENQUEUE_TOKEN`
- `CONSUMER_NAME`（由 Compose 的 `GO_WORKER_CONSUMER_NAME` 注入，空值时回退到容器 hostname）
- `GO_DOWNLOAD_TIMEOUT`
- `GO_DOWNLOAD_RETRIES`
- `GO_IMAGE_CONCURRENCY`
- `DOWNLOAD_PATH`
- `TEMP_PATH`
```

In the configuration classification section, replace queue configuration with:

```markdown
3. **worker 身份与执行**
   - `CONSUMER_NAME`
```

And keep infrastructure connection as:

```markdown
4. **基础设施连接**
   - `DATABASE_URL`
```

- [ ] **Step 4: Add Task Core queue fact to `task-lifecycle-baseline.md`**

Add this section after "当前任务类型":

```markdown
## 当前调度方式

当前任务调度不再依赖外部队列服务。`go-api` 写入 PostgreSQL `task_core_*` 表后，`go-worker` 周期性领取 `READY` 任务并持有 lease；执行过程中通过 heartbeat 保持租约，异常退出后由 recovery 重新释放过期任务。
```

- [ ] **Step 5: Extend `scripts/verify_no_legacy_runtime.sh`**

Use this full script:

```bash
#!/usr/bin/env bash
set -euo pipefail

! test -e app.py
! test -e requirements.txt
! test -d telegram_downloader
! test -d templates
! test -e static/index.js
! test -d static/v2

! rg -n 'REDIS_URL|STREAM_NAME|V2_STREAM_NAME|CONSUMER_GROUP' docker-compose.yml .env.example README.md docs/architecture internal/config >/dev/null
! rg -n 'FLASK_APP|flask run|requirements.txt|app.py' scripts/start_local.sh README.md .github/workflows/ci.yml docker-compose.yml scripts/verify_release_gates.sh >/dev/null
! rg -n 'pytest tests/web|GO_BACKEND_BASE_URL|PYTHON_WEB_BASE_URL' README.md .github/workflows/ci.yml docker-compose.yml scripts/verify_release_gates.sh >/dev/null
```

- [ ] **Step 6: Verify docs and legacy runtime contracts**

Run:

```bash
go test ./internal/config -run TestRuntimeDocsDescribeTaskCorePostgresMainline -count=1
bash scripts/verify_no_legacy_runtime.sh
```

Expected: both PASS.

---

### Task 5: Tidy Dependencies and Run Gates

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Potentially modify: files from Tasks 1-4 if verification finds missed references

- [ ] **Step 1: Remove stale Go module dependencies**

Run:

```bash
go mod tidy
```

Expected: `github.com/redis/go-redis/v9`, `github.com/alicebob/miniredis/v2`, and Redis transitive modules disappear if no current Go source imports them.

- [ ] **Step 2: Verify no current Redis/Python runtime residue**

Run:

```bash
rg -n 'REDIS_URL|STREAM_NAME|V2_STREAM_NAME|CONSUMER_GROUP' docker-compose.yml .env.example README.md docs/architecture internal/config
```

Expected: no output.

Run:

```bash
rg -n 'FLASK_APP|flask run|requirements.txt|app.py' scripts/start_local.sh README.md .github/workflows/ci.yml docker-compose.yml scripts/verify_release_gates.sh
```

Expected: no output.

- [ ] **Step 3: Run Go verification**

Run:

```bash
go test ./...
```

Expected: PASS.

Run:

```bash
go test -race ./...
```

Expected: PASS.

- [ ] **Step 4: Run frontend verification**

Run:

```bash
npm run test:frontend
npm run lint
npm run build
```

Expected: PASS.

- [ ] **Step 5: Validate Compose structure**

Run:

```bash
INTERNAL_ENQUEUE_TOKEN=test-token docker compose config
```

Expected: Compose renders services `postgres`, `go-api`, `go-worker`, and `gateway`, with no `redis` service and no retired stream env vars.

If Docker is unavailable in the local session, record the error and rely on `go test ./internal/config -count=1` plus `bash scripts/verify_no_legacy_runtime.sh` for static coverage.

- [ ] **Step 6: Final release gate**

Run:

```bash
bash scripts/verify_release_gates.sh
```

Expected: PASS, including Go tests, race tests, frontend tests, lint, build, and E2E.

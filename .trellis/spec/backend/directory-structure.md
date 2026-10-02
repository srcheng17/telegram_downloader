# Directory Structure

See [README](../../../README.md) and [module boundaries](../../../docs/development/module-boundaries.md).

| Path | Responsibility |
| --- | --- |
| `cmd/server/`, `cmd/worker/` | Runtime wiring and startup |
| `internal/httpapi/`, `internal/httpv2/`, `internal/httpui/` | Request validation, HTTP mapping, rendered UI |
| `internal/app/taskcore/`, `internal/app/tasks/` | Task lifecycle and artifact/Komga use cases |
| `internal/domain/taskcore/`, `internal/domain/metadata/` | States, action eligibility, metadata normalization |
| `internal/store/postgres/` | Persistence and numbered SQL migrations |
| `internal/worker/taskcore/` | Lease, heartbeat, recovery, task execution |
| `internal/downloader/`, `internal/archive/` | Downloading, CBZ writing, archive extraction |

Reuse domain/app decisions in HTTP and worker callers rather than duplicating
status or action rules. Keep database access in repositories. Use colocated
`*_test.go` files, as in `internal/app/taskcore/service_test.go` and
`internal/domain/taskcore/status_test.go`.

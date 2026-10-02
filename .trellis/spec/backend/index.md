# Backend Development Guidelines

The runtime is Go API + Go worker + PostgreSQL Task Core. These guides summarize
current code; [README](../../../README.md) owns the broader project overview.

## Pre-Development Checklist

- Read [module boundaries](../../../docs/development/module-boundaries.md), then the relevant guide below.
- Trace the HTTP adapter, application service, repository, and worker paths affected by the change.
- Use `internal/domain/taskcore/` for current states and actions; older documents may describe retired runtime paths.

## Guidelines Index

| Guide | Scope |
| --- | --- |
| [Directory structure](./directory-structure.md) | Entry points and module responsibilities |
| [Database](./database-guidelines.md) | pgx, transactions, migrations |
| [Errors](./error-handling.md) | Service errors and HTTP contracts |
| [Logging](./logging-guidelines.md) | Standard-library logging and redaction |
| [Quality](./quality-guidelines.md) | Regression checks and review |

## Quality Check

- Backend changes: `go test ./... -count=1`.
- Task lifecycle, repository, or worker changes: also `go test -race ./... -count=1`.
- Follow [quality guidelines](./quality-guidelines.md) for cross-layer and release checks.

# Database Guidelines

PostgreSQL is the current persistence and task-scheduling backend. Task Core
uses `task_core_*` tables; do not restore the retired Redis/Python runtime.

- Reuse `pgx/v5`, `pgxpool`, and existing repository interfaces; there is no ORM.
- Pass query parameters separately (`$1`, `$2`); never interpolate user input into SQL.
- Keep related lifecycle, progress, and event updates transactional. Preserve row-lock,
  lease-owner, and attempt checks from `internal/store/postgres/taskcore/store.go`.
- Add the next numbered SQL migration under `internal/store/postgres/migrations/`.
  Reuse its embedded runner and advisory lock; do not rewrite an applied migration.

Actual pattern from `internal/store/postgres/taskcore/store.go`:

```go
row := s.pool.QueryRow(ctx, taskViewQuery()+` WHERE t.id = $1`, taskID)
```

Tests: `internal/store/postgres/taskcore/store_test.go` and
`internal/store/postgres/migrations/runner_test.go`. For rollout context, read
[Task Core runbook](../../../docs/runbooks/2026-04-22-task-core-rebuild.md).

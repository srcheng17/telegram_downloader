# Approved work commits

All listed paths belong to the approved optimization; no unrecognized dirty files.
All final checks passed; see validation.md.
The user confirmed this local commit batch on 2026-10-03.
No push or merge is included.

## 1. `fix(taskcore): fence executions and bound task resources`

- `cmd/server/main.go`
- `cmd/server/main_test.go`
- `cmd/worker/main.go`
- `cmd/worker/main_test.go`
- `internal/app/taskcore/service.go`
- `internal/app/taskcore/service_test.go`
- `internal/app/taskcore/types.go`
- `internal/archive/external_extractor.go`
- `internal/archive/extractor.go`
- `internal/archive/extractor_test.go`
- `internal/config/config.go`
- `internal/domain/taskcore/status.go`
- `internal/domain/taskcore/status_test.go`
- `internal/downloader/cbz_writer.go`
- `internal/downloader/downloader.go`
- `internal/httpapi/api.go`
- `internal/httpapi/taskcore_handlers.go`
- `internal/httpapi/taskcore_handlers_test.go`
- `internal/httpapi/taskcore_logs.go`
- `internal/httpapi/taskcore_presenter.go`
- `internal/httpapi/taskcore_presenter_test.go`
- `internal/httpui/handler.go`
- `internal/httpui/handler_test.go`
- `internal/httpv2/postgres_task_store.go`
- `internal/httpv2/settings_handler.go`
- `internal/store/postgres/migrations/014_task_core_execution_settings.sql`
- `internal/store/postgres/settings_store.go`
- `internal/store/postgres/taskcore/store.go`
- `internal/store/postgres/taskcore/store_test.go`
- `internal/worker/taskcore/downloader.go`
- `internal/worker/taskcore/downloader_test.go`
- `internal/worker/taskcore/executor.go`
- `internal/worker/taskcore/executor_test.go`

## 2. `fix(frontend): handle errors and restored page lifecycles`

- `frontend/src/app.js`
- `frontend/src/home/api.js`
- `frontend/src/home/index.js`
- `frontend/src/home/upload_submission.js`
- `frontend/src/logs/action_controller.js`
- `frontend/src/logs/api.js`
- `frontend/src/logs/index.js`
- `frontend/src/logs/task_actions.js`
- `frontend/src/settings.js`
- `frontend/src/settings/api.js`
- `frontend/src/tests/bundle_contract.test.mjs`
- `frontend/src/tests/home_summary_panel.test.mjs`
- `frontend/src/tests/home_upload_submission.test.mjs`
- `frontend/src/tests/logs_action_controller.test.mjs`
- `frontend/src/tests/logs_task_actions.test.mjs`
- `frontend/src/tests/page_async_lifecycle.test.mjs`
- `frontend/src/tests/page_modules.test.mjs`
- `tests/e2e/specs/ui-regressions.spec.js`
- `web/static/dist/app.bundle.js`
- `web/static/dist/assets/polling-rjmliP_3.js`
- `web/static/dist/assets/server_messages-CJNRp_RB.js`
- `web/static/dist/assets/tasks_api-Bo2Ux7IN.js`
- `web/static/dist/assets/tasks_api-CMVBvp0F.js`
- `web/static/dist/index.bundle.js`
- `web/static/dist/logs.bundle.js`
- `web/static/dist/settings.bundle.js`
- `web/templates/base.html`
- `web/templates/settings.html`

## 3. `ci(release): exercise PostgreSQL and real worker artifacts`

- `.dockerignore`
- `.github/workflows/ci.yml`
- `Dockerfile`
- `docker-compose.yml`
- `go.mod`
- `package-lock.json`
- `package.json`
- `scripts/start_local.sh`
- `scripts/verify_release_gates.sh`
- `tests/e2e/browser_preflight.cjs`
- `tests/e2e/browser_preflight.test.cjs`
- `tests/e2e/run-e2e.sh`
- `tests/e2e/specs/real-upload.spec.js`
- `vite.config.js`
- `vite.config.mjs`

## 4. `docs(runtime): align contracts and rollout guidance`

- `.trellis/spec/backend/database-guidelines.md`
- `.trellis/spec/backend/index.md`
- `.trellis/spec/backend/quality-guidelines.md`
- `.trellis/spec/backend/task-runtime-contract.md`
- `.trellis/spec/frontend/hook-guidelines.md`
- `.trellis/spec/frontend/state-management.md`
- `.trellis/tasks/10-03-taskcore-optimization/check.jsonl`
- `.trellis/tasks/10-03-taskcore-optimization/commit-plan.md`
- `.trellis/tasks/10-03-taskcore-optimization/design.md`
- `.trellis/tasks/10-03-taskcore-optimization/implement.jsonl`
- `.trellis/tasks/10-03-taskcore-optimization/implement.md`
- `.trellis/tasks/10-03-taskcore-optimization/prd.md`
- `.trellis/tasks/10-03-taskcore-optimization/task.json`
- `.trellis/tasks/10-03-taskcore-optimization/validation.md`
- `README.md`
- `docs/architecture/config-inventory.md`
- `docs/architecture/current-system-overview.md`
- `docs/architecture/task-domain-model.md`
- `docs/architecture/task-lifecycle-baseline.md`
- `docs/development/module-boundaries.md`
- `docs/development/testing-strategy.md`
- `docs/runbooks/2026-10-03-taskcore-optimization.md`

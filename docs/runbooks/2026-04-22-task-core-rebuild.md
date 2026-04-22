# Task Core Rebuild Cutover Runbook

## What changes

- The rebuilt task lifecycle writes and reads canonical task state from the `task_core_*` PostgreSQL tables.
- URL tasks and upload-source tasks share the task-core lifecycle, status catalog, and backend-provided available actions used by the logs UI.
- Legacy task rows are preserved for rollback and historical reference, but they are **not migrated into the new logs view** after cutover.

## Before deploy

1. Back up the production PostgreSQL database, including legacy task tables and the new `task_core_*` tables if they already exist.
2. Run the release gates for the target commit:
   - Go unit/integration tests.
   - Frontend tests and build when frontend assets changed.
   - Playwright E2E coverage for URL, upload, logs status/actions, cancel/retry, and success actions.
3. Confirm the Docker Compose stack starts cleanly from the target commit and `/readyz` becomes healthy.

## After deploy

1. Submit a URL task from the home page.
2. Submit an upload source task from the home page.
3. Open the logs page and confirm task-core statuses and action buttons render from backend state:
   - active states such as `上传中`, `准备中`, or `运行中`;
   - terminal `成功` state;
   - cancel/retry controls where applicable;
   - success action button for download or Komga copy.
4. Confirm success actions work end to end:
   - browser download returns the generated CBZ; or
   - Komga copy writes to the configured Komga library path.

## Rollback

1. Roll back application code and containers to the previous release.
2. Keep the database in place. Legacy task tables are preserved by the cutover.
3. The rolled-back code ignores `task_core_*` tables; they can remain for later inspection or forward-fix retries.

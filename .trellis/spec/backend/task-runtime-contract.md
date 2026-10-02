# Task Core execution and upload contracts

## 1. Scope / Trigger

Changes to lifecycle, worker, upload, settings, artifacts or CI cross application,
SQL, files and HTTP boundaries. Use this contract with the existing database guide.

## 2. Signatures

- `CreateURLTask(ctx, CreateURLInput) (CreateURLResult, error)` includes `Force` and `RuntimeSettings`; result embeds Task and exposes Reused/NeedsConfirmation/Result.
- `Execute(ctx, app.Task)` receives a claimed lease owner and generation.
- Heartbeat/ReportProgress/AcknowledgeCancel/Complete/Fail carry generation.
- `QueryTasks(ctx, TaskQuery) (TaskPage, error)` filters/counts/pages in SQL; `StatusCounts` aggregates all tasks.
- Migration 014 adds `task_core_tasks.generation BIGINT`, `task_core_inputs.runtime_settings JSONB` and created_at/id ordering index.

## 3. Contracts

- `/download`: active canonical URL reuses a task atomically. Existing readable successful artifact yields HTTP 200 + duplicate/needs_confirmation/download_url; force creates anew unless an active task exists. New tasks yield 202.
- Upload init requires a ZIP/RAR/7Z filename; optional file_size is an integer <=64 MiB. Source PUT enforces actual 64 MiB, including chunked bodies.
- Extraction caps 300 images, 25 MiB/image, 500 MiB/all regular members; no-image archives fail. RAR/7Z stream through bsdtar, without full disk extraction.
- Download settings persist at task creation; retry uses the same snapshot. Supported fields: timeout, retries, image_concurrency, download_action_mode. Last field controls UI success action rather than worker execution.
- Retry requires FAILED/CANCELED and a kind-appropriate attached source (upload archive path or URL/canonical URL). UI and server consume the same qualification; no-source uploads must be initialized again.
- Claim increments generation monotonically; manual retry resets attempt only. All execution writes match owner/generation; terminal writes also match attempt. Cancel acknowledgment follows Execute exit and unpublished artifact cleanup.
- Files use DOWNLOAD_PATH/taskID/generation/displayName. Success cleanup removes upload source only after Complete; fail/cancel retain source.
- TEST_DATABASE_URL is isolated test PG; CI rejects its absence. E2E uses unique project, local URL, synthetic credentials and temporary writable volumes. APP_UID/GID must match shared directory permissions.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Invalid extension/declared upload size | 400 before task creation |
| Actual oversized upload | 413, partial file removed, no source attached |
| Archive count/size/no-image violation or cancellation | execution fails/stops, no published CBZ |
| Retry without a source | 409, no READY transition or retry action |
| Stale owner/generation/attempt | ErrConflict; no heartbeat/progress/terminal overwrite |
| Artifact outside real root including symlink | unavailable; no download/copy |
| Komga non-2xx or payload.ok != true | error feedback, no success toast |

## 5. Good/Base/Bad Cases

- Good: simultaneous canonical submissions create one active task; >10000 tasks retain complete counts.
- Base: older task without runtime_settings uses worker defaults; migration initializes generation from attempt.
- Bad: old run with same worker ID and reset attempt tries to complete after retry; generation rejects it.

## 6. Tests Required

- PostgreSQL store: concurrent dedupe/force, snapshot roundtrip, same-owner retry ABA fencing, cancellation recovery and >10000 query/count.
- Worker/archive/HTTP: Execute exit before cancel ack, settings application, same-name artifact isolation, source cleanup ordering, limits and partial upload cleanup.
- Frontend/browser: HTML413 recovery/cancel, Komga failure, cached and cache-miss history restore without stale callbacks.
- Real E2E: UI ZIP upload -> PG/worker -> SUCCEEDED -> downloaded CBZ exact image bytes/ComicInfo metadata -> Komga copy.
- Full gate: Go test/race/vet, Node tests/preflight, lint/build, tracked bundle diff and isolated Compose E2E.

## 7. Wrong vs Correct

Wrong: reset attempt to zero, then fence only by worker ID/attempt; acknowledge cancel while Execute still writes.

Correct: keep generation unchanged on retry, increment it on claim, check it on every execution write; cancel context, await Execute, remove its unpublished artifact, then acknowledge.

# Task Core execution and upload contracts

## 1. Scope / Trigger

Changes to lifecycle, worker, upload, settings, artifacts or CI cross application,
SQL, files and HTTP boundaries. Use this contract with the existing database guide.

## 2. Signatures

- `CreateURLTask(ctx, CreateURLInput) (CreateURLResult, error)` includes `Force` and `RuntimeSettings`; result embeds Task and exposes Reused/NeedsConfirmation/Result.
- Store `Retry(ctx, taskID string, progress domain.Progress) (app.Task, error)` reuses the URL identity lock before changing a terminal task to READY.
- `Execute(ctx, app.Task)` receives a claimed lease owner and generation.
- Heartbeat/ReportProgress/AcknowledgeCancel/Complete/Fail carry generation.
- `QueryTasks(ctx, TaskQuery) (TaskPage, error)` filters/counts/pages in SQL; `StatusCounts` aggregates all tasks.
- Migration 014 adds `task_core_tasks.generation BIGINT`, `task_core_inputs.runtime_settings JSONB` and created_at/id ordering index.

## 3. Contracts

- `/download`: active canonical URL reuses a task atomically. Existing readable successful artifact yields HTTP 200 + duplicate/needs_confirmation/download_url; force creates anew unless an active task exists. New tasks yield 202.
- Create and URL retry take `pg_advisory_xact_lock(hashtextextended(canonicalURL, 0))` before task row locks. Check active READY/RUNNING/CANCELING tasks in a **separate SQL statement after acquiring the lock**, so READ COMMITTED sees the prior holder's committed changes; do not combine lock acquisition and lookup in one statement/CTE. Retry excludes itself and returns ErrConflict for another active task.
- Canonical lookup keeps `i.canonical_url = $1` indexable with `idx_task_core_inputs_canonical_url`; only NULL/empty canonical values fall back to `btrim(i.url)`. Both create and retry use this predicate and identity fallback, including legacy URL-only rows. Do not wrap the indexed equality in COALESCE or normalize historical data implicitly.
- `normalizeTelegraphURL` keeps decoded `URL.Path` and escaped `URL.RawPath` consistent. Normalize literal path slashes without decoding `%2F` into a separator; spaces, Unicode and existing escapes must survive canonicalization and the worker's request without double encoding.
- Upload init requires a ZIP/RAR/7Z filename; optional file_size is an integer <=64 MiB. Source PUT enforces actual 64 MiB, including chunked bodies.
- Extraction caps 300 images, 25 MiB/image, 500 MiB/all regular members; no-image archives fail. RAR/7Z stream through bsdtar, without full disk extraction. ZIP and external tar streams share natural numeric filename ordering, including directory components; equal numbers put shorter zero padding first. Compare digit strings without integer conversion/overflow, preserve identical-name order, and retain existing CBZ output naming.
- Download settings persist at task creation; retry uses the same snapshot. Supported fields: timeout, retries, image_concurrency, download_action_mode. Last field controls UI success action rather than worker execution.
- Image request header/body timeouts use up to `1 + retries` attempts, then candidate fallback. `http.Client.Timeout` can wrap DeadlineExceeded while the task context is live: check `ctx.Err()` before stopping retries or the task. Exhausted request failures do not cancel other images; actual parent cancellation/deadline stops retries/fallback, and size limits still stop the whole download.
- Retry requires FAILED/CANCELED and a kind-appropriate attached source (upload archive path or URL/canonical URL). UI and server consume the same qualification; no-source uploads must be initialized again.
- Claim increments generation monotonically; manual retry resets attempt only. All execution writes match owner/generation; terminal writes also match attempt. Cancel acknowledgment follows Execute exit and unpublished artifact cleanup.
- Files use DOWNLOAD_PATH/taskID/generation/displayName. Success cleanup removes upload source only after Complete; fail/cancel retain source.
- `normalizeLogQuery` trims the keyword, caps it at 120 **bytes**, then removes invalid UTF-8 (including a split final rune) before SQL. Preserve this byte cap; do not replace it with 120 runes.
- TEST_DATABASE_URL is isolated test PG; CI rejects its absence. E2E uses unique project, local URL, synthetic credentials and temporary writable volumes. APP_UID/GID must match shared directory permissions.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Same submission key with changed metadata/source/hash/target | 409 `idempotency_conflict`, no new task |
| Canonical URL reuse with different keyed metadata | 409 `submission_source_conflict`, no discarded snapshot |
| Keyed upload bytes mismatch declared SHA-256 | Reject attachment, keep task recoverable |
| Komga target exists with different content | 409 `komga_target_conflict`, preserve destination |
| Komga scan accepted but record/projection unavailable | `pending`, never `verified` |
| Invalid extension/declared upload size | 400 before task creation |
| Actual oversized upload | 413, partial file removed, no source attached |
| Archive count/size/no-image violation or cancellation | execution fails/stops, no published CBZ |
| Image request timeout with live task context | retry/fallback; exhaustion returns PartialFailureError without stopping other images |
| Parent context canceled or expired | stop retries/fallback and return the context error |
| Retry without a source | 409, no READY transition or retry action |
| Retry finds another active task for the same canonical URL | 409/ErrConflict; original status, generation and events unchanged |
| Keyword crosses the 120-byte boundary inside a UTF-8 rune | discard incomplete bytes; valid UTF-8 of at most 120 bytes reaches SQL |
| Stale owner/generation/attempt | ErrConflict; no heartbeat/progress/terminal overwrite |
| Artifact outside real root including symlink | unavailable; no download/copy |
| Komga non-2xx or payload.ok != true | error feedback, no success toast |

## 5. Good/Base/Bad Cases

- Good: simultaneous create/force/retry operations leave one active canonical URL task; >10000 tasks retain complete counts. Archive pages `1, 2, 10` keep that order after CBZ renumbering.
- Base: older task without runtime_settings uses worker defaults; migration initializes generation from attempt. NULL/empty canonical URL rows deduplicate through their trimmed URL.
- Bad: old run with same worker ID and reset attempt tries to complete after retry; generation rejects it.

## 6. Tests Required

- Submission integration: same-key races, changed request conflicts, dropped create
  response recovery, replay after schema change, and keyed upload actual hash check.
- Real Komga opt-in: `bash scripts/check_komga_delivery.sh` checks exact path/IDs,
  ComicInfo readback, same-file retry, non-overwrite conflicts, and stale READY
  metadata remaining pending. Ordinary tests do not start a Komga container.
- PostgreSQL store: concurrent create/retry and retry/retry behind a held advisory lock must leave one active task; cover force, NULL/empty canonical fallback, READY/RUNNING/CANCELING conflicts without mutations, snapshot roundtrip, same-owner retry ABA fencing, cancellation recovery and >10000 query/count.
- Worker/archive/HTTP: Execute exit before cancel ack, settings application, same-name artifact isolation, source cleanup ordering, limits and partial upload cleanup. `timeout_test.go` covers header/body timeout recovery, fallback, exhausted requests retaining other images and parent cancellation. `natural_order_test.go` covers ZIP/external tar ordering with padding, nested paths, Unicode and unbounded digit runs. URL tests assert canonicalization is idempotent and the outbound request preserves encoded spaces/Unicode/separators; query tests assert valid UTF-8 and the byte cap.
- Frontend/browser: HTML413 recovery/cancel, Komga failure, cached and cache-miss history restore without stale callbacks.
- Real E2E: UI ZIP upload -> PG/worker -> SUCCEEDED -> downloaded CBZ exact image bytes/ComicInfo metadata -> Komga copy. Use distinct image bytes under shuffled `10/2/1` source names and assert output `1/2/3` maps to source `1/2/10`.
- Full gate: Go test/race/vet, Node tests/preflight, lint/build, tracked bundle diff and isolated Compose E2E.

## 7. Wrong vs Correct

Wrong: treat HTTP copy success as indexed, or create a fresh task after a lost upload-init reply.

Correct: recover the same submission receipt; report copy and verified Komga readback separately.

Wrong: reset attempt to zero, then fence only by worker ID/attempt; acknowledge cancel while Execute still writes.

Correct: keep generation unchanged on retry, increment it on claim, check it on every execution write; cancel context, await Execute, remove its unpublished artifact, then acknowledge.

Wrong: `errors.Is(err, context.DeadlineExceeded)` cancels all downloads; `URL{Path: escapedPath}` encodes `%` again; a lock-and-lookup CTE relies on a snapshot taken before waiting.

Correct: `ctx.Err()` decides task cancellation; `URL{Path: decodedPath, RawPath: escapedPath}` preserves path semantics; acquire the advisory lock before issuing a fresh lookup statement.

## Guided submission and Komga delivery

- `/download` and `/api/tasks/upload/init` accept optional `idempotency_key`
  (16–128 ASCII letters/digits/underscore/hyphen) and `delivery_target`
  (`download` or `komga`). No key retains the legacy contract. Keyed upload init
  requires `file_name`, positive `file_size`, and lowercase SHA-256
  `file_sha256`; attachment verifies filename, size, and actual bytes.
- Migration 025 creates a submission receipt in the same PostgreSQL transaction
  as the task/input/event, or canonical URL reuse. Acquire the receipt advisory
  lock before the existing URL identity lock. Same key and same reviewed input
  recovers the original task after response loss/restart; different input yields
  HTTP 409 `idempotency_conflict`. Creation-time server settings stay frozen;
  replay is checked before current registry/settings normalization.
- `GET /api/tasks/submissions/{key}` runs behind normal administrator middleware,
  returns the standard task payload and `upload_url` only for `CREATED` uploads,
  and uses `Cache-Control: no-store`. It creates nothing. New keyed URL requests
  may reuse a canonical task only with the same metadata document; differing
  documents return 409 `submission_source_conflict`, without bypassing an active
  URL even with `force`. Different keys may select different delivery targets
  for the same unchanged artifact. The target is part of key identity but does
  not itself schedule a copy; the confirmed client performs that action.
- Komga copy uses exact-content idempotency and no-replace atomic publication.
  Same target with different bytes returns 409 `komga_target_conflict`; symlinks
  are refused. The production delivery service resolves saved credentials and
  allowlisted library mapping, queues library scan, then GETs an exact-path book.
  `copy_completed`, `delivery_status` (`pending`/`indexed`), and `komga_indexed`
  (`pending`/`verified`) are separate facts. `verified` requires real book/library
  IDs, READY media, source hash, size/page count, and projected ComicInfo values.
  Accepted scan/analyze is never proof. A pending copy can resume via the same
  action without rewriting/recreating the work. Discovery and readback are
  bounded; incomplete discovery remains pending. Legacy copier-only embeddings
  explicitly report `pending`/`connection_unconfigured`.

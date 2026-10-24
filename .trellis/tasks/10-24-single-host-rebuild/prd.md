# 墨渡 PanelPort: single-host Komga preparation workflow

## Goal
Replace the four-service Go/PostgreSQL runtime with a small single-host Python application. Prepare ordered comic pages with user metadata and deliver the complete CBZ automatically into Komga's shared library. The user approved a complete rewrite, frontend redesign, and later explicitly removed browser downloads/manual delivery and separated home from history.

## Requirements
- One Python/FastAPI container, local SQLite, an exclusive application instance and one serial background executor.
- Accept a Telegraph URL or ZIP/RAR/7Z upload and seven optional metadata fields: author, series, number, title, summary, tags and genres.
- Delegate site fetching to pinned gallery-dl CLI, RAR/7Z reading to libarchive, and CBZ/XML to Python standard libraries.
- Persist queued jobs. After restart, mark interrupted running/delivering jobs failed for manual retry; do not silently rerun external effects.
- Publish automatically to the configured shared Komga directory before reporting succeeded/已入库. Browser download and manual-copy routes/buttons do not exist.
- Retain a validated generated CBZ after delivery failure for idempotent retry without re-fetching. Clear local CBZ and uploaded source only after successful delivery registration.
- Keep cancel/retry. Cancellation closes atomically before the external delivery commit; never report cancelled while still publishing a new file.
- Home is submission-only with a receipt linking to /tasks. History/progress/actions belong exclusively to /tasks, configuration exclusively to /settings.
- Configure komga_dir (required existing writable absolute path visible to this app) and komga_url (optional HTTP/HTTPS website link). Explain that a website URL alone does not transport CBZ bytes; no invented Komga upload API or scan completion claim.
- Brand is 墨渡 PanelPort; keep remote repository/image identifiers unchanged for compatibility.
- Use the highest-star verified relevant frontend skill upstream; retire duplicate design skills with recoverable backups. Keep testing/accessibility capabilities.
- Chinese, responsive, keyboard-accessible UI, safe text rendering, local static assets, useful loading/error feedback.
- Preserve upload/decompression/resource/path boundaries, argv-only tools, timeout/cancellation, per-task files and atomic no-clobber Komga publication.
- Do not mutate production PostgreSQL, existing CBZ, real Komga, remote repository or main branch.

## Acceptance
1. Real offline Telegraph fixture and real ZIP/7Z processing produce ordered image bytes and correct Chinese ComicInfo fields.
2. Automatic delivery succeeds only after Komga target bytes match; source/internal cache cleanup follows the committed success.
3. Partial/empty downloads, unsafe archives, oversized inputs and path/symlink violations never publish a partial CBZ.
4. Copy collisions preserve external content; delivery failures and interrupted delivery retain a retryable CBZ.
5. Cancel stops task subprocesses; queued tasks survive restart; active tasks become visibly interrupted/failed.
6. Browser tests verify desktop/mobile submission-only home, separate task history, automatic import, error recovery/retry/cancel and settings readback. Home makes no task-list request.
7. One-service Docker/Compose, Python CI/release gates and current specs/docs agree. Obsolete Go/PG/Vite runtime and old-only gates/tests are retired.
8. Skill source/star/license evidence, backup path, exact runtime discovery and strict post-change audit are recorded.

## Out of scope
Multi-host/HA, automatic crash reruns/resume, historical APIs, metadata history/duplicate confirmation, per-image concurrency controls, browser downloads/manual delivery, remote file transport protocols, Komga authentication/scan automation, production cutover and merge.

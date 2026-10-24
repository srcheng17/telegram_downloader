# PanelPort Automatic-delivery Contract

## Runtime and HTTP

One FastAPI process holds an exclusive DATA_DIR lock and runs a serial executor. SQLite queued work survives restart; interrupted running/delivering jobs fail for manual retry.

- GET /: submit-only home; GET /tasks: history; GET /settings: Komga configuration.
- POST /api/jobs/url accepts JSON/form; POST /api/jobs/upload accepts multipart file and metadata.
- GET /api/jobs and /api/jobs/{id} query jobs; POST cancel/retry are the only actions.
- GET/POST /api/settings returns {komga_url, komga_dir, configured}. Saved settings override initial KOMGA_DIR.
- komga_dir must be an existing writable absolute shared directory visible to the app. Unconfigured submission is 409.
- komga_url is an optional HTTP/HTTPS website link only. No official Komga CBZ upload API, API credentials or scan guarantee.
- Public jobs expose filename, status_label, phase/error and available_actions. No download/manual-Komga routes.

## Lifecycle and Files

queued -> running -> delivering -> succeeded. Cancellation is allowed only before delivering, with an atomic begin_delivery check. The short delivery commit is noncancellable. succeeded means 已入库, not only CBZ generated.

Use fixed gallery-dl argv, isolated work directories, timeout/cancel and temporary disk budget. ZIP reads per member; RAR/7Z use bsdtar tar stream. Never extractall. Reject unsafe paths, links/special entries; enforce 64 MiB upload, 300 images, 25 MiB/image and 500 MiB/all regular members.

Verify a positive complete image sequence and types/bytes. Write temporary CBZ then atomically publish internally; copy atomically into the configured Komga series directory (tankobon fallback). Check source/target real containment including symlinks. Do not overwrite a different same-name file; same bytes permit idempotent retry.

Delivery failure preserves internal CBZ/filename for retry without re-grabbing. Success removes internal CBZ and input source. Running cancellation stops subprocesses before final state; generated or partial media is never exposed via browser.

## Checks

Test configuration and 409, setting precedence, cancellation/delivery race, interrupted delivery, cached retry, byte/XML output, cleanup, unsafe paths and old-route 404. Browser/container tests perform actual auto-delivery using temporary shared directories and no production paths.

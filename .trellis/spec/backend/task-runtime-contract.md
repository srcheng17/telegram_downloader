# PanelPort Automatic-delivery Contract

## Runtime and HTTP

One FastAPI process holds an exclusive DATA_DIR lock and runs a serial executor. SQLite queued work survives restart; interrupted running/delivering jobs fail for manual retry.

- GET /: submit-only home; GET /tasks: history; GET /settings: Komga configuration.
- POST /api/jobs/url accepts JSON/form; POST /api/jobs/upload accepts multipart file, library_id and the seven metadata fields. The library selector is never part of ComicInfo metadata.
- GET /api/jobs and /api/jobs/{id} query jobs; POST cancel/retry are the only actions.
- GET /api/settings returns {komga_url, libraries:[{id,name,path}], komga_dir, configured}; komga_dir is the first-path compatibility alias. POST accepts libraries with server-generated IDs for new entries, or legacy single komga_dir. Saved settings override initial KOMGA_DIR.
- Library paths must be existing writable absolute shared roots visible to the app; normalized roots cannot duplicate or overlap. Library IDs/names are unique and bounded. These entries correspond to independently configured Komga libraries; they do not create remote libraries.
- Submission resolves library_id against server configuration and persists a private destination snapshot. A sole library permits omitted selection for compatibility; multiple require an explicit valid ID. Unconfigured submission is 409; missing/unknown selection is rejected before creating inputs/jobs. Queued and retrying tasks keep the original snapshot even after settings change or deletion.
- komga_url is an optional HTTP/HTTPS website link only. No official Komga CBZ upload API, API credentials or scan guarantee.
- Public jobs expose filename, library_id/library_name, status_label, phase/error and available_actions; never expose the private destination path. No download/manual-Komga routes.

## Lifecycle and Files

queued -> running -> delivering -> succeeded. Cancellation is allowed only before delivering, with an atomic begin_delivery check. The short delivery commit is noncancellable. succeeded means 已入库, not only CBZ generated.

Use fixed gallery-dl argv, isolated work directories, timeout/cancel and temporary disk budget. ZIP reads per member; RAR/7Z use bsdtar tar stream. Never extractall. Reject unsafe paths, links/special entries; enforce 64 MiB upload, 300 images, 25 MiB/image and 500 MiB/all regular members.

Verify a positive complete image sequence and types/bytes. Write temporary CBZ then atomically publish internally; copy atomically into the job's saved Komga root/series directory (tankobon fallback). Never reread live settings to choose its destination. Check source/target real containment including symlinks. Do not overwrite a different same-name file; same bytes permit idempotent retry.

Saved library roots are already canonical. Reject a stored root whose resolution changes, including a root/ancestor replaced by a symlink; never silently resolve it to another library at selection or copy time. New explicit settings may normalize a path before saving. Keep the canonical root unchanged through final path validation/publication.

Delivery failure preserves internal CBZ/filename for retry without re-grabbing. Success removes internal CBZ and input source. Running cancellation stops subprocesses before final state; generated or partial media is never exposed via browser.

## Checks

Test configuration and 409, two-library routing/selection errors, non-overlapping roots, version-1 migration, target stability through settings changes and cached retry, setting precedence, cancellation/delivery race, interrupted delivery, byte/XML output, cleanup, unsafe paths and old-route 404. Browser/container tests perform actual auto-delivery using temporary shared directories and no production paths.

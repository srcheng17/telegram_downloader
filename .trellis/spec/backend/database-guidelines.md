# Database Guidelines

SQLite via Python sqlite3 is the current local persistence backend. Use short per-operation connections/transactions and parameterized SQL; do not interpolate user data. Store file metadata in SQLite and bytes under DATA_DIR.

- Atomically claim queued work and arbitrate cancellation versus entry to delivering. The short delivery commit cannot be cancelled.
- Persist input before making a task executable.
- Startup recovery runs only after acquiring the exclusive DATA_DIR instance lock.
- Preserve queued rows; mark interrupted running/delivering rows failed for manual retry.
- Do not hold a database transaction while downloading, unpacking or copying files.
- Store komga_url/komga_dir and jobs here. Saved settings override the initial environment directory; do not add another queue/database.
- Preserve internal artifact_path/filename after delivery failure for retry without re-fetching. Successful Komga delivery clears the internal cache reference before cleanup.
- Schema changes need an explicit version/compatibility decision and regression test; do not silently discard existing rows.
- Use temporary test databases, never the application's persistent file.

Only one application process may use the data directory. Network filesystems, multiple Uvicorn workers and horizontal replicas are outside the runtime contract. For backups, stop the app and copy DATA_DIR or use SQLite backup API; copying only the live main file can omit WAL data.

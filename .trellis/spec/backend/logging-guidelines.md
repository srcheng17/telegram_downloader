# Logging Guidelines

Use Python's standard logging facilities. Keep startup failures at the entry point and map request failures to user-facing HTTP responses.

Record task ID and operation context needed to diagnose failure. Never log full requests, uploaded content, database/config dumps, credentials, private source URLs or raw downloader output that may contain those values.

Return concise Chinese errors to the browser. Detailed traces belong only in controlled internal diagnostics; routine cancellation is not an unexpected exception. Test that external tool errors are sanitized before storage/API display.

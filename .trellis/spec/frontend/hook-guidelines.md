# Async Requests

Use ordinary browser navigation and native events. No React hooks or retired htmx lifecycle.

- Form submission uses fetch and multipart FormData or the URL JSON/form contract.
- Check HTTP status and response shape; handle HTML/non-JSON failure bodies.
- Busy state prevents accidental repeated submit but always releases after failure.
- Poll task data without overlapping requests; clear timers/abort requests when appropriate.
- Ignore superseded results and preserve the user's current form/focus.
- Do not report cancellation as complete merely because the request was accepted.
- Surface automatic-delivery errors on the task page. Display 已入库 only from succeeded; delivering is a noncancellable short commit phase.

Use backend status labels and available_actions (cancel/retry only). Frontend requests match /api/jobs and /api/settings; do not add download/manual-copy routes or legacy fallbacks. komga_url only opens the configured HTTP/HTTPS website; shared komga_dir performs actual file delivery.

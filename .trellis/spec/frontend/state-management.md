# State Management

State lives in plain page-module objects and small state/view-model functions; there is no state-library dependency.
See `frontend/src/home/index.js`, `frontend/src/logs/index.js`, and `frontend/src/settings.js`.

## Existing State Boundaries

- Page modules own handlers, current roots, in-flight requests, polling timers, and transient UI state.
- Preserve existing `win.__telegraphHomeState`, `win.__telegraphLogsState`, and settings state where used.
- `frontend/src/home/state.js` collects form fields; `frontend/src/logs/state.js` builds filters/query strings.
- Logs form controls hold the draft; `state.filters` holds applied query conditions. Submit reads/normalizes the draft and starts page 1; polling and pagination use only applied filters. A response may update applied filters from `data.filters`, but must not call `syncFormWithFilters` or replace the user's current status selection while rebuilding catalog options.
- Synchronize logs form fields only on mount, explicit submit, or clear/reset. History remount retains existing lifecycle reset behavior; edits made while a fetch is pending survive its response. Verify query and status drafts, submit and clear using the real module fixture in `logs_polling.test.mjs`.
- `frontend/src/settings/state.js` applies successful server snapshots and caches the download-action mode.
- Derive logs presentation in `frontend/src/logs/view_model.js`, keeping render functions separate.

The existing logs query uses the native URL builder:

```js
const params = new URLSearchParams();
params.set('page', String(page));
params.set('per_page', String(perPage));
```

## Task Contracts

Consume backend `status_label`, progress, and `available_actions`; do not add frontend lifecycle rules.
Current states/actions are defined in `internal/domain/taskcore/status.go` and presented by `internal/httpapi/taskcore_presenter.go`.
Older lifecycle documents contain legacy statuses; consult current source when task payloads change.
Reset stale filters, handlers, and request state through the existing mount/unmount paths.
Test mappings with `frontend/src/tests/logs_view_model.test.mjs` and `frontend/src/tests/settings_state.test.mjs`.

Supported settings are timeout, retries, image_concurrency and download_action_mode.
New-task download settings are captured by the backend; UI action mode may change
for existing successful results. Do not expose task concurrency or retention
controls without a corresponding runtime implementation.

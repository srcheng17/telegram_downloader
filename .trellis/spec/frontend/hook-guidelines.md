# Lifecycle Events and Async Requests

This project has no React hooks. Stateful behavior uses page factories, DOM events, and plain functions.
The lifecycle is in `frontend/src/app.js`, `shared/app_lifecycle.js` and `shared/page_modules.js`.
See [workspace lifecycle](./workspace-lifecycle.md) for administrator gating and sensitive cleanup.

## Navigation

- On `DOMContentLoaded`, initialize the administrator session, then mount business modules only after authentication verifies; synchronize navigation.
- On `htmx:beforeSwap` for `#content`, unmount only if `detail.shouldSwap !== false` and the event is not prevented. Failed HTTP navigation still emits beforeSwap while retaining the current DOM; it must not disable that page.
- On `htmx:afterSwap` for `#content`, synchronize navigation and mount modules.
- On `htmx:historyRestore`, unmount and revalidate the administrator session before remounting. Keep `hx-history-elt` and `hx-history="false"` on protected `#content`; do not cache sensitive drafts in browser history.
- Keep event registration and cleanup paired; see `frontend/src/home/index.js` and `frontend/src/logs/index.js`.

## Requests and Polling

Use `frontend/src/shared/api/tasks_api.js`; it returns `{ response, payload }` and tolerates non-JSON responses.
Page API adapters specify endpoints and request options, as in `frontend/src/logs/api.js`:

```js
getLogs(url, options = {}) {
    return api.getJson(url, { cache: 'no-store', ...options });
}
```

- Inspect `response.ok` and payload availability before applying a response; expose recoverable errors in-page.
- Reuse `resolvePollDelay` from `frontend/src/shared/polling.js` for active/idle/hidden delays and failure backoff.
- Follow logs polling: every fetch first clears/nulls the old timer, then aborts the superseded request. A pending page/filter request must not be interrupted by a timer scheduled for the previous response.
- Only a non-aborted request with `state.inflightController === controller` may apply results/errors, update failures, or schedule the next poll in `finally`. Scheduling requires the same mounted root and no in-flight request; hiding the page must not install a competing timer during a slow fetch. Use the injected `win.setTimeout`/`win.clearTimeout`; unmount clears timers and aborts/nulls controllers.
- Check `frontend/src/tests/polling.test.mjs`, `page_modules.test.mjs` and `logs_polling.test.mjs`. Assert slow pagination is not aborted by an old timer; stale/unmounted/remounted responses cannot render or reschedule; failure backoff and visibility changes leave at most one poll timer.

Use AbortController and a mount version guard for action/settings hydration responses.
XHR upload handlers must settle on HTTP errors (including HTML), error/timeout/abort
and remove signal listeners. Once init created a task, upload failure must request
cancel independently of the aborted page signal; do not leave CREATED rows behind.

# Lifecycle Events and Async Requests

This project has no React hooks. Stateful behavior uses page factories, DOM events, and plain functions.
The existing lifecycle is in `frontend/src/app.js` and `frontend/src/shared/page_modules.js`.

## Navigation

- On `DOMContentLoaded`, mount registered page modules and synchronize navigation.
- On `htmx:beforeSwap` for `#content`, unmount modules before replacing page DOM.
- On `htmx:afterSwap` for `#content`, synchronize navigation and mount modules.
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
- Follow logs polling: abort superseded requests, avoid overlap, and clear polling on unmount.
- Check `frontend/src/tests/polling.test.mjs` and `frontend/src/tests/page_modules.test.mjs`.

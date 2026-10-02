# Runtime Type Safety

Frontend code is JavaScript ES modules; there is no TypeScript compiler or schema-validation dependency.
`.eslintrc.cjs` enables `eslint:recommended` with module parsing for `frontend/src/**/*.js`.

## Boundary Normalization

- Normalize strings and optional form fields in `frontend/src/home/state.js` and `frontend/src/logs/state.js`.
- Reuse `normalizeStatusCode` / `buildStatusCatalog` from `frontend/src/shared/models/status_catalog.js`.
- Check nullable DOM nodes and response payloads before reading properties.
- Use existing numeric/finite checks where required; see `frontend/src/shared/polling.js` and settings state.

`frontend/src/logs/view_model.js` guards untyped payloads before deriving display data:

```js
const task = log && typeof log === 'object' ? log : {};
const availableActions = Array.isArray(task.available_actions)
    ? task.available_actions.slice()
    : Array.isArray(task.availableActions)
      ? task.availableActions.slice()
      : [];
```

Preserve API field names and compatibility fallbacks when editing view models; validate malformed/empty input in Node tests.
References: `frontend/src/tests/status_catalog.test.mjs`, `frontend/src/tests/logs_view_model.test.mjs`, and `frontend/src/tests/settings_state.test.mjs`.
Do not introduce TypeScript assertions, a validation framework, or duplicated status/action catalogs for this stack.

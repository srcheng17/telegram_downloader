# Page Modules and DOM Rendering

Pages are Go templates enhanced by native JavaScript modules, not React components.
Use `web/templates/`, `web/static/style.css`, and the existing page factories.

## Module Lifecycle

`createHomeModule`, `createLogsModule`, and `createSettingsModule` accept `win` / `doc` for testability.
They expose the existing lifecycle contract, as in `frontend/src/settings.js`:

```js
return { mount, unmount };
```

- `mount()` must tolerate a missing page root and repeated calls without duplicating listeners.
- `unmount()` removes registered listeners, clears timers, and aborts owned requests where applicable.
- `frontend/src/shared/page_modules.js` dispatches lifecycle calls during navigation.
- Keep presentation transforms in existing view-model/render files; see `frontend/src/logs/view_model.js`.

## Rendering and Accessibility

- Put server/user text in `textContent`, as `frontend/src/logs/table_render.js` does for status labels.
- Keep product feedback Chinese; reuse `frontend/src/shared/server_messages.js` for known API messages.
- Preserve existing template IDs consumed by page modules and shared styles in `web/static/style.css`.
- Follow `web/templates/logs.html`: labeled buttons, table headers, live feedback, and an accessible dialog.
- Preserve Escape handling, focus trapping/restoration, and visibility cleanup in `frontend/src/logs/index.js`.
- Verify lifecycle behavior with `frontend/src/tests/page_modules.test.mjs` and page-specific tests.

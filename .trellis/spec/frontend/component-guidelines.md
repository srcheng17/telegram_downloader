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
- Keep nonempty field help in normal flow while focus changes. Toggling a hint's
  `display` on `:focus-within` can move the next control between mousedown and mouseup:
  clicking a collapsed metadata summary then fails to open it. Hide empty help only;
  cover typing in aliases followed by one real click on the custom-field summary in E2E.

## Responsive workbench order and touch targets

- The guided home keeps source → evidence on the materials step and metadata → delivery → final confirmation on the review step. Inactive steps remain mounted but hidden and inert. Keep visible DOM, Tab and screen-reader order aligned; do not use flex `order` to rearrange them. Focus the step heading after advancing/back, and preserve all controls when moving between steps.
- At mobile widths, the navigation open/close buttons and each visible source-mode label need at least a 44×44 CSS pixel hit area. The navigation drawer must fit inside a 180px CSS viewport (a 360px phone at 200% zoom), with its close button fully visible and clickable. `tests/e2e/specs/ui-shell.spec.js` and `new-task-mobile.spec.js` assert these bounds and focus order.
- When a field moves into a closed `<details>` group, browser flows must open its `summary` before filling or clicking that field. Keep the editor state and payload unchanged; the disclosure only changes presentation.

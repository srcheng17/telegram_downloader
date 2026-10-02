# Directory Structure

The runtime layout is documented in `README.md` and `docs/architecture/current-system-overview.md`.

```text
frontend/src/
  app.js                       htmx navigation and page lifecycle
  index.js, logs.js, settings.js  bundle entry points
  home/, logs/, settings/       page behavior, API adapters, state, and view models
  shared/                      API transport, polling, messages, and shared models
  tests/                       Node test runner tests (*.test.mjs)
web/templates/                 Go HTML templates
web/static/style.css           shared page styles
web/static/dist/               generated Vite bundles and shared chunks
```

## Placement Rules

- Keep page-specific behavior in its existing page directory; share only actual cross-page logic.
- Existing files use descriptive snake_case names such as `logs/view_model.js` and `home/form_submission.js`.
- Page entry points create/register modules; composition belongs in `home/index.js` or `logs/index.js`.
- Reuse `shared/api/tasks_api.js` for transport and `shared/page_modules.js` for lifecycle dispatch.
- Follow `docs/development/module-boundaries.md`: frontend presents backend semantics, not task rules.

## Build Boundary

`vite.config.js` builds `app`, `index`, `logs`, and `settings` into `web/static/dist/`.
`web/templates/base.html` loads those bundles; do not import raw `frontend/src/` paths into templates.
Run `npm run build` and include generated output whenever frontend source changes.

# Frontend Quality Guidelines

Follow `docs/development/testing-strategy.md` and `docs/development/module-boundaries.md`.
Behavior changes should start with a focused failing test; reuse existing tests and plain Node assertions.

## Required Checks for Frontend Code Changes

```bash
npm run test:frontend
npm run lint
npm run build
```

`package.json` defines these commands; Node tests live in `frontend/src/tests/*.test.mjs`.
Prefer state/view-model/action tests with small injected DOM/API doubles over full browser setup for every detail.
Examples: `frontend/src/tests/page_modules.test.mjs` and `frontend/src/tests/logs_view_model.test.mjs`.

## Review Checklist

- Source changes include rebuilt `web/static/dist/` output; verify `frontend/src/tests/bundle_contract.test.mjs`.
- Repeated mount/unmount does not leave listeners, polling, or stale requests behind.
- User-controlled display text uses safe DOM APIs; Chinese feedback and accessible controls remain usable.
- Task actions and labels follow current backend payloads, with existing compatibility fallbacks preserved.
- Shared functions are reused instead of copying polling, API transport, or status normalization.

`npm run e2e:test` runs Playwright against an isolated temporary Compose stack; reserve it for affected critical user flows.
`scripts/verify_release_gates.sh` is the full release gate and includes E2E; record environment-limited checks honestly.

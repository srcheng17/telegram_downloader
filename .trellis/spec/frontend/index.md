# Frontend Development Guidelines

Scope: `frontend/src/`, `web/templates/`, and `web/static/` in this single-repository Go application.
The frontend uses native JavaScript ES modules, htmx, Go templates, and Vite; it is not React or TypeScript.
Existing project conventions live in `README.md` and `docs/development/module-boundaries.md`.

## Guidelines Index

| Guide | Read for |
| --- | --- |
| [Directory Structure](./directory-structure.md) | Page modules, templates, and generated bundles |
| [Component Guidelines](./component-guidelines.md) | DOM rendering, module lifecycle, and accessibility |
| [Hook Guidelines](./hook-guidelines.md) | htmx events, API calls, polling, and cleanup |
| [State Management](./state-management.md) | Page state, settings snapshots, and backend task semantics |
| [Type Safety](./type-safety.md) | Runtime normalization of JavaScript inputs |
| [Quality Guidelines](./quality-guidelines.md) | Tests, lint, build, and review checks |

## Pre-Development Checklist

- Read directory and quality guidelines before changing frontend files.
- Read component and hook guidelines for DOM, event, or request changes.
- Read state and type guidelines when changing forms or API payloads.
- Read `.trellis/spec/guides/index.md`; trace API contracts into the backend when they change.
- Search existing page/shared modules before adding helpers; inspect the relevant Node tests.

## Quality Check

- Follow `docs/development/testing-strategy.md`; verify changed behavior with focused tests.
- Run `npm run test:frontend`, `npm run lint`, and `npm run build` for frontend code changes.
- Include updated `web/static/dist/` bundles with source changes; check Chinese feedback and lifecycle cleanup.
- Use current Task Core source contracts when older architecture documents describe legacy states.

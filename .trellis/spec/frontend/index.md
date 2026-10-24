# Frontend Development Guidelines

Scope: `telegraph_downloader/templates/` and `telegraph_downloader/static/`. The UI is Jinja HTML, CSS and native JavaScript served directly by FastAPI. No Node/Vite, React, generated bundles or htmx page lifecycle.

## Before Development

Read [module boundaries](../../../docs/development/module-boundaries.md), the task design, [directory](./directory-structure.md), [components](./component-guidelines.md) and [quality](./quality-guidelines.md). For requests/state changes read the corresponding guide and trace backend payloads. Read [shared guides](../guides/index.md).

| Guide | Scope |
| --- | --- |
| [Directory](./directory-structure.md) | Templates and static assets |
| [Components](./component-guidelines.md) | Safe rendering, Chinese UI and accessibility |
| [Async requests](./hook-guidelines.md) | Forms, fetch and polling |
| [State](./state-management.md) | Backend actions and local state |
| [Type safety](./type-safety.md) | Boundary validation |
| [Quality](./quality-guidelines.md) | Python/browser checks |

The product is 墨渡 PanelPort. Home only submits new tasks; /tasks owns history and /settings owns Komga configuration. No browser download or manual-copy controls.

Browser tests cover desktop/mobile, saved shared-directory settings and actual upload-to-auto-Komga delivery. Static source is runtime source; no rebuild artifacts are required.

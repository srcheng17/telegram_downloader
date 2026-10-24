# Rendering and Accessibility

Use Jinja's escaping and DOM textContent for user/server text; never interpolate task titles, errors, filenames or metadata into unsafe HTML. Controls and feedback are Chinese.

- Use semantic landmarks, headings, lists/tables and real buttons/links.
- Label every field, preserve keyboard focus visibility and give icon buttons accessible names.
- Errors/loading/progress use readable in-page feedback with suitable live regions.
- Restore submission controls after network, HTTP or parsing failure.
- Support narrow mobile screens without horizontal page overflow.
- Center the main frame within the space remaining beside the sidebar. Share panel gutters across forms and task content; align stacked library inputs without reserving a separate action column. Reset native legend padding so headings follow the same text edge.
- Desktop navigation and main content share a real equal-height grid row; keep sticky behavior on the navigation's inner wrapper. The outer sidebar must reach the full document bottom on long pages. Mobile restores a compact normal-flow top header.
- Do not replace user input during polling or move focus unexpectedly.
- A dialog, if needed, must handle Escape and focus correctly; avoid one when inline feedback is sufficient.
- Use local/system fonts and assets; no external font/CDN request is needed.
- Keep the shared workspace visually restrained: light navigation, system sans-serif typography, compact outline icons, neutral form surfaces and one action accent. Source segments remain native labeled radios with a visible keyboard focus state; they do not require tab-widget JavaScript.

Consume backend action eligibility; UI component visibility must not create new task rules.

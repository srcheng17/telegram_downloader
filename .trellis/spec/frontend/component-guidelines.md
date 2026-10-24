# Rendering and Accessibility

Use Jinja's escaping and DOM textContent for user/server text; never interpolate task titles, errors, filenames or metadata into unsafe HTML. Controls and feedback are Chinese.

- Use semantic landmarks, headings, lists/tables and real buttons/links.
- Label every field, preserve keyboard focus visibility and give icon buttons accessible names.
- Errors/loading/progress use readable in-page feedback with suitable live regions.
- Restore submission controls after network, HTTP or parsing failure.
- Support narrow mobile screens without horizontal page overflow.
- Do not replace user input during polling or move focus unexpectedly.
- A dialog, if needed, must handle Escape and focus correctly; avoid one when inline feedback is sufficient.
- Use local/system fonts and assets; no external font/CDN request is needed.

Consume backend action eligibility; UI component visibility must not create new task rules.

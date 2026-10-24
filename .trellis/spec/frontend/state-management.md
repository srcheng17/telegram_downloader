# State Management

SQLite owns job state, cancel/retry eligibility and Komga settings. Frontend owns form mode/file, busy controls and local feedback.

Home only submits; do not place history/recent-task previews there. Task history is /tasks. Settings stores komga_url and komga_dir with configured response; there is no default_action or manual destination mode.

komga_url is an optional HTTP/HTTPS site link. Actual automatic delivery requires the existing writable shared directory visible inside the app (typically /komga in Docker). Do not present URL-only setup as configured transfer or add API-key/scan fields.

Use status_label, phase, error, filename and available_actions. Only cancel/retry actions exist. delivering shows 入库中 without cancel; succeeded shows 已入库 after actual copy. Delivery failure retains internal CBZ and retry uses it; success cleans cache/source. Never add browser download or manual-copy controls.

Metadata fields remain author, series_name, series_number, comic_name, summary, tags and genres. Tags/genres are independent. Keep local state small; no global legacy state, duplicate confirmation or metadata history.

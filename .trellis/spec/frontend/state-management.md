# State Management

SQLite owns job state, cancel/retry eligibility and Komga settings. Frontend owns form mode/file, busy controls and local feedback.

Home only submits; do not place history/recent-task previews there. Task history is /tasks. Settings stores komga_url and libraries [{id,name,path}] with configured response; komga_dir remains a legacy alias. Preserve returned IDs when editing library rows. There is no default_action or manual destination mode.

komga_url is an optional HTTP/HTTPS site link. Actual automatic delivery requires existing writable shared directories visible inside the app (for example sibling roots /komga/日漫 and /komga/国漫). Each entry corresponds to an independently configured Komga library; names alone do not create libraries. Do not present URL-only setup as configured transfer or add API-key/scan fields.

Home submits library_id separately from metadata. Select a sole library automatically; multiple libraries require an explicit selection. Task rows render the saved library_name from the backend, preserving historical choice after later settings changes. Keep errors and unsaved library-row input visible on save failure; render names/paths with escaped templates or DOM text/value APIs.

Use status_label, phase, error, filename and available_actions. Only cancel/retry actions exist. delivering shows 入库中 without cancel; succeeded shows 已入库 after actual copy. Delivery failure retains internal CBZ and retry uses it; success cleans cache/source. Never add browser download or manual-copy controls.

Metadata fields remain author, series_name, series_number, comic_name, summary, tags and genres. Tags/genres are independent. Keep local state small; no global legacy state, duplicate confirmation or metadata history.

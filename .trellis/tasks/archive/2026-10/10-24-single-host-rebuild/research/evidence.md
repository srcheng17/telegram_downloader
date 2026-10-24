# Verified evidence

- Existing deployment is nginx + go-api + go-worker + PostgreSQL, while nginx routes to the same API.
- gallery-dl 1.32.17 isolated tests cover normal12 images, 404 partial CBZ, empty exit0, SIGINT partial output, and single-run info.json expected count.
- SQLite-only subprocess kill test preserves running/queued rows; startup policy marks running failed and keeps queued.
- Huey3.5.3 pending persistence works, but dequeued running task disappears after SIGKILL; adding it would still require business recovery state.
- Prior evidence paths: /var/folders/0g/bsmpgt_x26z50l2w874_8k_h0000gn/T/gallery-dl-evidence-tsukp79u and /var/folders/0g/bsmpgt_x26z50l2w874_8k_h0000gn/T/telegraph-queue-research-4i3eh659.
- User approved full rewrite and frontend redesign; single-host scope allows dropping historical API and cosmetic feature parity.

# Frontend Quality Guidelines

Follow [testing strategy](../../../docs/development/testing-strategy.md) and the selected frontend skill/task design.

```bash
python -m ruff check telegraph_downloader tests
python -m pytest -m 'not e2e'
python -m pytest -m e2e
```

Python Playwright browser tests verify desktop/mobile, labeled forms, keyboard use, submit-only home, separate task history, saved Komga settings and actual upload-to-automatic-delivery. Cover delivery failures/cache retry and queued/running cancellation; delivering is noncancellable. Use temporary data roots; mock only isolated interaction failures.

Check safe text rendering, focus visibility, mobile overflow, honest loading/cancellation feedback, HTTP/business error handling and input preservation. No npm test/lint/build or generated bundle comparison is part of the runtime.

Templates/CSS/JS are copied into the image; run isolated container smoke for packaging changes. Report actual browser/Docker results and any environment limitation.

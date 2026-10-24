# Frontend Quality Guidelines

Follow [testing strategy](../../../docs/development/testing-strategy.md) and the selected frontend skill/task design.

```bash
python -m ruff check telegraph_downloader tests
python -m pytest -m 'not e2e'
python -m pytest -m e2e
```

Python Playwright browser tests verify desktop/mobile, labeled forms, keyboard use, submit-only home, separate task history, saved Komga settings and actual upload-to-automatic-delivery. Cover delivery failures/cache retry and queued/running cancellation; delivering is noncancellable. Use temporary data roots; mock only isolated interaction failures.

Check safe text rendering, focus visibility, mobile overflow, honest loading/cancellation feedback, HTTP/business error handling and input preservation. No npm test/lint/build or generated bundle comparison is part of the runtime.

No horizontal overflow alone does not prove layout alignment. Measure shared outer edges, balanced sidebar-relative gutters, cross-page form insets, stacked field edges and task/action insets at mobile, tablet and wide-screen breakpoints. Include widths around the main frame's max-width transition, then inspect fresh screenshots; focus outlines are not border-box misalignment.

Also compare the actual desktop sidebar/main top and bottom on long pages. Scroll to the document bottom and verify the navigation remains visible; on mobile verify the page heading is visible without a viewport-height empty header. Full-page screenshots must show the sidebar column continuing alongside the whole page.

Templates/CSS/JS are copied into the image; run isolated container smoke for packaging changes. Report actual browser/Docker results and any environment limitation.

# Alignment validation

Verified 2026-10-24 on `refactor/single-host-workspace`.

## Evidence and fix

The original screenshots and live browser reproduced four shared-layout defects: 1920px main gutters were 185/217px; 1024px home/settings input left edges were 244/251px; 390px library name/path right edges were 299/351px; native legend padding indented text by 2px. Mobile navigation also started at 22px while page content started at 20px. The new browser regression failed on that original 320px navigation mismatch before implementation.

The sidebar width now defines body space and the main frame centers automatically within what remains. A shared panel gutter controls home/settings/task content across breakpoints. Legend padding is explicit. Library remove actions sit beside the name label, leaving only the input columns; both inputs occupy the same full width on mobile. Wrapped task actions remain aligned to the right content edge. No API, database, directory, business state or dependency changed.

Final measured examples: 1920px main outer gaps 201/201px; 1024px both form left edges 244px; 390px website, library name and path fields all span 39–351px.

## Checks actually run

- Ruff for application/tests: passed.
- Non-e2e suite: 95 passed.
- Browser suite: 12 passed, including the original four real business scenarios and eight alignment cases at widths 320, 390, 650, 768, 1024, 1440, 1536 and 1920.
- Alignment cases measure home/settings/tasks outer edges, sidebar-relative centering, navigation gutters, cross-page form insets, library field edges and top/bottom alignment, and actual task/action insets, with 1px tolerance. Actual temporary ZIP inputs prevent empty-list coverage gaps. Focus outlines are not treated as border-box failures.
- JavaScript syntax and git whitespace checks: passed. One existing upstream Starlette/httpx deprecation warning remains.
- Root and independent reviewer visually inspected fresh mobile/tablet/wide screenshots. No confirmed remaining alignment or accessibility issue.

Screenshots: `test-results/alignment-{390,1024,1920}-{home,settings,tasks}.png`. They are real unedited browser captures, with focus moved off form controls before capture. The existing local temporary preview at `http://127.0.0.1:49360` serves the updated static CSS/template.

## Limits

No production deployment or real Komga operation. Container smoke was not repeated because packaging and runtime paths are unchanged; CSS/templates are served directly and browser tests use the real application. Safari/Firefox were not separately run. Preserve the user's pre-existing `/progress/` ignore change outside all commits.

# Equal-height navigation and content columns

## Goal
Correct the screenshot defect explicitly identified after the prior input-alignment fix: the left navigation column ends at the viewport bottom while the right page continues. Treat this as a two-column layout issue, not another form-gutter adjustment.

## Evidence
At 1920x1000, the left fixed sidebar is 1000px high but main/document is about1318px. The previous geometry tests only compared horizontal edges and missed sidebar/main height. A separate question about the menu/title start baseline remains open; do not guess that spacing in this fix.

## Scope
Use an actual shared equal-height desktop grid with a stretched sidebar outer column and a sticky inner navigation wrapper. Preserve the current sticky-menu experience, widths, gutters, mobile top navigation, accessibility and all business behavior. Only base.html, app.css, narrow browser regression and relevant layout guidance change. No background-only masking, screenshot editing, backend/config changes or production deployment.

## Acceptance
- Desktop sidebar/main share top and bottom at both short and long page heights.
- Scrolling a long page keeps navigation inside the viewport while the sidebar background continues to the document bottom.
- Home, settings and tasks share the equal-height layout; existing responsive gutter alignment remains correct.
- Mobile navigation remains a compact top header, with no sidebar-sized empty grid column, no sticky viewport-height header and no horizontal overflow.
- Inspect fresh unedited viewport/full-page screenshots of the actual app before claiming the columns are aligned.

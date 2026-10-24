# Align page gutters and library form controls

## Goal
Fix the visible left/right alignment defects the user identified in the delivered desktop/mobile screenshots, preserving the existing design and functionality.

## Confirmed causes
- Main content caps at 1290px plus a 228px sidebar, but wide-screen centering switches at 1550 rather than 1518; at 1920 the outer gaps differ by 32px.
- At tablet widths home uses 23px panel padding while settings uses 30px, shifting input left edges by 7px across pages.
- The remove-action grid column consumes 54px desktop / 52px mobile, so directory/name right edges fail to align with the rest of the form.
- Native legend padding adds a 2px title indentation. Mobile navigation and page gutters also differ by 2px.

## Scope
Only shared CSS, the library settings row markup needed to place its remove action outside the input grid, and real-browser layout verification/screenshots. No API, database, Komga settings, production service, dependency or page-functionality changes.

## Acceptance
1. Main content is centered in the area to the right of the sidebar at wide desktop widths, and page headings/content/footer share outer edges.
2. Home and settings use the same inner left inset at desktop/tablet/mobile widths.
3. Directory fields align with the website field right edge; on mobile both library name and path share the full input width.
4. Legend text has no native horizontal padding offset, and the mobile navigation shares the main horizontal gutter.
5. Capture and inspect fresh desktop/mobile screenshots for home/settings/tasks. Verify multiple breakpoint widths and no horizontal page overflow in a real browser.
6. Task-row text and action edges use the same inner gutters as the forms, including wrapped actions at tablet/mobile widths.

## Out of scope
Visual rebranding, changing the settings help column, business logic, real-library writes, deployment, push or merge.

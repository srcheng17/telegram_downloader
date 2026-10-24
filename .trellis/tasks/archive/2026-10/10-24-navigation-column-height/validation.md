# Navigation/content column validation

Verified 2026-10-24 on `refactor/single-host-workspace`.

The user's clarification refers to the left navigation and right page rather than form input edges. This change addresses the independently confirmed full-page height defect. Menu-versus-title starting baselines were not changed; the optional clarification about that separate preference remains open.

## Reproduction and fix

At 1920x1000, the former fixed sidebar ended at1000px while main/document ended at1317.59px. The new desktop regression failed before implementation at1024x720: sidebar bottom720 versus main bottom1317.59.

The actual body now has two grid columns in the same row. The sidebar outer column stretches with the main document; its new inner wrapper stays sticky and viewport-height, with overflow for short windows. Mobile restores normal-flow compact navigation. This is structural layout, not a pseudo-background or edited screenshot. The existing frame widths, gutters, navigation starting position, labels, skip-link and business logic remain unchanged.

Readback after the change at1920x1000: sidebar and main both top0/bottom1317.59375; inner top0/bottom1000. After scrolling to the document bottom, the navigation remains visible at approximately y161.39–325.39. The mobile navigation remains approximately132px tall.

## Verification

- Ruff, JavaScript syntax and git whitespace checks passed.
- 95 non-e2e tests passed.
- 15 browser cases passed: the original four business scenarios, eight responsive horizontal alignment cases, and three new desktop column-height/scroll cases (1024/1440/1920).
- New tests compare actual sidebar/main top and bottom on all three pages, scroll and verify visible navigation, and preserve no-overflow checks. Existing mobile cases now also require the page heading to be visible in the initial viewport, catching accidental viewport-height mobile headers.
- Fresh unedited captures: `test-results/columns-preview-home.png` and `columns-1920-{home,settings,tasks}.png`. The former uses the same1920x1000 viewport as the reproduced screenshot. Root inspected the real full-page capture.

An existing upstream Starlette/httpx deprecation warning remains. No production deployment, real Komga write, API/database change, dependency change, push or merge. The user's pre-existing `/progress/` ignore entry remains outside task commits. Container smoke was not repeated for this layout-only change.

Independent review passed the real full-page capture, desktop scrolling, low-height navigation access, keyboard skip-link and compact mobile header. No confirmed remaining issue in this equal-height fix.

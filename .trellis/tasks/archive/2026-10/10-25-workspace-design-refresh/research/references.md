# Interface references inspected in Chromium

Inspected live on 2026-10-25 using the repository's Python Playwright environment at 1440 × 1000. Screenshots are unedited browser captures in ignored `test-results/`.

## Primary reference: shadcn/ui Dashboard

- URL: https://ui.shadcn.com/examples/dashboard — HTTP 200.
- Screenshot: `test-results/reference-shadcn-app.png` (scrolled to the actual dashboard, below the marketing introduction).
- Observed: pale neutral sidebar separated from the white work area by one thin rule; small outline navigation icons; compact sans-serif navigation and labels; modest rounded controls; restrained grey surfaces; data rows separated by fine lines; status carried by small text badges rather than large colored panels. Tabs and secondary buttons share the same height and visual weight.
- Adopt for PanelPort: light full-height sidebar, compact product identity, three icon-and-text page links, a quiet active row, a consistent form surface, grouped fields with clear spacing, and compact status badges in task rows.
- Do not adopt its analytics metrics/charts or many sidebar destinations. PanelPort has three distinct jobs; added dashboard content would make the interface harder to use and violate the submit-only home requirement.

## Secondary reference: Vercel Geist

- URLs: https://vercel.com/geist/introduction and https://vercel.com/geist/input — the Input page returned HTTP 200. Introduction rendered fully, but its `networkidle` wait timed out because background activity continued.
- Screenshots: `test-results/reference-geist.png`, `test-results/reference-geist-forms.png`.
- Observed: predominantly neutral surfaces; left-aligned headings and labels; moderate sans-serif heading scale; ample but systematic horizontal insets; small input radii and thin borders; visible separation between page title, explanation, field examples and secondary actions. The documentation sidebar uses a subtle filled active state.
- Adopt for PanelPort: a single type family, 28px-class page headings rather than display serif text, 14px-class labels/body, neutral field borders, clear labels above inputs, subdued helper text, and one consistent input/button height.
- Do not adopt the documentation catalog, search box, copy-code drawers or brand grid. These solve documentation needs absent from this application.

## Linear check and limitation

- https://linear.app/features/issues returned HTTP 404; it is not evidence for the product interface.
- https://linear.app/ and https://linear.app/build returned HTTP 200. The Build page shows a public product demonstration of an issue detail toolbar, but the inspected viewport was primarily marketing content. Screenshot: `test-results/reference-linear-build.png`.
- Linear's restraint is a broad secondary influence only. Do not claim that its signed-in workspace was inspected or copied. shadcn's actual public application example and Geist's real controls provide the strongest verifiable references here.

## Compact design direction

Use a neutral workspace with one indigo action accent. Keep the book identity in a small mark, not in decorative spines, serif display typography or slogans. A modest title and short explanation should lead directly to the primary task. Use a flat grey sidebar, white form surface and controlled 24–32px section gaps. Reserve color for the current location, primary action, state and feedback. The homepage remains a form; history remains in its own page; Komga configuration remains in settings.

This is visual reference research, not a recommendation to add React, Tailwind, shadcn packages, web fonts or any external runtime request. Existing Jinja/CSS/native JavaScript can implement the observed hierarchy directly.

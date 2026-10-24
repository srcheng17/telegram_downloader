# Interface design

## Boundary

The behavior gap is visual hierarchy and styling, owned by shared templates and CSS. Root owns `templates/base.html`, `index.html`, `tasks.html`, `settings.html`, `_jobs.html`, a shared local SVG macro and `static/app.css`. `app.js`, form IDs, field names, data hooks and backend routes stay compatible. Existing browser tests verify the actual workflows.

## Direction

Use the inspected shadcn/ui Dashboard application and Vercel Geist Input page as the primary references. Linear's public pages are secondary inspiration only; no authenticated Linear workspace was inspected. Adapt the approach to a Chinese personal comic archive rather than reproduce their marketing or dashboards.

- Canvas #fcfcfd; navigation #f7f8fa; surface #ffffff; text #20212b; secondary #626775; accent #4f46e5. Borders use #e4e6ec.
- Typography: system sans-serif with PingFang SC / Microsoft YaHei fallbacks; 26px page title, 15px section titles, 13-14px controls, 12px helper text. No external fonts.
- Layout: full-height 224px sidebar with compact icon navigation. Main frame centered in its remaining column. Breadcrumb/status row, page title, source and metadata sections, consistent footer. Settings has the form and quiet setup guidance; task rows retain honest statuses/actions.
- Source radio controls become a keyboard-accessible segmented selector. White surfaces and modest 8-12px rounding distinguish form containers from controls without shadows on every element.

```text
sidebar          breadcrumb                              library setup
brand            page title + description
new archive      +-----------------------------------------------+
tasks            | source heading               link / upload   |
settings         | link/file                                    |
                 | target library                               |
                 |----------------------------------------------|
                 | optional metadata, two columns               |
library setup    | helper                         create & send |
                 +-----------------------------------------------+
```

## Critique before implementation

Avoid turning a small utility into a dashboard template: no counts of jobs on home, metric cards, invented health indicators, ornamental eyebrows, marketing illustration, theme switch or unrelated toolbar. The comic/book motif appears only in the compact brand and relevant icons. Source and destination remain easy to find. Preserve proven grid containment and mobile alignment instead of rebuilding application state.

## Compatibility and rollback

No persisted data or interface changes. Existing JS clones server-rendered job/library templates unchanged. Reverting the frontend commit restores the prior appearance. All previews/tests use temporary storage; production remains untouched.

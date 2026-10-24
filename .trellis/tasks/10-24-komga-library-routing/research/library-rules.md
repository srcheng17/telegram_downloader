# Official Komga library constraints

Verified 2026-10-24 against current official docs and source commit 023ec905c10a35bbb8e29095420d266cd3adad60.

- https://komga.org/docs/guides/libraries explicitly allows sibling roots /books/mangas and /books/comics, and forbids identical or overlapping parent/child roots.
- https://github.com/gotson/komga/blob/023ec905c10a35bbb8e29095420d266cd3adad60/komga/src/main/kotlin/org/gotson/komga/domain/service/LibraryLifecycle.kt#L98-L116 enforces root relationships on create/update.
- https://komga.org/docs/guides/scan-analysis-refresh#what-happens-during-a-scan defines series from the direct parent directory of book files; nested directories do not form a nested series tree.
- A configured local name/path must correspond to a separately configured Komga library. This app's ID is not a Komga API ID.
- Writing selected-root/series/*.cbz is correct. Book visibility still depends on Komga scanning, exclusion and One-Shots settings; this task adds neither scans nor server-side library creation.

Current code reads live komga_dir inside Runner.process, so per-job destination snapshotting is necessary to avoid rerouting when configuration changes between submission and execution/retry. Upload currently permits only seven fields and must allow the eighth library_id field while keeping it out of ComicInfo metadata.

# Design

Extend the existing settings KV table with `libraries`, a JSON list of `{id,name,path}`. Keep `komga_url` as website-only. GET settings includes `libraries`, legacy `komga_dir` alias for the first configured path, and `configured`. Legacy POST `{komga_url,komga_dir}` maps to one `default` library named 默认书库. Saved multi-library configuration overrides the initial environment directory.

POST settings accepts `{komga_url,libraries:[{id?,name,path}]}`. Server assigns a stable ID to a new entry with no ID, preserves supplied IDs, normalizes existing writable absolute directories, and rejects duplicate IDs/names, same physical roots and parent/child root overlaps. List length and text are bounded. Directory configuration does not create a Komga server-side library.

Submissions add `library_id` to JSON/form/multipart, separate from the seven metadata fields. With one library, omission selects it for compatibility; with multiple, omission or an unknown ID is rejected. The server resolves the selected ID and saves a private destination snapshot `{id,name,path}` with the job. Public jobs expose only `library_id` and `library_name`. The existing metadata flows unchanged into ComicInfo.

SQLite schema version 2 adds one destination JSON column. Initialize under the existing exclusive instance lock; preserve all rows and artifacts. Version-1 jobs receive the existing sole directory (saved setting or initial environment default) as their snapshot. If none exists, do not guess a future target or silently reroute. A job's snapshot survives settings renames, path edits, deletions, retries and restart. The runner must use the snapshot, never live configuration, and preserve current cancellation/delivery/cache safety.

An already saved canonical root must not be rebound through later symlink changes, including ancestor links. Check that its resolution is unchanged when selecting a configured destination and at execution/copy boundaries; reuse media's existing containment checks without first resolving the saved root to a different value. A rejected delivery keeps its generated cache for retry.

Frontend: dynamic library rows on /settings, a target-library select on submission-only /, and destination name on /tasks. One configured library is selected automatically; multiple require an explicit selection. Use native accessible controls, existing local design, Chinese copy and safe text rendering. No extra framework or dependency.

For shared root /komga, configure sibling library paths such as /komga/日漫 and /komga/国漫, corresponding to separate roots in Komga. Names alone do not create bookshelves. No Komga API credentials, automatic library creation, scan integration, collections/tags changes or production mutations.

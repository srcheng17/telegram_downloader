# Select a target Komga library

## Goal
Allow family users to choose the Komga library (for example 日漫 or 国漫) for each new comic submission. The user explicitly clarified that 分类 means independent Komga libraries.

## Requirements
- Configure multiple named, existing writable shared library directories in settings.
- Choose a configured library on home submission; tasks show their chosen library.
- Persist the selected name/path with the task, so queueing, retries and later settings edits cannot silently redirect a comic.
- Preserve the existing single-directory configuration and existing task/artifact data on upgrade.
- Keep home/tasks/settings isolated and retain automatic safe delivery, cancellation/retry and existing path/resource validation.
- Require distinct non-overlapping library roots and explain that users configure corresponding libraries separately in Komga.

## Acceptance
1. Configuring 日漫 and 国漫, then submitting to 国漫, publishes the actual CBZ only below 国漫 with the existing series structure and metadata intact.
2. Unknown/deleted IDs and ambiguous missing selections are rejected without a queued job or leaked upload; a sole library remains compatible with omitted selection.
3. Editing/deleting a configuration entry after submission cannot change a queued or retrying job's persisted destination.
4. Version-1 databases/settings migrate without losing tasks or cached artifacts, and the previous destination becomes the default library.
5. Desktop/mobile settings and selection flows pass real-browser tests; homepage still makes no task-list requests.

## Out of scope
Komga server-side library creation, API authentication/scan integration, remote file transport, collections/tags management, production deployment, push and merge.

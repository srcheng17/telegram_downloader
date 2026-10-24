# Error Handling

Use explicit media cancellation and media failure exceptions; cancellation is not successful completion. Preserve operation context internally, but return Chinese user-facing explanations without traceback, credentials, private URLs or raw subprocess output.

HTTP routes require configured Komga delivery (409 otherwise), validate source/metadata and enforce upload limits before queuing. Invalid settings/actions/input and oversized uploads need appropriate HTTP errors. The frontend must handle non-JSON errors as well as JSON responses.

- Abort failed uploads and remove partial files.
- Stop and wait for task subprocesses on cancel/timeout before finalizing the task.
- Do not deliver a partial CBZ after execution failure; there is no browser download route.
- Keep uploaded input on failed/cancelled attempts, and retain valid internal CBZ after delivery failure for retry without re-fetching.
- Mark succeeded only after atomic Komga delivery, then clean the internal CBZ and uploaded source. There is no manual-copy endpoint.
- Do not swallow file-close, subprocess-exit or rename failures that affect correctness.

Test real error branches at HTTP/media/filesystem boundaries, rather than reproducing implementation details in mocks.

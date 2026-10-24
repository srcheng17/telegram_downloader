# Implementation

1. Confirm existing submission/configuration/delivery callers and official Komga directory constraints.
2. Extend Store settings/schema, HTTP validation/submission, Runner destination use and focused backend migration/routing/retry tests.
3. Extend settings library management, homepage selection and task destination rendering with focused frontend tests.
4. Test two real shared directories with real ZIP bytes; ensure only the selected library receives a CBZ. Prove queued/cached-retry destinations do not drift when settings change. Preserve existing cancellation/no-clobber boundaries.
5. Run Ruff, non-e2e and affected browser tests, JS syntax and whitespace checks. Run isolated container smoke only if deployment contract changes or packaging concerns arise.
6. Update current README/spec contracts and record actual validation. Preserve the pre-existing /progress/ ignore edit. Do not push, merge or deploy production.

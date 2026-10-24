# Komga transport evidence

Official docs https://komga.org/docs/guides/import-books/ and https://komga.org/docs/openapi/import-books/ describe sourceFile server paths and existing series destinations. Current controller source at gotson/komga commit023ec905c10a35bbb8e29095420d266cd3adad60: BookController's only multipart upload is book thumbnail, while books/import uses JSON paths. FileSystemController browses directories; LibraryController provides scan endpoints. Public demo OpenAPI fetch returned403, so no demo integration claim is made.

## Follow-up verification, 2026-10-24

The user approved the local commit and asked about existing Komga interfaces. A fresh independent check of latest release 1.29.1 (2026-10-14), master `023ec905` (2026-10-21), official OpenAPI and all 131 API Kotlin files confirmed:

- `POST /api/v1/books/import` accepts JSON whose `sourceFile` is a path visible on the Komga server. `upgradeBookId` can identify an existing book for replacement through this import workflow.
- `POST /api/v1/libraries/{id}/scan` triggers a library scan.
- These mutation endpoints require ADMIN privileges; supported authentication includes `X-API-Key` or HTTP Basic.
- There is no official full new-book CBZ binary upload endpoint, nor a `PUT /books/{id}/file` binary replacement endpoint in the verified version. The thumbnail multipart endpoint does not upload books.

For the approved single-host design, shared-directory publication remains the complete file transport. A future API integration could request a scan after publication, but neither import nor scan transfers a CBZ from this app's local filesystem to a remote Komga server. The current app does not call either API or claim indexing completion.

# Protected workspace and metadata editor

## 1. Scope / Trigger

Read when changing navigation, uploads, settings modules or metadata editing.
Backend payloads and limits are defined in `../backend/workspace-metadata-auth.md`.

## 2. Signatures

- `shared/app_lifecycle.js`: `createAppLifecycle(win, doc, session)` gates page modules.
- `shared/api/tasks_api.js`: `createTasksApi(fetchImpl, {win})` is the shared transport;
  `csrfHeaders(url)` supports XHR uploads, `unauthorized()` clears protected content.
- `createWorkspaceShell({root, adapters, doc})`: `mount/unmount`, `getDocument`,
  `showCandidate`, `markClean(revision)`, `canLeave` and `hasUnsavedChanges`.
- `createMetadataFieldsModule({doc,api})` and `createIntegrationsModule(win,doc)`
  mount only in settings; the existing page module owns their cleanup.
- `createTabs({root,win,defaultTab,hash,onChange})` owns `data-tab` buttons and
  `data-tab-panel` panels; exposes `mount/unmount/activate/getActive`.
- `createNavigationDisclosure({root,win,doc})` owns desktop collapse and the mobile
  drawer separately. `close()` closes only the mobile drawer.

## 3. Contracts

Initialize the singleton administrator session before mounting business modules.
Keep cookie auth HttpOnly and CSRF only in memory. Never put credentials/documents
in localStorage or htmx history; protected content uses `hx-history="false"`.
401 clears page modules, in-flight work and content before redirecting. Never replay
an unauthorized mutation. Credentials/CSRF are never sent to a different origin.
XHR upload receives shared CSRF headers in addition to its upload token.

On `pagehide`, abort modules, clear session/password inputs and hide content. On
BFCache restore, revalidate before showing business content. The login form must
be shown again when restored; its retained listeners can refresh preauth on submit.
Rejected htmx swaps keep the current module mounted. A cancelled dirty navigation
must not destroy the draft. Failed/superseded async responses never recreate it.

A metadata document is the only editable truth. Do not collect legacy seven inputs
alongside it. String arrays are newline-delimited to preserve spaces in a single
name/tag. Empty strings are invalid; empty arrays are explicit values and differ
from absent fields or a clear tombstone. Use the labeled clear operation to clear.
Validate fields against the server registry, including enabled/type/enum/bounds.
History selection previews submitted/effective snapshots, creates a candidate and
never changes the download source. Missing effective metadata is unavailable.
Old definition snapshots remain readable. Version changes permit explicitly previewed
adoption only when the same key/type and current constraints remain compatible;
disabled/incompatible fields remain read-only and show a warning.

After submission mark only the submitted revision clean, preserving edits made
while the request was running. A saved custom registry emits document event
`metadata-definitions-changed` with `detail.definitionsVersion`; old AI tests are
cancelled/invalidated. Edits and config changes invalidate late discovery/test results.

The primary navigation is new task / tasks / settings. The home download source
accepts Telegraph or upload only; keep existing Telegram APIs/history intact.
The authenticated legacy `GET /telegram` returns 303 to `/settings#connections`;
htmx requests return 200 with `HX-Redirect` because htmx ignores that header on 3xx.

Settings hashes are `download`, `sources`, `ai`, `fields`, `rules`, `connections`,
and `security`; unknown hashes select download. Tabs use ARIA roles, roving tabindex,
ArrowLeft/Right/Home/End and `hidden` for inactive panels. First activation lazily
mounts a module. Successful ordinary forms retain their DOM and unsaved input while
hidden. Failed initial hydration must retry on return without overwriting already
loaded drafts. Hiding integrations aborts discovery/tests, while in-flight saves
retain their independent scope and later edits/CAS protections. Leaving connections
unmounts Telegram to clear QR, password and EventSource; returning reads account state
again. Leaving settings disposes all modules. Home evidence tabs keep OCR/search and
the one metadata draft alive across tab switches.

Only `workspace.sidebar.collapsed` may persist in localStorage as a non-sensitive
preference. At <=767px, use an offcanvas drawer with inert background, focus trap,
Escape/backdrop dismissal and focus restoration. Resizing must not erase desktop
collapse preference. Screenshot checks wait for the actual hidden/visible final
state, since aria-expanded changes before CSS transitions finish.
Synchronize active navigation at the authenticated async mount boundary. A location
change while `session.refresh()` is pending must select the current path, subject
to the mount generation guard. htmx 1.9.10 pushes URL before `afterSwap`; do not
assume its event order is the cause of a stale active entry. Browser regression
must assert the active entry after actual settings/tasks navigation.

## 4. Validation & Error Matrix

| Condition | UI behavior |
| --- | --- |
| Schema loading/failure | Submission blocked, explicit retry |
| Invalid typed field or empty string | Inline error; document cannot be submitted |
| Locked/stale candidate | Explicit confirmation or conflict, no implicit overwrite |
| Settings CAS conflict | Preserve entered form values |
| 401/logout | Clear sensitive content and redirect |
| htmx failed navigation | Retain current page handlers/draft |
| Missing future OCR/tdl/provider module | Clearly unavailable; no fake success |
| Initial settings load fails | Error feedback; retry on tab return; preserve successful drafts |
| Switch away from connections | Dispose QR/password/events; reread account on return |

## 5. Good / Base / Bad Cases

Good: paste a multi-word tag as one line, clear another field and submit one document.
Base: leave an optional field absent, preserving the distinction from explicit clear.
Bad: send a seven-field projection alongside the typed document or silently replace a
hand-edited field with a late candidate. These lose meaning or overwrite user work.

## 6. Tests Required

Node tests cover Go fixture parity, clear/lock/conflict, schema errors, session and
page lifecycle, late responses, metadata settings and upload errors/cancellation.
Browser tests use real administrator login and production bundles; mock only specific
candidate/error UI fixtures. Preserve the real archive-byte/order/ComicInfo/Komga
flow. Run `npm run test:frontend`, `npm run lint`, `npm run build` and affected E2E.
Verify tab deep links, keyboard navigation, unsaved values, failed hydration recovery,
desktop collapse through htmx navigation, mobile inert/focus and `/telegram` redirects.

## 7. Wrong vs Correct

Wrong: IDs `metadata-title` on both a heading and generated title input, global fetch
interception, or direct history-to-input assignment.
Correct: reserve `metadata-<field-key>` for editor inputs, call the shared transport,
and route history through candidate preview/adoption into the single draft.
Wrong: unmount every form on tab change, or mark a failed first load as completed.
Correct: retain successful ordinary form instances and retry only incomplete hydration.

# Validation

## Completed

- Original nine-field bibliography fixture/schema guidance correction: real direct MiniCPM and later CPA-backed product connection tests passed without relaxing the expected values.
- CPA v8.0.13/d7914af registered exact model minicpm5-2b-q4 against http://minicpm-model:8080/v1; native management readback and /v1/models verified. Model-only Docker network deployment succeeded, healthy, all other RackNerd container IDs unchanged; --no-context-shift explicit, provider retry0, no payload rules.
- Real CPA chat probe preserved filtered seven-field upstream telemetry, model/build, usage/native counts and output limit. Large fixed synthetic input returned HTTP400 exceed_context_size_error. See protected cpa-minicpm/chat-boundaries.safe.json.
- Explicit saved protocol support passed independent settings/UI and transport review. Legacy reads default native, omitted writes retain stored protocol, invalid/null inputs rejected; credential URL/CAS boundary retained.
- Full Go normal and race tests with -p1 passed; go vet ./... passed. PostgreSQL-dependent cases skipped because TEST_DATABASE_URL was unset. Focused adapter/snapshot/application tests, race and vet passed separately.
- Frontend tests 217/217 and CLI tests69/69; lint, lint:cli, build, build:cli and bundle contract passed. Rebuilt settings.bundle.js and stable packaged CLI installed.
- CPA API/static image 82e8f1fb19fa deployed through Dockhand job8b8e1199-bfa9-46da-a4fa-e8f3f3277c60; healthy, HTTPS readyz200, binary/helper/public bundle hashes verified; other16 local container IDs unchanged.
- Independent post-deploy auth/Telegram/Komga regression:6 CLI commands exit0; Telegram verified connected, Komga connected with3 available read-only libraries.
- User explicitly delegated fixed synthetic send/evidence review. First CPA extraction completed transport but title included its label; assistant rejected with NO, closed workspace and disabled AI at version17. This is not a passed extraction acceptance.

## Semantic diagnosis and final correction

With unchanged source text, expected values, prompt, model and validation, changing the generated field schema property order from evidence_quote→value to value→evidence_quote passed the original title and a second title containing internal colon/full stop. Moving the label guidance into system alone, or adding a generic example, did not fix the production schema order. The final correction changes schema serialization order only; no system helper, input/output trimming or relaxed checks. Evidence: protected cpa-minicpm/label-diagnostic.safe.json and label-value-first.safe.json. An earlier unordered diagnostic is retained separately and not treated as production-equivalent evidence.

## Final acceptance completed

- Final field-order fix passed focused test/race/vet and independent review. After source freeze, full Go suite passed. An interim run overlapped a test import edit and was superseded by that stable pass.
- Final immutable API image telegram-downloader:62a2c88-cpa-title-3fef071da104 deployed successfully through native Dockhand job eddcd038-6dca-4426-bfc2-c143056d136b. Healthy, readyz200, API/helper/static hashes correct; other16 Mac containers unchanged.
- Full product CLI acceptance succeeded through CPA: original nine-field fixture passed; exact title 验收示例 returned, evidence0..7 matches original full input. Assistant reviewed and confirmed under explicit user delegation; evidence_assistant_confirmed=true, workspace_closed=true.
- Final readback AI enabled, llama_cpp_chat, minicpm5-2b-q4, config_version19. Telegram fresh verify connected and Komga test connected/3 allowed libraries.
- Actual PostgreSQL test suite and full browser release E2E not run; real production setting CAS/persistence and extraction were verified through the authenticated CLI.

Operational evidence and rollback sources are protected in the protected local evidence directory. Source changes were uncommitted during operational acceptance; the subsequent review submission includes them without further production changes.

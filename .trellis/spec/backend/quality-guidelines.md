# Quality Guidelines

Use [testing strategy](../../../docs/development/testing-strategy.md) for test
responsibilities and [README](../../../README.md) for current runtime commands.
Where older documents mention retired state names or Redis, current Task Core
code is authoritative.

- Keep Go formatted with `gofmt`; add the smallest regression check for changed behavior.
- Run `go test ./... -count=1` for backend changes; add `go test -race ./... -count=1`
  when changing state transitions, PostgreSQL repositories, or worker coordination.
- Reuse the existing state/action functions and artifact path validation. Review
  cancel/retry, lease/recovery, upload cleanup, and Komga-copy behavior when affected.
- For frontend changes also run its index Quality Check and commit rebuilt bundles.
- Review migration/configuration compatibility using the relevant runbook.

Release validation is `bash scripts/verify_release_gates.sh`. Its E2E phase
starts and cleans up a Compose test environment; it is not a requirement for
documentation-only or Trellis initialization changes. Report unrun checks as
not run; never infer success from a command being listed in a document.

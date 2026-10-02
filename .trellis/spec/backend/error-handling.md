# Error Handling

- Task Core service sentinels are `ErrInvalidInput`, `ErrNotFound`, and `ErrConflict`
  in `internal/app/taskcore/service.go`. Use `errors.Is` when mapping wrapped errors.
- Add operation context with `fmt.Errorf("operation: %w", err)` while preserving the cause.
- Reuse `writeServiceError`, `writeAPIErrorResponse`, and `writeInternalError` in
  `internal/httpapi/`; internal failures return a generic message to clients.
- Preserve endpoint contracts: `/api/*` uses `{error, code, message?, details?}`;
  `/v2/*` currently uses `{error}`. Do not change one contract to match another incidentally.

Actual example from `internal/store/postgres/migrations/runner.go`:

```go
return fmt.Errorf("acquire migration connection: %w", err)
```

Check error-code/status behavior with `internal/httpapi/error_response_test.go`
and affected handler tests. Preserve cancellation handling and cleanup errors
that protect uploaded or generated files.

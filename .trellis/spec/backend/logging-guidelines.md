# Logging Guidelines

Current runtime logging uses Go's standard-library `log`, not a structured
logging framework. Reuse the existing patterns in `cmd/server/main.go`,
`cmd/worker/main.go`, and `internal/httpapi/api.go`.

Actual internal-error pattern in `internal/httpapi/api.go`:

```go
if err != nil && !errors.Is(err, context.Canceled) {
    log.Printf("request failed: %v", err)
}
```

- Fatal startup errors stay in runtime entry points; request handlers return HTTP errors.
- Record operation/error context needed for diagnosis without dumping entire requests.
- Never add credentials, database DSNs, private source URLs, downloaded content,
  or full environment/configuration objects to logs or reports.
- Return the existing generic internal-error response rather than exposing logged details.

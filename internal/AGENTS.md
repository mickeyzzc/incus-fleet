# Backend agent guide

Scope: `cmd/` and `internal/`. Read the root `docs/standards.md` first; this file adds
repo-specific details for Go work.

## Package direction

```
cmd/incus-fleet
  └── internal/app
        ├── internal/httpapi
        │     ├── internal/auth
        │     ├── internal/clusters
        │     ├── internal/instances
        │     └── internal/migrations
        │           ├── internal/clusters
        │           └── internal/store
        └── internal/config
```

Do not import `httpapi` from services. Services return typed errors; the HTTP layer maps them.

## Testing

- Use `internal/testutil/incusfake` for Incus clients. Do not require live clusters in unit tests.
- Prefer table tests for validation and status mapping.
- Migration changes need tests for: successful move/copy, copy failure restore, live preflight
  rejection, source deletion failure, and interrupted task recovery.
- Run the focused suite while iterating, then the full gate:

```bash
go test ./internal/migrations ./internal/httpapi
make test-go
make coverage-go
```

- `go test ./...` walks `web/node_modules` in this repo. Use explicit packages
  (`./cmd/... ./internal/... ./web`) or the Makefile targets.

## Migration invariants

These are behavioral contracts; preserve them unless the task explicitly changes the design:

1. Containers are never live-migrated. Live mode requires a running VM with
   `migration.stateful=true` and `mode=move`.
2. A previously running source is restarted if the copy fails.
3. After a completed copy in move mode, the source is **not** restarted before deletion.
4. Copy mode always restarts a source that was stopped.
5. Target startup requires live mode or `auto_start` (default true) when the source ran.
6. A source deletion failure marks the migration failed and leaves both copies in place.
7. Only one migration per `source/instance` may run in this process.
8. Unfinished tasks are marked failed on startup; never auto-resume a transfer.

## Conventions

- Wrap errors with package context and `%w`.
- Keep sentinel errors stable; `httpapi` maps them to status codes.
- Persist timestamps in UTC (`time.Now().UTC()`).
- Do not log tokens, keys, request bodies, or full instance configs.
- Use `slog` key/value pairs; avoid string concatenation in log messages.
- Keep goroutines owned by `migrations.Service` tracked with `WaitGroup`.

## Quick checks

```bash
gofmt -l cmd internal
go vet ./cmd/... ./internal/... ./web
golangci-lint run
go test -race ./internal/migrations ./internal/httpapi
```

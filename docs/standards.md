# Engineering standards

These rules apply to every change in this repository. They are enforced by review, CI, and the
Makefile where possible.

## Test-driven development

1. Write a failing test that describes the behavior.
2. Implement the smallest change that makes it pass.
3. Refactor with the test suite green.
4. Add regression tests for every bug fix.

Coverage gates:

- **Go:** at least **90%** statement coverage across the `internal` packages, excluding
  `internal/testutil`. `main()` calls `os.Exit` and `web` only embeds assets, so `cmd` and
  `web` are built/tested but excluded from the denominator. `make coverage-go` and CI fail
  below the gate.
- **Frontend:** at least **90%** lines, statements, and functions, and **85%** branches.
  Thresholds live in `web/vite.config.ts`; `bun run coverage` fails below them.

Coverage is a floor, not a goal. Assert behavior and error paths, not implementation details.

## Go standards

### Layout and boundaries

- `cmd/` contains process wiring only.
- `internal/*` packages expose small interfaces and keep dependencies directed inward:
  `httpapi → instances/migrations/clusters → store/config`.
- Keep shared types in the package that owns the behavior.
- Do not add a package for a single helper; prefer a function in the existing owner.

### Style

- `gofmt` is mandatory; CI runs `make fmt-check`.
- Exported identifiers have doc comments starting with the identifier name.
- Wrap errors with context: `fmt.Errorf("migrations: copy instance: %w", err)`.
- Use sentinel errors (`ErrInvalidRequest`, `ErrConflict`, `ErrNotFound`) when the HTTP layer
  must map behavior.
- Accept `context.Context` as the first argument for I/O; never store it in a struct.
- Use `log/slog` with structured key/value pairs. Do not log secrets, tokens, certificate
  contents, or full request bodies.
- Prefer explicit timeouts on network and database operations.
- Guard shared state with a mutex; keep critical sections free of I/O.
- Avoid package-level mutable state. The `version` variables are the only build-time exception.

### Configuration

- Every new option needs a default, validation, and a test.
- Environment overrides use the `INCUS_FLEET_` prefix and take precedence over YAML.
- Validation errors must identify the offending field and cluster.
- Never put real tokens, keys, or passwords in example configs.

### Tests

- Table-driven tests for mapping/validation logic.
- `httptest` for API behavior; assert status codes, content types, and JSON shapes.
- Use `internal/testutil/incusfake` rather than live clusters.
- Migration tests must cover success, copy failure rollback, move cleanup, live validation, and
  interrupted-task recovery.
- Run with the race detector when touching concurrency (`go test -race ./internal/migrations`).
- Tests must not depend on wall-clock sleeps; inject clocks or use short timeouts.

## Frontend standards

### Stack conventions

- Vue 3 `<script setup lang="ts">`; no options API for new components.
- TypeScript strict mode with `noUncheckedIndexedAccess` and `noUnusedLocals`. Do not use `any`
  in application code; test files may use it only for narrow doubles.
- Pinia setup stores; one store per domain (`auth`, `clusters`, `instances`, `migrations`).
- All HTTP calls go through `src/api/client.ts`. Components do not call `fetch` directly.
- Shared formatting belongs in `src/utils/format.ts`.
- Element Plus is the component library; keep styling in `src/styles/main.css` or scoped blocks.

### Components

- Props and emits are explicitly typed.
- Prefer computed state over duplicated refs.
- Destructive or irreversible operations require an explicit confirmation in the UI.
- Show API errors near the affected view; use `ElMessage` for transient confirmations.
- Never store the admin token in `localStorage` or `sessionStorage`.

### Tests

- Vitest + `@vue/test-utils` + jsdom.
- `src/test/setup.ts` globally mocks `@/api` and `ElMessage`, stubs Teleport/transitions, and
  replaces `ElSelect` because Element Plus select sizing recurses under jsdom. Do not reintroduce
  the real select in component tests.
- Every store action needs success and error tests.
- Views need loading, empty, error, and interaction coverage.
- Router tests use `createFleetRouter(createMemoryHistory())`.
- Reset mocks between tests. Avoid coupling tests to Element Plus internals where a user-visible
  assertion works.

### Formatting and dependencies

- Bun is the package manager; commit `web/bun.lock`. Use `bun install --frozen-lockfile` in CI.
- Keep TypeScript pinned to 5.9.x until `vue-tsc` supports TypeScript 7.
- ESLint must pass with zero errors. Warnings should be fixed or explicitly justified.
- Do not add a dependency for behavior that can be implemented in a few well-tested lines.

## Security standards

- No secrets in git: tokens, private keys, passwords, WiFi credentials, `.env` files, or real
  production configs.
- The repository ignores `/AGENTS.md`, `/docs/local/`, `*.key`, `*.pem`, `config*.yaml`,
  `/data/`, and `/coverage*`. Do not weaken these rules.
- Root `AGENTS.md` and `docs/local/` are local-only and must never be pushed.
- Sanitize committed docs and examples: placeholders only, no hostnames, PSKs, or tokens.
- Run a secret scan before every push and before publishing the repository.
- Cluster endpoints must be HTTPS and use pinned client certificates.
- Use constant-time comparison for credentials and random, high-entropy session identifiers.
- Do not log secrets or include internal error details in 5xx responses.
- Keep dependencies current; review security advisories for Go and npm packages.

## Git and review

- Commit messages use the imperative mood and a scoped prefix when useful:
  `migrations: restart source when copy fails`, `web: add migration dialog tests`.
- Keep commits focused; do not mix refactors with behavior changes.
- Every PR describes behavior, tests run, and any operational impact.
- Do not commit generated `web/dist` assets other than `.gitkeep`.
- Do not rewrite shared history after review without coordination.

## Definition of done

A change is done when:

- tests were written first and pass;
- coverage gates pass;
- `make check` passes locally;
- user-facing docs/API docs are updated;
- no secrets or local-only files are staged;
- operational impact (migration behavior, config, deployment) is called out.

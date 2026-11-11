# Frontend agent guide

Scope: `web/`. Read the root `docs/standards.md` first; this file adds repo-specific details.

## Stack and commands

Vue 3 + TypeScript (strict) + Vite + Pinia + Element Plus. Package manager is **Bun**; commit
`bun.lock`.

```bash
cd web
bun install --frozen-lockfile
bun run dev        # Vite dev server, proxies /api to 127.0.0.1:8080
bun run test
bun run coverage
bun run lint
bun run typecheck
bun run build      # vue-tsc --noEmit && vite build
```

TypeScript is pinned to `5.9.x`. `vue-tsc@3.3.14` breaks with TypeScript 7
(`ERR_PACKAGE_PATH_NOT_EXPORTED ./lib/tsc`). Do not bump the major version until vue-tsc
supports it.

## Layout

```
src/api/client.ts     typed fetch client; only place that calls fetch
src/api/types.ts      JSON contracts mirroring Go structs
src/stores/*.ts       Pinia setup stores (auth, clusters, instances, migrations)
src/router/index.ts   createFleetRouter(history) + auth guard + titles
src/components        StatusTag, MigrationStatusTag, StorageBar, dialogs/drawers
src/views             Login, Dashboard, Cluster, Instances, Migrations
src/test/setup.ts     global mocks and stubs
src/utils             formatting and error helpers
```

## Testing rules

- Vitest + `@vue/test-utils` + jsdom. Cover success, error, empty, and loading states.
- `src/test/setup.ts` mocks `@/api` and Element Plus `ElMessage`. Import the mocked `api` and
  set `vi.mocked(api.method).mockResolvedValue(...)`.
- `ElSelect`/`ElOption` are stubbed in setup because Element Plus select measurement recurses
  under jsdom (`Maximum recursive updates exceeded`). Use
  `wrapper.findComponent({ name: 'ElSelect' }).props('modelValue')` and emit
  `update:modelValue`; do not mount the real select.
- Router tests use `createFleetRouter(createMemoryHistory())`.
- Reset state per test with `setActivePinia(createPinia())`.
- Coverage thresholds are lines/statements/functions 90 and branches 85. They are enforced by
  `bun run coverage`.

## Component conventions

- `<script setup lang="ts">`, typed props/emits, computed over duplicated refs.
- No direct `fetch` in components; use the `api` singleton or a store action.
- Never persist the admin token in browser storage. Auth uses the HttpOnly session cookie.
- Surface request errors near the view; use `ElMessage` only for transient feedback.
- Keep destructive actions explicit (`MigrateDialog` validates target and mode).

## Common pitfalls

- `noUncheckedIndexedAccess` is on: array access returns `T | undefined`; guard it.
- `verbatimModuleSyntax` is on: use `import type` for types.
- `web/dist` is generated and gitignored except `.gitkeep`; do not commit bundles.
- The Go embed directive requires `all:dist`; see `web/embed.go` before changing ignore rules.

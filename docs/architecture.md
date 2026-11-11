# Architecture

Incus Fleet is a stateless-ish control plane that sits in front of two or more independent
Incus clusters. It provides inventory, health, and a single-click cross-cluster migration API.

## Goals

- Operate independent Incus clusters without joining them into one Incus cluster.
- Give operators one UI/API for inventory and migration.
- Move or copy containers and VMs between clusters, with rollback on failure.
- Keep each deployment a single Go binary plus a small SQLite database.
- Allow clusters to be added over time without redesigning the control plane.

## Non-goals

- Incus cluster membership across sites.
- Replication, mutual backup, or automatic failover between clusters.
- Container live migration. Containers are stopped while their filesystem is copied.
- A shared control-plane database across app instances. Each app instance is independent.

## Runtime topology

```
                         ┌──────────────────────────────┐
   Browser / API client  │  Caddy (sticky load balancer) │
                         └───────────────┬──────────────┘
                                         │
                 ┌───────────────────────┴───────────────────────┐
                 │                                               │
     ┌───────────▼────────────┐                     ┌────────────▼───────────┐
     │ incus-fleet @ host A   │                     │ incus-fleet @ host B   │
     │  HTTP :8080            │                     │  HTTP :8080            │
     │  SQLite / PostgreSQL   │                     │  SQLite / PostgreSQL   │
     └───────┬────────┬───────┘                     └────────┬───────┬───────┘
             │        │                                      │       │
             │        └──────────── HTTPS + client cert ─────┼───────┘
             │                                               │
     ┌───────▼────────┐                             ┌────────▼─────────┐
     │ Incus c1      │                             │ Incus c2        │
     │ API :8443      │                             │ API :8443        │
     │ storage /srv   │                             │ storage /srv     │
     └────────────────┘                             └──────────────────┘
```

Each app instance connects to **both** Incus clusters. The app instances do not talk to each
other. Caddy load-balances browsers and pins each browser to one app instance (see
[Sticky sessions](#sticky-sessions)).

## Components

| Package | Responsibility |
| --- | --- |
| `cmd/incus-fleet` | Flags, config loading, structured logging, signal handling |
| `internal/config` | YAML parsing, environment overrides, defaults, validation, cert file checks |
| `internal/clusters` | Dials Incus endpoints, holds pinned-cert clients, builds health summaries, reconnect |
| `internal/instances` | Lists instances across clusters and builds safe detail views |
| `internal/migrations` | Validates migration requests, persists tasks, runs copy/move, rollback |
| `internal/store` | SQLite/PostgreSQL schema and migration task persistence |
| `internal/auth` | Constant-time admin token check and in-memory session table |
| `internal/httpapi` | REST routes, auth middleware, error mapping, security headers, SPA fallback |
| `internal/app` | Dependency wiring, HTTP server timeouts, graceful shutdown |
| `web` | Embedded Vue 3 SPA |

## Startup sequence

1. Parse CLI flags (`-config`, `-version`, `-check`).
2. Load YAML, apply `INCUS_FLEET_*` environment overrides and defaults, validate.
3. Verify referenced certificate files exist.
4. Open the database and create the `migrations` table/index if needed.
5. Mark any tasks left in a non-final state as failed (`MarkInterrupted`). A previous process
   may have been killed during a transfer.
6. Dial every configured Incus endpoint with its pinned client certificate.
7. Build the HTTP handler and serve the embedded SPA and API.
8. On `SIGINT`/`SIGTERM`, stop accepting requests, shut down the HTTP server, and close the
   database. In-flight migration goroutines are allowed to finish or time out.

## Cluster connections

- Every cluster has its own `tls_cert` + `tls_key` client certificate and an optional
  `tls_server_cert` for server verification.
- Trust tokens are never used by the running service. The certificate is added to the Incus
  trust store ahead of time (`incus config trust add-certificate`).
- Endpoints must be `https://` URLs; trailing slashes are normalized.
- A dial failure does not stop the process. The cluster is marked offline with an error, and
  `POST /api/v1/clusters/{name}/reconnect` re-dials it.
- The manager exposes a narrow client interface to callers and retains the full Incus client
  internally for operations that need it (notably `CopyInstance`).

## Inventory

`instances.List` calls `GetInstancesFull` for one cluster and converts each record into a
summary. `ListAll` iterates configured clusters, so one offline cluster currently fails the
whole request (the cluster dashboard still shows per-cluster health).

Summaries include status, type, node, project, addresses, memory, process count, timestamps,
storage pool, and whether the instance supports stateful/live migration. Detail requests add
guest OS information, selected configuration, devices, and live state, while avoiding
unbounded or sensitive config maps.

## Migration flow

A migration request names a source cluster, target cluster, instance, mode (`move` or `copy`),
whether to include snapshots, and whether to start the target (`auto_start`, default `true`).

### Preflight (synchronous)

`Submit` rejects invalid requests before creating a task:

- source and target must differ and be known/connected;
- the instance must exist on the source and not on the target;
- the mode must be `move` or `copy`;
- `live` requires `mode=move`;
- live mode requires a running VM with `migration.stateful=true`;
- the source root disk's storage pool must exist on the target.

A per-instance lock (`source/instance`) prevents two migrations of the same instance.

### Execution (asynchronous, 6h default timeout)

1. `preflight` — status persisted as validated.
2. If the source is running and the migration is not live: `stopping` — stop the source with a
   300s timeout.
3. `transferring` — the target calls `CopyInstance` in `pull` mode using the source's full
   Incus client, then waits for the remote operation. Live VMs transfer memory and state while
   running.
4. `verifying` — live moves stop the source before target startup.
5. `starting` — start the target when the migration is live, or when the source was running and
   `auto_start` is true.

Live migration preserves the guest's network state. If the source and target clusters use
different subnets, the guest can keep its source DHCP lease and become unreachable until it
renews (for example `dhclient -r <if>` then `dhclient <if>`, or `networkctl reconfigure <if>`).
The control plane does not run commands inside guests; use matching subnets or arrange a guest
DHCP renewal after the move.
6. For `move`, delete the source instance (`verifying` / "removing source"). For `copy`, restart
   the source if it was stopped.
7. `done` — progress "complete".

### Rollback rules

- If the source was stopped and the copy fails, the source is restarted.
- If a move fails after the copy completed but before the source deletion, the source is not
  restarted (the target copy exists; the operator decides what to keep).
- If the source deletion fails after a successful copy, the migration is marked failed but both
  copies remain; the source is not restarted.
- A process crash leaves the task non-final; the next startup marks it failed. Automatic
  cleanup is not attempted.

### Status values

`pending`, `preflight`, `stopping`, `transferring`, `starting`, `verifying`, `done`, `failed`.
Progress text is a short stage label plus transfer metadata when Incus reports it.

## Persistence

Only migration tasks are persisted. Sessions, cluster summaries, and instance inventory are
in memory.

```
migrations(
  id, source_cluster, target_cluster, instance, instance_type,
  mode, live, status, progress, error, created_at, updated_at
)
```

- SQLite uses WAL and a single connection so in-memory test databases stay coherent.
- PostgreSQL uses `pgx` with `?` placeholders rebound to `$n`.
- Timestamps are stored as Unix nanoseconds.

Migration history is operational data, not a source of truth for cluster state. Losing it does
not lose instances.

## Authentication and sessions

- The server has one shared admin token (`server.auth_token` or `INCUS_FLEET_AUTH_TOKEN`).
- `POST /auth/login` compares the token in constant time and creates a random 32-byte session ID
  stored in memory with a 12-hour TTL.
- The browser receives `incus_fleet_session` as an `HttpOnly`, `SameSite=Lax`, path `/` cookie.
  It is `Secure` when the request is TLS or carries `X-Forwarded-Proto: https`, so Caddy
  termination is detected while plain-HTTP LAN testing still works.
- Protected API routes accept either that cookie or `Authorization: Bearer <admin token>`.
- Logout removes the session and expires the cookie.
- Session IDs are not persisted and are not shared between app instances.

### Sticky sessions

Because sessions live in one process, Caddy must pin a browser to one upstream. The sample
Caddyfile uses `lb_policy cookie`, not round-robin. API clients using the bearer token do not
need stickiness. If an app instance restarts or becomes unavailable, its sessions are lost and
the user signs in again.

## Frontend

- Vue 3 composition API, TypeScript strict mode, Vite, Pinia, Element Plus.
- `createFleetRouter(history)` is a factory so tests can use memory history; the default export
  uses `createWebHistory`.
- The router initializes the session once and guards protected routes, preserving the requested
  path in `?redirect=`.
- The API client is a typed wrapper over `fetch` with an `ApiError` envelope parser and an
  unauthorized hook that clears auth state.
- Views poll migration state only while tasks are active.
- Production assets are embedded with `//go:embed all:dist`; the handler serves files from
  `dist`, caches hashed assets immutably, and falls back to `index.html` for SPA routes.

## Security headers

The HTTP handler sets `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
`Referrer-Policy: same-origin`, and a restrictive CSP. JSON error responses never include
stack traces; 5xx details are logged server-side only.

## Scaling and expansion

- To add a cluster, add a config entry with its endpoint and certificate, trust the certificate
  on that cluster, and restart or reconnect. No app-cluster membership is required.
- To add a node to an Incus cluster, use normal Incus cluster join procedures; this app needs
  no change.
- To add an app instance, deploy the same binary/config shape on another host and add it to the
  Caddy upstream pool. It will share no state with existing instances; that is intentional.
- The centralized migration lock is per process. Two app instances can attempt the same
  instance migration concurrently. Incus itself will reject conflicting operations, and the
  target existence check limits damage, but multi-instance coordination is not provided.

## Observability

- Structured JSON logs via `log/slog` to stdout.
- Request logs include method, path, status, bytes, and duration; non-GET or failing requests log
  at info, successful GETs at debug.
- `GET /api/v1/healthz` (unauthenticated) returns status, version, and time for load balancers.
- `GET /api/v1/version` returns the build version.

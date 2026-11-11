# HTTP API

Base path: `/api/v1`. All request and response bodies are JSON unless noted. The same server
also serves the Vue SPA at `/`.

## Authentication

The server has one shared admin token. There are two ways to authenticate:

1. **Browser session.** `POST /api/v1/auth/login` with the token; the response sets an
   `HttpOnly`, `SameSite=Lax` cookie named `incus_fleet_session`. The cookie is valid for 12
   hours. It is marked `Secure` when the request is TLS or carries
   `X-Forwarded-Proto: https` (as sent by Caddy).
2. **Bearer token.** Send `Authorization: Bearer <admin token>` on protected endpoints. This is
   intended for automation and does not need a login step.

`GET /api/v1/healthz`, `GET /api/v1/version`, `POST /auth/login`, and `POST /auth/logout` are
public. Everything under `/clusters`, `/instances`, `/migrations`, and `GET /auth/session`
requires authentication.

```bash
curl -sS https://fleet.example/api/v1/clusters \
  -H "Authorization: Bearer $INCUS_FLEET_AUTH_TOKEN"
```

### Login

```http
POST /api/v1/auth/login
Content-Type: application/json

{"token": "<admin token>"}
```

```json
{
  "authenticated": true,
  "expires_at": "2026-11-12T09:30:00Z"
}
```

Invalid tokens return `401` with code `invalid_token`.

### Session

```http
GET /api/v1/auth/session
```

```json
{"authenticated": true}
```

### Logout

```http
POST /api/v1/auth/logout
```

Returns `204 No Content` and expires the cookie. Logout is idempotent.

## Endpoints

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| GET | `/healthz` | no | Liveness/readiness probe |
| GET | `/version` | no | Build version |
| POST | `/auth/login` | no | Exchange admin token for a session cookie |
| POST | `/auth/logout` | no | Remove the current session |
| GET | `/auth/session` | yes | Report whether the request is authenticated |
| GET | `/clusters` | yes | All configured clusters with health summaries |
| GET | `/clusters/{cluster}` | yes | One cluster summary |
| POST | `/clusters/{cluster}/reconnect` | yes | Re-dial a cluster and return its summary |
| GET | `/clusters/{cluster}/instances?type=` | yes | Instances on one cluster |
| GET | `/clusters/{cluster}/instances/{instance}` | yes | Instance detail |
| GET | `/instances?type=` | yes | Instances across all clusters |
| GET | `/migrations?limit=` | yes | Recent migration tasks (default 50) |
| POST | `/migrations` | yes | Submit a migration (`202 Accepted`) |
| GET | `/migrations/{id}` | yes | One migration task |

`type` accepts `any` (default), `container`, or `virtual-machine`.

## Health and version

```json
// GET /api/v1/healthz
{"status": "ok", "version": "1.2.3", "time": "2026-11-11T09:30:00Z"}

// GET /api/v1/version
{"version": "1.2.3"}
```

## Clusters

```json
// GET /api/v1/clusters
{
  "clusters": [
    {
      "name": "c1",
      "display_name": "Primary site",
      "endpoint": "https://192.0.2.10:8443",
      "online": true,
      "server_name": "host-a",
      "server_version": "7.0.2",
      "clustered": true,
      "architectures": ["x86_64"],
      "members": [
        {
          "name": "host-a",
          "url": "https://192.0.2.10:8443",
          "status": "Online",
          "message": "Fully operational",
          "database": true,
          "roles": ["database-leader"],
          "architecture": "x86_64"
        }
      ],
      "instances": {
        "total": 3,
        "containers": 2,
        "virtual_machines": 1,
        "running": 2
      },
      "storage": {"pool": "default", "used": 250000000000, "total": 1000000000000},
      "networks": ["incusbr0"]
    }
  ]
}
```

Offline clusters keep the same shape with `online: false` and an `error` string. Storage and
instance counts may be zero when the cluster cannot be inspected.

`POST /clusters/{cluster}/reconnect` returns the updated summary object directly (not wrapped).

## Instances

```json
// GET /api/v1/instances?type=any
{
  "instances": [
    {
      "name": "web",
      "cluster": "c1",
      "type": "container",
      "status": "Running",
      "status_code": 103,
      "location": "host-a",
      "project": "default",
      "architecture": "x86_64",
      "description": "web frontend",
      "profiles": ["default"],
      "ipv4": ["10.0.0.0.5"],
      "ipv6": [],
      "memory": 134217728,
      "processes": 12,
      "created_at": "2026-11-10T08:00:00Z",
      "started_at": "2026-11-11T09:00:00Z",
      "storage_pool": "default",
      "migration_stateful": false,
      "live_migratable": false
    }
  ]
}
```

Instance detail adds:

- `os`, `os_version`, `kernel_version`, `hostname` from guest state;
- `ephemeral`;
- `config` — only safe selected keys (`image.*`, `limits.cpu`, `limits.memory`,
  `migration.stateful`, `security.secureboot`);
- `devices` — expanded devices;
- `state` — raw Incus state when available.

`live_migratable` is true only for a VM with `migration.stateful=true`. It does not guarantee the
VM is currently running; the migration preflight checks that separately.

## Migrations

### Submit

```http
POST /api/v1/migrations
Content-Type: application/json

{
  "source_cluster": "c1",
  "target_cluster": "c2",
  "instance": "web",
  "mode": "move",
  "live": false,
  "include_snapshots": false,
  "auto_start": true
}
```

| Field | Required | Notes |
| --- | --- | --- |
| `source_cluster` | yes | Must exist and be connected |
| `target_cluster` | yes | Must differ from source |
| `instance` | yes | Must exist on source and not on target |
| `mode` | no | `move` (default) or `copy` |
| `live` | no | Only `move`; requires a running VM with `migration.stateful=true` |
| `include_snapshots` | no | Default `false`; otherwise copies instance only |
| `auto_start` | no | Default `true`; starts target when source was running |

Returns `202 Accepted` with the migration object. The task runs asynchronously.

### Migration object

```json
{
  "id": "8f14e45fceea167a5a36dedd4bea2543",
  "source_cluster": "c1",
  "target_cluster": "c2",
  "instance": "web",
  "instance_type": "container",
  "mode": "move",
  "live": false,
  "status": "transferring",
  "progress": "transferring instance",
  "error": "",
  "created_at": "2026-11-11T09:30:00Z",
  "updated_at": "2026-11-11T09:30:04Z"
}
```

Status progression:

```
pending → preflight → stopping → transferring → verifying → starting → done
```

Not every stage occurs. `starting` is skipped for stopped instances with `auto_start=false`;
`stopping` is skipped for live migrations and already-stopped sources. Failures stop at
`failed`, with `progress` naming the failed stage and `error` containing the detail.

`GET /api/v1/migrations?limit=20` returns `{"migrations": [...]}`. `limit` must be a positive
integer. `GET /api/v1/migrations/{id}` returns one object.

## Errors

All errors use one envelope:

```json
{
  "error": {
    "code": "invalid_request",
    "message": "migrations: live migration requires a running instance"
  }
}
```

| HTTP | Code | Meaning |
| --- | --- | --- |
| 400 | `invalid_request` | Malformed JSON, bad `type`, bad `limit`, invalid migration fields |
| 401 | `unauthorized` | Missing/invalid session or bearer token |
| 401 | `invalid_token` | Wrong admin token at login |
| 404 | `not_found` | Unknown cluster, instance, or migration |
| 409 | `conflict` | Instance already on target, or migration already running for that instance |
| 503 | `cluster_unavailable` | Configured cluster cannot be reached |
| 500 | `internal_error` | Unexpected failure (details are logged, not returned) |

## Limits and behavior

- Request bodies are limited to 1 MiB.
- Migration execution has a 6-hour default timeout.
- Source stop operations use a 300-second timeout.
- A process restart marks unfinished migrations as failed; it never resumes a transfer.
- Concurrent migrations of the same source instance are rejected per app process. Running two
  app instances against the same clusters can still race; Incus will reject overlapping work.

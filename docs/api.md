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
| POST | `/clusters/{cluster}/instances` | yes | Create an instance from a local image |
| GET | `/clusters/{cluster}/instances/{instance}` | yes | Instance detail |
| PATCH | `/clusters/{cluster}/instances/{instance}` | yes | Update limits, root disk, NIC, description |
| POST | `/clusters/{cluster}/instances/{instance}/state` | yes | Start, stop, or restart an instance |
| GET | `/clusters/{cluster}/instances/{instance}/metrics` | yes | Live resource snapshot (CPU, memory, disk, network) |
| DELETE | `/clusters/{cluster}/instances/{instance}?force=` | yes | Delete an instance |
| GET | `/clusters/{cluster}/instances/{instance}/snapshots` | yes | List instance snapshots |
| POST | `/clusters/{cluster}/instances/{instance}/snapshots` | yes | Create a snapshot |
| POST | `/clusters/{cluster}/instances/{instance}/snapshots/{snapshot}/restore` | yes | Restore a snapshot |
| DELETE | `/clusters/{cluster}/instances/{instance}/snapshots/{snapshot}` | yes | Delete a snapshot |
| POST | `/clusters/{cluster}/instances/{instance}/devices` | yes | Attach a disk or NIC device |
| DELETE | `/clusters/{cluster}/instances/{instance}/devices/{device}` | yes | Detach a device |
| GET | `/clusters/{cluster}/instances/{instance}/console` | yes | Interactive console (WebSocket) |
| GET | `/clusters/{cluster}/instances/{instance}/exec` | yes | Interactive exec session (WebSocket) |
| GET | `/clusters/{cluster}/instances/{instance}/files?action=list&path=` | yes | List a directory inside the instance |
| GET | `/clusters/{cluster}/instances/{instance}/files?path=` | yes | Download one file |
| POST | `/clusters/{cluster}/instances/{instance}/files?path=` | yes | Upload a file (multipart `file` field) |
| POST | `/clusters/{cluster}/instances/{instance}/files?action=mkdir&path=` | yes | Create a directory |
| DELETE | `/clusters/{cluster}/instances/{instance}/files?path=` | yes | Delete one file |
| GET | `/clusters/{cluster}/images` | yes | Local images on a cluster |
| DELETE | `/clusters/{cluster}/images/{fingerprint}` | yes | Delete an image |
| GET | `/clusters/{cluster}/resources` | yes | Storage pools and networks with usage |
| POST | `/clusters/{cluster}/storage-pools` | yes | Create a storage pool |
| PATCH | `/clusters/{cluster}/storage-pools/{pool}` | yes | Update pool description/config |
| DELETE | `/clusters/{cluster}/storage-pools/{pool}` | yes | Delete a storage pool |
| GET | `/clusters/{cluster}/storage-pools/{pool}/volumes` | yes | List custom volumes on a pool |
| POST | `/clusters/{cluster}/storage-pools/{pool}/volumes` | yes | Create a custom volume |
| DELETE | `/clusters/{cluster}/storage-pools/{pool}/volumes/{volume}` | yes | Delete a custom volume |
| POST | `/clusters/{cluster}/networks` | yes | Create a managed network |
| PATCH | `/clusters/{cluster}/networks/{network}` | yes | Update a managed network |
| DELETE | `/clusters/{cluster}/networks/{network}` | yes | Delete a managed network |
| GET | `/clusters/{cluster}/profiles` | yes | List instance profiles |
| POST | `/clusters/{cluster}/profiles` | yes | Create a profile |
| PATCH | `/clusters/{cluster}/profiles/{profile}` | yes | Update a profile |
| DELETE | `/clusters/{cluster}/profiles/{profile}` | yes | Delete a profile |
| PATCH | `/clusters/{cluster}/members/{member}` | yes | Update member description, failure domain, groups |
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

### Instance management

Create (`POST /clusters/{cluster}/instances`) takes `name` (required, `[A-Za-z0-9][A-Za-z0-9_.-]*`,
max 63 chars), `type` (`container` default, or `virtual-machine`), `image` (required, fingerprint of
a local image), `description`, `profiles`, `cpu` (count, range, or list), `memory` (e.g. `512MiB`),
`root_disk_size`, `storage_pool` (default `default`), `network`, `ipv4`, `ipv6` (static addresses
require `network`), and `start` (default `true`). Wait: up to 15 minutes.

`PATCH .../instances/{instance}` accepts nullable fields; empty strings clear values. NIC changes
target `device` (default `eth0`).

`POST .../state` body: `{"action": "start|stop|restart", "force": false}`.

### Snapshots

```json
// GET .../instances/web/snapshots
{
  "snapshots": [
    { "name": "snap0", "created_at": "2026-11-10T08:00:00Z", "size": 1024, "stateful": true }
  ]
}
```

Create body: `{"name": "backup", "stateful": false}`. Stateful snapshots require a stateful VM.
Restore rolls the instance back (`InstancePut.Restore`). Snapshot and restore waits: up to 15
minutes.

### Devices

Attach body (disk):

```json
{ "name": "data", "type": "disk", "pool": "default", "source": "data", "path": "/mnt/data", "size": "10GiB" }
```

Attach body (NIC): `{ "name": "eth1", "type": "nic", "network": "incusbr0", "ipv4": "10.0.0.0.9" }`.
Disk requires `path`; NIC requires an existing `network`. Device names must not collide with
existing devices. Both return the updated instance detail.

### Terminal sessions

`GET .../console` and `GET .../exec` upgrade to WebSockets (session cookie or bearer). The exec
first frame is JSON: `{"command": ["bash"], "cwd": "/root", "width": 100, "height": 30}`. The UI
starts a persistent interactive shell (`command: ["bash"]`) and forwards each submitted line on
stdin, so environment and working directory survive across commands; when the session ends the UI
reopens it and replays the pending line.

- Binary frames carry raw terminal bytes both ways;
- text frames from the server are base64-encoded output and are decoded before delivery;
- text frames from the client are control messages: `{"type":"resize","width":W,"height":H}` or
  `{"type":"signal","signal":"SIGINT"}`.

The session ends when either side closes or the Incus operation finishes.

### Instance metrics

`GET .../metrics` returns `{"metrics": {...}}` with the live state of one instance: `status`,
`pid`, `processes`, `cpu.usage` (cumulative nanoseconds), `memory` (usage/peak/total/swap in
bytes), `disk` (per-mountpoint usage/total), and `network` (per-interface cumulative counters,
loopback excluded). Counters are cumulative since instance start; clients derive rates by
differencing consecutive samples.

### Files

All paths must be absolute. `GET .../files?action=list&path=/var/log` returns
`{"files": [{"name", "size", "mode", "is_dir", "modtime"}]}` (directories first). Download streams
the file as an attachment. Upload is `multipart/form-data` with a `file` field (32 MiB body cap,
mode 0644). `action=mkdir` creates a directory. Delete removes one file.

### Cluster resources and administration

`GET .../resources` returns storage pools (with `config`, usage, `used_by`) and networks (with
`config`, managed flag, addresses). Pool create takes `name` + `driver` + optional
`description`/`config`; updates merge config and delete keys set to the empty string. Networks
follow the same merge rules; only managed networks can be updated or deleted.

Profiles support full CRUD; updates replace the device map wholesale when `devices` is provided.

`PATCH .../members/{member}` accepts `description`, `failure_domain`, and `groups` (nil keeps the
current groups).

### Volumes and images

`GET .../storage-pools/{pool}/volumes` lists custom volumes only. Create takes `name`,
`description`, `size` (validated byte string), and extra `config`. `DELETE` removes one custom
volume. `DELETE .../images/{fingerprint}` removes a local image; the UI's cleanup action deletes
every image with `cached=true`.

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

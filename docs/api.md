# HTTP API

**English** | [简体中文](zh-CN/api.md)

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
public. Everything under `/clusters`, `/instances`, `/migrations`, `/keys`, `/scripts`, and
`/batch`, plus `GET /auth/session`, requires authentication.

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
| POST | `/clusters/{cluster}/images/import?type=&alias=` | yes | Upload an image (`multipart/form-data`) |
| POST | `/clusters/{cluster}/images/{fingerprint}/copy` | yes | Distribute a local image to another cluster |
| POST | `/clusters/{cluster}/images/{fingerprint}/refresh` | yes | Re-pull a cached image from its origin |
| GET | `/clusters/{cluster}/iso` | yes | List installer ISO volumes |
| POST | `/clusters/{cluster}/iso?pool=` | yes | Upload an ISO installer (`multipart/form-data`) |
| DELETE | `/clusters/{cluster}/iso/{pool}/{name}` | yes | Delete an ISO volume |
| POST | `/clusters/{cluster}/iso/{pool}/{name}/launch` | yes | Create a blank VM that boots the ISO |
| POST | `/clusters/{cluster}/instances/{instance}/clone` | yes | Clone an instance within its cluster |
| POST | `/clusters/{cluster}/instances/{instance}/rebuild` | yes | Rebuild the root disk from another image |
| POST | `/clusters/{cluster}/instances/{instance}/ops` | yes | Run a script, set a password, or inject a key |
| GET | `/clusters/{cluster}/instances/{instance}/vga` | yes | Graphical console (WebSocket) |
| GET | `/clusters/{cluster}/instances/{instance}/spice?port=` | yes | SPICE channel bridge (WebSocket) |
| POST | `/clusters/{cluster}/instances/{instance}/spice/prepare` | yes | Configure the VM SPICE listener |
| GET | `/keys` | yes | List shared SSH public keys |
| POST | `/keys` | yes | Store a public key |
| PATCH | `/keys/{id}` | yes | Update a stored public key |
| DELETE | `/keys/{id}` | yes | Delete a stored public key |
| GET | `/scripts` | yes | List shared scripts |
| POST | `/scripts` | yes | Store a script |
| PATCH | `/scripts/{id}` | yes | Update a stored script |
| DELETE | `/scripts/{id}` | yes | Delete a stored script |
| POST | `/batch/keys` | yes | Inject stored keys into many instances |
| POST | `/batch/scripts` | yes | Run one stored script on many instances |
| POST | `/batch/power` | yes | Start, stop, or restart many instances |

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

### Dynamic cluster registration

```json
// POST /api/v1/clusters
{
  "name": "c64",
  "display_name": "Cluster 64",
  "endpoint": "https://192.0.2.10:8443",
  "project": "default",
  "tls_cert": "-----BEGIN CERTIFICATE-----\n...",
  "tls_key": "-----BEGIN EC PRIVATE KEY-----\n...",
  "tls_server_cert": "-----BEGIN CERTIFICATE-----\n..."
}
```

`tls_cert`/`tls_key` are PEM bodies (the UI reads them from the picked files);
`tls_server_cert` is optional. The cluster is dialed first; only a successful connection is
persisted in the store (SQLite/PostgreSQL) and re-registered on startup. Failure returns the
connection error and leaves no phantom entry. `201` returns the cluster summary.

`DELETE /api/v1/clusters/{cluster}` removes a dynamically registered cluster (`204`). Clusters
defined in the configuration file return `404` here; unregistering a name that only exists in the
manager also returns `404`.

### Cluster traffic

```json
// GET /api/v1/clusters/{cluster}/traffic
{
  "at": "2026-11-17T05:13:07Z",
  "total_rx_bytes": 3003418965,
  "total_tx_bytes": 40544880,
  "total_rx_packets": 41558733,
  "total_tx_packets": 636161,
  "instances": [
    {"name": "deb13-a", "rx_bytes": 1010964553, "tx_bytes": 14306432,
     "rx_packets": 14000000, "tx_packets": 210000}
  ]
}
```

Counters are cumulative since instance boot and only include running instances; the dashboard
polls every 3 seconds and derives per-second rates from consecutive samples.

## Adding a cluster member

`POST /api/v1/clusters/{cluster}/members` provisions a new host and joins it to the cluster. The
fleet server connects to the host over SSH, optionally installs Incus from an apt repository,
submits the Incus join request through the host's local socket, and waits for the member to report
`Online`.

```json
{
  "name": "node-c",
  "address": "198.51.100.11",
  "ssh_user": "node-admin",
  "ssh_port": 22,
  "ssh_password": "optional-ssh-password",
  "sudo_password": "optional-sudo-password",
  "install_incus": true,
  "install_vms": true,
  "ignore_host_key": true,
  "storage_source": "/srv/incus",
  "incus_repo": "https://pkgs.zabbly.com/incus/lts-7.0",
  "incus_repo_key": "https://pkgs.zabbly.com/key.asc"
}
```

- `name` is optional; the node hostname is used when omitted.
- `ssh_user` defaults to `node_access.ssh_user`; the fleet SSH key is used unless
  `ssh_password` is supplied.
- `sudo_password` is used once when the SSH account needs a password for sudo. After a successful
  join the fleet installs a passwordless whitelist for the account.
- `install_incus` adds the apt source (`incus_repo`/`incus_repo_key`) and installs the package.
  The installed version must match the cluster's Incus version.
- `ignore_host_key` skips SSH host key verification for a node that is not in `known_hosts` yet.
- `storage_source` fills the per-member `storage-pool/default/source` value; it defaults to
  `/srv/incus`.

`201` returns the joined member and the provisioning steps:

```json
{
  "member": {"name": "node-c", "status": "Online", "roles": [], "url": "https://198.51.100.11:8443"},
  "steps": [
    {"name": "connect", "status": "ok", "detail": "SSH connection established to 198.51.100.11"},
    {"name": "install", "status": "ok", "detail": "Incus installed from https://pkgs.zabbly.com/incus/lts-7.0"},
    {"name": "token", "status": "ok", "detail": "Join token generated for node-c"},
    {"name": "join", "status": "ok", "detail": "Join request accepted by cluster c2"},
    {"name": "online", "status": "ok", "detail": "Member node-c is online"},
    {"name": "sudoers", "status": "ok", "detail": "Passwordless node network sudo rules installed"},
    {"name": "fleet_key", "status": "ok", "detail": "Fleet SSH key authorized for node-admin"}
  ]
}
```

Post-join setup steps report `warning`/`skipped` without failing the request because the member is
already part of the cluster. Errors map to `400 invalid_request`, `404 not_found`, `409 conflict`
(member name already present), `503 cluster_unavailable`, or `500 internal_error`.

The request can take several minutes; the handler extends its write deadline and the server's
global timeout still applies to the request context. Passwords are used only in memory and are
never persisted or logged.

## Node network (per cluster member)

These endpoints manage host networking on one cluster member. The fleet host's own member is
served locally through `sudo -n`; every other member is reached over SSH with the identity from
`node_access`. Both paths require the sudo whitelist described in `docs/deployment.md`.

```json
// GET /api/v1/clusters/c2/members/node-b/node?wifi=wlan0
{
  "host": "node-b",
  "nics": [
    {"name": "eth0", "state": "up", "kind": "ethernet", "mac": "aa:bb:cc:dd:ee:ff", "mtu": 1500,
     "addresses": [{"family": "inet", "address": "198.51.100.10", "prefixlen": 24}]},
    {"name": "wlan0", "state": "up", "kind": "wifi", "addresses": [],
     "wifi": {"connected": true, "ssid": "Office", "signal": -55, "frequency": 2437}}
  ],
  "routes": [{"destination": "default", "gateway": "198.51.100.1", "device": "eth0", "metric": "100"}],
  "firewall": [{"handle": "7", "rule": "tcp dport 22 accept comment \"fleet_rule\""}],
  "wifi_scan": [{"ssid": "Office", "signal": -45, "frequency": 2437, "secure": true}],
  "wifi_device": "wlan0",
  "capabilities": ["nics", "routes", "firewall"]
}
```

The `wifi` query parameter selects the interface to scan; without it the first wireless interface
is used. Scan results are deduplicated by SSID (strongest BSS kept) and require the interface to
be up.

All paths below are relative to `/api/v1/clusters/{cluster}/members/{member}/node`:

| Method & path | Purpose |
|---|---|
| `POST /networks/{name}/state` `{"up": bool}` | bring a NIC up/down |
| `PATCH /networks/{name}` `{"dhcp": bool, "address": "10.0.0.50/24", "gateway": "...", "persist": bool}` | static address or DHCP; `persist` writes `/etc/network/interfaces.d/<name>` |
| `POST /networks/{name}/wifi` `{"ssid": "...", "password": "..."}` | associate + DHCP lease via wpa_supplicant |
| `GET/POST /routes`, `DELETE /routes/{destination}` | routing table and static routes |
| `GET/POST /firewall`, `DELETE /firewall/{handle}` | fleet_filter nftables table rules |

Handler errors map to `400 invalid_request` (bad input), `404 not_found` (unknown cluster or
member), and `503 node_unavailable` (missing SSH access, sudo rules, binaries, or host failure).

The snapshot includes `backend` (`local`, `ssh`, or, when the agent is enabled, `agent`). Mutating
responses set `X-Incus-Fleet-Backend` and, when the operation was recorded, `X-Incus-Fleet-Task`.
The backend choice comes from `node_access.mode` (`auto`, `agent`, `ssh`, or `local`). With the
default `auto`, reads may fall back to SSH; mutating operations fall back only when
`node_access.ssh_write_fallback` is true (the default for deployments that predate the agent).

## Agents

The host agent is optional. Enrollment tokens are single-use and bound to one cluster member. The
agent gateway itself listens on the separate `agent.listen` TLS port (`:8444` by default) and is
not part of the browser API; only the admin endpoints below are exposed on the normal HTTP port.

| Method & path | Purpose |
|---|---|
| `POST /api/v1/agents/enrollment-tokens` | create a one-time token; requires an existing cluster |
| `GET /api/v1/agents?cluster=&member=` | list enrolled agents, optionally filtered by cluster/member |
| `GET /api/v1/agents/{id}/snapshot` | dispatch a read-only `nodenet.snapshot` to a connected agent |

```json
// POST /api/v1/agents/enrollment-tokens
{"cluster": "c1", "member": "node-a", "ttl_seconds": 900}
// 201
{"token": "one-time-value", "cluster": "c1", "member": "node-a",
 "expires_at": "2026-11-18T00:15:00Z"}
```

The token value is returned only once; the database stores its SHA-256 hash. Enrollment itself is
`POST /api/v1/agent/enroll` on the agent gateway with a PKIX public key, cluster, member, and token.
The gateway returns the agent ID, client certificate, and CA certificate. The agent then connects
to `GET /api/v1/agent/connect` with mTLS and sends protobuf `Envelope` frames over the WebSocket.
Certificates are renewed automatically: the agent calls `POST /api/v1/agent/renew` with its current
mTLS identity when the certificate is within 30 days of expiry. The gateway verifies the current
fingerprint, signs a certificate for the submitted PKIX public key, and returns the new PEM and CA.

`GET /api/v1/agents/{id}/snapshot` returns:

```json
{
  "snapshot": {
    "host": "node-a",
    "nics": [{"name": "eth0", "state": "up", "kind": "ethernet",
              "addresses": [{"family": "inet", "address": "192.0.2.10", "prefixlen": 24}]}],
    "routes": [{"destination": "default", "gateway": "192.0.2.1", "device": "eth0"}],
    "capabilities": ["nics", "routes"]
  }
}
```

`503 agent_offline` means the agent has no live WebSocket; `504 agent_timeout` means it did not
answer before the dispatch timeout; `502 agent_error` means the agent rejected or failed the task.

`POST /api/v1/agents/{id}/bootstrap` builds, signs, and dispatches an Incus install/init/join
spec to the agent's privileged helper. The spec is signed with the agent CA key; the helper
verifies it against the CA certificate in the agent identity directory before executing anything.
`mode=join` requests a single-use token from the existing cluster and renders a join preseed;
`mode=init` renders a new-cluster preseed with a directory pool and `incusbr0` bridge.

```json
// join an existing cluster; install Incus only if the repository is preconfigured
{"mode": "join", "address": "198.51.100.11", "storage_source": "/srv/incus",
 "install_mode": "package", "version_pin": "7.0", "packages": ["incus"]}
// initialize a new cluster
{"mode": "init", "address": "192.0.2.10", "network_cidr": "10.0.0.1/24",
 "install_mode": "existing"}
// 200
{"result": {"ok": true, "task_id": "...", "mode": "bootstrap",
            "message": "Incus bootstrap complete", "steps": ["install", "service", "preseed", "ready"]}}
```

The helper is idempotent per task ID: completed install/service/preseed/ready steps are recorded in
`/var/lib/incus-fleet-helper/bootstrap-<task_id>.json` and skipped on retry. `install_mode=package`
uses the distribution's configured repositories and supports apt, dnf, zypper, apk, and pacman;
the current helper rejects `repo_url` rather than adding an unverified repository. Incus version
pins are checked after installation. `confirm`/`rollback` do not undo package installation.

`POST /api/v1/agents/{id}/network` dispatches one allowlisted network mutation through the
privileged helper. Supported actions are `link-state`, `address`, `route-add`, `route-delete`,
`firewall-add`, `firewall-delete`, `wifi-connect`, `confirm`, and `rollback`.

```json
// bring eth0 down
{"action": "link-state", "interface": "eth0", "up": false}
// apply a static address and persist through the detected manager;
// rollback_after_seconds is optional and starts a detached rollback timer
{"action": "address", "interface": "eth0", "address": "10.0.0.50/24",
 "gateway": "10.0.0.1", "persist": true, "task_id": "task-1",
 "rollback_after_seconds": 120}
// confirm or roll back a previous operation
{"action": "confirm", "task_id": "task-1"}
```

The response is `{"result": {"ok": true, "action": "...", "task_id": "...", "backend": "helper",
"persistence": "ifupdown"}}`. `confirm` removes the rollback snapshot; `rollback` restores the
captured addresses, routes, link state, and persistence file for that interface. Tasks are
allowlisted; the helper rejects unknown actions, interface names, addresses, and task IDs. Wi-Fi
passwords are passed only to the helper and are never stored in task/audit records.

## Metrics

The center keeps a **one-hour in-memory ring buffer** of agent telemetry (`fleet_` prefix,
10-second interval). It does not persist metrics to the database. `cluster`, `member`, and
`agent_id` labels are injected by the center and cannot be overridden by an agent.

| Method & path | Purpose |
|---|---|
| `GET /api/v1/metrics/query?cluster=&member=&agent=&metric=&from=&to=` | series with points in range |
| `GET /api/v1/metrics/snapshot` | latest point per matching series |
| `GET /api/v1/metrics/export?format=prometheus\|json\|csv` | stream the current buffer |

`from`/`to` accept RFC3339, a duration such as `1h` (relative to now), or Unix seconds. The default
range is the last hour. Metrics are exposed as:

```json
{
  "series": [
    {
      "name": "fleet_host_cpu_usage_ratio",
      "labels": {"cluster": "c1", "member": "node-a", "agent_id": "..."},
      "type": "gauge",
      "points": [{"at": "2026-11-18T00:00:00Z", "value": 0.25}]
    }
  ]
}
```

Prometheus export includes `# TYPE` lines and millisecond timestamps. CSV is long-form:
`timestamp,metric,cluster,member,agent_id,value`. If `metrics.enabled` is false, all three endpoints
return `503 unavailable`. Restarting the center resets the one-hour history; this is intentional.

The agent exports host series (`fleet_host_cpu_usage_ratio`, `fleet_host_memory_bytes`,
`fleet_host_load`, `fleet_host_uptime_seconds`, `fleet_host_network_bytes_total`) and, from the
cgroup v2 fallback, per-instance series (`fleet_instance_cpu_seconds_total`,
`fleet_instance_memory_bytes`, `fleet_instance_processes`, `fleet_instance_io_bytes_total`). The
optional eBPF object adds host counters (`fleet_host_tcp_retransmits_total`,
`fleet_host_oom_kills_total`) and cgroup-attributed instance series
(`fleet_instance_tcp_retransmits_total`, `fleet_instance_oom_kills_total`, and
`fleet_instance_block_latency_seconds_bucket|_sum|_count`). `fleet_agent_telemetry_mode{mode="ebpf"|"procfs"}`
reports the active path. Block-latency buckets use powers of two nanoseconds (roughly 2ns through
1s) plus `+Inf`. TCP retransmit cgroup attribution is best-effort because retransmit timers can run
in softirq context; the host counter is exact.

## External metrics

The center can forward the same telemetry to Prometheus or VictoriaMetrics and proxy PromQL
queries back for the UI. External integration is disabled by default; see
[Deployment](deployment.md#prometheusvictoriametrics).

| Method & path | Auth | Purpose |
|---|---|---|
| `GET /api/v1/metrics/query_range?query=&from=&to=&step=` | admin session/bearer | PromQL range query proxy |
| `GET /api/v1/sd/agents` | HTTP SD token | Prometheus HTTP service discovery |
| `GET /api/v1/agents/{id}/metrics/prometheus` | HTTP SD token | center-proxied agent metrics scrape |

Remote write uses the Prometheus 1.0 wire format (snappy-compressed protobuf) at the configured
`metrics.remote_write.url`. Prometheus requires `--web.enable-remote-write-receiver`; VictoriaMetrics
accepts `/api/v1/write` (single-node) or `/insert/<accountID>/prometheus/api/v1/write` (cluster).

`query_range` accepts `from`/`to` in RFC3339, relative duration (`1h`), or Unix seconds; `step` is a
duration such as `1m`. The center forwards to Prometheus `/api/v1/query_range` or
VictoriaMetrics `/prometheus/api/v1/query_range` and returns the upstream JSON. Queries are
admin-only and bounded by the configured timeout.

The HTTP SD response follows the Prometheus format:

```json
[
  {
    "targets": ["fleet.example"],
    "labels": {
      "__scheme__": "https",
      "__metrics_path__": "/api/v1/agents/AGENT_ID/metrics/prometheus",
      "cluster": "c1",
      "member": "node-a",
      "agent_id": "AGENT_ID"
    }
  }
]
```

The SD token is configured separately from the admin token and is sent as
`Authorization: Bearer <token>`. Only online agents are returned. `mode=proxy` (default) has Prometheus scrape the center, which returns the latest in-memory sample
plus `fleet_agent_up`. `mode=direct` returns the agent's own metrics listener from the
`metrics_address` fact; the agent must run with `--metrics-listen` and the same bearer token used by
the scrape configuration. Agents without an advertised address are skipped.

```yaml
# Prometheus scrape config for direct mode
scrape_configs:
  - job_name: incus-fleet
    authorization:
      credentials_file: /etc/prometheus/fleet-metrics.token
    http_sd_configs:
      - url: https://fleet.example/api/v1/sd/agents
        authorization:
          credentials_file: /etc/prometheus/fleet-sd.token
```

Agent identity lifecycle:

| Method & path | Purpose |
|---|---|
| `POST /api/v1/agents/{id}/certificate` | sign a new certificate for the agent's PKIX public key |
| `POST /api/v1/agents/{id}/revoke` | revoke an agent; the gateway rejects future connections |
| `POST /api/v1/tasks` | enqueue an async `agent.network` task for the HA dispatcher |

```json
// rotate
{"public_key": "-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----\n"}
// 200
{"certificate": "-----BEGIN CERTIFICATE-----\n...\n", "fingerprint": "...",
 "expires_at": "2027-02-16T00:00:00Z"}
// revoke
POST /api/v1/agents/AGENT_ID/revoke -> 204
```

## Host tasks and audit

Every mutating node operation is recorded as a host task plus an immutable audit entry. Tasks and
audit records live in the same SQLite/PostgreSQL database as migration history; the schema is
created or upgraded automatically at startup.

| Method & path | Purpose |
|---|---|
| `GET /api/v1/tasks?cluster=&member=&limit=` | recent tasks, newest first; `limit` 1-200, default 50 |
| `POST /api/v1/tasks` | enqueue `{cluster,member,type,plan}`; `202` with the queued task |
| `GET /api/v1/tasks/{id}` | one task and its state-transition events |
| `GET /api/v1/audit?cluster=&member=&limit=` | host audit records, newest first |
| `GET /api/v1/audit/export?format=jsonl\|csv` | full audit export, no UI limit |

```json
// GET /api/v1/tasks/{id}
{
  "task": {
    "id": "b4f0...",
    "cluster": "c1",
    "member": "node-a",
    "type": "node.route.add",
    "state": "succeeded",
    "plan": "destination=10.0.1.0/24 gateway=192.0.2.1 device=eth0",
    "plan_hash": "...",
    "result": "succeeded",
    "created_by": "admin",
    "created_at": "2026-11-18T00:00:00Z",
    "started_at": "2026-11-18T00:00:00Z",
    "finished_at": "2026-11-18T00:00:01Z",
    "updated_at": "2026-11-18T00:00:01Z"
  },
  "events": [
    {"state": "running", "message": "started", "at": "2026-11-18T00:00:00Z"},
    {"state": "succeeded", "message": "succeeded", "at": "2026-11-18T00:00:01Z"}
  ]
}
```

An asynchronous network task uses `type: "agent.network"` and a `NodenetApplySpec`-shaped plan:

```json
POST /api/v1/tasks
{"cluster":"c1","member":"node-a","type":"agent.network",
 "plan":{"action":"route-add","destination":"10.0.1.0/24","gateway":"192.0.2.1","device":"eth0"}}
// 202
{"task":{"id":"...","state":"queued","lease_owner":""}}
```

The task is claimed by a center instance running the dispatcher (`ha.enabled=true`) and executed
through the member's online agent. Leases prevent two instances from running the same task.

Task states are `queued`, `dispatched`, `accepted`, `running`, `succeeded`, `failed`, `canceled`,
and `unknown`. `unknown` means the control plane lost contact with the executor while the
operation may have completed; it is not treated as a failure. Audit records include actor, source
IP, backend, action, task ID, result, and a redacted plan. Wi-Fi passwords, SSH passwords, sudo
passwords, Incus join tokens, and private keys are never stored in tasks or audit records.

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
      "ipv4": ["10.0.0.5"],
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

Attach body (NIC): `{ "name": "eth1", "type": "nic", "network": "incusbr0", "ipv4": "10.0.0.9" }`.
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

```mermaid
flowchart LR
    pending --> preflight --> stopping --> transferring --> verifying --> starting --> done
```

Not every stage occurs. `starting` is skipped for stopped instances with `auto_start=false`;
`stopping` is skipped for live migrations and already-stopped sources. Failures stop at
`failed`, with `progress` naming the failed stage and `error` containing the detail.

`GET /api/v1/migrations?limit=20` returns `{"migrations": [...]}`. `limit` must be a positive
integer. `GET /api/v1/migrations/{id}` returns one object.

## Images

### Import

```http
POST /api/v1/clusters/{cluster}/images/import?type=container&alias=debian/13
Content-Type: multipart/form-data
```

Multipart fields:

- `image` — the image metadata tar archive, or a unified image tar archive (required);
- `rootfs` — the `.root` root filesystem, only for split exports.

`type` accepts `container` (default) or `virtual-machine`; `alias` is optional. The request
body is capped at 8 GiB. A successful import returns
`{"fingerprint": "<sha256>"}`; if the Incus daemon reports no fingerprint the response is
`204 No Content`.

### Distribute

`POST /clusters/{cluster}/images/{fingerprint}/copy` with
`{"target_cluster": "c2", "alias": "debian/13"}` streams the image directly to another
cluster and returns `204 No Content`. The target must differ from the source and be connected.

### Refresh

`POST /clusters/{cluster}/images/{fingerprint}/refresh` asks Incus to pull a cached image
again from its origin, then returns `204 No Content`. The alias is preserved.

## ISO installers

| Method | Path | Result |
| --- | --- | --- |
| GET | `.../iso` | `{"isos": [{"name", "pool", "description", "size"}]}` |
| POST | `.../iso?pool=default` | Upload (`image` multipart field, 12 GiB cap) → `201 {"name","pool"}` |
| DELETE | `.../iso/{pool}/{name}` | `204 No Content` |
| POST | `.../iso/{pool}/{name}/launch` | Create a VM from the ISO → `201` instance detail |

Launch body:

```json
{
  "name": "db-installer",
  "storage_pool": "default",
  "network": "incusbr0",
  "root_disk": "20GiB",
  "start": true
}
```

`iso_name` comes from the URL path and does not need to be repeated in the body. Fleet
relaxes Secure Boot for installer compatibility and configures SPICE automatically.

## Instance operations

### Clone

`POST .../instances/{instance}/clone` with `{"target": "web-2", "start": true}` copies a
complete instance (disks and config) within the same cluster and returns `201` with the new
instance detail. `start` defaults to `true`.

### Rebuild

`POST .../instances/{instance}/rebuild` with `{"image": "debian/13"}` replaces the root disk
with the given image (alias or fingerprint). The instance must be stopped. Data volumes are
not changed. Returns the updated instance detail.

### Maintenance actions

`POST .../instances/{instance}/ops` executes one action as root inside the guest:

```json
{"action": "run-script", "script": "apt-get update && apt-get -y upgrade"}
{"action": "set-password", "user": "root", "password": "<new password>"}
{"action": "inject-key", "user": "root", "key": "ssh-ed25519 AAAA... user@example"}
```

The response is `{"exit_code": 0, "stdout": "...", "stderr": "..."}`. Scripts run through
`sh -s`; passwords go to `chpasswd`; keys are appended to `~/.ssh/authorized_keys`. Payloads
are written to stdin and never appear in process arguments.

### Graphical console

`POST .../instances/{instance}/spice/prepare` with `{"port": 0, "apply": true}` configures the
VM's QEMU SPICE TCP listener. A zero port derives a stable port from the instance name in the
range 5910-5989. The response is:

```json
{"port": 5931, "applied": true, "restart_required": false}
```

`apply: true` restarts a running VM so the QEMU arguments take effect; the first session may
therefore require a restart. `GET .../spice?port=5931` upgrades to a WebSocket and bridges one
SPICE channel to the VM's TCP listener. `GET .../vga` upgrades to the Incus graphical console
WebSocket.

The SPICE listener uses `disable-ticketing=on` and binds on all interfaces of the cluster
member. Restrict the 5910-5989 range with a firewall; do not expose it to untrusted networks.

## Shared assets

### SSH public keys

`GET /keys` returns `{"keys": [...]}`. Each key has `id`, `name`, `public_key`, `comment`, and
`created_at`. `POST /keys` takes `name`, `public_key`, and an optional `comment` and returns
`201` with the stored object. `public_key` may contain multiple newline-separated key lines;
identical material is stored once under a content-derived id. `PATCH /keys/{id}` updates a
key, and `DELETE /keys/{id}` returns `204 No Content`.

### Scripts

`GET /scripts` returns `{"scripts": [...]}` with `id`, `name`, `description`, `body`,
`created_at`, and `updated_at`. `POST /scripts` requires `name` and `body` and returns `201`.
`PATCH /scripts/{id}` updates a script; `DELETE /scripts/{id}` returns `204 No Content`.

### Batch operations

All three endpoints take a `targets` array of `{"cluster", "instance", "user"}` objects.
`user` is optional and defaults to `root` for key injection; scripts always run as root.

```json
// POST /batch/keys
{"targets": [{"cluster": "c1", "instance": "web"}], "key_ids": ["key-1a2b3c4d5e6f7788"]}

// POST /batch/scripts
{"targets": [{"cluster": "c1", "instance": "web"}], "script_id": "script-..."}

// POST /batch/power
{"targets": [{"cluster": "c1", "instance": "web"}], "action": "restart", "force": true}
```

Each call returns `{"items": [...]}` where an item is
`{"cluster", "instance", "ok", "exit_code", "stdout", "stderr", "error"}`. Instances run
independently: one failure does not stop the rest. The response is `200 OK` when every item
succeeds and `207 Multi-Status` when any item reports an error.

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
| 500 | `internal_error` | Unexpected failure; `message` contains the underlying error detail |

`207 Multi-Status` is not an error envelope: batch endpoints use it to return per-instance
results when at least one target failed.

## Limits and behavior

- JSON request bodies are limited to 1 MiB.
- Multipart uploads have separate caps: 8 GiB for images, 12 GiB for ISO files, and 32 MiB
  for instance file uploads.
- Migration execution has a 6-hour default timeout.
- Source stop operations use a 300-second timeout.
- Instance `exec` and maintenance actions use a 10-minute timeout; image transfer uses 30
  minutes.
- A process restart marks unfinished migrations as failed; it never resumes a transfer.
- Concurrent migrations of the same source instance are rejected per app process. Running two
  app instances against the same clusters can still race; Incus will reject overlapping work.

# Deployment

**English** | [简体中文](zh-CN/deployment.md)

This guide deploys one incus-fleet instance per management host, with each instance connecting
to every Incus cluster. A separate Caddy host load-balances the UI/API.

The examples use documentation addresses and placeholder names. Replace them with your values.
Never commit real tokens, private keys, or production configuration.

## Topology

```mermaid
flowchart LR
    caddy["Caddy<br/>sticky sessions · TLS"]

    subgraph apps["App hosts · independent instances"]
        direction TB
        fleetA["incus-fleet<br/>@ app-a<br/>HTTP :8080"]
        fleetB["incus-fleet<br/>@ app-b<br/>HTTP :8080"]
    end

    subgraph clusters["Incus clusters"]
        direction TB
        c1["c1<br/>https://192.0.2.10:8443"]
        c2["c2<br/>https://192.0.2.11:8443"]
        c1 ~~~ c2
    end

    caddy -->|sticky cookie| apps
    apps -->|HTTPS · client cert| clusters
```

Each app instance is independent: separate binary, config, certificates, database, and
in-memory sessions. Do not point both app instances at one SQLite file.

## Prerequisites

On each app host:

- Linux with systemd, Go is **not** required if you deploy a release binary.
- Network access to every Incus API endpoint on port 8443.
- A dedicated service account and directory (examples use `/home/incus`).
- SQLite (default) or a PostgreSQL server.

On each Incus cluster:

- Incus initialized with a storage pool and network matching the instances you migrate.
- Permission to add a trusted client certificate (`incus-admin` group or root).

## 1. Build a release binary

```bash
make release
ls dist/incus-fleet-linux-amd64
```

Copy the binary to each app host:

```bash
sudo install -d -o incus-fleet -g incus-fleet -m 0755 /home/incus/bin
sudo install -o incus-fleet -g incus-fleet -m 0755 \
  dist/incus-fleet-linux-amd64 /home/incus/bin/incus-fleet
```

If the account does not exist yet:

```bash
sudo useradd --system --home-dir /home/incus --create-home \
  --shell /usr/sbin/nologin incus-fleet
sudo install -d -o incus-fleet -g incus-fleet -m 0750 \
  /home/incus/bin /home/incus/certs /home/incus/data
```

## 2. Create and trust client certificates

Generate a distinct certificate for each app host. Run this on a trusted workstation:

```bash
scripts/gen-client-cert.sh app-a ./certs
scripts/gen-client-cert.sh app-b ./certs
```

Copy `app-a.crt`/`app-a.key` to app host A and `app-b.crt`/`app-b.key` to app host B, under
`/home/incus/certs`, owned by `incus-fleet` with mode `0600` on keys.

Trust both certificates on **each** Incus cluster that either app instance will manage:

```bash
sudo incus config trust add-certificate /path/to/app-a.crt --name incus-fleet-app-a
sudo incus config trust add-certificate /path/to/app-b.crt --name incus-fleet-app-b
incus config trust list
```

No trust token is used at runtime. If a certificate is rotated, add the new certificate and
remove the old one after both app instances are updated.

## 3. Configure the application

Copy an example and edit it:

```bash
sudo -u incus-fleet install -m 600 \
  deploy/examples/config.sqlite.yaml /home/incus/config.yaml
sudo -u incus-fleet "$EDITOR" /home/incus/config.yaml
```

`deploy/examples/config.sqlite.yaml`:

```yaml
server:
  listen: 0.0.0.0:8080
  auth_token: ""
  log_level: info

database:
  driver: sqlite
  dsn: /home/incus/data/fleet.db

clusters:
  - name: c1
    display_name: Primary site
    endpoint: https://192.0.2.10:8443
    project: default
    tls_cert: /home/incus/certs/app-a.crt
    tls_key: /home/incus/certs/app-a.key
    # tls_server_cert: /home/incus/certs/incus-server.crt

  - name: c2
    display_name: Secondary site
    endpoint: https://192.0.2.11:8443
    project: default
    tls_cert: /home/incus/certs/app-a.crt
    tls_key: /home/incus/certs/app-a.key
```

On app host B, point the certificate paths at `app-b.crt`/`app-b.key`.

`auth_token` is intentionally empty in the example. Supply it through the environment file:

```bash
sudo -u incus-fleet bash -c 'umask 077; openssl rand -hex 32 > /home/incus/auth-token'
sudo -u incus-fleet install -m 600 /dev/null /home/incus/incus-fleet.env
sudo -u incus-fleet tee /home/incus/incus-fleet.env >/dev/null <<'EOF'
INCUS_FLEET_AUTH_TOKEN=replace-with-the-generated-token
# Optional overrides:
# INCUS_FLEET_LISTEN=0.0.0.0:8080
# INCUS_FLEET_DATABASE_DRIVER=postgres
# INCUS_FLEET_DATABASE_DSN=postgres://...
EOF
sudo chmod 600 /home/incus/incus-fleet.env
sudo chown incus-fleet:incus-fleet /home/incus/incus-fleet.env
```

Validate before starting:

```bash
sudo -u incus-fleet env $(grep -v '^#' /home/incus/incus-fleet.env | xargs) \
  /home/incus/bin/incus-fleet -config /home/incus/config.yaml -check
```

Expected output:

```
configuration valid: 2 cluster(s), database sqlite
```

### PostgreSQL instead of SQLite

Select PostgreSQL at initialization by changing the database block:

```yaml
database:
  driver: postgres
  dsn: postgres://incus_fleet:CHANGE_ME@127.0.0.1:5432/incus_fleet?sslmode=disable
```

The schema is created automatically. Keep the DSN in the environment file rather than the YAML
if you prefer:

```bash
INCUS_FLEET_DATABASE_DRIVER=postgres
INCUS_FLEET_DATABASE_DSN=postgres://...
```

Migration history is operational data. To move an existing deployment from SQLite to PostgreSQL,
provision the new database, stop the app, switch the driver/DSN, and start it again. History
starts fresh; instance state is never stored in this database. (A dump/restore helper is not
included yet.)

## 4. Install the systemd unit

```bash
sudo install -m 644 deploy/systemd/incus-fleet.service /etc/systemd/system/incus-fleet.service
sudo systemctl daemon-reload
sudo systemctl enable --now incus-fleet
sudo systemctl status incus-fleet --no-pager
```

The sample unit runs as `incus-fleet`, reads `/home/incus/incus-fleet.env`, and writes only to
`/home/incus/data` plus the node-network configuration paths below. Adjust paths if you use a
different deployment directory.

### Node network management (optional)

Each cluster member has its own **Node network** page (Cluster → Nodes → Manage node). The local
app host is managed through `sudo -n`; remote members use an online host agent when available,
otherwise SSH. The SSH path needs the sudo whitelist below; the agent path uses
`deploy/agent-sudoers`.

```bash
sudo install -m 0440 -o root -g root deploy/nodesudoers /etc/sudoers.d/incus-fleet-nodesudoers
sudo visudo -cf /etc/sudoers.d/incus-fleet-nodesudoers   # parse check
sudo mkdir -p /etc/network/interfaces.d /etc/wpa_supplicant
sudo systemctl daemon-reload && sudo systemctl restart incus-fleet
```

The whitelist covers `ip`, `nft`, `iw`, `dhcpcd`, `wpa_supplicant`, `hostname`, and `tee` limited
to `/etc/network/interfaces.d/*` and `/etc/wpa_supplicant/fleet.conf` (interface names are
validated before use). Binary paths differ by distribution; adjust the file if your host keeps
them elsewhere (the runner resolves the binary through the same PATH order and passes the absolute
path to sudo).

Remote members authenticate with a dedicated SSH identity. Generate the key on the app host as
the service account and authorize its public key for the management user on every member:

```bash
sudo -u incus-fleet install -d -m 0700 /home/incus/.ssh
sudo -u incus-fleet ssh-keygen -t ed25519 -N '' -f /home/incus/.ssh/id_ed25519
# On each member (the management user must have the sudo whitelist above):
ssh-copy-id -i /home/incus/.ssh/id_ed25519.pub node-admin@192.0.2.20
```

Configure the identity in `/home/incus/config.yaml`:

```yaml
node_access:
  # auto: prefer agent, then local, then SSH. agent/ssh/local force one backend.
  mode: auto
  # Mutating operations may use SSH when no agent is available. Set false once
  # every member runs an agent; reads still fall back for break-glass access.
  ssh_write_fallback: true
  ssh_user: node-admin
  ssh_key: /home/incus/.ssh/id_ed25519
  ssh_port: 22
  # Pin host keys in production:
  # ssh_known_hosts: /home/incus/.ssh/known_hosts
  ssh_insecure_ignore_host_key: true   # lab only; prefer known_hosts
```

The SSH account needs passwordless sudo for the whitelisted binaries. If the account already has
passwordless sudo for everything, the whitelist is optional. The **Add node** workflow installs
this whitelist automatically for the SSH account it uses.

On Linux the agent helper uses native netlink for link state, static addresses, and fleet routes
(protocol 99); the command fallback is only for non-Linux development/tests. Set
`rollback_after_seconds` on an agent network request to start a detached rollback timer; `confirm`
cancels it.

The sample unit sets `NoNewPrivileges=false` and `PrivateDevices=false` because sudo is a setuid
binary and `ip`/`iw`/`nft` need the real device view; `ReadWritePaths` grants the two `/etc`
directories above. Keep the remaining hardening. Wi-Fi scan/connect additionally needs
`wpasupplicant` installed on each node (`apt-get install -y wpasupplicant`).

The first startup after upgrading creates the host task and audit tables through the schema
migration; existing migration history is preserved. The API exposes `GET /api/v1/tasks`,
`GET /api/v1/tasks/{id}`, and `GET /api/v1/audit`. Audit records are query-limited (maximum 200 per
request); automatic retention/export is planned with the agent lifecycle work.

### Host agent (optional)

The Rust host agent is optional. It enrolls with a one-time token, connects outbound over mTLS
WebSocket, reports facts/heartbeats, and serves read-only `nodenet.snapshot` tasks. The center
does not need an inbound port on the node.

Generate a private CA and a server certificate for the agent gateway on the center host:

```bash
sudo install -d -m 0755 /etc/incus-fleet
sudo openssl ecparam -name prime256v1 -genkey -noout -out /etc/incus-fleet/agent-ca.key
sudo openssl req -x509 -new -key /etc/incus-fleet/agent-ca.key -sha256 -days 3650 \
  -subj "/CN=incus-fleet-agent-ca" -out /etc/incus-fleet/agent-ca.crt
sudo openssl ecparam -name prime256v1 -genkey -noout -out /etc/incus-fleet/agent-server.key
sudo openssl req -new -key /etc/incus-fleet/agent-server.key \
  -subj "/CN=fleet.example" -out /etc/incus-fleet/agent-server.csr
printf 'subjectAltName=DNS:fleet.example,IP:192.0.2.20\nextendedKeyUsage=serverAuth\n' \
  | sudo tee /etc/incus-fleet/agent-server.ext >/dev/null
sudo openssl x509 -req -in /etc/incus-fleet/agent-server.csr \
  -CA /etc/incus-fleet/agent-ca.crt -CAkey /etc/incus-fleet/agent-ca.key \
  -CAcreateserial -days 825 -sha256 -extfile /etc/incus-fleet/agent-server.ext \
  -out /etc/incus-fleet/agent-server.crt
```

Enable the gateway in the center configuration:

```yaml
agent:
  enabled: true
  listen: 0.0.0.0:8444
  tls_cert: /etc/incus-fleet/agent-server.crt
  tls_key: /etc/incus-fleet/agent-server.key
  client_ca: /etc/incus-fleet/agent-ca.crt
  ca_key: /etc/incus-fleet/agent-ca.key
```

Create a one-time token from the UI/API (it is shown once), then build and enroll the agent:

```bash
cargo build --manifest-path agent/Cargo.toml --release
go build -o bin/incus-fleet-helper ./cmd/incus-fleet-helper
sudo install -m 0755 agent/target/release/incus-fleet-agent /usr/local/bin/incus-fleet-agent
sudo install -m 0755 bin/incus-fleet-helper /usr/libexec/incus-fleet-helper
sudo install -m 0440 -o root -g root deploy/agent-sudoers \
  /etc/sudoers.d/incus-fleet-agent
sudo visudo -cf /etc/sudoers.d/incus-fleet-agent
sudo useradd --system --home /var/lib/incus-fleet-agent \
  --shell /usr/sbin/nologin incus-fleet-agent
sudo install -d -m 0700 -o incus-fleet-agent -g incus-fleet-agent /var/lib/incus-fleet-agent
sudo install -m 0644 deploy/systemd/incus-fleet-agent.service \
  /etc/systemd/system/incus-fleet-agent.service
sudo systemctl daemon-reload

read -r -s AGENT_TOKEN
printf '%s' "$AGENT_TOKEN" | sudo -u incus-fleet-agent \
  /usr/local/bin/incus-fleet-agent enroll \
  --center https://fleet.example:8444 \
  --ca /etc/incus-fleet/agent-ca.crt \
  --cluster c1 --member node-a \
  --state-dir /var/lib/incus-fleet-agent
sudo systemctl enable --now incus-fleet-agent
```

The agent reports facts/heartbeats and read-only snapshots, and can apply allowlisted network
mutations through the helper. eBPF, Incus bootstrap, and telemetry storage are later phases. The
sample unit runs with `NoNewPrivileges=false` (sudo needs the setuid transition),
`ProtectSystem=strict`, `MemoryMax=128M`, and `CPUQuota=25%`; verify the resource targets on the
lowest-spec node before widening use.

#### Host metrics

Enable the center's one-hour buffer:

```yaml
metrics:
  enabled: true
  retention: 1h
  interval: 10s
```

The agent sends host CPU/memory/load/uptime/network counters every 10 seconds. The center never
writes metrics to SQLite/PostgreSQL; restarting it clears the buffer. The node detail page shows
the last hour, and the API exports JSON, Prometheus text, or CSV. Prometheus/VictoriaMetrics
remote write and HTTP SD are later phases.

#### Agent lifecycle, HA, and audit

Certificate rotation and revocation are admin API calls. Export the agent's public key from its
identity directory first:

```bash
sudo openssl pkey -in /var/lib/incus-fleet-agent/identity/client.key \
  -pubout -out /tmp/agent-public.pem
```

```bash
# Sign a new certificate for the agent's existing public key
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"public_key\":$(jq -Rs . < agent-public.pem)}" \
  https://fleet.example/api/v1/agents/AGENT_ID/certificate
# Revoke
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  https://fleet.example/api/v1/agents/AGENT_ID/revoke
```

The gateway checks revocation on every connection. The agent renews automatically when its
certificate is within 30 days of expiry: it calls `POST /api/v1/agent/renew` over its current mTLS
identity and writes the returned certificate into the identity directory before reconnecting.

The audit history can be exported without the UI's 200-row limit:

```bash
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
  'https://fleet.example/api/v1/audit/export?format=jsonl' > audit.jsonl
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
  'https://fleet.example/api/v1/audit/export?format=csv' > audit.csv
```

Host tasks have database-backed leases (`lease_owner`, `lease_until`,
`ClaimHostTask`/`RenewHostTaskLease`/`ReleaseHostTask`). PostgreSQL uses
`FOR UPDATE SKIP LOCKED`; SQLite serializes through a transaction. Enable the background dispatcher
to consume queued tasks:

```yaml
ha:
  enabled: true
  worker_id: app-a          # unique per center instance; defaults to hostname-pid
  lease: 30s
  interval: 2s
```

Start two or more center instances against the same PostgreSQL database. Each claims at most one
task, renews its lease while running, and releases nothing on success. Enqueue asynchronous agent
network work with `POST /api/v1/tasks`; synchronous node-page operations keep their existing
behavior. On SQLite, leases still work but only one instance should run the dispatcher.

Upgrade an agent with a detached Ed25519 signature:

```bash
incus-fleet-agent upgrade \
  --binary incus-fleet-agent-linux-amd64 \
  --signature incus-fleet-agent-linux-amd64.sig \
  --public-key release-ed25519.pub \
  --target /usr/local/bin/incus-fleet-agent
```

The installer verifies the signature, keeps `incus-fleet-agent.prev`, installs the new binary with
mode 0755, and runs `--version`; if the health check fails it restores the previous binary. After a
successful install it runs `systemctl try-restart incus-fleet-agent` when systemd is running. Use
`--no-restart` (or `INCUS_FLEET_UPGRADE_NO_RESTART=1`) to skip the restart.

#### Prometheus/VictoriaMetrics

Enable remote write and the query proxy:

```yaml
metrics:
  enabled: true
  remote_write:
    enabled: true
    url: https://prometheus.example/api/v1/write
    bearer_token_file: /etc/incus-fleet/prom.token
    timeout: 10s
    queue_size: 10000
    batch_size: 500
    spool_dir: /home/incus/data/remote-write
    spool_partition: app-a     # unique per center instance
    max_bytes: 67108864
  query:
    enabled: true
    url: https://prometheus.example
    type: prometheus          # or victoriametrics
    timeout: 15s
  http_sd:
    enabled: true
    mode: proxy
    token: replace-with-a-random-sd-token
    public_url: https://fleet.example
```

The remote write queue is disk-backed. Each center instance writes to its own partition
`spool_dir/<spool_partition>`. `spool_partition` defaults to `ha.worker_id`, then a collision-free
`hostname-pid`; set it explicitly when you want a stable name. Every partition holds a `lock` file.
A live owner keeps an exclusive flock, so another instance cannot replay or delete its files. On
startup the new owner holds a root lock, claims any unlocked stale partitions (for example after a
crash or PID change), and replays their batches before opening its own. Starting a second instance
with the same explicit partition fails fast instead of double-sending. Each batch is written before
it is queued; successful writes delete the file, and files left by a crash/restart are replayed.
Legacy files directly under `spool_dir` are migrated into the current partition on upgrade.
`queue_size` bounds the in-memory queue, `max_bytes` bounds each partition (64 MiB default), and the
oldest files are removed when either limit is exceeded. 429/5xx responses retry with backoff; 4xx
batches are dropped as permanently rejected. `spool_dir` defaults to `data/remote-write` next to a
file-backed SQLite database. Prometheus needs
`--web.enable-remote-write-receiver`; VictoriaMetrics single-node uses
`http://vm:8428/api/v1/write`, cluster uses
`http://vminsert:8480/insert/<accountID>/prometheus/api/v1/write`. Keep tokens in files or
environment variables, not committed YAML.

Prometheus can discover the proxy scrape target with:

```yaml
scrape_configs:
  - job_name: incus-fleet
    http_sd_configs:
      - url: https://fleet.example/api/v1/sd/agents
        authorization:
          credentials_file: /etc/prometheus/fleet-sd.token
```

`mode=proxy` has Prometheus scrape the center. For `mode=direct`, start each agent with a metrics
listener and the same scrape token:

```bash
# /etc/incus-fleet/agent-metrics.token contains the bearer token
incus-fleet-agent run --state-dir /var/lib/incus-fleet-agent \
  --metrics-listen 0.0.0.0:9100 \
  --metrics-advertise 192.0.2.11:9100 \
  --metrics-token-file /etc/incus-fleet/agent-metrics.token
```

With `metrics.http_sd.mode: direct`, the SD endpoint returns `metrics_address` targets with
`__metrics_path__=/metrics`. Configure the Prometheus scrape job with the same bearer token. Agents
without an advertised address are skipped; the SD endpoint only returns online agents.

The scrape path can be validated without a center connection:

```bash
incus-fleet-agent metrics-serve \
  --listen 127.0.0.1:19100 \
  --token-file /etc/incus-fleet/agent-metrics.token \
  --interval-secs 2
curl -fsS -H 'Authorization: Bearer <token>' http://127.0.0.1:19100/metrics
```

A real Prometheus 3.x scrape job with `authorization.credentials` and
`http_sd_configs.authorization.credentials` resolves the center SD response, scrapes
`http://<metrics_address>/metrics`, and applies `cluster`/`member`/`agent_id` target labels. The
bearer scheme is case-insensitive while the token stays case-sensitive.

#### eBPF telemetry (optional)

The agent always collects per-cgroup v2 instance metrics from `/sys/fs/cgroup` and host fallback
counters from `/proc`. eBPF is optional and adds host TCP retransmit and OOM-kill counters. It
requires Linux kernel >= 5.8 with BTF (`/sys/kernel/btf/vmlinux`), `CAP_BPF`/`CAP_PERFMON` (or
root), and an object built from `agent/bpf/metrics.bpf.c`:

```bash
sudo apt-get install -y clang llvm libbpf-dev linux-headers-$(uname -r)
make agent-ebpf
sudo install -d -m 0755 /usr/lib/incus-fleet
sudo install -m 0644 agent/bpf/metrics.bpf.o /usr/lib/incus-fleet/metrics.bpf.o
```

Add `--ebpf-object /usr/lib/incus-fleet/metrics.bpf.o` to the agent `ExecStart` and enable
`AmbientCapabilities=CAP_BPF CAP_PERFMON` in the unit. If the object, BTF, or permissions are
missing, the agent logs the reason and keeps using procfs/cgroup; no telemetry is lost.

Verify the object with the agent's own loader before enabling it in the service:

```bash
sudo incus-fleet-agent ebpf-self-test \
  --object /usr/lib/incus-fleet/metrics.bpf.o --hold-secs 10
# initial_tcp_retransmits=0
# initial_oom_kills=0
# tcp_retransmits=...
# oom_kills=...
```

`--debug-cgroups` prints the observed cgroup ids and the inode mapping when attribution looks wrong.
A non-zero `tcp_retransmits` delta confirms the host tracepoints are attached; block I/O or
retransmits inside a container should add an `instance=<name> ... tcp_retransmits=...` line. The
selftest also prints `socket_cgroups` to show how many flow mappings sockops has learned. Both IPv4
and IPv6 loopback/container flows are supported.

Per-cgroup eBPF metrics are attributed by reading each cgroup's kernel file handle (including nested
systemd cgroups), so container block I/O becomes `fleet_instance_block_latency_seconds_*` and
retransmit/OOM counters. Block-latency buckets are powers of two nanoseconds. VMs are host-side
cgroup metrics only. TCP retransmits are attributed via a sockops-maintained IPv4/IPv6 flow→cgroup
map, so softirq retransmit timers keep the container attribution. The sockops program is attached
with `BPF_PROG_ATTACH` + `BPF_F_ALLOW_MULTI`, so it coexists with other sockops programs; if that
attach mode is unavailable the agent skips sockops instead of replacing anything and falls back to
current-task attribution. OOM kills are aggregated per instance across nested cgroups. When a
host-triggered OOM's current cgroup is root, a ring-buffer event carries the victim pid and the agent
resolves `/proc/<pid>/cgroup` into an instance before the process disappears; otherwise the kill stays
in the host counter. The host counters remain exact. `fleet_agent_telemetry_mode` reports `ebpf` or
`procfs`.

#### Incus bootstrap through the agent

Once the agent is online, the center can provision Incus through
`POST /api/v1/agents/{id}/bootstrap`. The spec is signed with the same CA key used for agent
enrollment; the helper verifies it against `/var/lib/incus-fleet-agent/identity/ca.pem`. Keep the
agent CA private key on the center only.

```bash
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"mode":"join","address":"192.0.2.11","storage_source":"/srv/incus",
       "install_mode":"existing","version_pin":"7.0"}' \
  https://fleet.example/api/v1/agents/AGENT_ID/bootstrap
```

For `install_mode=package`, configure the Incus package repository on the node first. The helper
uses apt, dnf, zypper, apk, or pacman and refuses to add an unverified `repo_url`. For a new
cluster use `"mode":"init"` and a `network_cidr`; the defaults create a dir pool at `/srv/incus`
and an `incusbr0` bridge. Re-running the same request is safe: completed steps are recorded under
`/var/lib/incus-fleet-helper/bootstrap-<task_id>.json`.

#### Adding a node from the UI

The **Add node** dialog defaults to the **Host agent** flow and falls back to **SSH** when needed.

For an agent-first join:

1. Enter the exact member name and the cluster address.
2. Click **Create enrollment token**. The dialog shows a one-time token and the `incus-fleet-agent
   enroll` command. Run it on the node; the dialog polls for the agent to come online.
3. Once the agent is online, click **Bootstrap via agent**. The center signs the join spec and the
   agent helper installs Incus (only with **Install Incus through the package manager** enabled),
   preseed the cluster join, and reports the completed steps.

The member name must match the agent's `--member` value. Package installation uses the node's
already-configured Incus repository; the helper does not add an unverified repository. Use **Use
SSH instead** at any time to switch to the SSH/nodejoin path.

The SSH path needs:

- SSH connectivity to the target and an account that can run `sudo` (a one-time sudo password can
  be entered in the dialog).
- Network access from the target to the Incus apt repository used by the existing cluster. The
  installed Incus version must match the cluster version.
- A writable storage source path for the member's local storage pool (`/srv/incus` by default).
- For VMs, the dialog can install `qemu`, `ovmf`, and `swtpm`.

The dialog skips SSH host key verification by default for convenience on a new node; enable
`ssh_known_hosts` and uncheck that option for production. SSH and sudo passwords are used only for
the request and are never stored.

## 5. Configure Caddy

Caddy must pin each browser to one app instance because sessions are in memory. The sample
`deploy/caddy/Caddyfile` uses `lb_policy cookie` plus passive/active health checks:

```caddyfile
fleet.example.internal {
    encode zstd gzip

    reverse_proxy 192.0.2.20:8080 192.0.2.21:8080 {
        lb_policy cookie incus_fleet_lb
        lb_try_duration 5s
        lb_try_interval 250ms
        fail_duration 30s
        max_fails 2
        health_uri /api/v1/healthz
        health_interval 10s
        health_timeout 2s
    }
}
```

Use a real DNS name or Caddy's internal CA for LAN-only deployments. Terminate TLS at Caddy; the
app instances serve plain HTTP on the private network. Caddy sets `X-Forwarded-Proto`, so the
app marks the session cookie `Secure` even though the upstream connection is HTTP. Direct
plain-HTTP access to an app instance also works for initial LAN testing, but the cookie is not
marked `Secure` in that case.

If you round-robin without stickiness, browser logins will intermittently fail because the
session cookie is only valid on the instance that created it.

Caddy proxies WebSockets transparently: the terminal, VGA, SPICE, and file endpoints
(`/api/v1/.../console|exec|vga|spice|files`) ride the same sticky cookie as the UI, so no extra
configuration is needed. SPICE clients open several websocket connections per session, and all
of them must reach the same app instance; the sticky cookie covers this when the browser
supports it.

## 6. Firewall and SPICE

Incus Fleet configures a SPICE TCP listener for VMs that need a graphical console. The listener
has no ticket authentication and binds on all interfaces of the cluster member, with ports
allocated per instance name in the **5910-5989** range. Do not expose that range to untrusted
networks. Allow it only from the app hosts that bridge browser websockets, for example:

```bash
sudo nft add rule inet filter input tcp dport 5910-5989 \
  ip saddr @fleet_app_hosts accept
```

Container exec and serial consoles do not need any extra cluster-side port; they travel
through the Incus API.

## 7. Deploy upgrades

1. Build and copy the new binary to `/home/incus/bin/incus-fleet`.
2. `sudo systemctl restart incus-fleet` on one host; verify health.
3. Repeat on the other host.
4. Unfinished migrations are marked failed at startup. Re-submit them after checking that no
   partial copy remains on the target.

Roll back by restoring the previous binary and restarting. The database schema is additive; no
downgrade step is currently required.

## 8. Backup

Back up per app host:

- `/home/incus/config.yaml` (contains no secrets if the token is in the env file);
- `/home/incus/incus-fleet.env` (contains the admin token — protect it);
- `/home/incus/certs/` (private keys);
- `/home/incus/data/fleet.db` (SQLite) or the PostgreSQL database.

The database now holds migration history plus the shared SSH key and script library. A lost
database does not lose instances, but it does lose those reusable assets, so include it in
regular backups.

Stop the service or use SQLite's online backup before copying the database file. For
PostgreSQL, use `pg_dump` against the fleet database.

## 9. Troubleshooting

| Symptom | Check |
| --- | --- |
| `401 unauthorized` from API | Correct `Authorization: Bearer` token or valid session cookie |
| `401 invalid_token` at login | Token in the env file matches what the client sends |
| Cluster marked offline | Endpoint reachable, `tls_server_cert` correct, client cert trusted |
| `503 cluster_unavailable` | Reconnect from the UI/API and inspect app logs |
| Live migration rejected | Running VM, `migration.stateful=true`, `mode=move` |
| VM unreachable after live migration | Guest kept its source DHCP lease; renew DHCP in the guest or use matching cluster subnets |
| Target storage pool missing | Create the same pool name on the target cluster |
| Login works then fails after refresh | Caddy is not using sticky sessions |
| Migration stuck/failed after restart | Unfinished tasks are marked failed; inspect target for partial copies |
| ISO upload or launch fails | ISO volume exists in the pool, the VM name is free, the pool has room |
| Remote desktop cannot connect | VM running, Incus agent ready, firewall allows 5910-5989 from the app host |
| Image upload returns 413 | Image is over 8 GiB; compress the root filesystem or raise the limit in code |
| Batch action reports partial failure | Inspect per-instance `error`/`stderr` in the 207 response; retry failed targets |

Check the Incus side with:

```bash
incus config trust list
incus list
incus info --show-log <instance>
```

## Security checklist

- [ ] Example files unchanged except placeholders removed
- [ ] Admin token generated with at least 32 random bytes
- [ ] Env file and private keys owned by the service account, mode `0600`
- [ ] No tokens, keys, or local-only docs in git
- [ ] Terminal websockets stay sticky (a reconnect must hit the same app instance)
- [ ] Caddy presents TLS and uses sticky sessions
- [ ] App API port 8080 is reachable only from the Caddy host (firewall)
- [ ] Incus API 8443 is reachable only from the app hosts (firewall)
- [ ] SPICE ports 5910-5989 are reachable only from the app hosts (firewall)
- [ ] Database backups include the shared key and script library
- [ ] Secret scan run before pushing

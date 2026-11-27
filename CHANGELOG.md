# Changelog

**English** | [简体中文](docs/zh/CHANGELOG.zh-CN.md)

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- Plugin install orchestrator: checkpointed `plugin.install` host tasks with
  progress events, cooperative cancel, resume, reverse rollback, manifest
  templates, data-disk preparation, config injection and health-gated wiring
  activation. Approved overrides are durable in the new `plugin_wiring` table
  (migration 15), and `GET`/`PUT /api/v1/plugins/{id}/wiring` manages them.
- Event plumbing: signed outbound webhooks for agent, host-task, migration and
  instance-state events, the opt-in inbound `POST /api/v1/plugins/events`
  endpoint, and `GET /api/v1/logs/query` for log search through a wired backend.
- Community packages can be signed: `scripts/pack-plugin.sh` writes a
  `manifest.yaml.sig` sidecar verified with `plugins.signing_key_file` (or
  `INCUS_FLEET_PLUGINS_SIGNING_KEY`); a MiBeeNVR example package is included.
- Data-driven plugin UI: `router.addRoute` pages, a generic plugin page, icon
  and i18n mappings, one sidebar entry with a badge, Catalog/Installed views,
  a three-step install wizard, a task panel, a five-tab detail page, and one
  status vocabulary.
- Documentation now describes plugin phases P0–P5, the task API, wiring keys,
  event endpoints and the remaining limitations in English and Chinese.

### Changed

- Installed plugins activate wiring only after the health check passes;
  uninstall restores the captured static defaults and revokes the plugin token.
- `manifest.yaml` is the preferred plugin format; legacy `plugin.json` catalog
  entries are still accepted. The older synchronous
  `POST /api/v1/plugins/install` endpoint is retained alongside the task API.
- Plugin UIs and descriptors use the `/plugin-ui/<id>/…` namespace; the plugin
  manager itself stays at `/plugins`.
- Agent telemetry and log shipping stay off by default. Installing `vmlogs`
  enables log search, while remote write still requires static configuration.

### Fixed

- Documentation corrections only: the plugin docs no longer label P2–P5 as
  "specified", and the user guides no longer describe one-click install as
  connecting an existing upstream only.

## 0.2.0 - 2026-11-24

### Added

- Tagged releases (`v*`) are built and published automatically by GitHub Actions:
  amd64/arm64 binaries for the center, privileged helper, and host agent, `.deb`/`.rpm`
  packages for each component, per-architecture tarballs, and SHA256 checksums.
- Documentation is split into `docs/en/` and `docs/zh/`, each with its own `images/`;
  local-only operator notes live in the gitignored `docs/local/`. Deployment and user
  guides now cover the 0.2.0 UI, agent installer, plugins, and scoped tokens.
- Instance creation accepts `disks` for extra empty data volumes, and the form
  offers a guided disk editor with name, mount path and size validation.
- Size inputs (memory, root disk, volumes, ISO installs) share one validator:
  clear unit hints, MiB/GiB/TiB suffixes, bare numbers normalized to MiB, and
  inline errors instead of server round-trips.
- Information architecture reworked around user tasks: Workspace (overview,
  instances, storage), Resources (images & automation), Clusters → Nodes, and
  Administration. Migration history moved into the instances page; answer
  profiles, SSH keys, and scripts joined images/ISO under one tabbed page with
  the clearer name **Images & automation**. Cluster pages became scoped tabs
  (overview & nodes, instances, storage, media, network, profiles) and no
  longer list node-local physical NICs.
- Sidebar navigation is grouped into Fleet / Clusters → Nodes / Administration,
  with breadcrumbs and an auto-expanded current node. Node pages explain their
  scope and hide mutations from read-only roles.
- Administrators can list and revoke an account's sessions.

### Fixed

- Batch power confirmation, cluster unregistration title, and image delete
  errors showed wrong text.
- Failed asset/instance/inventory requests no longer masquerade as empty data;
  partial cluster failures are labelled and the fleet instance view degrades
  gracefully instead of returning 503.
- Migration deduplication is checked against the database, so two centers
  sharing a store cannot start the same migration.
- Auth endpoints are rate limited per client in production, forwarded-proto
  headers are only trusted from local proxies, and task audit records keep the
  real operator name.
- Polling recovers to its base interval after outages and pauses in background
  tabs; ISO uploads can be canceled and survive navigation; concurrent instance
  actions track busy state per row.

- Leak prevention is now enforced rather than remembered: a hardened
  `.gitignore` denies whole local-material directories and credential-shaped
  files, `scripts/secret-scan.sh` inspects real content (private keys,
  credential assignments, bearer tokens, operator subnets and hostnames) before
  a commit, and a CI job runs the same scan over every tracked file and every
  commit. Certificates moved out of the repository to
  `~/.config/incus-fleet/certs`; see docs/en/security.md.
- Cluster private interconnect networks: fleet creates a host bridge on every
  member, joins them with point-to-point VXLAN tunnels, and runs DHCP/NAT from
  one gateway member so instances on all nodes share a private network without
  touching guest configuration. Includes enable/reconcile/disable endpoints and
  a cluster page panel.

- Local accounts with roles (`admin`/`operator`/`viewer`), bcrypt passwords, server-side
  sessions shared across centers, lockout, and the shared admin token retiring once the first
  account exists.
- A graphical console for virtual machines that works without VNC or SPICE support in the
  host's QEMU: the fleet attaches a second QEMU monitor through `raw.qemu.conf` (QMP control
  mode) and serves `desktop/prepare`, `desktop/screenshot` (PNG framebuffer polling), and
  `desktop/input` (keyboard with modifier chords, absolute pointer, wheel). The browser canvas
  refreshes automatically; the first preparation restarts a running VM once and later calls
  are idempotent.
- node-exporter-compatible host metrics from the Rust agent (`node_cpu_seconds_total`,
  `node_memory_*`, `node_load1`, network and OOM counters) alongside the instance metrics.
- Guest DHCP lease renewal after a cross-cluster move, so a moved VM picks up an address from
  the target cluster's bridge instead of holding its source lease for up to an hour.

- `rqlite` as a third database driver, so several fleet instances can share one replicated
  SQLite store without running PostgreSQL. Includes a wrapper driver that converts rqlite's
  JSON-decoded floats back to int64 timestamps, a single-statement task claim that stays atomic
  without transactions, and database-backed sessions whenever the driver is shared.
- A `owner` column on migrations so a restarting instance fails only its own interrupted tasks.
- Add a cluster member from the UI: SSH provisioning, optional Incus installation, join, sudo
  whitelist, and fleet SSH key authorization.
- Per-member node network management for the local app host and remote members.
- Typed host operation routing (`hostops`) with `node_access.mode` (`auto`, `agent`, `ssh`,
  `local`) and explicit backend reporting.
- Durable host task and audit records with `GET /api/v1/tasks`, `GET /api/v1/tasks/{id}`, and
  `GET /api/v1/audit`, plus a **History** dialog on the node network page.
- Schema migration tracking and host task, audit, agent, enrollment, and certificate tables.
- Wi-Fi and node network task plans are redacted; passwords and join tokens are never persisted.
- Rust host agent MVP: protobuf `incusfleet.agent.v1` contract, one-time enrollment tokens,
  center mTLS WebSocket gateway, facts/heartbeat reporting, and read-only network snapshots.
- Admin endpoints `POST /api/v1/agents/enrollment-tokens`, `GET /api/v1/agents`,
  `GET /api/v1/agents/{id}/snapshot`, and `POST /api/v1/agents/{id}/network`.
- Privileged network helper and agent dispatch for link, address, route, firewall, and Wi-Fi
  changes, with protocol-99 fleet routes and per-task rollback snapshots.
- Persistence adapters for ifupdown/ifupdown-ng, systemd-networkd, netplan, wicked, and
  NetworkManager; confirm/rollback actions restore the captured state.
- Signed Incus bootstrap specs, join/init preseed generation, apt/dnf/zypper/apk/pacman install
  adapters, version pinning, and idempotent bootstrap state recovery.
- `POST /api/v1/agents/{id}/bootstrap` for agent-driven Incus provisioning.
- Agent host telemetry every 10 seconds, a one-hour center ring buffer, query/snapshot/export
  endpoints, and host metrics charts on the node detail page.
- cgroup v2 per-instance CPU/memory/pids/IO metrics, bounded histogram primitives, and an optional
  Linux eBPF collector for host TCP retransmit/OOM counters with procfs fallback.
- Prometheus/VictoriaMetrics remote write with a bounded retry queue, a PromQL range-query proxy,
  and token-authenticated HTTP service discovery with a center-proxied scrape endpoint.
- Host-task lease primitives for PostgreSQL/SQLite, agent certificate rotation/revocation APIs,
  signed agent upgrade with automatic rollback, and JSONL/CSV audit export.
- The host metrics panel can switch between the center ring buffer and the Prometheus/VictoriaMetrics
  query proxy.
- An HA dispatcher consumes queued `agent.network` tasks with database leases; enqueue with
  `POST /api/v1/tasks`. Agents auto-renew certificates within 30 days of expiry, and signed upgrades
  restart the systemd unit unless `--no-restart` is set.
- Direct HTTP SD mode using an agent `/metrics` listener with bearer-token auth, in addition to the
  center proxy mode.
- `incus-fleet-agent ebpf-self-test` validates the aya loader and attached tracepoints; the eBPF
  object Makefile now finds Debian multiarch headers.
- Per-cgroup eBPF attribution (including nested systemd cgroups) and block-latency histograms
  emitted as `fleet_instance_tcp_retransmits_total`, `fleet_instance_oom_kills_total`, and
  `fleet_instance_block_latency_seconds_*`.
- Direct HTTP SD validated end-to-end with Prometheus 3.8 scraping the agent `/metrics` listener;
  add `metrics-serve` for standalone scrape validation and keep bearer tokens case-sensitive.
- Raft center mesh with leader election, leased agent ownership refreshed by heartbeats, relay
  dispatch, shared PostgreSQL sessions, and ordered cluster endpoint failover with active health
  checks. Includes forced single-node recovery and learner rejoin APIs.
- One-command Linux agent installer served by the center (`agent.install`): checksummed agent/helper
  downloads, per-architecture binary selection, CA/sudoers/systemd setup, automatic enrollment, and
  service start. The UI shows this
  command instead of assuming the agent is already installed.
- Agent-first **Add node** flow in the UI: enrollment-token display with polling, signed agent
  bootstrap, step progress, and an explicit SSH fallback. `GET /api/v1/agents` accepts
  `cluster`/`member` filters.
- Durable remote-write spool with restart replay, bounded queue/byte retention, older-file
  eviction, per-instance spool partitions, and per-partition flock ownership with stale-partition
  takeover. Default partitions are collision-free (`hostname-pid`). The HA dispatcher now also runs
  `agent.bootstrap` and `agent.snapshot` handlers.
- Reliable IPv4/IPv6 TCP retransmit attribution through a sockops flow→cgroup map, validated with
  container packet loss, attached with `BPF_F_ALLOW_MULTI` so existing sockops programs are kept.
  OOM counters are aggregated per instance; root-context global OOM is attributed exactly through
  `bpf_task_from_pid` and a CO-RE cgroup-id read, validated with a controlled global OOM in an
  isolated VM. Kernels without the kfuncs keep the ring-buffer `/proc` fallback.
- Native Linux netlink for link/address/fleet-route writes, an opt-in detached network rollback
  timer, and agent-backed node network routing for the existing UI.

- Device editing: `PATCH /clusters/{cluster}/instances/{instance}/devices/{device}` merges an
  edit into the attached device (network switch, static addresses, size), preserving keys the
  form does not manage; the devices panel shows an expandable full-configuration view per
  device.
- The remote desktop toolbar gains start/restart/stop controls and the guest power state, so a
  stopped VM can be booted without leaving the console.
- Terminal dialogs now surface the server's websocket close reason (missing incus agent,
  stopped instance, busy console) and hint VM serial-console expectations.

- One-click install of the official incus-agent into agent-less virtual machines: the fleet
  reaches the guest over SSH (jumping through the member host), mounts the `config` 9p share
  Incus already provides, runs the official `install.sh`, and verifies the agent starts
  reporting state. Guest credentials stay in the request and are never stored. Linux/systemd
  guests only.

### Fixed

- Instance NIC edits no longer fail or silently do nothing: switching a legacy
  `nictype`/`parent` NIC to a managed network now drops the conflicting keys instead of
  sending both, and the edit form disables device-level addresses for physical NICs (which
  take their addresses from the LAN DHCP server) instead of dropping them quietly.
- Cluster TLS certificate fields accept inline PEM bodies as well as file paths.
- `.gitignore` excludes `*.crt` certificate files.
- Large uploads no longer die at the 60-second handler deadline; image and ISO imports get an
  extended read/write window and the response-writer unwrapping they need.

## 0.1.0 - 2026-11-14

First tagged release of the multi-cluster control plane.

### Added

- Initial control plane for multiple independent Incus clusters.
- Fleet dashboard with cluster health, membership, storage, instance counts, and host
  CPU/memory utilization.
- Container and VM inventory with detail views.
- Cross-cluster move and copy migrations with rollback.
- Live migration for stateful VMs.
- SQLite and PostgreSQL persistence.
- Admin-token authentication with HttpOnly sessions and bearer tokens for automation.
- Single Go binary with an embedded Vue 3 UI (Simplified Chinese and English).
- systemd, Caddy, and example configuration deployment assets.
- Image import, cross-cluster distribution, cached-image refresh, clone, and rebuild.
- ISO installer upload and blank-VM launch for manual OS installation.
- Browser exec terminals with persistent shells, serial consoles, and SPICE remote desktop.
- Instance file management, snapshots, device mounting, password and key injection.
- Shared asset library for SSH public keys and shell scripts.
- Batch key injection, script execution, and power operations across clusters.
- Host and per-instance resource monitoring (CPU, memory, disk, network) without guest agents.
- Bilingual documentation: English canonical files and Simplified Chinese mirrors for the
  README, guides, API reference, standards, and community documents, with screenshots in
  both UI languages.

### Changed

- Licensed under the GNU Affero General Public License v3.0.
- Sanitized documentation, examples, tests, and screenshots to remove real infrastructure
  details.

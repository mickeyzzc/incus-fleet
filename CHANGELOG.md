# Changelog

**English** | [简体中文](CHANGELOG.zh-CN.md)

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

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
- Agent-first **Add node** flow in the UI: enrollment-token display with polling, signed agent
  bootstrap, step progress, and an explicit SSH fallback. `GET /api/v1/agents` accepts
  `cluster`/`member` filters.
- Durable remote-write spool with restart replay, bounded queue/byte retention, older-file
  eviction, per-instance spool partitions, and per-partition flock ownership with stale-partition
  takeover. Default partitions are collision-free (`hostname-pid`). The HA dispatcher now also runs
  `agent.bootstrap` and `agent.snapshot` handlers.
- Reliable IPv4/IPv6 TCP retransmit attribution through a sockops flow→cgroup map, validated with
  container packet loss, attached with `BPF_F_ALLOW_MULTI` so existing sockops programs are kept.
  OOM counters are aggregated per instance; root-context global OOM resolves the victim pid through
  a ring-buffer event before falling back to the host counter.
- Native Linux netlink for link/address/fleet-route writes, an opt-in detached network rollback
  timer, and agent-backed node network routing for the existing UI.

### Fixed

- Cluster TLS certificate fields accept inline PEM bodies as well as file paths.
- `.gitignore` excludes `*.crt` certificate files.

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

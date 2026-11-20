# Roadmap

**English** | [简体中文](zh-CN/roadmap.md)

This document records planned work for incus-fleet. It is deliberately
sanitized: no internal addresses, hostnames, credentials, or topology appear
here. Detailed internal audit notes are kept out of the repository.

The current release already covers: image import/distribution, ISO installers,
instance create/snapshot/clone/rebuild, cross-cluster migration, online
terminals and SPICE desktops, script/password/key maintenance actions, shared
asset library, batch operations, and cluster/instance/host-resource monitoring.
The work below extends that foundation.

## 1. Authentication, users, and access control — **DELIVERED**

Shipped in `v0.1.0-userauth`. The single shared admin token is now only a
bootstrap credential; it is retired automatically once the first local account
exists. `INCUS_FLEET_BOOTSTRAP_ADMIN_USERNAME` / `..._PASSWORD` create that first
administrator.

Delivered:

- Local accounts with bcrypt password hashes, create/update/disable/delete,
  forced password change, failed-attempt lockout.
- Roles `admin` / `operator` / `viewer`, enforced on every mutating route, not
  only hidden in the UI.
- Server-side sessions with identity, shared across centers via the database.
- Self-service password change and current-session inspection.
- The last administrator cannot be deleted or demoted.

Still planned:

- **API tokens**: per-user scoped tokens with explicit permissions and expiry.
- **Session revocation UI**: list and revoke every session for an account.
- **Login audit stream**: persisted sign-in history with failure reasons.
- **Optional SSO**: OIDC provider integration mapped onto the same roles.

Migration path: keep the shared admin token as a bootstrap credential for one
release, then require creation of the first admin account and deprecate it.

## 2. UI information architecture (cross-cluster / cluster / node)

The current navigation mixes scopes. We will re-organize it into three
explicit levels. The guiding rule: **an object appears at the narrowest scope
that can operate on it; anything that spans nodes or clusters is escalated to
a shared "Fleet" view.**

### Fleet scope (cross-cluster)

- **Fleet dashboard**: aggregated health, capacity, alerts across all clusters.
- **Fleet instances**: all instances across all clusters in one table with
  cluster/instance/node columns; supports filtering, bulk power/key/script
  actions, and migration initiation. (This is the evolution of the current
  "All instances" page.)
- **Fleet assets**: shared SSH keys, scripts, images, and ISOs, with
  distribution to any cluster.
- **Fleet migrations**: all migration tasks with cancel/retry and progress.
- **Fleet topology / networking**: cross-cluster reachability and overlays
  (see section 5).
- **Fleet users, roles, sessions, audit**: administration from section 1.

**Delivered so far:** the cluster summary now lists only *managed* networks, so host
NICs no longer appear at cluster level; physical NICs, addresses, routes, firewall and
Wi-Fi all live on the node page.

### Cluster scope (single cluster)

- Cluster overview: members, health, counts, resource capacity.
- Instances belonging to that cluster (create, snapshot, clone, rebuild,
  console/desktop).
- Storage pools / volumes, profiles, managed networks, network forwards/
  load balancers/ACLs where supported, images local to the cluster.
- Cluster membership: add/remove members, roles, failure domains.

### Node scope (single member)

- Host network: physical NICs, addresses, routes, firewall, Wi-Fi — **moved
  out of the cluster view**. Network interface state is node-local data and
  does not belong on a cluster summary.
- Host resources and metrics (CPU, memory, disk, TCP retransmits, OOM events,
  block latency), backed by the agent (section 4).
- Host operations history / audit for this node.
- Node upgrade and agent lifecycle (enroll, revoke, rotate certificate).
- Per-node instance placement view.

### Cross-scope resources to centralize

Resources that are naturally fleet-wide will get a single authoritative view
rather than being duplicated per cluster:

- **Images / ISOs**: one catalog, with per-cluster location badges and
  distribute/copy actions.
- **Volumes / storage**: a fleet storage view that can drill down to a cluster
  and pool.
- **Networks / IPs / addresses**: a fleet IPAM and reachability view, with
  per-node NIC details remaining under node scope.
- **Tasks / audit**: one durable task queue and audit stream, filterable by
  cluster/node/user.

## 3. Bug-fix and hardening backlog (from the 2026-11-20 audit)

All findings below were verified against source. They are tracked publicly
without internal details.

### High priority

- Make schema migrations atomic/idempotent on shared-DB drivers; a crash or
  concurrent first start must not permanently brick startup.
- Apply placeholder rebinding to every parameterized query so PostgreSQL
  deployments work for cluster registration and asset CRUD.
- Restrict the SPICE listener and bridge: no unauthenticated all-interface
  listener, and do not allow the bridge to dial arbitrary ports.
- Fix agent revocation so heartbeats cannot revert a revoked agent to online.
- Fence host-task lease write-back by owner; reclaim `running` tasks after a
  worker crash; make lease-renewal loss a hard abort.
- Move migration de-duplication from process memory into the database.
- Make migration progress updates monotonic so trailing callbacks cannot
  resurrect a failed task or erase its error.
- Tie migration execution to the application lifecycle; wait for in-flight
  migrations before closing the store; write terminal status with a fresh
  context.
- Fix the agent firewall rule protocol field dropped by a misplaced comment.
- Make `statusRecorder` forward `SetWriteDeadline` so long node-join requests
  are not cut off by the global write timeout.
- Frontend: redirect to login on session expiry; confirm destructive image
  deletion; surface asset delete failures; catch polling rejections; preserve
  batch selection across polls; refresh fleet status automatically.

### Medium priority

- Bound rqlite connection pool and discovery chatter.
- Move remote-write fsync off the agent read loop; report dropped telemetry.
- Add lightweight cluster member lookup instead of a full summary per host op.
- Keep health checks from tearing down clients with in-flight operations.
- Make agent disconnect/reconnect status updates connection-scoped.
- Add telemetry/audit retention jobs and session cleanup scheduling.
- Harden request body/error handling: no raw internal errors on 5xx, add HSTS.
- Add per-cluster traffic aggregation without an N+1 call per instance.
- Fix idempotency issues: duplicate key injection, script create conflicts,
  non-idempotent address config and temp-file races, Wi-Fi PSK file mode.
- Add rate limiting and validation hardening across auth and registration.
- Frontend follow-ups: polling backoff/in-flight guards, migration cancel/
  retry, confirmations, form validation, i18n key parity, accessibility,
  responsive tables, relative timestamps, pagination edge cases, global error
  handling, terminal reconnect, loading/empty states.

## 4. Rust agent: host monitoring, node-exporter compatibility, eBPF

The agent already collects a basic `fleet_*` metric set (CPU ratio, memory,
load, uptime, network bytes, TCP retransmits, OOM kills, collect duration) and
has an eBPF collector (sockops, tcp_retransmit, oom, block I/O latency) that
falls back to procfs/cgroup, plus a token-protected `/metrics` endpoint.

Planned work:

- **node-exporter compatibility layer**: expose the common host metrics under
  standard `node_*` names and labels (filesystem, CPU, memory, network, disk,
  systemd, time) alongside the existing `fleet_*` series, so existing Prometheus
  dashboards and alert rules work without modification. Provide a
  compatibility flag and document the mapping.
- **Metrics completeness**: per-CPU, filesystem/ MountPoint usage, disk IOPS/
  latency, TCP connection states, process counts, entropy, time sync — the
  standard node-exporter baseline.
- **Remote ingest**: the fleet center already accepts `TelemetryBatch` over the
  agent WebSocket; keep that path and add configurable remote-write to
  Prometheus/VictoriaMetrics. Ensure series validation and label sanitization
  remain consistent for both naming schemes.
- **eBPF expansion**: keep the existing probes; add TCP connect latency/error
  and DNS latency tracing where kernel support allows, with a documented
  capability probe and graceful procfs fallback. Ship an optional prebuilt
  eBPF object; never require it for basic operation.
- **Security**: mTLS identity already in place; keep scrape token protection;
  add allow-listing of scraped label cardinality.
- **UI**: surface host metrics in the node-scope view (section 2) with
  drill-down and PromQL-backed historical charts.

Compatibility target: Prometheus node-exporter metric names and label
conventions, verified against a standard node-exporter instance in CI where
feasible.

## 5. Cross-node / cross-cluster networking

### Research conclusion

- Incus **bridge** networks are node-local; each member runs its own dnsmasq
  and NAT. There is no built-in L2 stretch between clusters.
- The only native overlay is **OVN** (Geneve), actively maintained in Incus,
  but it needs external ovn-central/ovn-host, a quorum of DB nodes, and a
  shared L2 underlay — heavy for a few nodes.
- Kubernetes CNIs (Calico/Cilium/Flannel/Weave/Kube-OVN) do not cleanly fit
  Incus without Kubernetes and add large operational cost.
- WireGuard-based meshes (WireGuard, Nebula, NetBird, ZeroTier) give flat L3
  reachability; ZeroTier also offers L2. They run best on hosts with subnet
  routes rather than inside every guest.
- The real cross-cluster migration pain is **guest DHCP lease persistence**
  (RFC 2131: clients keep the old address until expiry; Incus bridge default
  lease is 1h). Overlays are not required to fix it.

### Decision

1. **Do not build a custom overlay.** This problem is solved by mature options;
   self-building means owning key management, MTU/fragmentation, multicast,
   failover, NAT traversal, and observability forever.
2. Keep per-cluster native bridges with **non-overlapping subnets**. Fix
   cross-subnet migration with static/fixed DHCP assignments, optionally a
   DHCP relay and a unified DNS zone, plus shortened leases as mitigation.
3. Add an optional **WireGuard-based mesh at the host layer** (Nebula or
   NetBird self-hosted, or plain WireGuard) for cross-cluster management and
   workload reachability, exposed as a fleet networking capability.
4. Treat **OVN** as a future opt-in for users who need multi-tenant logical
   networks; use the Incus-integrated form, not a separate deployment.
5. Build fleet-level **IPAM, reachability, and topology visualization** and
   migration-aware DHCP/DNS handling; that is the genuine gap our control
   plane can fill.

## 6. Delivery principles

- Every user-facing change ships with browser-level operation tests and an
  entry in the sanitized user manual; screenshots are captured and redacted
  before being added to docs.
- Keep the three database deployment shapes (single-node SQLite, multi-node
  rqlite, multi-node PostgreSQL) working for every new feature.
- Shared-DB correctness (no in-memory-only coordination) is a hard
  requirement for all new background work.
- Security changes (auth, SPICE, tokens) get a dedicated review and a secret
  scan before release.

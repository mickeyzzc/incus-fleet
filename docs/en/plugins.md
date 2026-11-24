# Plugin System Specification (Design Draft)

**English** | [简体中文](../zh/plugins.md)

> Status: **P0/P1 implemented, P2+ specified**. The manifest/registry package,
> the plugin manager API, the same-origin proxy, per-user API tokens with
> scopes, and the management UI are live. The install orchestrator and catalog
> are still specification only — see §10 for the phase status of each item.
> This document is deliberately sanitized: all addresses and
> hostnames use RFC 5737 documentation ranges and `example.com`. No credentials,
> internal topology, or deployment-specific values appear here.

## 1. What the plugin system is — and is not

incus-fleet gains three built-in capabilities:

1. **A plugin specification** — a manifest format that any external service can
   implement.
2. **A web plugin manager** — browse the catalog, review permissions, install,
   configure, start/stop/uninstall, inspect wiring and health.
3. **An install & wiring orchestrator** — one-click installation that deploys a
   plugin as a managed Incus instance and automatically connects it to fleet's
   capabilities.

Plugins themselves are **not built into fleet**. Plugin code lives in its own
repository, ships as its own image, and upgrades independently. The fleet core
contains only the specification parser, the management UI, the orchestrator,
the same-origin proxy, and the wiring registry.

This gives two tiers of plugins:

| Tier | Who authors it | Capabilities | Example |
|---|---|---|---|
| **Official / deep-wired** | Maintained with fleet | May declare `wiring` effects that change how fleet core subsystems behave (metrics ingestion target, query backend, notification sink, etc.) | VictoriaMetrics, VictoriaLogs, MiBeeHive, MiBeeBell |
| **Community / catalog** | Third parties following the public spec | General capabilities only: deploy instance, allocate volume, inject config, expose UI through proxy, receive event webhooks | MiBeeNVR, monitoring dashboards, custom tools |

Deep wiring is opt-in and explicitly permission-gated: the install screen always
shows exactly which fleet behaviors a plugin will take over. Community plugins
cannot alter core behavior — they can only consume documented APIs.

## 2. Design principles

- **External by default.** A plugin is a normal Incus instance running any
  image. Fleet does not introspect the payload; it manages lifecycle and wiring.
- **Reuse existing primitives.** Installation is composed from fleet's existing
  API: image import, instance create, storage volumes, keys/scripts/batch,
  `instances/{i}/ops`, overlay networking, health checks. No new executor.
- **Same-origin UI.** The current CSP sets `frame-ancestors 'none'` and
  `default-src 'self'`. Plugin UIs are therefore served through a fleet
  same-origin reverse proxy (with a deliberately scoped `frame-ancestors 'self'`
  adjustment), never loaded cross-origin.
- **Static config stays authoritative by default.** Wiring registry values
  override static configuration only for keys a plugin explicitly declares and
  the operator approved at install time.
- **Permissions are visible.** Every manifest declares capabilities; the UI
  shows them before install and after install.
- **Fail safe.** Install is a checkpointed, resumable, rollback-capable task;
  wiring is activated only after the plugin health check passes.

## 3. Manifest format

A plugin package is a signed archive (`.fleetplugin`) containing:

```
manifest.yaml       # required, see schema below
icon.svg            # optional, 64x64
logo.svg            # optional
README.md           # required for catalog listings
LICENSE             # required
images/             # optional image artifacts / import metadata
templates/          # optional install config templates
```

`manifest.yaml` (all URLs use RFC 5737 documentation addresses):

```yaml
schema: fleet-plugin/v1
id: victoria-metrics                 # lowercase DNS label, globally unique
name: VictoriaMetrics
version: 1.0.0
publisher: fleet-official             # or a community publisher ID
tier: official                        # official | community
description: Time-series storage for cluster and agent telemetry
homepage: https://example.com/plugins/victoria-metrics

artifact:
  image:
    source: registry                  # registry | local-tarball
    reference: example.com/plugins/victoria-metrics:1.0.0
    architectures: [amd64, arm64]
  size_hints:
    min_memory_mb: 2048
    min_disk_gb: 20

install:
  defaults:
    cpu: 2
    memory_mb: 4096
    root_disk_gb: 20
    network: managed                 # managed | overlay
  data_disk:
    required: true
    default_gb: 200
    filesystem: ext4                 # ext4 | xfs; formatted automatically
    mount_point: /data
    storage_pool_selector: true      # UI offers fleet's known pools
  templates:                         # rendered into instance config at install
    - name: retention
      label: Data retention
      type: choice
      options: [7d, 30d, 90d]
      default: 30d
    - name: listen_port
      type: port
      default: 8428

capabilities:                         # shown verbatim on the install screen
  deploy_instance: true
  proxy_ui:
    path_prefix: /plugins/victoria-metrics/
    upstream_port: 8428
  proxy_api:
    path_prefix: /api/v1/plugins/victoria-metrics/
    upstream_port: 8428
    paths: [/api/v1/query, /api/v1/query_range]
  event_webhook:
    path: /events
    events: [agent.connected, agent.disconnected, host.task.completed]
  wiring:                             # official plugins only; requires approval
    - key: metrics.remote_write_url
      effect: "fleet telemetry remote_write targets this plugin"
    - key: metrics.query_url
      effect: "fleet PromQL proxy queries this plugin"

health:
  endpoint: /health
  initial_delay_seconds: 10
  interval_seconds: 15
  failure_threshold: 5

permissions:
  host: [api_proxy, event_delivery]
  fleet: [read:clusters, read:instances, read:agents]
```

For community plugins, any `wiring` declaration is rejected at validation time.

## 4. One-click install: orchestration steps

Installing a plugin is a long-running fleet task (modeled on the existing host
task / lease machinery), with progress, cancellation, and rollback:

1. **Validate** — parse and verify the manifest signature; check architecture,
   fleet version compatibility, and requested permissions.
2. **Select placement** — operator chooses target cluster, storage pool,
   network, and installs template values (CPU/memory/disk/format/mount/data
   location). The UI shows the permission and wiring summary for confirmation.
3. **Import image** — `POST /clusters/{cluster}/images/...` (or load the packaged
   tarball into the target cluster's storage).
4. **Allocate data disk** — `POST .../storage-pools/{pool}/volumes`; size and
   filesystem come from the manifest defaults or operator overrides.
5. **Format and mount** — `POST .../instances/{instance}/ops` with
   `run-script`: create filesystem, create mount point, write fstab, mount.
6. **Create instance** — `POST /clusters/{cluster}/instances` with CPU/memory,
   root disk, attached data volume, network (fleet overlay or managed network),
   and a static address where required.
7. **Inject configuration** — keys/scripts/`ops` deliver the rendered config,
   endpoint URLs, and a plugin-scoped credential (see §7).
8. **Start and health-check** — start the instance; poll the plugin health
   endpoint until healthy; plugin-specific services register themselves.
9. **Activate wiring** — only after health passes, write approved wiring values
   into the wiring registry (§5); fleet subsystems reload and begin using the
   plugin.
10. **Publish UI and nav** — register proxy routes and a navigation descriptor.
    The plugin appears in the fleet web UI.

Rollback reverses steps 9→3 on failure: deactivate wiring, remove proxy routes,
stop/delete instance, delete volume/image as appropriate. Each step is a
checkpoint; a failed install can be resumed after fixing the underlying issue.

### Install form (generated from the manifest)

| Field | Source | Example |
|---|---|---|
| CPU / memory | manifest presets + override | 2 vCPU / 4096 MiB |
| Root disk | manifest default | 20 GiB |
| Data disk size & filesystem | manifest + operator | 200 GiB, ext4, mounted at `/data` |
| Storage pool | fleet's known pools | `ssd-pool` / `hdd-pool` |
| Network / address | fleet overlay or managed network + static IP | overlay with static address |
| Plugin settings | manifest `templates` | retention, ports, bind address, credentials |

## 5. Wiring registry

Fleet core subsystems read certain configuration keys dynamically: static
config provides defaults, and the wiring registry provides active overrides.

```
GET/PUT /api/v1/plugins/{id}/wiring      # admin; inspect/change approved wiring
```

Initial wiring keys (grows as plugins are added):

| Key | Effect |
|---|---|
| `metrics.remote_write_url` | Telemetry remote-write destination (VictoriaMetrics plugin) |
| `metrics.query_url` + `metrics.query_type` | PromQL query proxy backend |
| `logs.query_url` | Log search proxy backend (VictoriaLogs plugin) |
| `notifications.sink_url` | Event webhook destination (bell plugin) |

Implementation note: the registry currently keeps wiring in memory
(`Registry.SetWiring` / `WiringValue` / `ClearWiring`); it is not persisted yet,
so a restart clears overrides and static config becomes authoritative again.
Persistence plus "restore default on uninstall" land in P2.

Design rules:

- Only keys declared in the manifest and approved at install time can be set.
- Fleet reloads the relevant subsystem after a wiring change; no process restart.
- Removing a plugin restores the static-config default for its keys.
- Wiring changes are audited like any other privileged operation.

## 6. First-party plugins (planned)

| Plugin | What one-click install achieves |
|---|---|
| **VictoriaMetrics** | Deploys VM; points fleet's telemetry remote-write and PromQL proxy at it; metrics navigation appears; fleet's in-memory 1h metrics ring can be retired as the display source. |
| **VictoriaLogs** | Deploys VictoriaLogs; enables the agent log-shipping path **only through install wiring** (off by default, today the Rust agent only reports metrics); log search navigation appears. |
| **MiBeeHive** | Deploys hive; injects fleet endpoints and scoped credentials; hive registers its IT/ops pages (PXE answering, DNS management, internal apt/yum sources and cache, kite-agent automation). |
| **MiBeeBell** | Later phase: deploys bell; fleet/hive/VM events are delivered to bell's webhook; notification center navigation appears. |

Complementary capabilities: fleet continues to own instance/ISO/unattended
install management; hive adds network-boot (DHCP/TFTP) and answer-file
distribution. hive may optionally distribute kite-agent to selected nodes via
fleet's batch scripts/ops primitives, but never by opening its own SSH path.

## 7. Identity, credentials, and permissions

Current fleet constraints (see `docs/en/architecture.md`): bearer token is a
bootstrap credential that retires after the first local account; sessions carry
a single role (`admin`/`operator`/`viewer`); no per-user API tokens or scopes.

**Decision: build per-user API tokens with scopes directly, as a prerequisite of
the plugin system.** There is no temporary local-account fallback — a weaker
interim credential would create migration debt and an audit blind spot. This
work lands in P0/P1, before any plugin can be installed by the orchestrator.

Token design:

- Per-user API tokens: hashed at rest, explicit scope list, expiry, rotation,
  revocation, and last-used timestamp; issued from the existing user/admin UI.
- Plugin-scoped tokens derive from this: each install issues a token bound to
  `(plugin id, instance id)` carrying only the scopes declared in the manifest
  and approved at install time; uninstall revokes it immediately.
- A `requireScope` middleware sits alongside `requireAuth`/`requireWriter`/
  `requireAdmin`; sessions carry scopes in addition to their single role.
- Plugin-to-fleet calls use the scoped token over the same-origin proxy;
  fleet-to-plugin calls use a per-install shared secret (HMAC for webhooks,
  bearer for proxied API calls).
- Every plugin API call is audited with plugin id and scope, so privileged
  actions are attributable to a specific install rather than a shared account.

## 8. Web UI and frontend seams

Implemented endpoints (P1):

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/plugins` | Registered plugins plus whether the manager is enabled |
| `GET` | `/api/v1/plugins/{id}` | One plugin descriptor |
| `GET` | `/plugin-ui/{id}/…` | Same-origin proxy to the plugin UI (the only proxy namespace) |
| `GET` | `/api/v1/tokens` | List tokens (admins see all; others see their own) |
| `POST` | `/api/v1/tokens` | Issue a token; the response carries the secret exactly once |
| `DELETE` | `/api/v1/tokens/{id}` | Revoke a token |
| `GET` | `/api/v1/tokens/scopes` | Scope vocabulary |

Every plugin endpoint is guarded by `requireScope`, which accepts an API token
bearing the scope or a browser session whose role implies it. Token management
itself is guarded by `read:own-tokens`, which every role implies — an account can
always manage its own credentials; only administrators may act on another
account's tokens or request scopes beyond their own. Planned for P2:
`GET /api/v1/plugins/catalog` (store listings) and the install endpoints.
- Frontend adds a plugin store (persistent nav + data-driven routes):
  `router.addRoute` for each installed plugin's UI path, before the catch-all;
  a generic `PluginPage.vue` renders the proxy target; an icon component map
  and `i18n.mergeLocaleMessage` handle per-plugin icons and titles.
- Hardcoded core navigation remains as a fallback so existing behavior and
  tests stay stable.
- CSP change for same-origin plugin UI: `frame-ancestors 'self'`; `default-src`
  and `connect-src` remain `'self'`. External origins must be proxied, never
  loaded directly.

### 8.1 Interaction design and information architecture

**Rule: a new capability is not delivered until its entry points and page
content are redesigned for the interaction, not appended to the existing UI.**
Adding a feature means re-deriving where the user starts, what they see first,
and what they can ignore — every time.

**Entry points (at most two, never per-plugin top-level items)**

- One sidebar item **Plugins**, carrying a single status badge (update
  available / needs attention). Installed plugins do not each get a top-level
  nav item; they are reached through the plugin detail page, with an optional
  pinned shortcut for daily-use plugins.
- One primary action **Install plugin**, on the plugin list header only.
  Nowhere else in the app offers an install button.

**Two top-level views, never one mixed list**

| View | Content | Purpose |
|---|---|---|
| **Catalog** | Card grid: name, publisher, one-line value, permission count, tier chip | Discover and choose |
| **Installed** | Table: status chip, version, target cluster, health, wiring summary | Operate what exists |

**Install wizard: three steps, one decision each**

1. *Placement* — target cluster, storage pool, network; template values
   pre-filled so the default path needs no typing.
2. *Resources* — CPU / memory / data disk with presets; filesystem, mount point,
   and extra templates collapsed under "Advanced".
3. *Permissions and wiring* — the manifest's effects rendered as human sentences
   ("will take over fleet metric ingestion target"), each with its consequence
   stated; a single acceptance checkbox; then install with per-step progress
   and a resumable failure state.

Raw manifest keys, scope identifiers, and wiring key names are never the primary
label in the UI.

**Plugin detail page: five tabs, fixed order**

*Overview* (status, health history, instance link, version, start/stop/reinstall)
· *Configuration* (only operator-editable template values) · *Wiring* (table of
fleet capability → current target, with "restore default") · *Events* (recent
deliveries and errors) · *Danger zone* (uninstall, typed confirmation, explicit
statement of what data is destroyed).

**State, wording, and failure rules**

- One status vocabulary across the whole product: *installing / healthy /
  degraded / stopped / failed*, same word and same color everywhere.
- Every empty state names the single next action.
- Every failure states what failed, at which step, and what to do next. No raw
  stack traces, no bare "error occurred".
- Destructive actions name the object being destroyed and require typed
  confirmation; a bare "OK" is never sufficient.

**Cognitive-load budget**

- At most one primary button per view; secondary actions live in an overflow
  menu.
- At most seven top-level navigation items.
- No nested modals. Long operations move to a task panel with progress instead
  of blocking dialogs.
- Numbers carry units and a comparison; a percentage alone is not a status.

**Enforcement**

A pull request that adds a user-visible capability must include an IA note:
entry point(s), page structure, and the loading / empty / error / permission-denied
states — plus which existing control it replaces or folds into. A PR that only
adds another button or another table is rejected in review.

## 9. Catalog (community plugins)

The catalog is a signed listing of community plugin packages:

- Authors package a `.fleetplugin` archive following §3 and submit it to the
  catalog repository (pull request + signature verification).
- The install screen shows publisher, version, source, permissions, and wiring
  declarations before confirmation.
- Community plugins cannot declare `wiring`; they receive only general
  capabilities (instance/volume/network/config/proxy/events).
- Example: MiBeeNVR can be packaged as a catalog plugin and installed on fleet
  with one click, without changes to fleet core.

## 10. Phased implementation plan

- **P0 — Contracts & credentials.** ✅ **Done.** `internal/plugin` (manifest
  parse/validate, registry, lifecycle, event fan-out, wiring registry); the
  `plugins:` config section; per-user API tokens with scopes and expiry
  (migration 10, `auth.TokenStore`, `requireScope`), including plugin-scoped
  tokens bound to `(plugin id, instance id)`.
- **P1 — Server seams.** ✅ **Done.** Plugin routes and the same-origin proxy are
  mounted from `httpapi.Deps`; `GET /api/v1/plugins`, `/api/v1/plugins/{id}`,
  `/plugins/…`, `/plugin-ui/…`, and the `/api/v1/tokens` family are live.
  CSP relaxed to `frame-ancestors 'self'` and `X-Frame-Options: SAMEORIGIN` so
  the proxied UI can render. Not yet done in P1: instance creation, health
  polling, and wiring activation after install (they need the P2 orchestrator).
- **P2 — Orchestrator.** Checkpointed install task, manifest-driven templates,
  volume/instance/config workflow, rollback, Kubernetes-free placement using
  existing fleet APIs.
- **P3 — Data-driven UI.** Plugin store, `router.addRoute`, `PluginPage.vue`,
  icon map, i18n merge, CSP adjustment. **Interaction design per §8.1**: catalog
  vs installed split, three-step install wizard, fixed five-tab detail page, one
  status vocabulary, and the four states (loading / empty / error /
  permission-denied) designed per view — not appended to the existing UI.
- **P4 — Events & opt-in telemetry.** Agent/cluster/task event fan-out.
  **Agent log/telemetry reporting stays OFF by default and is enabled only by
  plugin install wiring** — e.g. installing VictoriaMetrics enables metrics
  remote-write, installing VictoriaLogs enables the Rust agent log-shipping path;
  no plugin installed means the agent reports nothing extra and fleet keeps only
  its baseline behavior.
- **P5 — First-party plugins & catalog.** VictoriaMetrics, VictoriaLogs,
  MiBeeHive, later MiBeeBell; community catalog and signature enforcement.

## 11. Out of scope

- Plugins compiled into the fleet binary or sharing fleet's process.
- Cross-origin UI embedding without a same-origin proxy.
- A generic replicated KV or message bus inside fleet's Raft mesh (initial
  phases) — plugins own their state; fleet stores only registry/wiring metadata.
- Automatic backup/restore of plugin data (plugin's responsibility).

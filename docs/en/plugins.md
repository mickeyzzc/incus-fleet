# Plugin system

**English** | [简体中文](../zh/plugins.md)

> Status: **P0–P5 implemented**. The manifest parser and registry, scoped API
> tokens, same-origin proxy, task-based install orchestrator, durable wiring
> registry, event webhooks, data-driven UI, first-party catalog and signed
> community packages are live. See §10 for the phase status of each item and
> §11 for current limitations.
> This document is deliberately sanitized: all addresses and
> hostnames use RFC 5737 documentation ranges and `example.com`. No credentials,
> internal topology, or deployment-specific values appear here.

## 1. What the plugin system is — and is not

incus-fleet provides three built-in capabilities:

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

A plugin package is a `.fleetplugin` archive containing:

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
    path_prefix: /plugin-ui/victoria-metrics/
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

Validation enforced by `internal/plugin.ParseManifest`:

- `schema` must be `fleet-plugin/v1`, `id` must be a lowercase DNS label, and
  `tier` is `official` or `community`.
- `install.defaults.network` is `managed` or `overlay`; `data_disk.filesystem`
  is `ext4` or `xfs`; the mount point is absolute and a required data disk has a
  positive size. Template types are `text`, `choice`, `port` and `bool`.
- Proxy prefixes start and end with `/` and differ from each other; a webhook
  path is absolute and lists at least one event.
- Community plugins may not declare `wiring`; every wiring entry needs a key and
  a human-readable effect.

`manifest.yaml` is the canonical format. Legacy `plugin.json` catalog entries
(connect-only or with an install script) remain supported. `scripts/pack-plugin.sh`
validates either format and, when a signing key is available, writes a
`manifest.yaml.sig` sidecar: a base64 HMAC-SHA256 digest of the exact manifest
bytes, optionally prefixed with `sha256:`. The key comes from
`plugins.signing_key_file` or `INCUS_FLEET_PLUGINS_SIGNING_KEY`. When a key is
configured, unsigned community packages are skipped by the catalog and rejected
by the task install endpoint; with no key there is no trust anchor and local
development packages are still admitted. Built-in manifests are not signed.

## 4. One-click install: orchestration steps

The installer (`internal/plugininstall`) stores each install as a durable host
task of type `plugin.install`. Its ten steps run in order; every successful step
writes a checkpoint into `HostTask.Progress` and appends a `HostTaskEvent`:

1. **validate** — re-check the manifest, reject a community `approvals` list and
   verify the signature when a signing key is configured.
2. **placement** — use the requested `cluster`/`member`, otherwise choose the
   first online cluster and member.
3. **image** — resolve `artifact.image.reference` to a fingerprint already
   present on the target cluster; the task does not import images.
4. **data_disk** — when `data_disk_gb` (or the required manifest default) is
   positive, create custom volume `plugin-<id>-data` on `storage_pool` (default
   `default`) and attach it at `mount_point` during instance creation.
5. **format_mount** — run `plugin.DataDiskScript` through `instances/{i}/ops`
   (`run-script`) against the supplied `instance` name: create the filesystem
   if needed, mount the volume and add an fstab entry.
6. **create_instance** — create container `instance` (default `fleet-<id>`)
   with CPU, memory, root disk and network taken from the request or manifest.
7. **inject_config** — render manifest `templates`, rejecting unknown keys, then
   write `/etc/fleet-plugin/config.env` from a temporary script run with `ops`.
8. **health** — probe `<upstream><health.endpoint>` after
   `initial_delay_seconds`, retrying at `interval_seconds` up to
   `failure_threshold` times. Wiring is activated only after a 2xx response.
9. **activate_wiring** — register a descriptor for the installed plugin, then
   persist and activate each approved key, capturing its static default.
10. **publish** — store the `installed_plugins` row, issue a plugin-scoped token
    with `read:plugins` bound to `(plugin id, instance id)`, and register an
    outbound webhook subscriber for the declared event kinds.

A failure or cancel walks the completed steps backwards: the installed-plugin
entry, wiring, instance and data volume are removed, and the checkpoint rewinds
to the data-disk step so a resumed task replays them. `Resume` accepts only
failed or canceled tasks; only one queued/running task may own a plugin id. With
HA disabled the task runs inline in the server process; with HA enabled the
regular host-task dispatcher claims it.

### Task endpoints

| Method | Path | Scope | Purpose |
| --- | --- | --- | --- |
| `POST` | `/api/v1/plugin-installs` | `manage:plugins` | Enqueue an install; `202` with `{"task": ...}` |
| `GET` | `/api/v1/plugin-installs` | `read:plugins` | List install tasks |
| `GET` | `/api/v1/plugin-installs/{id}` | `read:plugins` | One task including its events |
| `POST` | `/api/v1/plugin-installs/{id}/cancel` | `manage:plugins` | Cooperative cancel at the next checkpoint; `204` |
| `POST` | `/api/v1/plugin-installs/{id}/resume` | `manage:plugins` | Re-queue a failed/canceled task |

The POST body accepts `id` (required catalog plugin id), `name`, `cluster`,
`member`, `instance`, `upstream`, `cpu`, `memory`, `disk`, `network`, `values`
(template overrides), `data_disk_gb`, `filesystem`, `mount_point`,
`storage_pool`, `approvals` (wiring keys), `defaults` (static values to restore),
`health_delay_ms`, `health_interval_ms`, `health_failure_threshold`, and
`signature` (base64 HMAC of the manifest bytes). A task reports `id`,
`plugin_id`, `state`, `progress`, `attempt`, `error`, `created_at`, `updated_at`
and, on the detail route, `events`. States are `queued`, `dispatched`, `running`,
`succeeded`, `failed` and `canceled`. Typical errors are `400 unknown_plugin`,
`403 invalid_signature`, `409 install_in_progress` and `409 invalid_state`.

The older `POST /api/v1/plugins/install` endpoint is retained: it deploys
synchronously, returns `201` with a one-time token, and is still the path used
by the web wizard. It has no checkpoints, cancel or resume.

### Install form (generated from the manifest)

| Field | Source | Example |
|---|---|---|
| CPU / memory | manifest presets + override | 2 vCPU / 4096 MiB |
| Root disk | manifest default | 20 GiB |
| Data disk size & filesystem | manifest + operator | 200 GiB, ext4, mounted at `/data` |
| Storage pool | fleet's known pools | `ssd-pool` / `hdd-pool` |
| Network | manifest default or request `network` | `managed` or `overlay` |
| Plugin settings | manifest `templates` | retention, ports, bind address, credentials |

## 5. Wiring registry

Fleet core subsystems read certain configuration keys dynamically: static
config provides defaults, and the wiring registry provides active overrides.
Approved overrides live in the `plugin_wiring` table (migration 15) and are
loaded at startup; the in-memory registry holds the active values.

```
GET /api/v1/plugins/{id}/wiring          # read:plugins; inspect approved wiring
PUT /api/v1/plugins/{id}/wiring          # manage:plugins; change approved wiring
```

`GET` returns entries with `key`, `value`, `default_value`, `effect`, `enabled`
and `approved`. `PUT` takes `{"values":[{"key","value","enabled"}]}`; each key
must be declared by the manifest and approved at install time, and each value
must be an absolute http(s) URL (a trailing `/` is trimmed). Sending
`"enabled": false` restores the captured default and notifies consumers.
Changes are audited as `plugin.wiring.update` and published as a
`plugin.wiring_changed` event.

Keys consumed today, plus keys declared by built-in manifests:

| Key | Effect | Consumer |
|---|---|---|
| `metrics.remote_write_url` | Telemetry remote-write destination | Fleet startup/remote writer |
| `metrics.query_url` + `metrics.query_type` | PromQL query proxy backend | Metrics proxy |
| `logs.query_url` | Log search proxy backend | Log query proxy |
| `bootstrap.answer_file_url` | Answer-file source for unattended installs | Declared by `hive`; no consumer yet |
| `notifications.sink_url` | Event webhook destination | Declared by `bell`; no consumer yet |

Design rules:

- Only keys declared in the manifest and approved at install time can be set.
- The first plugin declaring a key wins; values are normalized http(s) URLs.
- Fleet reloads the relevant subsystem after a wiring change; no process restart.
- Uninstall, and "restore default", write the captured default back and disable
  the row; the approval record is kept so reinstall needs no second approval.
- Wiring changes are audited like any other privileged operation.

## 6. First-party plugins

Built-in manifests are descriptor-only — fleet never runs plugin code. The
catalog exposes these ids, each with one wiring declaration and an install
port; image references use `example.com` placeholders and must be imported onto
a cluster before a task install can resolve them (see
[one-click plugin install](plugins-one-click.md)):

| Plugin | Catalog id | Port | Declared wiring |
|---|---|---|---|
| **VictoriaMetrics** | `vm` | 8428 | `metrics.query_url` |
| **VictoriaLogs** | `vmlogs` | 9428 | `logs.query_url` |
| **MiBeeHive** | `hive` | 8080 | `bootstrap.answer_file_url` |
| **MiBeeBell** | `bell` | 9090 | `notifications.sink_url` |

Installing `vm` switches the PromQL proxy to VictoriaMetrics; installing
`vmlogs` enables the log-search proxy. Neither remote-write nor Rust agent log
shipping is switched on by these manifests (see §11). The other two keys are
declared so the install screen can show the intended takeover, but fleet does
not consume them yet.

Complementary capabilities: fleet continues to own instance/ISO/unattended
install management; hive adds network-boot (DHCP/TFTP) and answer-file
distribution. hive may optionally distribute kite-agent to selected nodes via
fleet's batch scripts/ops primitives, but never by opening its own SSH path.

## 7. Identity, credentials, and permissions

Per-user API tokens with scopes were implemented in P0/P1 as a prerequisite of
the plugin system (see `docs/en/architecture.md`): the bootstrap bearer token
retires after the first local account, and sessions carry a single role
(`admin`/`operator`/`viewer`) plus implied scopes. There is no temporary
local-account credential.

Token design as implemented:

- Per-user API tokens are hashed at rest, with an explicit scope list, expiry,
  rotation, revocation and last-used timestamp.
- Plugin-scoped tokens are bound to `(plugin id, instance id)` and carry
  `read:plugins`; uninstall revokes them immediately. The task installer and
  the legacy install endpoint both issue such a token.
- `requireScope` sits alongside `requireAuth`/`requireWriter`/`requireAdmin`;
  plugin reads use `read:plugins` and mutations use `manage:plugins`.
- Plugin-to-fleet calls use the scoped token; fleet-to-plugin webhooks carry an
  HMAC signature derived from the signing key. The same-origin proxy strips the
  Fleet session cookie before forwarding.
- Privileged wiring changes are audited with the acting username.

## 8. Web UI and frontend seams

Manager endpoints are mounted from `httpapi.Deps` and guarded by `requireScope`,
which accepts an API token bearing the scope or a browser session whose role
implies it; the inbound event endpoint authenticates with HMAC and an optional
bearer token instead. Token management is guarded by
`read:own-tokens`, implied by every role, so an account can always manage its
own credentials; only administrators may act on another account's tokens.

| Method | Path | Scope | Purpose |
|---|---|---|---|
| `GET` | `/api/v1/plugins` | `read:plugins` | Plugins visible to fleet plus whether the manager is enabled |
| `GET` | `/api/v1/plugins/catalog` | `read:plugins` | Catalog entries (built-ins plus packages under `plugins.dir`) |
| `GET` | `/api/v1/plugins/{id}` | `read:plugins` | One plugin descriptor |
| `POST` | `/api/v1/plugins/install` | `manage:plugins` | Legacy synchronous install; `201` with a one-time token |
| `POST` | `/api/v1/plugins/{id}/toggle` | `manage:plugins` | Enable/disable an installed plugin |
| `GET`, `PUT` | `/api/v1/plugins/{id}/wiring` | `read:plugins`, `manage:plugins` | Inspect/change approved wiring |
| `POST` | `/api/v1/plugin-installs` | `manage:plugins` | Enqueue a task install |
| `GET` | `/api/v1/plugin-installs`, `/api/v1/plugin-installs/{id}` | `read:plugins` | List one/all install tasks |
| `POST` | `/api/v1/plugin-installs/{id}/cancel`, `.../resume` | `manage:plugins` | Cancel/resume an install task |
| `DELETE` | `/api/v1/plugins/{id}` | `manage:plugins` | Uninstall; restores wiring defaults and revokes tokens |
| `POST` | `/api/v1/plugins/events` | HMAC + optional bearer | Inbound plugin events (route mounted only when enabled) |
| `GET` | `/api/v1/logs/query` | `requireAuth` | Log search proxy; `503 unavailable` until a backend is wired |
| `GET` | `/plugin-ui/{id}/…` | `read:plugins` | Same-origin proxy to plugin UI (the only proxy namespace) |
| `GET`, `POST`, `DELETE` | `/api/v1/tokens`, `/api/v1/tokens/{id}` | `read:own-tokens` | Token management; scope vocabulary at `/api/v1/tokens/scopes` |

Configuration keys (all under `plugins:`): `enabled` (default false), `dir`
(default `data/plugins`), `health_interval` (default `15s`),
`signing_key_file` (or `INCUS_FLEET_PLUGINS_SIGNING_KEY`), and `events.enabled`
(default false), `events.path` (default `/api/v1/plugins/events`),
`events.secret_file` (required when events are enabled) and
`events.bearer_token_file` (optional). Entries under `plugins.entries` declare
`id`, `enabled`, `upstream` and/or `upstream_port`, `bearer_token_file`, `role`
and `ui` (`path` must be under `/plugin-ui/`).

- Frontend registers a data-driven route per installed plugin with
  `router.addRoute` before the catch-all; `PluginPage.vue` renders the proxied
  UI; an icon component map and `i18n.mergeLocaleMessage` supply per-plugin
  icons and titles.
- Hardcoded core navigation remains as a fallback so existing behavior and
  tests stay stable.
- CSP for same-origin plugin UI uses `frame-ancestors 'self'`; `default-src`
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

**Status (P3):** the single sidebar entry with one badge, Catalog/Installed
views, three-step wizard, task panel, fixed five-tab detail page and the five
status words (`installing`, `healthy`, `degraded`, `stopped`, `failed`) are
implemented. See §11 for parts of the UI that are not yet backed by the server.

## 9. Catalog (community plugins)

The catalog merges built-in descriptors with community packages unpacked under
`plugins.dir/<id>/`:

- Authors package a `.fleetplugin` archive following §3 with
  `scripts/pack-plugin.sh` and submit it to the catalog repository (pull
  request + signature verification).
- The install screen shows publisher, version, source, permissions, and wiring
  declarations before confirmation.
- Community plugins cannot declare `wiring`; they receive only general
  capabilities (instance/volume/network/config/proxy/events). With a signing key
  configured, a community `manifest.yaml` needs a valid `manifest.yaml.sig`.
- Example: MiBeeNVR is packaged under `plugins/examples/mibee-nvr/` and can be
  installed on fleet without changes to fleet core.

## 10. Implementation status

- **P0 — Contracts & credentials.** ✅ **Done.** `internal/plugin` (manifest
  parse/validate, registry, lifecycle, event fan-out, wiring registry); the
  `plugins:` config section; per-user API tokens with scopes and expiry
  (migration 10, `auth.TokenStore`, `requireScope`), including plugin-scoped
  tokens bound to `(plugin id, instance id)`.
- **P1 — Server seams.** ✅ **Done.** Plugin routes and the same-origin proxy
  are mounted from `httpapi.Deps`; `GET /api/v1/plugins`,
  `GET /api/v1/plugins/{id}`, `/plugin-ui/…`, and the `/api/v1/tokens` family
  are live. CSP relaxed to `frame-ancestors 'self'` and
  `X-Frame-Options: SAMEORIGIN` so the proxied UI can render.
- **P2 — Orchestrator.** ✅ **Done.** `internal/plugininstall` runs checkpointed
  `plugin.install` host tasks with progress events, cooperative cancel, resume,
  reverse rollback, manifests/templates, data-disk formatting and config
  injection, health-gated wiring activation, migration 15 for durable wiring,
  manifest.yaml priority with plugin.json compatibility, and HMAC verification
  of signed community packages.
- **P3 — Data-driven UI.** ✅ **Done.** Plugin store with `router.addRoute`,
  `PluginPage.vue`, icon map and i18n merge; single sidebar entry with a badge;
  Catalog/Installed views; three-step wizard; task panel; five-tab detail page;
  one status vocabulary; loading / empty / error / permission-denied states per
  §8.1.
- **P4 — Events & opt-in telemetry.** ✅ **Done.** Agent, host-task, migration
  and instance-state events are published to registered subscribers; signed
  outbound webhooks, opt-in inbound `POST /api/v1/plugins/events` and the
  `GET /api/v1/logs/query` proxy are live. Log shipping and log search stay off
  by default; only an installed plugin's `logs.query_url` wiring enables the
  search proxy. The Rust agent's telemetry envelope is unchanged.
- **P5 — First-party plugins & catalog.** ✅ **Done.** Built-in descriptors for
  `vm`, `vmlogs`, `hive` and `bell`; MiBeeNVR community example; catalog
  signature sidecars produced by `scripts/pack-plugin.sh`.

## 11. Known limitations

- The web wizard still calls the legacy synchronous `POST /api/v1/plugins/install`.
  The task API in §4 is available but not yet driven from the browser, and the
  task panel currently reflects the wizard's local progress rather than polling
  a task id.
- The detail view's Configuration save (`PUT /api/v1/plugins/{id}/config`) and
  per-key restore (`DELETE /api/v1/plugins/{id}/wiring/{key}`) call routes that
  do not exist on the server. Use `PUT /api/v1/plugins/{id}/wiring` with
  `"enabled": false` to restore a default today. The descriptor returned by
  `GET /api/v1/plugins/{id}` also has no wiring, event, health-history or config
  fields, so those three tabs render empty states.
- Wiring rows and approvals persist, but only built-in descriptors are
  re-registered on restart. An installed community plugin needs its descriptor
  registered again before its wiring loads or its wiring endpoint responds;
  dynamically registered event subscribers are also in-memory only.
- The `hive` and `bell` wiring keys are declarations only: no fleet subsystem
  consumes `bootstrap.answer_file_url` or `notifications.sink_url` yet. No
  built-in manifest declares `metrics.remote_write_url`, so remote write still
  requires static configuration.
- Installed plugins that allocate a data disk should supply an explicit
  `instance` name: the format/mount step runs on that name before instance
  creation. The task resolves image aliases but does not import images.
- Agent log shipping is not implemented: the Rust agent still reports metrics
  only, and there is no agent log envelope or sender to change.
- Community catalog entries are unsigned only while no signing key is
  configured; configure `plugins.signing_key_file` (or the environment
  override) before accepting third-party packages in production.

## 12. Out of scope

- Plugins compiled into the fleet binary or sharing fleet's process.
- Cross-origin UI embedding without a same-origin proxy.
- A generic replicated KV or message bus inside fleet's Raft mesh (initial
  phases) — plugins own their state; fleet stores only registry/wiring metadata.
- Automatic backup/restore of plugin data (plugin's responsibility).

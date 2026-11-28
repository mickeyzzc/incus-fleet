# One-click plugin install (official services)

**English** | [中文](../zh/plugins-one-click.md)

The `vm` (VictoriaMetrics) and `vmlogs` (VictoriaLogs) catalog entries are official
service plugins. One-click install now **creates and configures the service**, rather
than only registering an existing upstream. Third-party manifests are covered by the
[plugin specification](plugin-spec.md).

> Screenshots are sanitized: hosts appear as `x.x.x.x`, accounts as `<user>`, and
> credential values as `<redacted>`. No password is stored in the repository.

## Prerequisite: import the official OCI images

Import each image once per cluster:

```bash
incus remote add quay https://quay.io --protocol=oci --public
incus image copy quay:victoriametrics/victoria-metrics:latest local: --alias vm-oci
incus image copy quay:victoriametrics/victoria-logs:latest local: --alias vmlogs-oci
```

The image keeps data on the instance root disk (20GiB by default); attach a data
volume for production retention.

## What install does

1. Pick a cluster; an empty member means the lightweight scheduler chooses the online
   member with the lowest `max(memory ratio, storage ratio)`.
2. Create a container with the manifest defaults: `2 vCPU / 2GiB / 20GiB`, NIC
   `incusbr0`.
3. Wait for an IPv4 address.
4. The OCI entrypoint starts VictoriaMetrics (8428) or VictoriaLogs (9428) automatically.
5. Record `http://<ip>:8428` as the upstream and issue a plugin-scoped token.
6. Add the sidebar entry; fleet's metrics query proxy switches to the new service.

Any failed step deletes the freshly created instance and revokes the token.

## Metrics takeover and charts

Once `vm` is installed:

- Fleet pushes cluster/member totals every 30 seconds into VictoriaMetrics
  (`fleet_cluster_*`, `fleet_member_*`, `fleet_host_memory_bytes`), with no agent or
  manual remote-write configuration;
- the node metrics panel automatically falls back to VictoriaMetrics when center
  telemetry is empty;
- the cluster overview gains a one-hour memory/storage trend panel.

![Cluster trend](images/plugins/04-trend.jpg)

CPU usage and load still require the actual `incus-fleet-agent`; the built-in push
only carries capacity and usage totals Fleet can read directly.

## Multi-replica deployments

The installer stores the instance's `incusbr0` address
(`http://<vm-bridge-ip>:8428`) as the upstream. Only the member hosting that
instance can reach it. When several Fleet replicas share one database, the other
replicas will time out on chart queries and metric pushes. Expose the service on
an address every replica can reach, then update the installed plugin's upstream:

```bash
incus config device add fleet-vm fleet-http proxy \
  listen=tcp:<fleet-reachable-ip>:8428 connect=tcp:<vm-bridge-ip>:8428
```

The upstream lives in `installed_plugins.upstream` (a plugin wiring
`metrics.query_url` overrides it). No restart is needed; the next push and query
pick up the new address.

## Steps

1. Open **Plugins → Catalog** and pick VictoriaMetrics or VictoriaLogs.

   ![Catalog](images/plugins/01-catalog.jpg)

2. Click **One-click install**. The dialog pre-fills resources, NIC and instance
   name; keep the defaults or adjust them. Leave the member as **Auto** to schedule.

3. After install, the Installed tab shows the status and the sidebar gains a menu
   entry. The plugin UI is framed inside the Fleet shell.

   ![Installed](images/plugins/02-installed.jpg)

   ![Sidebar](images/plugins/03-sidebar.jpg)

## Official vm integration

- The PromQL query proxy automatically points at the deployed VictoriaMetrics; no
  `metrics.query` config and no restart are needed.
- Node pages fall back to VictoriaMetrics automatically when center telemetry is
  absent, so operators no longer toggle the source manually.
- The same data source is ready for cluster/node resource charts; the existing
  metrics panels already read through it.

## Disable and uninstall

Disable only hides the proxy and menu entry. Uninstall deletes the service instance
created by Fleet, revokes the token and removes the entry. Connect-only plugins never
modify an external service.

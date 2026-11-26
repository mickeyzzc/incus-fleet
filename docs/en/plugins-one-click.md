# One-click plugin install (official services)

**English** | [中文](../zh/plugins-one-click.md)

The `vm` (VictoriaMetrics) and `vmlogs` (VictoriaLogs) catalog entries are official
service plugins. One-click install now **creates and configures the service**, rather
than only registering an existing upstream. Third-party manifests are covered by the
[plugin specification](plugin-spec.md).

> Screenshots are sanitized: hosts appear as `x.x.x.x`, accounts as `<user>`, and
> credential values as `<redacted>`. No password is stored in the repository.

## What install does

1. Pick a cluster; an empty member means the lightweight scheduler chooses the online
   member with the lowest `max(memory ratio, storage ratio)`.
2. Create a container with the manifest defaults: `2 vCPU / 2GiB / 20GiB`, NIC
   `incusbr0`.
3. Wait for an IPv4 address.
4. Download and start VictoriaMetrics (port 8428) or VictoriaLogs (port 9428) inside
   the instance; systemd is used when available, otherwise `nohup`.
5. Record `http://<ip>:8428` as the upstream and issue a plugin-scoped token.
6. Add the sidebar entry; fleet's metrics query proxy switches to the new service.

Any failed step deletes the freshly created instance and revokes the token.

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

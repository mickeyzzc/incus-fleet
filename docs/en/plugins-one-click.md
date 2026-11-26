# One-click plugin connect and default unattended templates

**English** | [中文](../zh/plugins-one-click.md)

This guide covers (1) connecting VictoriaMetrics (`vm`) and VictoriaLogs (`vmlogs`) from the
plugin catalog with one click, and (2) the four default unattended-install templates.

> All screenshots are sanitized: host addresses appear as `x.x.x.x`, account names as
> `<user>`, and credential-shaped values as `<redacted>`. No password is stored in this
> repository or in the documentation.

## Prerequisites

The plugin manager must be enabled explicitly:

```yaml
plugins:
  enabled: true
  entries: []
```

Restart `incus-fleet` afterwards. Installed plugins persist in the `installed_plugins`
database table and survive a restart.

## One-click connect

This connects an **already running** service; it does not deploy one. Fleet registers the
upstream, issues a scoped API token bound to the plugin id, adds a menu entry and a
same-origin proxy under `/plugin-ui/<id>/`, and never forwards the Fleet session cookie to
the upstream. Disable and uninstall are both available from the web UI; uninstall revokes the
token but leaves the external service untouched. No VM/container is created, no binary is
downloaded, and the Rust agent's log collection capability is unchanged.

Run VictoriaMetrics (default port 8428) or VictoriaLogs (default port 9428) yourself, then:

1. Open **Plugins → Catalog**. Two built-in entries are shown: `vm`
   (`metrics.query_url`) and `vmlogs` (`logs.query_url`).

   ![Catalog](images/plugins/01-catalog.jpg)

2. Click **One-click install**, enter the real upstream address, and confirm.
3. Open the **Installed** tab to see status, role, wiring, and Disable / Uninstall actions.

   ![Installed plugins](images/plugins/02-installed.jpg)

4. Each installed plugin adds a menu entry below the Plugins item in the sidebar. Clicking it
   keeps the Fleet shell and embeds the proxied UI in a same-origin iframe at `/plugin/<id>`;
   an unreachable upstream only produces an error inside the frame, never a full-page detour.

   ![Plugin menu entry](images/plugins/03-sidebar.jpg)

After a restart the entries remain. Disabling makes the proxy return 404; uninstalling
revokes the token and removes the entry.

## Default unattended templates

Four templates are seeded into the Fleet database, one default per installer family:

| Template | Installer | Notes |
| --- | --- | --- |
| `default-debian-preseed` | Debian / d-i preseed | target disk `/dev/vda` |
| `default-ubuntu-autoinstall` | Ubuntu autoinstall | disk chosen by installer |
| `default-kickstart` | RHEL / Fedora kickstart | target disk `/dev/vda` |
| `default-nocloud` | Generic cloud-init (NoCloud) | disk chosen by installer |

They share timezone `Asia/Shanghai`, locale `zh_CN`, keyboard `us`, and the extra package
`openssh-server`. The account and password follow the internal operator convention and are
deliberately not reproduced here.

![Templates](images/answer-profiles/01-default-templates.jpg)

Select the matching default in the unattended-install dropdown when launching from an ISO;
an empty hostname is filled with the VM name. Templates can be edited under **Images &
automation → Unattended install**.

Rotate the credentials there when needed. Never put the password in code, docs, screenshots,
logs or tickets; share only sanitized captures. Deleting a template does not change accounts
inside already installed systems.

# Fleet service plugin specification

**English** | [中文](../zh/plugin-spec.md)

A plugin is a service instance Fleet creates and manages. One-click install:

1. picks the online member with the lowest `max(memory ratio, storage ratio)`;
2. creates a container from the declared image with CPU/memory/disk/NIC defaults;
3. waits for an IPv4 address;
4. runs the install script inside the instance;
5. records `http://<ip>:<port>` as the upstream, issues a plugin-scoped token and
   mounts the UI at `/plugin/<id>`;
6. deletes the instance if any step fails.

## Layout

```text
my-plugin/
├── plugin.json      # required manifest
└── install.sh       # required when the plugin deploys an instance
```

## Manifest

```json
{
  "id": "hello",
  "name": "Hello service",
  "description": "Minimal example on port 8080.",
  "ui_path": "/plugin/hello",
  "wiring": "example.hello_url",
  "image": "debian/13",
  "default_cpu": "1",
  "default_memory": "512MiB",
  "default_disk": "10GiB",
  "default_network": "incusbr0",
  "port": 8080,
  "health_path": "/",
  "install_script_file": "install.sh"
}
```

| Field | Required | Notes |
| --- | --- | --- |
| `id` | yes | lowercase DNS label, unique |
| `name` | yes | display name |
| `description` | no | catalog card text |
| `ui_path` | yes | must start with `/plugin/` |
| `wiring` | no | e.g. `metrics.query_url`, `logs.query_url` |
| `image` | deploy | existing cluster image alias |
| `default_cpu` / `default_memory` / `default_disk` | deploy | resource defaults |
| `default_network` | deploy | usually `incusbr0` |
| `port` | deploy | service port used to build the upstream |
| `health_path` | no | health endpoint |
| `install_script_file` | deploy | relative path, no `..` |
| `upstream_hint` | connect-only | http(s) URL for existing services |

Supplying `upstream` at install time skips deployment and connects an existing service.

## Install script

Runs as `root` via `sh -s` with a 10 minute timeout. Make the service survive a
restart (systemd when available, otherwise `nohup`). A non-zero exit fails the
install and Fleet deletes the instance. Never write Fleet tokens or passwords into
the script.

## Packaging

```bash
scripts/pack-plugin.sh plugins/examples/hello
# -> dist/hello.fleetplugin
```

The script validates required fields, the id/port, and the install script, then
creates a tar.gz. Extract it into `plugins.dir` (default `data/plugins`) to make it
appear in the catalog.

## Security

Each install receives a token with only `read:plugins`, bound to the plugin id.
Uninstall deletes the instance and revokes the token. The same-origin proxy never
forwards the Fleet session cookie, and plugins never receive host-level access.

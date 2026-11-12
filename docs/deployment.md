# Deployment

This guide deploys one incus-fleet instance per management host, with each instance connecting
to every Incus cluster. A separate Caddy host load-balances the UI/API.

The examples use documentation addresses and placeholder names. Replace them with your values.
Never commit real tokens, private keys, or production configuration.

## Topology

```
Caddy (sticky sessions, TLS)
  ├── incus-fleet @ app-a  ──▶ Incus c1 (https://192.0.2.10:8443)
  │                       └─▶ Incus c2 (https://192.0.2.11:8443)
  └── incus-fleet @ app-b  ──▶ Incus c1
                          └─▶ Incus c2
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
`/home/incus/data`. Adjust paths if you use a different deployment directory.

Follow logs with:

```bash
sudo journalctl -u incus-fleet -f
```

Verify locally:

```bash
curl -fsS http://127.0.0.1:8080/api/v1/healthz
curl -fsS http://127.0.0.1:8080/api/v1/version
```

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

Caddy proxies WebSockets transparently: the terminal and file endpoints
(`/api/v1/.../console|exec|files`) ride the same sticky cookie as the UI, so no extra
configuration is needed.

## 6. Deploy upgrades

1. Build and copy the new binary to `/home/incus/bin/incus-fleet`.
2. `sudo systemctl restart incus-fleet` on one host; verify health.
3. Repeat on the other host.
4. Unfinished migrations are marked failed at startup. Re-submit them after checking that no
   partial copy remains on the target.

Roll back by restoring the previous binary and restarting. The database schema is additive; no
downgrade step is currently required.

## 7. Backup

Back up per app host:

- `/home/incus/config.yaml` (contains no secrets if the token is in the env file);
- `/home/incus/incus-fleet.env` (contains the admin token — protect it);
- `/home/incus/certs/` (private keys);
- `/home/incus/data/fleet.db` (SQLite) or the PostgreSQL database.

Stop the service or use SQLite's online backup before copying the database file.

## 8. Troubleshooting

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
- [ ] Secret scan run before pushing

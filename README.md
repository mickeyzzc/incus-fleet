# Incus Fleet

Self-hosted control plane for managing multiple independent [Incus](https://linuxcontainers.org/incus/)
clusters from one UI and API. Instances can be copied or moved between clusters with one click,
including live migration for stateful virtual machines.

## Features

- Fleet dashboard with per-cluster health, membership, storage, and instance counts
- Container and VM inventory with detail drawers
- Cross-cluster `move` and `copy` migrations, with optional snapshots
- Live migration for running VMs with `migration.stateful=true`
- Automatic source restart when a migration fails or is interrupted
- SQLite by default; PostgreSQL selectable at initialization
- Single Go binary with the Vue UI embedded
- Admin-token login with HttpOnly sessions; bearer tokens for automation
- Cluster connections pinned to per-deployment client certificates

## Architecture at a glance

```
Browser ──▶ Caddy ──▶ incus-fleet (server A) ──▶ Incus cluster c1
                  └─▶ incus-fleet (server B) ──▶ Incus cluster c2
```

The two app instances are independent. Each one connects to both Incus clusters over the
network and drives migrations through the Incus API. There is no application-level cluster
membership, shared database, or mutual backup relationship between `c1` and `c2`.

See [docs/architecture.md](docs/architecture.md) for components, migration flow, and failure
semantics.

## Requirements

- Go 1.27 or newer
- Bun 1.4 or newer (frontend)
- Two or more Incus clusters reachable over HTTPS
- A client certificate trusted by each cluster
- SQLite (default) or PostgreSQL

## Quick start

```bash
make deps
make build
./bin/incus-fleet -version
```

Generate a client certificate, trust it on every cluster, and write a config:

```bash
scripts/gen-client-cert.sh incus-fleet ./certs
incus config trust add-certificate ./certs/incus-fleet.crt --name incus-fleet
install -m 600 deploy/examples/config.sqlite.yaml config.local.yaml
$EDITOR config.local.yaml
INCUS_FLEET_AUTH_TOKEN="$(openssl rand -hex 32)" ./bin/incus-fleet -config config.local.yaml -check
INCUS_FLEET_AUTH_TOKEN="..." ./bin/incus-fleet -config config.local.yaml
```

Open `http://127.0.0.1:8080` and sign in with the admin token.

The config reference and full deployment steps are in [docs/deployment.md](docs/deployment.md).
The HTTP API is documented in [docs/api.md](docs/api.md).

## Development

```bash
make test        # Go + frontend tests
make coverage    # Go >= 90% gate, frontend 90/90/90/85 thresholds
make lint        # go vet, golangci-lint, ESLint
make check       # format check, vet, lint, tests
```

Frontend development runs Vite with an API proxy to `127.0.0.1:8080`:

```bash
cd web && bun run dev
```

Coding conventions and the TDD contract are in [docs/standards.md](docs/standards.md).

## Repository layout

```
cmd/incus-fleet        process entry point
internal/app           service wiring and HTTP lifecycle
internal/auth          admin token and in-memory sessions
internal/clusters      Incus connection manager and summaries
internal/config        YAML config loading and validation
internal/httpapi       REST API and embedded SPA handler
internal/instances     instance inventory
internal/migrations    migration orchestration and rollback
internal/store         SQLite/PostgreSQL persistence
internal/testutil      test doubles for the Incus API
deploy/                systemd, Caddy, and example configs
docs/                  architecture, API, standards, deployment
web/                   Vue 3 + TypeScript + Vite + Element Plus UI
```

## Security

- Treat the admin token, client private keys, and database credentials as secrets.
- The example configuration files contain placeholders only.
- Cluster endpoints must use HTTPS and pinned client certificates; trust tokens are not used.
- The UI stores no token in browser storage. Login exchanges the token for an HttpOnly cookie.
- Report vulnerabilities privately to the repository owner.

## License

Proprietary. All rights reserved. See [LICENSE](LICENSE).

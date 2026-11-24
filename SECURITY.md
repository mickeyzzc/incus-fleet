# Security Policy

**English** | [简体中文](docs/zh/SECURITY.zh-CN.md)

## Supported versions

Incus Fleet is developed on `main`. Security fixes are applied to the latest commit and the
most recent release, if one exists. Older commits and forks are not supported.

## Reporting a vulnerability

**Do not open a public issue for a security problem.**

Use GitHub's private vulnerability reporting:

1. Open <https://github.com/mickeyzzc/incus-fleet/security/advisories/new>.
2. Describe the issue, affected version or commit, reproduction steps, and impact.
3. Include logs or a proof of concept with secrets and real infrastructure details removed.

If private reporting is unavailable, contact the maintainer [@mickeyzzc](https://github.com/mickeyzzc)
through a private GitHub message. Please do not disclose the issue publicly before a fix is
available.

You can expect an acknowledgement within a few days. This is a volunteer-maintained project,
so timelines are best effort. We will credit reporters in the advisory unless you ask to stay
anonymous.

## Scope

Reports are especially useful for:

- Authentication or session bypass, token leakage, or cookie handling flaws;
- Authorization gaps between the UI, API, and Incus operations;
- Server-side request forgery or unsafe Incus endpoint handling;
- Command or script injection through instance names, device fields, file paths, or migration
  fields;
- Cross-site scripting in the Vue UI or API error rendering;
- Sensitive data exposure through logs, API errors, screenshots, or the asset library;
- SPICE or terminal exposure beyond the documented trust boundary;
- Data loss or source deletion during migration rollback.

## Deployment hardening

Operators are responsible for the surrounding environment:

- Serve the UI over HTTPS (directly or behind Caddy) and restrict the API port to the proxy.
- Generate a high-entropy admin token and store it in an environment file with mode `0600`.
- Protect client private keys and the database.
- Restrict Incus API port 8443 to the app hosts.
- Restrict SPICE ports 5910-5989 to the app hosts; the SPICE listener has no ticket
  authentication.
- Keep Go modules and Bun dependencies patched.
- Back up the database; it contains migration history and the shared key/script library.

## Out of scope

- Vulnerabilities in Incus itself; report those to the Incus project.
- Misconfiguration of Caddy, systemd, firewalls, or the host OS.
- Physical access attacks.
- Denial of service from an authenticated administrator who already controls the fleet.

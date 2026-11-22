# Handling secrets and operator-specific data

This repository is public. Two rules keep it that way: **nothing sensitive is
committed**, and **the tooling enforces that automatically** rather than relying
on a person remembering to check.

## What must never be committed

- Private keys, certificates, CSR material, and anything under a `certs/` path.
- Passwords, API tokens, cluster join tokens, and database credentials.
- Real LAN subnets, hostnames, and NOC notes. Use the RFC 5737 documentation
  ranges instead: `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`.
  Hostnames should end in `.example`.
- Operator notes and audit trails. These belong in a directory outside the
  repository (see below), never in `docs/`.

## Where operator material lives instead

Keep certificates and runtime state outside the working tree:

```
~/.config/incus-fleet/certs/<cluster>/<name>.crt
~/.config/incus-fleet/certs/<cluster>/<name>.key
```

Point configuration at those paths:

```yaml
clusters:
  - name: c62
    endpoint: https://incus.example:8443
    tls_cert: /home/incus/.config/incus-fleet/certs/c62/app.crt
    tls_key:  /home/incus/.config/incus-fleet/certs/c62/app.key
```

Environment overrides work the same way, for example
`INCUS_FLEET_AUTH_TOKEN` for the bootstrap admin token. Generate tokens at
runtime rather than pasting literals into a file:

```bash
INCUS_FLEET_AUTH_TOKEN="$(openssl rand -hex 32)" ./bin/incus-fleet -config config.local.yaml -check
```

Local notes and audit documents belong under a path the ignore rules already
cover — `docs/local/`, `local-notes/`, `notes/`, or `scratch/` — or entirely
outside the repository. All four are ignored as whole directories.

## The three enforcement layers

The protection is layered so a single mistake does not leak credentials.

**Layer 1 — `.gitignore`.** Denies by location (whole local-material
directories) and by credential shape (`*.key`, `id_ed25519`, `*.kubeconfig`,
`*.env`, `*.tfstate`, `secrets/`, and similar). These rules apply at any depth,
so a note copied out of `docs/local/` into a new folder is still caught by
content scanning even when the name looks innocent.

**Layer 2 — pre-commit hook.** `scripts/secret-scan.sh --staged` inspects the
actual bytes about to enter history: private-key blocks, credential
assignments, bearer tokens, the operator's LAN subnets, and hostnames. It also
refuses to stage paths that should stay local.

Install it once per clone:

```bash
make hooks          # sets core.hooksPath to .githooks
```

**Layer 3 — CI.** The `secrets` job in `.github/workflows/ci.yml` runs the same
scanner over every tracked file and every reachable commit. It catches
`git commit --no-verify` bypasses and forks where hooks were never installed.

## Checking by hand

```bash
make secret-scan           # staged changes
make secret-scan-all       # every tracked file
make secret-scan-history   # every commit, looking for private keys
```

A clean run prints `secret-scan: clean`. Findings print the file, line, and
which rule matched; fix them rather than adding exceptions. If a value is a
deliberate fixture, switch it to a documentation range or an `.example`
hostname — that is what keeps the allowlist small and auditable.

## If something does leak

1. Rotate the credential first. Assume any pushed secret is compromised;
   scrubbing history does not un-publish it.
2. Then remove it from history if it is still recoverable in a branch.
3. Record what happened and which rule missed it, and extend the scanner.

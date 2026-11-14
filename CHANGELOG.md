# Changelog

**English** | [简体中文](CHANGELOG.zh-CN.md)

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- Bilingual documentation: English canonical files and Simplified Chinese mirrors for the
  README, guides, API reference, standards, and community documents.
- Shared asset library for SSH public keys and shell scripts.
- Batch key injection, script execution, and power operations across clusters.
- ISO installer upload and blank-VM launch for manual OS installation.
- Image import, cross-cluster distribution, cached-image refresh, clone, and rebuild.
- Browser exec terminals with persistent shells, serial consoles, and SPICE remote desktop.
- Host and per-instance resource monitoring without guest agents.
- Instance file management, snapshots, device mounting, password and key injection.

### Changed

- Licensed under the GNU Affero General Public License v3.0.
- Sanitized documentation, examples, tests, and screenshots to remove real infrastructure
  details.

## 0.1.0 - 2026-11-11

### Added

- Initial control plane for multiple independent Incus clusters.
- Fleet dashboard with cluster health, membership, storage, and instance counts.
- Container and VM inventory with detail views.
- Cross-cluster move and copy migrations with rollback.
- Live migration for stateful VMs.
- SQLite and PostgreSQL persistence.
- Admin-token authentication with HttpOnly sessions and bearer tokens for automation.
- Single Go binary with an embedded Vue 3 UI.
- systemd, Caddy, and example configuration deployment assets.

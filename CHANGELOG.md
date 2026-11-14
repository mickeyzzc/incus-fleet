# Changelog

**English** | [简体中文](CHANGELOG.zh-CN.md)

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 0.1.0 - 2026-11-14

First tagged release of the multi-cluster control plane.

### Added

- Initial control plane for multiple independent Incus clusters.
- Fleet dashboard with cluster health, membership, storage, instance counts, and host
  CPU/memory utilization.
- Container and VM inventory with detail views.
- Cross-cluster move and copy migrations with rollback.
- Live migration for stateful VMs.
- SQLite and PostgreSQL persistence.
- Admin-token authentication with HttpOnly sessions and bearer tokens for automation.
- Single Go binary with an embedded Vue 3 UI (Simplified Chinese and English).
- systemd, Caddy, and example configuration deployment assets.
- Image import, cross-cluster distribution, cached-image refresh, clone, and rebuild.
- ISO installer upload and blank-VM launch for manual OS installation.
- Browser exec terminals with persistent shells, serial consoles, and SPICE remote desktop.
- Instance file management, snapshots, device mounting, password and key injection.
- Shared asset library for SSH public keys and shell scripts.
- Batch key injection, script execution, and power operations across clusters.
- Host and per-instance resource monitoring (CPU, memory, disk, network) without guest agents.
- Bilingual documentation: English canonical files and Simplified Chinese mirrors for the
  README, guides, API reference, standards, and community documents, with screenshots in
  both UI languages.

### Changed

- Licensed under the GNU Affero General Public License v3.0.
- Sanitized documentation, examples, tests, and screenshots to remove real infrastructure
  details.

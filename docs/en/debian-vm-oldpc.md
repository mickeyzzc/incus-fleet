# Deploy a test Debian VM on old-pc-server

**English** | [中文](../zh/debian-vm-oldpc.md)

This guide provisions a Debian test VM on the `c63` cluster member `old-pc-server` through
the Incus Fleet web UI: 2 vCPU / 4 GiB, a 20 GiB root disk, a 100 GiB volume mounted at
`/var`, and NICs on both the site LAN segment and the private overlay segment.

> All screenshots are sanitized: host addresses are replaced with `x.x.x.x` and MAC
> addresses with `xx:xx:...`. No passwords, tokens, or keys appear in the captures.

## Final configuration

| Item | Value |
| --- | --- |
| Instance | `debian-lab-oldpc` |
| Cluster / member | `c63` / `old-pc-server` |
| Type | Debian trixie virtual machine |
| CPU / memory | 2 vCPU / 4 GiB |
| Root disk | `root`, 20 GiB, `/` |
| Data disk | `vardata`, 100 GiB, `/var` |
| LAN NIC | `eth0`, bridged to host bridge `lan63br2` (uplink `enp4s0`) |
| Overlay NIC | `eth1`, bridged to `fleetbr0` |
| Addresses | Both obtained by DHCP |

## One-time CLI prerequisites

The web UI can only import image files (with a size cap) and cannot set member-specific
network `parent` keys, so these steps use the CLI. Everything after them is done in the UI.

1. Import the VM image: `incus image copy images:debian/13 local: --alias debian-13-vm --vm`
2. On `old-pc-server`, create host bridge `lan63br2`, move the management address from
   `enp4s0` to the bridge, and persist the setup with a systemd oneshot unit. Add
   `denyinterfaces enp4s0 lan63br2` to `/etc/dhcpcd.conf`. Restart `incus` so the bridge is
   detected.
3. Cluster networks require a two-step define-then-instantiate flow; a network left
   `Pending` cannot be used by instances.

## Web steps

1. Open **Clusters → c63 → Network** to inspect the network list
   (`01-physical-network.jpg`). The `lan63` physical network in the capture was created
   during troubleshooting and was removed afterwards; the final setup uses the host bridge
   directly.
2. **Create instance**: name `debian-lab-oldpc`, type VM, image `debian-13-vm`, CPU `2`,
   memory `4 GiB`, root disk `20 GiB`, pool `default`, network `lan63`
   (`02-create-form.jpg`). Leave **Additional storage** empty — see BUG-01. Start the VM.
3. **Devices → Add disk**: name `vardata`, disk, pool `default`, no source, path `/var`,
   size `100GiB` (`03-attach-var-disk.jpg`).
4. **Devices → Add NIC**: name `eth1`, network `fleetbr0`, no static addresses
   (`04-attach-nic.jpg`).
5. **Files**: upload `/etc/systemd/network/enp6s0.network` (see
   `enp6s0.network.example`) so the second NIC is configured on boot
   (`05-network-config.jpg`), then restart `systemd-networkd` in the guest.
6. Restart the VM and verify both DHCP leases (`06-final-list.jpg`,
   `07-final-devices.jpg`).

## Access

The official Debian image ships **only a locked `root` account** and no SSH server, so there
is no default username/password. Use the web **Terminal** (exec) for a passwordless root
shell, set a root password with `passwd root` before using the remote-desktop console, or
install `openssh-server` if you need SSH.

Opening **Remote desktop** calls `POST .../spice/prepare` with `apply: true`. Because Incus
refuses `raw.qemu` changes while a VM runs, Fleet force-stops the VM, writes the `-spice`
listener, and starts it again. Seeing a boot sequence after clicking Remote desktop, even
though the list already said "Running", is expected.

## Known web gaps

| ID | Issue | Workaround |
| --- | --- | --- |
| BUG-01 | Create-form extra disks lack `source`; instance creation fails | Add the disk from the Devices tab instead |
| BUG-02 | Unmanaged bridges are treated as macvlan parents, leaving NICs link-down | Recreate the NIC with `nictype=bridged` and the bridge as parent |
| BUG-03 | The network dialog cannot set member-specific `parent` | Define per member and instantiate via CLI |
| BUG-04 | `dir` pools do not enforce the recorded 100 GiB volume size | Use a pool with quota support |

See the [Chinese guide](../zh/debian-vm-oldpc.md) for detailed commands and verification.

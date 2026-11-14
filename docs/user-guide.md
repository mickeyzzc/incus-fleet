# User Guide

**English** | [简体中文](zh-CN/user-guide.md)

This guide walks through a complete two-cluster workflow, from signing in to delivering a
usable instance. Replace the example cluster names `c1` and `c2` with your own. All addresses
and subnets are documentation examples.

Prerequisites:

- Fleet is deployed as described in [deployment](deployment.md) and reachable (example:
  `http://fleet.example:8080`).
- You have the admin token (written to `INCUS_FLEET_AUTH_TOKEN` at deploy time).
- Both clusters are [registered](#register-a-cluster) and online.

## Contents

1. [Sign in and choose a language](#sign-in-and-choose-a-language)
2. [Tour the dashboard](#tour-the-dashboard)
3. [Images: import and distribute](#images-import-and-distribute)
4. [ISO installers and OS installation](#iso-installers-and-os-installation)
5. [Create an instance](#create-an-instance)
6. [Day-to-day instance management](#day-to-day-instance-management)
7. [Online terminals and remote desktop](#online-terminals-and-remote-desktop)
8. [Maintenance actions: scripts, passwords, keys](#maintenance-actions-scripts-passwords-keys)
9. [Image upgrades and cloning](#image-upgrades-and-cloning)
10. [Port forwarding](#port-forwarding)
11. [Walkthrough: deploy a PostgreSQL container](#walkthrough-deploy-a-postgresql-container)
12. [Shared asset library and batch operations](#shared-asset-library-and-batch-operations)
13. [Resource monitoring](#resource-monitoring)
14. [Storage pools and networks](#storage-pools-and-networks)
15. [FAQ](#faq)

## Sign in and choose a language

1. Open the fleet URL, enter the admin token, and sign in. Sessions last 12 hours; sign in
   again after they expire.
2. Use the gear icon in the top-right corner → **Language** to switch between English and
   中文. The choice applies immediately and is stored in the browser.

## Tour the dashboard

The dashboard opens after sign-in and shows:

- Online status, Incus version, and instance count for every cluster;
- Total instances and the running/stopped split;
- Resource overview (CPU, memory, storage).

Click a cluster to open its detail page: instances, images, storage pools, networks,
profiles, and members.

## Images: import and distribute

Fleet uploads a local image file to one cluster, then distributes it to other clusters with
one click: upload once, use everywhere.

### Step 1: prepare the image files

Export the image on any machine with Incus:

```bash
# Unified file (one tar archive)
incus image export debian/13 debian13

# Split files (metadata tar archive + .root filesystem)
# Produces debian13 (metadata) and debian13.root (root filesystem)
```

ISO installers use the separate [ISO workflow](#iso-installers-and-os-installation); this
image import handles container and VM images.

### Step 2: upload to a cluster

1. Open the target cluster (for example `c2`) → **Images** → **Import**.
2. Choose the image files:
   - Unified file: select only the tar archive;
   - Split files: select the metadata tar archive, then the matching `.root` file under
     **Rootfs file**.
3. Choose the image type (container / virtual machine). Optionally set an alias (for example
   `debian/13`) so the image is easy to pick when creating instances.
4. Click **Import**. When the upload finishes, the image appears in the list by fingerprint.

The API is equivalent to the UI:

```bash
curl -H "Authorization: Bearer $INCUS_FLEET_AUTH_TOKEN" \
  -F "image=@debian13" -F "rootfs=@debian13.root" \
  "http://fleet.example:8080/api/v1/clusters/c2/images/import?type=container&alias=debian/13"
```

### Step 3: distribute to other clusters

1. In the image list of the cluster that owns the image (for example `c2`), click
   **Distribute**.
2. Choose the target cluster (for example `c1`), optionally set an alias, and confirm.
3. The same fingerprint appears in the target cluster's image list when the transfer
   completes.

## ISO installers and OS installation

Fleet can upload ISO installers (for example the official Debian, Ubuntu, or CentOS images)
and boot a blank virtual machine from the ISO to install the OS.

1. Open the cluster → **ISO installers** → **Upload ISO**, then choose the `.iso` file and a
   storage pool.
2. The ISO appears in the list. Click **Create VM**:
   - Enter a VM name (required);
   - Optionally choose a network (recommended when the installer needs to download packages;
     a physical NIC or a managed bridge both work);
   - Optionally set the root disk size (for example `20GiB`, enough for the OS).
3. Fleet creates a blank VM that boots from the ISO. It relaxes Secure Boot for compatibility
   with most installers and configures the SPICE graphics channel in advance.
4. Open the VM's **Remote desktop** (see below) to see the installer in the browser and
   complete the installation as if you were at a physical machine.
5. When installation finishes, shut the VM down, detach the ISO in the device panel (or
   delete and recreate the VM), then boot into the installed system.

ISO files live in the storage pool and can be deleted from the list at any time.

## Create an instance

Open a cluster → **Instances** → **Create instance**. Key fields:

| Field | Notes |
| --- | --- |
| Name | Unique within the cluster |
| Type | Container / virtual machine |
| Image | Pick an imported image by alias or fingerprint |
| Network | See below |
| CPU / memory / root disk size | Limits; leave blank for defaults |

### Choosing a network: same-subnet IPs

The instance address depends on the selected network:

- **Managed bridge** (for example `incusbr0`): the instance joins the Incus internal NAT
  subnet (for example `10.x.x.x`). Outbound traffic is NATed and the instance is not directly
  reachable from outside.
- **Physical NIC (macvlan)**: NICs without the managed marker (for example the host NIC
  `eth0`). The instance is bridged directly onto the physical LAN via macvlan and **gets a
  DHCP address from the same subnet as the host** (for example, if the host is on
  `198.51.100.0/24`, the instance receives `198.51.100.x`). Other LAN devices can reach the
  instance directly.

To give an instance an address on the host subnet, select the **physical NIC** (parent is the
host NIC name) when creating it. Do not enter a static IP: macvlan addresses come from the LAN
DHCP server.

After creation, confirm in the address column that the IP is in the expected subnet. If no
address appears, check the switch/router DHCP service and the NIC bridge state.

## Day-to-day instance management

From the instance list or the detail drawer you can:

- **Start / stop / restart**: stop and restart ask for confirmation and offer a **force**
  option.
- **Snapshots**: create (optionally with running state), restore, delete. Restoring rolls the
  disk back; confirm before proceeding.
- **Terminal**:
  - *Exec*: an interactive shell inside the instance;
  - *Console*: the serial console (prefer Exec for containers; console close code 1006 is a
    normal shutdown).
- **File management**: browse the instance filesystem, upload, download, create directories,
  and delete files.
- **Devices**: attach a disk (storage volume or host path) or a second NIC; detach devices in
  the same panel.
- **Delete instance**: asks for confirmation and can delete the instance's storage volumes at
  the same time.

## Online terminals and remote desktop

Fleet provides three browser sessions, available from the detail drawer or the list actions:

- **Terminal (Exec)**: works for containers and VMs. Opens an interactive root shell, supports
  window resizing and interrupt signals. In command-line mode the session runs a **persistent
  shell**: every submitted command goes to the same shell, so environment variables and the
  working directory survive across commands. If the command exits and the session closes, the
  next input reopens the session automatically and runs the pending command.
- **Serial console (Console)**: useful for boot problems and kernel output.
- **Remote desktop (VMs)**: running VMs can open a graphical console. Fleet configures a QEMU
  SPICE channel for the VM (a `-spice` TCP listener in `raw.qemu`, with a port allocated per
  instance name). The browser connects to the SPICE server channel by channel through a
  websocket-TCP bridge and shows the installer or desktop live. The first configuration
  restarts a running VM once; later sessions connect immediately. Containers have no display
  device and do not offer remote desktop.

  Security note: the SPICE TCP listener has no ticket authentication and listens on all
  interfaces of the cluster member. Restrict access to the port range with a firewall.

Sessions close with the page and leave no daemon process on the host.

## Maintenance actions: scripts, passwords, keys

The **Maintenance** panel in the instance detail drawer offers three frequent actions, all
executed as root inside the instance:

| Action | Usage | Notes |
| --- | --- | --- |
| Run script | Paste a shell script → run | Executes via `sh -s`, streams exit code, stdout, and stderr |
| Set password | Account + new password | Piped to `chpasswd` on stdin; the password never appears in the process list |
| Inject SSH key | Account + one public key line | Appends to `~/.ssh/authorized_keys`, fixes directory and file permissions |

After injecting a key you can `ssh account@instance-ip` from the LAN without a password (with
the same-subnet IP described above).

## Image upgrades and cloning

- **Image upgrade (rebuild)**: choose the target image (alias or fingerprint) in the
  maintenance panel. Fleet rebuilds the instance root disk from the new image. The instance
  **must be stopped** and the root disk is wiped; data disks (storage volumes) are unaffected.
- **Refresh cached images**: in a cluster image list, a cached image has a **Refresh** action
  that asks Incus to pull the latest version from the source (the alias stays, the fingerprint
  changes).
- **Clone instance**: use **Clone** in the instance row, enter a new name, and copy a complete
  instance (disks and config) within the same cluster. Optionally start it after cloning. Good
  for scaling out from a template.

## Port forwarding

Instance LAN addresses may not be directly reachable (for example when only a few host ports
are exposed). Attach a **proxy port-forward** device to the instance:

1. Instance detail → **Devices** → attach device, type **Port forward**.
2. Enter the listen address (host side, for example `tcp:0.0.0.0:8080`) and the connect
   address (instance side, for example `tcp:127.0.0.1:80`).
3. Save; traffic to the host port now reaches the service inside the instance.

## Walkthrough: deploy a PostgreSQL container

This walkthrough ties the previous sections together: create a Debian container on cluster
`c2` with a LAN (same-subnet) address, install the latest PostgreSQL in the online terminal,
configure its password and listener, and verify it from another machine. Passwords are shown
as `<database password>`; replace them with your own values.

### Step 1: create the container on the LAN

On the cluster detail page click **Create instance**:

- Name `demo-db`, type **container**, image `debian/13` already present on the cluster;
- Set the root disk as needed (20GiB in this example; leave headroom for a database);
- Choose the **physical NIC** as the network (the LAN DHCP server assigns a same-subnet
  address), and enable **Start after creation**.

![Create a PostgreSQL container](images/pg-create-form.png)

When creation finishes, `demo-db` is running and has an address on the host subnet (for
example `198.51.100.x` in the `198.51.100.0/24` documentation subnet):

![demo-db running](images/pg-running.png)

### Step 2: install the latest PostgreSQL in the online terminal

Open instance detail → **Terminal** → **Command line**, enter commands, and press Enter. The
Debian archive may lag behind; add the official [PGDG](https://www.postgresql.org/download/linux/debian/)
repository to install the current major version (PostgreSQL 18 in this example):

```sh
apt-get update
apt-get install -y curl ca-certificates gnupg
install -d /usr/share/postgresql-common/pgdg
curl -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc \
  https://www.postgresql.org/media/keys/ACCC4CF8.asc
echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] \
  https://apt.postgresql.org/pub/repos/apt trixie-pgdg main" > /etc/apt/sources.list.d/pgdg.list
apt-get update
apt-get install -y postgresql-18
```

After installation, `psql --version` reports the new version and the install script
initializes the cluster data directory:

![Install PostgreSQL 18](images/pg-install.png)

### Step 3: configure the password and listener

Make the database listen on all addresses and allow the LAN subnet (replace
`198.51.100.0/24` with your subnet), then set a password for the `postgres` account:

```sh
sed -i "s/^#\?listen_addresses.*/listen_addresses = '*'/" \
  /etc/postgresql/18/main/postgresql.conf
echo "host all all 198.51.100.0/24 scram-sha-256" >> \
  /etc/postgresql/18/main/pg_hba.conf
systemctl restart postgresql
```

```sh
su - postgres -c "psql -c \"ALTER USER postgres PASSWORD '<database password>';\""
```

### Step 4: verify password authentication across machines

Inside the terminal, connect over local TCP (`127.0.0.1`) to verify password authentication and
TLS. Debian's default `pg_hba.conf` already allows `host all all 127.0.0.1/32 scram-sha-256`:

```sh
PGPASSWORD='<database password>' psql -h 127.0.0.1 -U postgres -c '\conninfo'
```

`Password Used: true` and `SSL Connection: true` confirm that the password and encryption are
active:

![Password login verification](images/psql-login.png)

Then connect from another instance on the same subnet (or any LAN machine) to verify the LAN
listener and the subnet rule:

```sh
PGPASSWORD='<database password>' psql -h 198.51.100.x -U postgres -c 'select version();'
```

A result such as `PostgreSQL 18.x ...` means the deployment is complete. Continue using the
online terminal for `systemctl status postgresql`, logs, and database creation.

> Note: macvlan instances are isolated from their host at the kernel level. **The host
> itself** cannot reach a macvlan instance on the same parent NIC, but other LAN machines
> (including instances on other hosts) can. Verify from another machine.

## Shared asset library and batch operations

Keeping common content in one place and reusing it is key to reducing repetitive operations.
Fleet provides an **Asset library** for SSH public keys and frequently used scripts, plus
one-click batch execution against many instances.

### Asset library: one place to keep shared content

Open **Asset library** in the left menu:

- **SSH public keys**: stores keys (name + key material, one or more lines; identical content
  is de-duplicated). Storing a key does not deploy it; you decide when to inject it.
- **Scripts**: stores maintenance scripts (name + description + body), such as bootstrap,
  package installation, or tuning scripts, ready to reuse.

### Batch operations: apply to many instances at once

Open **Instances** and select multiple instances (across clusters is fine). A batch toolbar
appears:

| Batch action | Notes |
| --- | --- |
| Batch inject keys | Choose one or more keys from the library and inject them into the target account on each instance; shows success/failure and exit code per instance |
| Batch run script | Choose a script and run it as root on every instance; captures output per instance |
| Batch start / stop / restart | Power operations on the selected instances |

Typical flow: create a batch of instances → select all → batch inject a public key → SSH in
with the matching private key → batch run the bootstrap script. Key injection and scripts run
as root inside the instance, and key/script content travels over stdin, so it never appears in
the process list.

### Other shared capabilities (roadmap)

The same pattern can be extended to instance groups/tags (batch by group), scheduled snapshot
policies, and backup export templates. These are planned.

## Resource monitoring

Fleet includes two layers of resource monitoring with **no agent installed inside instances**.
Data comes directly from the Incus live state API.

### Cluster level: host resources

Every cluster card on the dashboard and the top of the cluster detail page shows host capacity
and live utilization:

- **CPU cores** and architecture;
- **Memory usage**: used/total with a utilization bar;
- Storage, instance counts, and more.

![Dashboard resource overview](images/05-dashboard-resources.png)

![Cluster resource utilization](images/06-cluster-resources.png)

### Instance level: per container/VM monitoring

The instance detail drawer has a **Monitoring** tab that samples every 3 seconds:

- **CPU**: utilization relative to one core (derived from the CPU time delta between
  consecutive samples);
- **Memory**: used/total bar, with peak and swap;
- **Disk**: usage and capacity per mount point;
- **Network**: per-interface receive/transmit rates (delta between samples) and cumulative
  traffic, with rate charts.

![Instance monitoring panel](images/07-metrics-panel.png)

Stopped instances only show the status row. VM metrics depend on a running Incus agent (VMs
created from official cloud images become ready a few seconds after boot).

### Deeper metrics (roadmap)

For guest-internal metrics such as per-process data, disk I/O, or custom collection, the plan
is a lightweight **Rust** guest agent installed through Fleet that reports richer telemetry.
The Incus state API already covers the core metrics needed for routine operations.

## Storage pools and networks

The cluster detail page provides:

- **Storage pools**: view driver and capacity; create, edit, and delete pools; create and
  delete custom volumes under each pool (for device mounts).
- **Networks**: list networks; managed networks can be created, edited, and deleted, while
  physical NICs are read-only (macvlan is defined by the instance device, not as a separate
  network).
- **Profiles**: create, update, and delete configuration templates; select them when creating
  instances.

## FAQ

- **Image upload returns 413 or fails?** The per-image limit is 8 GiB; compress the root
  filesystem first for larger images.
- **Image distribution seems stuck?** Distribution transfers directly between the source and
  target clusters; cross-site transfers depend on bandwidth. Check the target cluster's image
  list for the new image.
- **Console disconnects with 1006?** The container serial console closes with that code on
  exit, which is normal. Prefer Exec for interactive work.
- **File manager cannot delete a folder?** Recursive directory deletion is not supported yet;
  empty the directory and remove it inside the instance.
- **A physical NIC is missing?** The NIC must be unmanaged on the host (not part of an Incus
  managed bridge).
- **Remote desktop will not open or says it is being configured?** Remote desktop only works
  for VMs. The first open configures the SPICE listener and restarts the VM; wait for it to
  boot and open the session again.
- **Which ports must be allowed for remote desktop?** Each VM uses one TCP port, allocated
  automatically in the 5910-5989 range by instance name. Restrict the source subnet with a
  firewall.
- **Maintenance actions report an agent error?** VMs need a running Incus agent to execute
  commands. VMs created from official cloud images become ready a few seconds after boot.
- **Rebuild says the instance must be stopped?** An image upgrade rewrites the root disk, and
  Incus requires the instance to be stopped. Stop it and retry.
- **Does one failed batch key injection affect the others?** No. Every instance runs
  independently and reports its own result; failed instances can be retried directly.
- **What happens when the same public key is added twice?** Identical key material is stored
  once (de-duplicated by content).
- **The host cannot ping a macvlan instance?** This is a kernel restriction of macvlan:
  the host cannot talk directly to a macvlan instance on the same parent NIC, while other LAN
  machines can. Use a bridge network if the host must reach the instance, or verify from
  another machine.
- **The monitoring tab has no data?** The instance must be running, and VMs need the Incus
  agent (ready a few seconds after boot). Rates are deltas between samples, so the first few
  seconds show —.

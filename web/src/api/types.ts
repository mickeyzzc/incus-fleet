/** Shared API types mirroring the Go JSON responses. */

export type InstanceType = 'any' | 'container' | 'virtual-machine'

export type MigrationMode = 'move' | 'copy'

export type MigrationStatus =
  | 'pending'
  | 'preflight'
  | 'stopping'
  | 'transferring'
  | 'starting'
  | 'verifying'
  | 'done'
  | 'failed'

export interface ClusterMember {
  name: string
  url?: string
  status: string
  message?: string
  database: boolean
  roles: string[]
  architecture?: string
  description?: string
  failure_domain?: string
  groups?: string[]
}

export interface InstanceCounts {
  total: number
  containers: number
  virtual_machines: number
  running: number
}

export interface StorageUsage {
  pool?: string
  used: number
  total: number
}

export interface ClusterSummary {
  name: string
  display_name: string
  endpoint: string
  online: boolean
  error?: string
  server_name?: string
  server_version?: string
  clustered: boolean
  architectures?: string[]
  members: ClusterMember[]
  instances: InstanceCounts
  storage: StorageUsage
  networks: string[]
}

export interface InstanceSummary {
  name: string
  cluster: string
  type: string
  status: string
  status_code: number
  location?: string
  project?: string
  architecture?: string
  description?: string
  profiles: string[]
  ipv4: string[]
  ipv6: string[]
  memory: number
  processes: number
  created_at: string
  started_at?: string
  storage_pool?: string
  migration_stateful: boolean
  live_migratable: boolean
}

export interface InstanceNetworkAddress {
  family: string
  address: string
  netmask?: string
  scope?: string
}

export interface InstanceNetwork {
  addresses?: InstanceNetworkAddress[]
  counters?: Record<string, number>
  hwaddr?: string
  host_name?: string
  mtu?: number
  state?: string
  type?: string
}

export interface InstanceState {
  status: string
  status_code: number
  memory?: { usage?: number; usage_peak?: number; total?: number }
  processes?: number
  started_at?: string
  network?: Record<string, InstanceNetwork>
  os_info?: {
    os?: string
    os_version?: string
    kernel_version?: string
    hostname?: string
    fqdn?: string
  }
}

export interface InstanceDetail extends InstanceSummary {
  os?: string
  os_version?: string
  kernel_version?: string
  hostname?: string
  ephemeral: boolean
  config: Record<string, string>
  devices: Record<string, Record<string, string>>
  state?: InstanceState
}

/** Local Incus image available on a cluster. */
export interface InstanceImage {
  fingerprint: string
  aliases: string[]
  type: string
  architecture: string
  os?: string
  release?: string
  description?: string
  size: number
  cached: boolean
  created_at: string
}

/** Storage pool details reported by a cluster. */
export interface StoragePoolResource {
  name: string
  driver: string
  source?: string
  status?: string
  description?: string
  config?: Record<string, string>
  used_by: string[]
  locations: string[]
  total: number
  used: number
}

/** Network details reported by a cluster. */
export interface NetworkResource {
  name: string
  type: string
  managed: boolean
  status?: string
  description?: string
  ipv4?: string
  ipv6?: string
  config?: Record<string, string>
  used_by: string[]
  locations: string[]
}

/** Storage pools and networks for one cluster. */
export interface ClusterResources {
  storage_pools: StoragePoolResource[]
  networks: NetworkResource[]
}

/** Request used to create or update a storage pool. */
export interface StoragePoolRequest {
  name?: string
  driver?: string
  description?: string
  config?: Record<string, string>
}

/** Request used to create or update a managed network. */
export interface NetworkRequest {
  name?: string
  type?: string
  description?: string
  config?: Record<string, string>
}

/** One instance profile. */
export interface Profile {
  name: string
  description?: string
  config: Record<string, string>
  devices: Record<string, Record<string, string>>
  used_by: string[]
}

/** Request used to create or update a profile. */
export interface ProfileRequest {
  name?: string
  description?: string
  config?: Record<string, string>
  devices?: Record<string, Record<string, string>>
}

/** Non-destructive cluster member metadata changes. */
export interface MemberUpdateRequest {
  description?: string
  failure_domain?: string
  groups?: string[]
}

/** Request used to create an instance from a local image. */
export interface CreateInstanceRequest {
  name: string
  type?: 'container' | 'virtual-machine'
  image: string
  description?: string
  profiles?: string[]
  cpu?: string
  memory?: string
  root_disk_size?: string
  storage_pool?: string
  network?: string
  ipv4?: string
  ipv6?: string
  start?: boolean
}

/** Request used to change instance limits, disk size, or networking. */
export interface UpdateInstanceRequest {
  description?: string
  cpu?: string
  memory?: string
  root_disk_size?: string
  device?: string
  network?: string
  ipv4?: string
  ipv6?: string
}

export interface ImportImageRequest {
  file: File
  rootfs?: File
  type: 'container' | 'virtual-machine'
  alias?: string
}

export interface CopyImageRequest {
  target_cluster: string
  alias?: string
}

export interface ISOSummary {
  name: string
  pool: string
  description?: string
  size?: string
}

export interface ImportISORequest {
  file: File
  pool?: string
}

export interface LaunchISORequest {
  name: string
  storage_pool?: string
  network?: string
  root_disk?: string
  start?: boolean
}

export interface OpsRequest {
  action: 'run-script' | 'set-password' | 'inject-key'
  script?: string
  user?: string
  password?: string
  key?: string
}

export interface OpsResult {
  exit_code: number
  stdout: string
  stderr: string
}

export interface SPICEPrepare {
  port: number
  applied: boolean
  restart_required: boolean
}

export interface CloneRequest {
  target: string
  start?: boolean
}

export type InstanceAction = 'start' | 'stop' | 'restart'

export interface InstanceActionRequest {
  action: InstanceAction
  force?: boolean
}

/** One instance snapshot. */
export interface InstanceSnapshot {
  name: string
  created_at?: string
  size: number
  stateful: boolean
}

/** Request used to create an instance snapshot. */
export interface SnapshotRequest {
  name: string
  stateful?: boolean
}

/** One custom storage volume. */
export interface StorageVolume {
  name: string
  type: string
  content_type?: string
  description?: string
  config: Record<string, string>
  used_by: string[]
  location?: string
}

/** Request used to create a custom storage volume. */
export interface VolumeRequest {
  name: string
  description?: string
  size?: string
  config?: Record<string, string>
}

/** Request used to attach one disk or NIC device to an instance. */
export interface DeviceRequest {
  name: string
  type: 'disk' | 'nic' | 'proxy'
  pool?: string
  source?: string
  path?: string
  size?: string
  network?: string
  ipv4?: string
  ipv6?: string
  listen?: string
  connect?: string
  boot?: string
}

/** One file or directory inside an instance. */
export interface FileEntry {
  name: string
  size: number
  mode: number
  is_dir: boolean
  modtime: string
}

export interface Migration {
  id: string
  source_cluster: string
  target_cluster: string
  instance: string
  instance_type: string
  mode: MigrationMode
  live: boolean
  status: MigrationStatus
  progress: string
  error: string
  created_at: string
  updated_at: string
}

export interface MigrationRequest {
  source_cluster: string
  target_cluster: string
  instance: string
  mode: MigrationMode
  live: boolean
  include_snapshots: boolean
  auto_start?: boolean
}

export interface SessionResponse {
  authenticated: boolean
  expires_at?: string
}

export interface VersionResponse {
  version: string
}

export interface HealthResponse {
  status: string
  version: string
  time: string
}

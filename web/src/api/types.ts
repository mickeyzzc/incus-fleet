/** Shared API types mirroring the Go JSON responses. */

/** Fleet-wide inventory result, including clusters that could not be reached. */
export interface InventoryResult<T> {
  items: T[]
  failedClusters: string[]
}

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
  | 'canceled'

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
  cpu?: ClusterCPU
  memory?: ClusterMemory
}

/** Processor capacity reported for the cluster member. */
export interface ClusterCPU {
  total: number
  architecture?: string
}

/** Memory usage on the cluster member, in bytes. */
export interface ClusterMemory {
  used: number
  total: number
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

/** One member's participation in a cluster private overlay network. */
export interface OverlayMemberStatus {
  member: string
  underlay_ip?: string
  bridge_ip?: string
  links: number
  gateway: boolean
  error?: string
}

/** A fleet-managed private network that spans every cluster member. */
export interface OverlayStatus {
  cluster: string
  name: string
  bridge: string
  vni: number
  subnet: string
  gateway_member: string
  mtu: number
  dhcp_start?: string
  dhcp_end?: string
  enabled: boolean
  members: OverlayMemberStatus[]
  tunnels: string[][]
  steps?: string[]
}

/** Request used to create or update a private overlay network. */
export interface OverlayRequest {
  name?: string
  bridge?: string
  vni?: number
  subnet?: string
  gateway_member?: string
  mtu?: number
  dhcp_start?: string
  dhcp_end?: string
  underlay_nic?: string
  underlay_ips?: Record<string, string>
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
export interface CreateInstanceDisk {
  name: string
  path: string
  size?: string
  pool?: string
}

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
  /** Pin creation to the member that owns a physical NIC. */
  member?: string
  start?: boolean
  /** Additional empty disk volumes created with the instance. */
  disks?: CreateInstanceDisk[]
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
  /** Set when listed across all clusters by the assets page. */
  cluster?: string
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

/** Installer families supported by the unattended install pipeline. */
export type UnattendedProfile = 'debian-preseed' | 'ubuntu-autoinstall' | 'kickstart' | 'nocloud'

export interface UnattendedAnswerSpec {
  hostname?: string
  username?: string
  password?: string
  timezone?: string
  disk?: string
  locale?: string
  keyboard?: string
  /** Extra packages the installer should add to the base system. */
  packages?: string
}

export interface UnattendedLaunchRequest {
  name: string
  profile: UnattendedProfile
  /** Overrides the built-in template with operator-provided content. */
  answer_file?: string
  /** Selects a stored answer profile; the server resolves it when set. */
  answer_profile_id?: string
  spec?: UnattendedAnswerSpec
  network?: string
  root_disk?: string
  start?: boolean
}

/** A reusable unattended-install answer configuration. */
export interface AnswerProfile {
  id: string
  name: string
  profile: UnattendedProfile
  hostname?: string
  username?: string
  password?: string
  timezone?: string
  disk?: string
  locale?: string
  keyboard?: string
  packages?: string
  answer_file?: string
  is_default: boolean
  created_at: string
  updated_at: string
}

export interface UnattendedMedia {
  device: string
  volume: string
  pool: string
  seed: boolean
}

export interface UnattendedLaunchResult {
  name: string
  media: UnattendedMedia[]
  profile: UnattendedProfile
  answer_file_name: string
}

export interface UnattendedAnswerPreview {
  content: string
  file_name: string
  profile: UnattendedProfile
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

export interface Keystone {
  id: string
  name: string
  public_key: string
  comment?: string
  created_at: string
}

export interface StoredScript {
  id: string
  name: string
  description?: string
  body: string
  created_at: string
  updated_at: string
}

export interface BatchTarget {
  cluster: string
  instance: string
  user?: string
}

export interface BatchItem {
  cluster: string
  instance: string
  ok: boolean
  exit_code?: number
  stdout?: string
  stderr?: string
  error?: string
}

export interface BatchKeysRequest {
  targets: BatchTarget[]
  key_ids: string[]
}

/** One image as seen across the whole fleet. */
export interface ClusterImage {
  cluster: string
  fingerprint: string
  alias: string
  type: string
  os?: string
  release?: string
  description?: string
  size: number
  cached: boolean
  created_at: string
}

export interface BatchScriptsRequest {
  targets: BatchTarget[]
  script_id: string
}

export interface BatchPowerRequest {
  targets: BatchTarget[]
  action: 'start' | 'stop' | 'restart'
  force?: boolean
}

export interface SPICEPrepare {
  port: number
  applied: boolean
  restart_required: boolean
}

/** Result of arming the QMP graphical console for one VM. */
export interface DesktopPrepare {
  ready: boolean
  restarted: boolean
}

/** One keyboard or absolute-pointer event for the graphical console. */
export interface DesktopInputEvent {
  type: 'key' | 'pointer'
  key?: string
  modifiers?: string[]
  repeat?: number
  x?: number
  y?: number
  button?: string
  down?: boolean
  move?: boolean
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

/** Cumulative CPU time of an instance, in nanoseconds. */
export interface MetricsCPU {
  usage: number
  allocated_time: number
}

/** Memory usage of an instance, in bytes. */
export interface MetricsMemory {
  usage: number
  usage_peak: number
  total: number
  swap_usage: number
  swap_usage_peak: number
}

/** One mounted filesystem inside an instance. */
export interface MetricsDisk {
  mountpoint: string
  usage: number
  total: number
}

/** One network interface with cumulative traffic counters. */
export interface MetricsNet {
  interface: string
  state: string
  type: string
  rx_bytes: number
  rx_packets: number
  rx_errors: number
  rx_dropped: number
  tx_bytes: number
  tx_packets: number
  tx_errors: number
  tx_dropped: number
}

/** Point-in-time resource snapshot of one instance. */
export interface InstanceMetrics {
  name: string
  status: string
  pid: number
  processes: number
  cpu: MetricsCPU
  memory: MetricsMemory
  disk: MetricsDisk[]
  network: MetricsNet[]
  at: string
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

/** One custom volume with the cluster and pool it lives in. */
export interface FleetVolume extends StorageVolume {
  cluster: string
  pool: string
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

/** One stage of the in-guest incus-agent install. */
export interface GuestAgentStep {
  name: string
  status: 'ok' | 'failed' | 'warn'
  detail?: string
}

/** Request to install the official incus-agent into a VM over SSH. */
export interface GuestAgentInstallRequest {
  address?: string
  ssh_port?: number
  ssh_user: string
  ssh_password?: string
  sudo_password?: string
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
  username?: string
  role?: UserRole
}

/** A server-side browser session as returned by /auth/sessions. */
export interface SessionInfo {
  id: string
  username: string
  role: UserRole
  expires_at: string
  current: boolean
}

/** Permission level of a signed-in account. */
export type UserRole = 'admin' | 'operator' | 'viewer'

/** One local account. Password material never leaves the server. */
export interface User {
  id: string
  username: string
  role: UserRole
  disabled: boolean
  must_change_password: boolean
  created_at: string
  updated_at: string
}

export interface CreateUserRequest {
  username: string
  password: string
  role: UserRole
  disabled?: boolean
  must_change_password?: boolean
}

export interface UpdateUserRequest {
  username?: string
  password?: string
  role?: UserRole
  disabled?: boolean
  must_change_password?: boolean
}

export interface VersionResponse {
  version: string
}

export interface HealthResponse {
  status: string
  version: string
  time: string
}

/** One NIC of the node fleet runs on. */
export interface NodeAddress {
  family: string
  address: string
  prefixlen: number
}

export interface NodeWiFiLink {
  connected: boolean
  ssid?: string
  signal?: number
  frequency?: number
}

export interface NodeNIC {
  name: string
  state: string
  kind: string
  mac?: string
  mtu?: number
  addresses: NodeAddress[]
  wifi?: NodeWiFiLink
}

export interface NodeRoute {
  destination: string
  gateway?: string
  device?: string
  metric?: string
}

export interface NodeFirewallRule {
  handle: string
  rule: string
}

export interface NodeWiFiNetwork {
  ssid: string
  signal: number
  frequency: number
  secure: boolean
}

export interface NodeSnapshot {
  host: string
  nics: NodeNIC[]
  routes: NodeRoute[]
  firewall: NodeFirewallRule[]
  wifi_scan?: NodeWiFiNetwork[]
  wifi_device?: string
  capabilities: string[]
  backend?: string
}

export interface NodeTask {
  id: string
  cluster: string
  member: string
  type: string
  state: string
  progress?: string
  result?: string
  error?: string
  plan?: string
  created_at: string
  started_at?: string
  finished_at?: string
}

export interface NodeTaskEvent {
  id: string
  task_id: string
  at: string
  state: string
  message?: string
}

export interface NodeAudit {
  id: string
  at: string
  actor?: string
  source_ip?: string
  backend: string
  cluster: string
  member: string
  action: string
  task_id?: string
  result: string
  detail?: string
}

export interface NodeAddressRequest {
  dhcp: boolean
  address?: string
  gateway?: string
  persist: boolean
}

export interface NodeFirewallRequest {
  port: number
  proto: 'tcp' | 'udp'
  action: 'accept' | 'drop'
  comment?: string
}

/** Provisions a new host and joins it to a cluster. */
export interface ClusterMemberJoinRequest {
  name?: string
  address: string
  ssh_user?: string
  ssh_port?: number
  ssh_password?: string
  sudo_password?: string
  install_incus: boolean
  install_vms: boolean
  ignore_host_key: boolean
  storage_source?: string
  incus_repo?: string
  incus_repo_key?: string
}

export interface ClusterMemberJoinStep {
  name: string
  status: string
  detail?: string
}

export interface ClusterMemberJoinResult {
  member: ClusterMember
  steps: ClusterMemberJoinStep[]
}

export interface AgentSummary {
  id: string
  cluster: string
  member: string
  hostname?: string
  status: string
  version?: string
  last_seen?: string
  cert_not_after?: string
}

export interface AgentEnrollmentToken {
  token: string
  cluster: string
  member: string
  expires_at: string
}

export interface AgentBootstrapRequest {
  mode: 'join' | 'init'
  address: string
  storage_source?: string
  network_cidr?: string
  install_mode?: 'existing' | 'package'
  version_pin?: string
  packages?: string[]
}

export interface AgentBootstrapResult {
  ok: boolean
  task_id: string
  mode: string
  message: string
  steps: string[]
}

export interface ClusterRegisterRequest {
  name: string
  display_name?: string
  endpoint: string
  project?: string
  tls_cert: string
  tls_key: string
  tls_server_cert?: string
}

export interface TrafficInstance {
  name: string
  location?: string
  rx_bytes: number
  tx_bytes: number
  rx_packets: number
  tx_packets: number
}

export interface ClusterTraffic {
  at: string
  total_rx_bytes: number
  total_tx_bytes: number
  total_rx_packets: number
  total_tx_packets: number
  instances: TrafficInstance[]
}

export interface TelemetryPoint {
  at: string
  value: number
}

export interface TelemetrySeries {
  name: string
  labels: Record<string, string>
  type: string
  points: TelemetryPoint[]
}

export interface PrometheusMatrixResult {
  metric: Record<string, string>
  values: Array<[number, string]>
}

export interface PrometheusQueryResponse {
  status: string
  data?: {
    resultType: string
    result: PrometheusMatrixResult[]
  }
  error?: string
}

/** One plugin registered with fleet. */
export interface PluginEntry {
  id: string
  name: string
  version: string
  enabled: boolean
  upstream?: string
  role: string
  kind: string
  health_path?: string
  ui: PluginUI
}

export interface PluginUI {
  path?: string
  icon?: string
  title_key?: string
  requires_role?: string
}

export interface PluginListResponse {
  plugins: PluginEntry[] | null
  enabled: boolean
}

/** One issued API token. The secret is only present in the creation response. */
export interface APIToken {
  id: string
  name: string
  user_id: string
  username?: string
  scopes: string[]
  created_at: string
  expires_at?: string
  last_used_at?: string
  plugin_id?: string
  instance_id?: string
}

export interface CreatedAPIToken extends APIToken {
  token: string
}

export interface TokenScopesResponse {
  scopes: string[]
  enabled: boolean
}

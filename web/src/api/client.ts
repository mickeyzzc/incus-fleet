import type {
  BatchItem,
  ClusterMemberJoinRequest,
  ClusterMemberJoinResult,
  ClusterRegisterRequest,
  ClusterTraffic,
  NodeAddressRequest,
  AgentBootstrapRequest,
  AgentBootstrapResult,
  AgentEnrollmentToken,
  AgentSummary,
  NodeAudit,
  NodeFirewallRequest,
  NodeSnapshot,
  NodeTask,
  NodeTaskEvent,
  TelemetrySeries,
  PrometheusQueryResponse,
  BatchKeysRequest,
  BatchPowerRequest,
  BatchScriptsRequest,
  ClusterImage,
  ClusterResources,
  ClusterSummary,
  CloneRequest,
  CreateInstanceRequest,
  DeviceRequest,
  GuestAgentInstallRequest,
  GuestAgentStep,
  FileEntry,
  HealthResponse,
  ImportImageRequest,
  ImportISORequest,
  CopyImageRequest,
  InstanceActionRequest,
  InstanceDetail,
  InstanceImage,
  InstanceMetrics,
  InstanceSnapshot,
  InstanceSummary,
  InstanceType,
  ISOSummary,
  LaunchISORequest,
  MemberUpdateRequest,
  Migration,
  MigrationRequest,
  NetworkRequest,
  OpsRequest,
  OpsResult,
  SPICEPrepare,
  UnattendedAnswerPreview,
  UnattendedLaunchRequest,
  UnattendedLaunchResult,
  DesktopPrepare,
  DesktopInputEvent,
  Profile,
  ProfileRequest,
  Keystone,
  StoredScript,
  SessionResponse,
  User,
  CreateUserRequest,
  UpdateUserRequest,
  SnapshotRequest,
  StoragePoolRequest,
  StorageVolume,
  FleetVolume,
  UpdateInstanceRequest,
  VersionResponse,
  VolumeRequest,
} from './types'

/** Error thrown for non-successful API responses. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

export interface ApiClientOptions {
  baseURL?: string
  fetchImpl?: typeof fetch
  onUnauthorized?: () => void
}

interface ErrorEnvelope {
  error?: {
    code?: string
    message?: string
  }
}

const jsonHeaders = { 'Content-Type': 'application/json' }

/**
 * Typed client for the incus-fleet HTTP API.
 *
 * Authentication uses an HttpOnly session cookie created by `login`. A bearer
 * token may also be supplied manually for automation.
 */
export class ApiClient {
  private readonly baseURL: string
  private readonly fetchImpl: typeof fetch
  private bearerToken = ''
  onUnauthorized?: () => void

  constructor(options: ApiClientOptions = {}) {
    this.baseURL = (options.baseURL ?? '').replace(/\/$/, '')
    this.fetchImpl = options.fetchImpl ?? globalThis.fetch.bind(globalThis)
    this.onUnauthorized = options.onUnauthorized
  }

  /** Sets a bearer token used instead of the session cookie. */
  setBearerToken(token: string): void {
    this.bearerToken = token
  }

  async login(username: string, password: string): Promise<SessionResponse> {
    return this.request<SessionResponse>('POST', '/api/v1/auth/login', { username, password })
  }

  /** Exchanges the shared bootstrap token for a session. */
  async loginWithToken(token: string): Promise<SessionResponse> {
    return this.request<SessionResponse>('POST', '/api/v1/auth/login', { token })
  }

  async changePassword(currentPassword: string, newPassword: string): Promise<void> {
    await this.request<void>('POST', '/api/v1/auth/password', {
      current_password: currentPassword,
      new_password: newPassword,
    })
  }

  async listUsers(): Promise<User[]> {
    const data = await this.request<{ users: User[] }>('GET', '/api/v1/users')
    return data.users ?? []
  }

  async createUser(request: CreateUserRequest): Promise<User> {
    return this.request<User>('POST', '/api/v1/users', request)
  }

  async updateUser(id: string, request: UpdateUserRequest): Promise<User> {
    return this.request<User>('PATCH', `/api/v1/users/${encodeURIComponent(id)}`, request)
  }

  async deleteUser(id: string): Promise<void> {
    await this.request<void>('DELETE', `/api/v1/users/${encodeURIComponent(id)}`)
  }

  async logout(): Promise<void> {
    await this.request<void>('POST', '/api/v1/auth/logout')
  }

  async session(): Promise<SessionResponse> {
    return this.request<SessionResponse>('GET', '/api/v1/auth/session')
  }

  async health(): Promise<HealthResponse> {
    return this.request<HealthResponse>('GET', '/api/v1/healthz')
  }

  async version(): Promise<VersionResponse> {
    return this.request<VersionResponse>('GET', '/api/v1/version')
  }

  async listClusters(): Promise<ClusterSummary[]> {
    const data = await this.request<{ clusters: ClusterSummary[] }>('GET', '/api/v1/clusters')
    return data.clusters ?? []
  }

  async getCluster(name: string): Promise<ClusterSummary> {
    return this.request<ClusterSummary>('GET', `/api/v1/clusters/${encodeURIComponent(name)}`)
  }

  async reconnectCluster(name: string): Promise<ClusterSummary> {
    return this.request<ClusterSummary>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(name)}/reconnect`,
    )
  }

  async listClusterInstances(
    cluster: string,
    type: InstanceType = 'any',
  ): Promise<InstanceSummary[]> {
    const data = await this.request<{ instances: InstanceSummary[] }>(
      'GET',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances?type=${encodeURIComponent(type)}`,
    )
    return data.instances ?? []
  }

  async listInstances(type: InstanceType = 'any'): Promise<InstanceSummary[]> {
    const data = await this.request<{ instances: InstanceSummary[] }>(
      'GET',
      `/api/v1/instances?type=${encodeURIComponent(type)}`,
    )
    return data.instances ?? []
  }

  async getInstance(cluster: string, name: string): Promise<InstanceDetail> {
    return this.request<InstanceDetail>(
      'GET',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(name)}`,
    )
  }

  async listImages(cluster: string): Promise<InstanceImage[]> {
    const data = await this.request<{ images: InstanceImage[] }>(
      'GET',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/images`,
    )
    return data.images ?? []
  }

  async importImage(
    cluster: string,
    request: ImportImageRequest,
  ): Promise<string | null> {
    const query = new URLSearchParams({ type: request.type })
    if (request.alias) {
      query.set('alias', request.alias)
    }

    const form = new FormData()
    form.append('image', request.file, request.file.name)
    if (request.rootfs) {
      form.append('rootfs', request.rootfs, request.rootfs.name)
    }

    const data = await this.request<{ fingerprint?: string } | undefined>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/images/import?${query.toString()}`,
      form,
    )

    return data?.fingerprint ?? null
  }

  async copyImage(cluster: string, fingerprint: string, request: CopyImageRequest): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/images/${encodeURIComponent(fingerprint)}/copy`,
      request,
    )
  }

  async refreshImage(cluster: string, fingerprint: string): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/images/${encodeURIComponent(fingerprint)}/refresh`,
    )
  }

  async listISOs(cluster: string): Promise<ISOSummary[]> {
    const data = await this.request<{ isos: ISOSummary[] }>(
      'GET',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/iso`,
    )
    return data.isos ?? []
  }

  async importISO(cluster: string, request: ImportISORequest): Promise<{ name: string; pool: string }> {
    const query = new URLSearchParams()
    if (request.pool) {
      query.set('pool', request.pool)
    }

    const form = new FormData()
    form.append('image', request.file, request.file.name)

    return this.request<{ name: string; pool: string }>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/iso?${query.toString()}`,
      form,
    )
  }

  async deleteISO(cluster: string, pool: string, name: string): Promise<void> {
    await this.request<void>(
      'DELETE',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/iso/${encodeURIComponent(pool)}/${encodeURIComponent(name)}`,
    )
  }

  async launchFromISO(cluster: string, pool: string, name: string, request: LaunchISORequest): Promise<InstanceSummary> {
    return this.request<InstanceSummary>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/iso/${encodeURIComponent(pool)}/${encodeURIComponent(name)}/launch`,
      request,
    )
  }

  async previewUnattendedAnswer(
    cluster: string,
    pool: string,
    name: string,
    request: UnattendedLaunchRequest,
  ): Promise<UnattendedAnswerPreview> {
    return this.request<UnattendedAnswerPreview>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/iso/${encodeURIComponent(pool)}/${encodeURIComponent(name)}/launch-unattended/preview`,
      request,
    )
  }

  async launchUnattended(
    cluster: string,
    pool: string,
    name: string,
    request: UnattendedLaunchRequest,
  ): Promise<{ instance: InstanceSummary; launch: UnattendedLaunchResult }> {
    return this.request<{ instance: InstanceSummary; launch: UnattendedLaunchResult }>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/iso/${encodeURIComponent(pool)}/${encodeURIComponent(name)}/launch-unattended`,
      request,
    )
  }

  async runOps(cluster: string, instance: string, request: OpsRequest): Promise<OpsResult> {
    return this.request<OpsResult>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/ops`,
      request,
    )
  }

  async rebuildInstance(cluster: string, instance: string, image: string): Promise<InstanceDetail> {
    return this.request<InstanceDetail>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/rebuild`,
      { image },
    )
  }

  async cloneInstance(cluster: string, instance: string, request: CloneRequest): Promise<InstanceSummary> {
    return this.request<InstanceSummary>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/clone`,
      request,
    )
  }

  async listKeys(): Promise<Keystone[]> {
    const data = await this.request<{ keys: Keystone[] }>('GET', '/api/v1/keys')
    return data.keys ?? []
  }

  async createKey(request: { name: string; public_key: string; comment?: string }): Promise<Keystone> {
    return this.request<Keystone>('POST', '/api/v1/keys', request)
  }

  async updateKey(id: string, request: { name: string; public_key: string; comment?: string }): Promise<Keystone> {
    return this.request<Keystone>('PATCH', `/api/v1/keys/${encodeURIComponent(id)}`, request)
  }

  async deleteKey(id: string): Promise<void> {
    await this.request<void>('DELETE', `/api/v1/keys/${encodeURIComponent(id)}`)
  }

  async listScripts(): Promise<StoredScript[]> {
    const data = await this.request<{ scripts: StoredScript[] }>('GET', '/api/v1/scripts')
    return data.scripts ?? []
  }

  async createScript(request: { name: string; description?: string; body: string }): Promise<StoredScript> {
    return this.request<StoredScript>('POST', '/api/v1/scripts', request)
  }

  async updateScript(id: string, request: { name: string; description?: string; body: string }): Promise<StoredScript> {
    return this.request<StoredScript>('PATCH', `/api/v1/scripts/${encodeURIComponent(id)}`, request)
  }

  async deleteScript(id: string): Promise<void> {
    await this.request<void>('DELETE', `/api/v1/scripts/${encodeURIComponent(id)}`)
  }

  async batchKeys(request: BatchKeysRequest): Promise<BatchItem[]> {
    const data = await this.request<{ items: BatchItem[] }>('POST', '/api/v1/batch/keys', request)
    return data.items ?? []
  }

  async batchScripts(request: BatchScriptsRequest): Promise<BatchItem[]> {
    const data = await this.request<{ items: BatchItem[] }>('POST', '/api/v1/batch/scripts', request)
    return data.items ?? []
  }

  async batchPower(request: BatchPowerRequest): Promise<BatchItem[]> {
    const data = await this.request<{ items: BatchItem[] }>('POST', '/api/v1/batch/power', request)
    return data.items ?? []
  }

  async prepareSPICE(cluster: string, instance: string, port?: number): Promise<SPICEPrepare> {
    return this.request<SPICEPrepare>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/spice/prepare`,
      port ? { port, apply: true } : { apply: true },
    )
  }

  spiceURL(cluster: string, instance: string, port: number): string {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    return `${proto}://${window.location.host}/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/spice?port=${port}`
  }

  vgaURL(cluster: string, instance: string): string {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    return `${proto}://${window.location.host}/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/vga`
  }

  /** Arms the QMP graphical console for one VM (installs the monitor socket). */
  async prepareDesktop(cluster: string, instance: string): Promise<DesktopPrepare> {
    return this.request<DesktopPrepare>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/desktop/prepare`,
    )
  }

  /** Fetches the current framebuffer of a VM as a PNG blob. */
  async desktopScreenshot(cluster: string, instance: string): Promise<Blob> {
    const headers: Record<string, string> = { Accept: 'image/png' }
    if (this.bearerToken) {
      headers.Authorization = `Bearer ${this.bearerToken}`
    }

    const response = await this.fetchImpl(
      `${this.baseURL}/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/desktop/screenshot`,
      { headers, credentials: 'same-origin' },
    )
    if (!response.ok) {
      if (response.status === 401) {
        this.onUnauthorized?.()
      }

      throw await this.toError(response)
    }

    return await response.blob()
  }

  /** Injects one keyboard or pointer event into a VM's graphical console. */
  async sendDesktopInput(cluster: string, instance: string, event: DesktopInputEvent): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/desktop/input`,
      event,
    )
  }

  async deleteImage(cluster: string, fingerprint: string): Promise<void> {
    await this.request<void>(
      'DELETE',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/images/${encodeURIComponent(fingerprint)}`,
    )
  }

  async listSnapshots(cluster: string, instance: string): Promise<InstanceSnapshot[]> {
    const data = await this.request<{ snapshots: InstanceSnapshot[] }>(
      'GET',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/snapshots`,
    )
    return data.snapshots ?? []
  }

  async createSnapshot(cluster: string, instance: string, request: SnapshotRequest): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/snapshots`,
      request,
    )
  }

  async restoreSnapshot(cluster: string, instance: string, snapshot: string): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/snapshots/${encodeURIComponent(snapshot)}/restore`,
    )
  }

  async deleteSnapshot(cluster: string, instance: string, snapshot: string): Promise<void> {
    await this.request<void>(
      'DELETE',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/snapshots/${encodeURIComponent(snapshot)}`,
    )
  }

  /**
   * Lists every custom volume across all clusters, tagged with its cluster and
   * pool. Pools that fail to answer are skipped so one unreachable cluster does
   * not blank the whole fleet view.
   */
  async listAllVolumes(): Promise<FleetVolume[]> {
    const clusters = await this.listClusters()
    const perCluster = await Promise.all(
      clusters.map(async (cluster) => {
        try {
          const resources = await this.getResources(cluster.name)
          const pools = resources.storage_pools ?? []
          const perPool = await Promise.all(
            pools.map(async (pool) => {
              try {
                const volumes = await this.listVolumes(cluster.name, pool.name)
                return volumes.map((volume) => ({
                  ...volume,
                  cluster: cluster.name,
                  pool: pool.name,
                }))
              } catch {
                return []
              }
            }),
          )
          return perPool.flat()
        } catch {
          return []
        }
      }),
    )

    return perCluster.flat()
  }

  async listVolumes(cluster: string, pool: string): Promise<StorageVolume[]> {
    const data = await this.request<{ volumes: StorageVolume[] }>(
      'GET',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/storage-pools/${encodeURIComponent(pool)}/volumes`,
    )
    return data.volumes ?? []
  }

  async createVolume(cluster: string, pool: string, request: VolumeRequest): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/storage-pools/${encodeURIComponent(pool)}/volumes`,
      request,
    )
  }

  async deleteVolume(cluster: string, pool: string, name: string): Promise<void> {
    await this.request<void>(
      'DELETE',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/storage-pools/${encodeURIComponent(pool)}/volumes/${encodeURIComponent(name)}`,
    )
  }

  async attachDevice(
    cluster: string,
    instance: string,
    request: DeviceRequest,
  ): Promise<InstanceDetail> {
    return this.request<InstanceDetail>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/devices`,
      request,
    )
  }

  async detachDevice(cluster: string, instance: string, device: string): Promise<InstanceDetail> {
    return this.request<InstanceDetail>(
      'DELETE',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/devices/${encodeURIComponent(device)}`,
    )
  }

  async updateDevice(
    cluster: string,
    instance: string,
    device: string,
    request: DeviceRequest,
  ): Promise<InstanceDetail> {
    return this.request<InstanceDetail>(
      'PATCH',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/devices/${encodeURIComponent(device)}`,
      request,
    )
  }

  /** Installs the official incus-agent into a virtual machine over SSH. */
  async installGuestAgent(
    cluster: string,
    instance: string,
    request: GuestAgentInstallRequest,
  ): Promise<{ steps: GuestAgentStep[] }> {
    return this.request<{ steps: GuestAgentStep[] }>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/agent/install`,
      request,
    )
  }

  async getResources(cluster: string): Promise<ClusterResources> {
    return this.request<ClusterResources>(
      'GET',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/resources`,
    )
  }

  async createInstance(
    cluster: string,
    request: CreateInstanceRequest,
  ): Promise<InstanceDetail> {
    return this.request<InstanceDetail>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances`,
      request,
    )
  }

  async updateInstance(
    cluster: string,
    name: string,
    request: UpdateInstanceRequest,
  ): Promise<InstanceDetail> {
    return this.request<InstanceDetail>(
      'PATCH',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(name)}`,
      request,
    )
  }

  async instanceAction(
    cluster: string,
    name: string,
    request: InstanceActionRequest,
  ): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(name)}/state`,
      request,
    )
  }

  /** Reads a live resource snapshot of one instance. */
  async instanceMetrics(cluster: string, name: string): Promise<InstanceMetrics> {
    const data = await this.request<{ metrics: InstanceMetrics }>(
      'GET',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(name)}/metrics`,
    )

    return data.metrics
  }

  async deleteInstance(cluster: string, name: string, force = false): Promise<void> {
    const query = force ? '?force=true' : ''
    await this.request<void>(
      'DELETE',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(name)}${query}`,
    )
  }

  async createStoragePool(cluster: string, request: StoragePoolRequest): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/storage-pools`,
      request,
    )
  }

  async updateStoragePool(
    cluster: string,
    name: string,
    request: StoragePoolRequest,
  ): Promise<void> {
    await this.request<void>(
      'PATCH',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/storage-pools/${encodeURIComponent(name)}`,
      request,
    )
  }

  async deleteStoragePool(cluster: string, name: string): Promise<void> {
    await this.request<void>(
      'DELETE',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/storage-pools/${encodeURIComponent(name)}`,
    )
  }

  async createNetwork(cluster: string, request: NetworkRequest): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/networks`,
      request,
    )
  }

  async updateNetwork(cluster: string, name: string, request: NetworkRequest): Promise<void> {
    await this.request<void>(
      'PATCH',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/networks/${encodeURIComponent(name)}`,
      request,
    )
  }

  async deleteNetwork(cluster: string, name: string): Promise<void> {
    await this.request<void>(
      'DELETE',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/networks/${encodeURIComponent(name)}`,
    )
  }

  async listProfiles(cluster: string): Promise<Profile[]> {
    const data = await this.request<{ profiles: Profile[] }>(
      'GET',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/profiles`,
    )
    return data.profiles ?? []
  }

  async createProfile(cluster: string, request: ProfileRequest): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/profiles`,
      request,
    )
  }

  async updateProfile(cluster: string, name: string, request: ProfileRequest): Promise<void> {
    await this.request<void>(
      'PATCH',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/profiles/${encodeURIComponent(name)}`,
      request,
    )
  }

  async deleteProfile(cluster: string, name: string): Promise<void> {
    await this.request<void>(
      'DELETE',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/profiles/${encodeURIComponent(name)}`,
    )
  }

  async updateMember(cluster: string, name: string, request: MemberUpdateRequest): Promise<void> {
    await this.request<void>(
      'PATCH',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/members/${encodeURIComponent(name)}`,
      request,
    )
  }

  async listMigrations(limit = 50): Promise<Migration[]> {
    const data = await this.request<{ migrations: Migration[] }>(
      'GET',
      `/api/v1/migrations?limit=${limit}`,
    )
    return data.migrations ?? []
  }

  async getMigration(id: string): Promise<Migration> {
    return this.request<Migration>('GET', `/api/v1/migrations/${encodeURIComponent(id)}`)
  }

  async submitMigration(request: MigrationRequest): Promise<Migration> {
    return this.request<Migration>('POST', '/api/v1/migrations', request)
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' }
    if (this.bearerToken) {
      headers.Authorization = `Bearer ${this.bearerToken}`
    }

    let payload: string | BodyInit | undefined
    if (body instanceof FormData) {
      // The browser sets the multipart boundary; a manual Content-Type
      // would drop it.
      payload = body
    } else if (body instanceof File) {
      payload = body
      headers['Content-Type'] = 'application/octet-stream'
    } else if (body !== undefined) {
      headers['Content-Type'] = jsonHeaders['Content-Type']
      payload = JSON.stringify(body)
    }

    const response = await this.fetchImpl(`${this.baseURL}${path}`, {
      method,
      headers,
      body: payload,
      credentials: 'same-origin',
    })

    if (!response.ok) {
      if (response.status === 401) {
        this.onUnauthorized?.()
      }

      throw await this.toError(response)
    }

    if (response.status === 204) {
      return undefined as T
    }

    const text = await response.text()
    if (!text) {
      return undefined as T
    }

    return JSON.parse(text) as T
  }

  private async toError(response: Response): Promise<ApiError> {
    let code = 'http_error'
    let message = `${response.status} ${response.statusText || 'request failed'}`

    try {
      const data = (await response.json()) as ErrorEnvelope
      if (data.error?.code) {
        code = data.error.code
      }
      if (data.error?.message) {
        message = data.error.message
      }
    } catch {
      // Non-JSON error bodies keep the status text.
    }

    return new ApiError(response.status, code, message)
  }

  /** Lists one directory inside an instance. */
  async listInstanceFiles(cluster: string, instance: string, dir: string): Promise<FileEntry[]> {
    const query = new URLSearchParams({ action: 'list', path: dir })
    const data = await this.request<{ files: FileEntry[] }>(
      'GET',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/files?${query.toString()}`,
    )
    return data.files ?? []
  }

  /** Uploads one file into an instance. */
  async uploadInstanceFile(
    cluster: string,
    instance: string,
    path: string,
    file: File,
  ): Promise<void> {
    const form = new FormData()
    form.append('file', file)

    await this.request<void>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/files?path=${encodeURIComponent(path)}`,
      form,
    )
  }

  /** Creates a directory inside an instance. */
  async mkdirInstanceDir(cluster: string, instance: string, path: string): Promise<void> {
    const query = new URLSearchParams({ action: 'mkdir', path })
    await this.request<void>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/files?${query.toString()}`,
    )
  }

  /** Deletes one file inside an instance. */
  async deleteInstanceFile(cluster: string, instance: string, path: string): Promise<void> {
    const query = new URLSearchParams({ path })
    await this.request<void>(
      'DELETE',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/files?${query.toString()}`,
    )
  }

  /** Builds the authenticated download URL for one instance file. */
  downloadFileURL(cluster: string, instance: string, path: string): string {
    return `/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/files?path=${encodeURIComponent(path)}`
  }

  /** Lists every image across all reachable clusters, tagged with its cluster. */
  async listAllImages(): Promise<ClusterImage[]> {
    const clusters = await this.listClusters()
    const perCluster = await Promise.all(
      clusters.map(async (cluster) => {
        try {
          const images = await this.listImages(cluster.name)
          return images.map((img) => ({
            cluster: cluster.name,
            fingerprint: img.fingerprint,
            alias: img.aliases[0] ?? '',
            type: img.type,
            os: img.os,
            release: img.release,
            description: img.description,
            size: img.size,
            cached: img.cached,
            created_at: img.created_at,
          }))
        } catch {
          return []
        }
      }),
    )

    return perCluster.flat()
  }

  /** Lists every ISO volume across all reachable clusters, tagged with its cluster. */
  async listAllISOs(): Promise<ISOSummary[]> {
    const clusters = await this.listClusters()
    const perCluster = await Promise.all(
      clusters.map(async (cluster) => {
        try {
          const isos = await this.listISOs(cluster.name)
          return isos.map((iso) => ({ ...iso, cluster: cluster.name }))
        } catch {
          return []
        }
      }),
    )

    return perCluster.flat()
  }

  /**
   * Uploads one multipart form while reporting upload progress. Uses XHR
   * because fetch cannot observe upload progress. Progress reports
   * (loaded, total) in bytes.
   */
  uploadFormWithProgress<T>(
    path: string,
    form: FormData,
    onProgress: (loaded: number, total: number) => void,
  ): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('POST', path)
      xhr.responseType = 'json'

      xhr.upload.onprogress = (event) => {
        if (event.lengthComputable) {
          onProgress(event.loaded, event.total)
        }
      }

      xhr.onload = () => {
        const body = (xhr.response ?? null) as { code?: string; message?: string } | null
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve(body as T)
          return
        }
        reject(
          new ApiError(
            xhr.status,
            body?.code ?? 'http_error',
            body?.message ?? `upload failed with status ${xhr.status}`,
          ),
        )
      }

      xhr.onerror = () => {
        reject(new ApiError(0, 'network_error', 'network error during upload'))
      }

      xhr.send(form)
    })
  }

  /** Uploads one ISO with progress reporting. */
  async importISOWithProgress(
    cluster: string,
    request: ImportISORequest,
    onProgress: (loaded: number, total: number) => void,
  ): Promise<{ name: string; pool: string }> {
    const query = new URLSearchParams()
    if (request.pool) {
      query.set('pool', request.pool)
    }

    const form = new FormData()
    form.append('image', request.file, request.file.name)

    return this.uploadFormWithProgress<{ name: string; pool: string }>(
      `/api/v1/clusters/${encodeURIComponent(cluster)}/iso?${query.toString()}`,
      form,
      onProgress,
    )
  }

  /** Returns the physical network snapshot of one cluster member. */
  async nodeSnapshot(cluster: string, member: string, wifiDevice?: string): Promise<NodeSnapshot> {
    const query = wifiDevice ? `?wifi=${encodeURIComponent(wifiDevice)}` : ''
    return this.request<NodeSnapshot>('GET', `${this.nodePath(cluster, member)}${query}`)
  }

  /** Brings one NIC of a cluster member up or down. */
  async nodeNICState(cluster: string, member: string, name: string, up: boolean): Promise<void> {
    await this.request<void>(
      'POST',
      `${this.nodePath(cluster, member)}/networks/${encodeURIComponent(name)}/state`,
      { up },
    )
  }

  /** Configures one NIC of a cluster member with DHCP or a static address. */
  async nodeApplyAddress(cluster: string, member: string, name: string, request: NodeAddressRequest): Promise<void> {
    await this.request<void>(
      'PATCH',
      `${this.nodePath(cluster, member)}/networks/${encodeURIComponent(name)}`,
      request,
    )
  }

  /** Associates one Wi-Fi interface of a cluster member with an access point. */
  async nodeWifiConnect(cluster: string, member: string, name: string, ssid: string, password: string): Promise<void> {
    await this.request<void>(
      'POST',
      `${this.nodePath(cluster, member)}/networks/${encodeURIComponent(name)}/wifi`,
      { ssid, password },
    )
  }

  /** Adds one static route on a cluster member. */
  async nodeAddRoute(cluster: string, member: string, destination: string, gateway: string, device: string): Promise<void> {
    await this.request<void>('POST', `${this.nodePath(cluster, member)}/routes`, { destination, gateway, device })
  }

  /** Deletes one static route from a cluster member. */
  async nodeDeleteRoute(cluster: string, member: string, destination: string): Promise<void> {
    await this.request<void>(
      'DELETE',
      `${this.nodePath(cluster, member)}/routes/${encodeURIComponent(destination)}`,
    )
  }

  /** Adds one firewall input rule to the fleet-managed nftables table. */
  async nodeAddFirewallRule(cluster: string, member: string, request: NodeFirewallRequest): Promise<void> {
    await this.request<void>('POST', `${this.nodePath(cluster, member)}/firewall`, request)
  }

  /** Deletes one firewall rule by handle. */
  async nodeDeleteFirewallRule(cluster: string, member: string, handle: string): Promise<void> {
    await this.request<void>(
      'DELETE',
      `${this.nodePath(cluster, member)}/firewall/${encodeURIComponent(handle)}`,
    )
  }

  /** Lists recent host tasks for one cluster member. */
  async nodeTasks(cluster: string, member: string, limit = 50): Promise<NodeTask[]> {
    const query = new URLSearchParams({ cluster, member, limit: String(limit) })
    const data = await this.request<{ tasks: NodeTask[] }>('GET', `/api/v1/tasks?${query.toString()}`)
    return data.tasks ?? []
  }

  /** Loads one host task and its event timeline. */
  async nodeTask(id: string): Promise<{ task: NodeTask; events: NodeTaskEvent[] }> {
    const data = await this.request<{ task: NodeTask; events: NodeTaskEvent[] }>(
      'GET',
      `/api/v1/tasks/${encodeURIComponent(id)}`,
    )
    return { task: data.task, events: data.events ?? [] }
  }

  /** Lists host audit records for one cluster member. */
  async nodeAudit(cluster: string, member: string, limit = 50): Promise<NodeAudit[]> {
    const query = new URLSearchParams({ cluster, member, limit: String(limit) })
    const data = await this.request<{ audit: NodeAudit[] }>('GET', `/api/v1/audit?${query.toString()}`)
    return data.audit ?? []
  }

  /** Returns the center's one-hour telemetry buffer for one member. */
  async hostMetrics(cluster: string, member: string, from = '1h'): Promise<TelemetrySeries[]> {
    const query = new URLSearchParams({ cluster, member, from })
    const data = await this.request<{ series: TelemetrySeries[] }>(
      'GET',
      `/api/v1/metrics/query?${query.toString()}`,
    )
    return data.series ?? []
  }

  /** Runs a PromQL range query through the center's external TSDB proxy. */
  async metricQueryRange(query: string, from = '1h', step = '1m', to = ''): Promise<TelemetrySeries[]> {
    const params = new URLSearchParams({ query, from, step })
    if (to) {
      params.set('to', to)
    }
    const data = await this.request<PrometheusQueryResponse>(
      'GET',
      `/api/v1/metrics/query_range?${params.toString()}`,
    )
    return (data.data?.result ?? []).map((result) => {
      const labels = { ...result.metric }
      const name = labels.__name__ ?? ''
      delete labels.__name__
      return {
        name,
        labels,
        type: 'gauge',
        points: result.values.map(([timestamp, value]) => ({
          at: new Date(timestamp * 1000).toISOString(),
          value: Number(value),
        })),
      }
    })
  }

  /** Lists enrolled host agents, optionally filtered by cluster or member. */
  async listAgents(cluster?: string, member?: string): Promise<AgentSummary[]> {
    const query = new URLSearchParams()
    if (cluster) {
      query.set('cluster', cluster)
    }
    if (member) {
      query.set('member', member)
    }
    const suffix = query.toString() ? `?${query.toString()}` : ''
    const data = await this.request<{ agents: AgentSummary[] }>('GET', `/api/v1/agents${suffix}`)
    return data.agents ?? []
  }

  /** Creates a one-time enrollment token bound to one cluster member. */
  async createAgentEnrollmentToken(
    cluster: string,
    member: string,
    ttlSeconds = 900,
  ): Promise<AgentEnrollmentToken> {
    return this.request<AgentEnrollmentToken>('POST', '/api/v1/agents/enrollment-tokens', {
      cluster,
      member,
      ttl_seconds: ttlSeconds,
    })
  }

  /** Runs the signed Incus bootstrap for one enrolled agent. */
  async bootstrapAgent(agentId: string, request: AgentBootstrapRequest): Promise<AgentBootstrapResult> {
    const data = await this.request<{ result: AgentBootstrapResult }>(
      'POST',
      `/api/v1/agents/${encodeURIComponent(agentId)}/bootstrap`,
      request,
    )
    return data.result
  }

  /** Provisions a new host and joins it to a cluster. */
  async joinClusterMember(cluster: string, request: ClusterMemberJoinRequest): Promise<ClusterMemberJoinResult> {
    return this.request<ClusterMemberJoinResult>(
      'POST',
      `/api/v1/clusters/${encodeURIComponent(cluster)}/members`,
      request,
    )
  }

  private nodePath(cluster: string, member: string): string {
    return `/api/v1/clusters/${encodeURIComponent(cluster)}/members/${encodeURIComponent(member)}/node`
  }

  /** Aggregates the network counters of every running instance on one cluster. */
  async clusterTraffic(cluster: string): Promise<ClusterTraffic> {
    return this.request<ClusterTraffic>('GET', `/api/v1/clusters/${encodeURIComponent(cluster)}/traffic`)
  }

  /** Registers a new cluster at runtime. */
  async registerCluster(request: ClusterRegisterRequest): Promise<void> {
    await this.request<void>('POST', '/api/v1/clusters', request)
  }

  /** Removes one dynamically registered cluster. */
  async unregisterCluster(name: string): Promise<void> {
    await this.request<void>('DELETE', `/api/v1/clusters/${encodeURIComponent(name)}`)
  }

  /** Builds the websocket URL for an instance console or exec session. */
  terminalURL(cluster: string, instance: string, kind: 'console' | 'exec'): string {
    const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws'
    return `${scheme}://${window.location.host}/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/${kind}`
  }
}

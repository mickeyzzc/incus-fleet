import type {
  ClusterResources,
  ClusterSummary,
  CloneRequest,
  CreateInstanceRequest,
  DeviceRequest,
  FileEntry,
  HealthResponse,
  ImportImageRequest,
  ImportISORequest,
  CopyImageRequest,
  InstanceActionRequest,
  InstanceDetail,
  InstanceImage,
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
  Profile,
  ProfileRequest,
  SessionResponse,
  SnapshotRequest,
  StoragePoolRequest,
  StorageVolume,
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

  async login(token: string): Promise<SessionResponse> {
    return this.request<SessionResponse>('POST', '/api/v1/auth/login', { token })
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

  /** Builds the websocket URL for an instance console or exec session. */
  terminalURL(cluster: string, instance: string, kind: 'console' | 'exec'): string {
    const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws'
    return `${scheme}://${window.location.host}/api/v1/clusters/${encodeURIComponent(cluster)}/instances/${encodeURIComponent(instance)}/${kind}`
  }
}

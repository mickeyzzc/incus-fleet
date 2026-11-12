import type {
  ClusterResources,
  ClusterSummary,
  CreateInstanceRequest,
  HealthResponse,
  InstanceActionRequest,
  InstanceDetail,
  InstanceImage,
  InstanceSummary,
  InstanceType,
  Migration,
  MigrationRequest,
  SessionResponse,
  UpdateInstanceRequest,
  VersionResponse,
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

    let payload: string | undefined
    if (body !== undefined) {
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
}

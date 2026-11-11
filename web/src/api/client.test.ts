import { describe, expect, it, vi } from 'vitest'

import { ApiClient, ApiError } from './client'
import type { MigrationRequest } from './types'

interface MockResponseInit {
  status?: number
  statusText?: string
}

function mockResponse(body: unknown, init: MockResponseInit = {}): Response {
  const status = init.status ?? 200
  const text = body === undefined ? '' : typeof body === 'string' ? body : JSON.stringify(body)

  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: init.statusText ?? (status < 300 ? 'OK' : ''),
    async text() {
      return text
    },
    async json() {
      return JSON.parse(text) as unknown
    },
  } as Response
}

function clientWith(response: Response = mockResponse({})) {
  const fetchImpl = vi.fn<typeof fetch>(async () => response)

  return {
    fetchImpl,
    client: new ApiClient({ baseURL: 'https://fleet.local/', fetchImpl }),
  }
}

describe('ApiClient', () => {
  it('logs in and posts the token as JSON', async () => {
    const { client, fetchImpl } = clientWith(
      mockResponse({ authenticated: true, expires_at: '2026-11-12T00:00:00Z' }),
    )

    const session = await client.login(' secret ')

    expect(session.authenticated).toBe(true)
    expect(fetchImpl).toHaveBeenCalledWith(
      'https://fleet.local/api/v1/auth/login',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ token: ' secret ' }),
        credentials: 'same-origin',
      }),
    )
    const init = fetchImpl.mock.calls[0]?.[1] as RequestInit
    expect(init.headers).toMatchObject({
      Accept: 'application/json',
      'Content-Type': 'application/json',
    })
  })

  it('sends a bearer token when configured', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({ version: '1.0.0' }))
    client.setBearerToken('abc123')

    await client.version()

    const init = fetchImpl.mock.calls[0]?.[1] as RequestInit
    expect(init.headers).toMatchObject({ Authorization: 'Bearer abc123' })
  })

  it('handles 204 and empty responses', async () => {
    const { client: logoutClient } = clientWith(mockResponse(undefined, { status: 204 }))
    await expect(logoutClient.logout()).resolves.toBeUndefined()

    const { client: emptyClient } = clientWith(mockResponse(''))
    await expect(emptyClient.health()).resolves.toBeUndefined()
  })

  it('calls session and health endpoints', async () => {
    const session = { authenticated: true }
    const { client, fetchImpl } = clientWith(mockResponse(session))

    await expect(client.session()).resolves.toEqual(session)
    await client.health()
    await client.version()

    expect(fetchImpl.mock.calls.map((call) => call[0])).toEqual([
      'https://fleet.local/api/v1/auth/session',
      'https://fleet.local/api/v1/healthz',
      'https://fleet.local/api/v1/version',
    ])
  })

  it('unwraps cluster lists and tolerates missing fields', async () => {
    const clusters = [{ name: 'c1' }]
    const { client, fetchImpl } = clientWith(mockResponse({ clusters }))

    await expect(client.listClusters()).resolves.toEqual(clusters)
    expect(fetchImpl).toHaveBeenCalledWith(
      'https://fleet.local/api/v1/clusters',
      expect.objectContaining({ method: 'GET' }),
    )

    const { client: empty } = clientWith(mockResponse({}))
    await expect(empty.listClusters()).resolves.toEqual([])
  })

  it('encodes cluster and instance names', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({ name: 'c1' }))

    await client.getCluster('c 62')
    await client.reconnectCluster('c/62')
    await client.getInstance('c1', 'web/one')

    expect(fetchImpl.mock.calls.map((call) => call[0])).toEqual([
      'https://fleet.local/api/v1/clusters/c%2062',
      'https://fleet.local/api/v1/clusters/c%2F62/reconnect',
      'https://fleet.local/api/v1/clusters/c1/instances/web%2Fone',
    ])
  })

  it('lists cluster instances and all instances with a type query', async () => {
    const instances = [{ name: 'web' }]
    const { client, fetchImpl } = clientWith(mockResponse({ instances }))

    await expect(client.listClusterInstances('c1', 'virtual-machine')).resolves.toEqual(instances)
    await expect(client.listClusterInstances('c1')).resolves.toEqual(instances)
    await expect(client.listInstances('container')).resolves.toEqual(instances)
    await expect(client.listInstances()).resolves.toEqual(instances)

    expect(fetchImpl.mock.calls.map((call) => call[0])).toEqual([
      'https://fleet.local/api/v1/clusters/c1/instances?type=virtual-machine',
      'https://fleet.local/api/v1/clusters/c1/instances?type=any',
      'https://fleet.local/api/v1/instances?type=container',
      'https://fleet.local/api/v1/instances?type=any',
    ])
  })

  it('tolerates missing instance arrays', async () => {
    const { client } = clientWith(mockResponse({}))
    await expect(client.listInstances()).resolves.toEqual([])
    await expect(client.listClusterInstances('c1')).resolves.toEqual([])
  })

  it('lists and fetches migrations', async () => {
    const migrations = [{ id: 'm1' }]
    const { client, fetchImpl } = clientWith(mockResponse({ migrations }))

    await expect(client.listMigrations()).resolves.toEqual(migrations)
    await expect(client.listMigrations(5)).resolves.toEqual(migrations)

    const { client: getClient } = clientWith(mockResponse({ id: 'm1' }))
    await expect(getClient.getMigration('m/1')).resolves.toEqual({ id: 'm1' })

    expect(fetchImpl.mock.calls.map((call) => call[0])).toEqual([
      'https://fleet.local/api/v1/migrations?limit=50',
      'https://fleet.local/api/v1/migrations?limit=5',
    ])
  })

  it('tolerates a missing migration array and submits requests', async () => {
    const request: MigrationRequest = {
      source_cluster: 'c1',
      target_cluster: 'c2',
      instance: 'web',
      mode: 'move',
      live: false,
      include_snapshots: true,
    }
    const { client, fetchImpl } = clientWith(mockResponse({ id: 'm1' }))

    await expect(client.listMigrations()).resolves.toEqual([])
    await expect(client.submitMigration(request)).resolves.toEqual({ id: 'm1' })
    expect(fetchImpl).toHaveBeenCalledWith(
      'https://fleet.local/api/v1/migrations',
      expect.objectContaining({ method: 'POST', body: JSON.stringify(request) }),
    )
  })

  it('maps JSON error envelopes and invokes the unauthorized hook', async () => {
    const onUnauthorized = vi.fn()
    const fetchImpl = vi.fn<typeof fetch>(async () =>
      mockResponse(
        { error: { code: 'invalid_token', message: 'invalid admin token' } },
        { status: 401, statusText: 'Unauthorized' },
      ),
    )
    const client = new ApiClient({ fetchImpl, onUnauthorized })

    await expect(client.session()).rejects.toMatchObject({
      name: 'ApiError',
      status: 401,
      code: 'invalid_token',
      message: 'invalid admin token',
    })
    expect(onUnauthorized).toHaveBeenCalledTimes(1)
  })

  it('falls back to status text for non-JSON errors', async () => {
    const { client } = clientWith(mockResponse('not json', { status: 500, statusText: 'Server Error' }))

    await expect(client.health()).rejects.toMatchObject({
      status: 500,
      code: 'http_error',
      message: '500 Server Error',
    })
  })

  it('uses a generic message when status text is empty', async () => {
    const { client } = clientWith(mockResponse('bad gateway', { status: 502 }))

    await expect(client.health()).rejects.toMatchObject({ message: '502 request failed' })
  })

  it('can use an error envelope without message or code', async () => {
    const { client } = clientWith(mockResponse({ error: {} }, { status: 400, statusText: 'Bad Request' }))

    await expect(client.health()).rejects.toMatchObject({
      status: 400,
      code: 'http_error',
      message: '400 Bad Request',
    })
  })

  it('keeps ApiError details', () => {
    const error = new ApiError(418, 'teapot', 'short and stout')

    expect(error).toBeInstanceOf(Error)
    expect(error.name).toBe('ApiError')
    expect(error.status).toBe(418)
    expect(error.code).toBe('teapot')
    expect(error.message).toBe('short and stout')
  })
})

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

  it('creates, updates, and deletes storage pools', async () => {
    const { client, fetchImpl } = clientWith()

    await client.createStoragePool('c1', { name: 'fast', driver: 'zfs' })
    await client.updateStoragePool('c1', 'fast', { description: 'ssd' })
    await client.deleteStoragePool('c1', 'fast')

    expect(fetchImpl.mock.calls.map(([url, init]) => [url, (init as RequestInit).method])).toEqual([
      ['https://fleet.local/api/v1/clusters/c1/storage-pools', 'POST'],
      ['https://fleet.local/api/v1/clusters/c1/storage-pools/fast', 'PATCH'],
      ['https://fleet.local/api/v1/clusters/c1/storage-pools/fast', 'DELETE'],
    ])
    expect(JSON.parse((fetchImpl.mock.calls[0]?.[1] as RequestInit).body as string)).toEqual({
      name: 'fast',
      driver: 'zfs',
    })
  })

  it('creates, updates, and deletes managed networks', async () => {
    const { client, fetchImpl } = clientWith()

    await client.createNetwork('c1', { name: 'fleet0', type: 'bridge' })
    await client.updateNetwork('c1', 'fleet0', { description: 'fleet' })
    await client.deleteNetwork('c1', 'fleet0')

    expect(fetchImpl.mock.calls.map(([url, init]) => [url, (init as RequestInit).method])).toEqual([
      ['https://fleet.local/api/v1/clusters/c1/networks', 'POST'],
      ['https://fleet.local/api/v1/clusters/c1/networks/fleet0', 'PATCH'],
      ['https://fleet.local/api/v1/clusters/c1/networks/fleet0', 'DELETE'],
    ])
  })

  it('lists, creates, updates, and deletes profiles', async () => {
    const { client, fetchImpl } = clientWith(
      mockResponse({ profiles: [{ name: 'default', config: {}, devices: {}, used_by: [] }] }),
    )

    const profiles = await client.listProfiles('c1')
    await client.createProfile('c1', { name: 'gpu' })
    await client.updateProfile('c1', 'gpu', { description: 'passthrough' })
    await client.deleteProfile('c1', 'gpu')

    expect(profiles).toHaveLength(1)
    expect(fetchImpl.mock.calls.map(([url, init]) => [url, (init as RequestInit).method])).toEqual([
      ['https://fleet.local/api/v1/clusters/c1/profiles', 'GET'],
      ['https://fleet.local/api/v1/clusters/c1/profiles', 'POST'],
      ['https://fleet.local/api/v1/clusters/c1/profiles/gpu', 'PATCH'],
      ['https://fleet.local/api/v1/clusters/c1/profiles/gpu', 'DELETE'],
    ])
  })

  it('updates cluster member metadata', async () => {
    const { client, fetchImpl } = clientWith()

    await client.updateMember('c1', 'node-1', { description: 'primary', groups: ['rack-a'] })

    expect(fetchImpl).toHaveBeenCalledWith(
      'https://fleet.local/api/v1/clusters/c1/members/node-1',
      expect.objectContaining({ method: 'PATCH' }),
    )
    expect(JSON.parse((fetchImpl.mock.calls[0]?.[1] as RequestInit).body as string)).toEqual({
      description: 'primary',
      groups: ['rack-a'],
    })
  })

  it('manages instance snapshots', async () => {
    const { client, fetchImpl } = clientWith(
      mockResponse({ snapshots: [{ name: 'snap0', size: 1024, stateful: true }] }),
    )

    const snapshots = await client.listSnapshots('c1', 'web')
    await client.createSnapshot('c1', 'web', { name: 'snap1', stateful: true })
    await client.restoreSnapshot('c1', 'web', 'snap0')
    await client.deleteSnapshot('c1', 'web', 'snap0')

    expect(snapshots).toHaveLength(1)
    expect(fetchImpl.mock.calls.map(([url, init]) => [url, (init as RequestInit).method])).toEqual([
      ['https://fleet.local/api/v1/clusters/c1/instances/web/snapshots', 'GET'],
      ['https://fleet.local/api/v1/clusters/c1/instances/web/snapshots', 'POST'],
      ['https://fleet.local/api/v1/clusters/c1/instances/web/snapshots/snap0/restore', 'POST'],
      ['https://fleet.local/api/v1/clusters/c1/instances/web/snapshots/snap0', 'DELETE'],
    ])
  })

  it('manages custom storage volumes', async () => {
    const { client, fetchImpl } = clientWith(
      mockResponse({ volumes: [{ name: 'data', type: 'custom', config: {}, used_by: [] }] }),
    )

    const volumes = await client.listVolumes('c1', 'default')
    await client.createVolume('c1', 'default', { name: 'assets', size: '5GiB' })
    await client.deleteVolume('c1', 'default', 'assets')

    expect(volumes).toHaveLength(1)
    expect(fetchImpl.mock.calls.map(([url, init]) => [url, (init as RequestInit).method])).toEqual([
      ['https://fleet.local/api/v1/clusters/c1/storage-pools/default/volumes', 'GET'],
      ['https://fleet.local/api/v1/clusters/c1/storage-pools/default/volumes', 'POST'],
      ['https://fleet.local/api/v1/clusters/c1/storage-pools/default/volumes/assets', 'DELETE'],
    ])
  })

  it('attaches and detaches instance devices', async () => {
    const detail = { name: 'web', cluster: 'c1' }
    const { client, fetchImpl } = clientWith(mockResponse(detail))

    const attached = await client.attachDevice('c1', 'web', {
      name: 'data',
      type: 'disk',
      path: '/data',
    })
    const detached = await client.detachDevice('c1', 'web', 'data')

    expect(attached).toMatchObject({ name: 'web' })
    expect(detached).toMatchObject({ name: 'web' })
    expect(fetchImpl.mock.calls.map(([url, init]) => [url, (init as RequestInit).method])).toEqual([
      ['https://fleet.local/api/v1/clusters/c1/instances/web/devices', 'POST'],
      ['https://fleet.local/api/v1/clusters/c1/instances/web/devices/data', 'DELETE'],
    ])
    expect(JSON.parse((fetchImpl.mock.calls[0]?.[1] as RequestInit).body as string)).toEqual({
      name: 'data',
      type: 'disk',
      path: '/data',
    })
  })

  it('deletes an image', async () => {
    const { client, fetchImpl } = clientWith()

    await client.deleteImage('c1', '053059ea4e28')

    expect(fetchImpl).toHaveBeenCalledWith(
      'https://fleet.local/api/v1/clusters/c1/images/053059ea4e28',
      expect.objectContaining({ method: 'DELETE' }),
    )
  })

  it('uploads an image import as a raw file and copies images', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({ fingerprint: 'abc123' }))

    const file = new File(['tarball'], 'debian13.tar', { type: 'application/octet-stream' })
    const fingerprint = await client.importImage('c1', {
      file,
      type: 'container',
      alias: 'debian/13',
    })
    await client.copyImage('c1', 'abc123', { target_cluster: 'c2', alias: 'debian/13' })

    expect(fingerprint).toBe('abc123')
    expect(fetchImpl.mock.calls[0]?.[0]).toBe(
      'https://fleet.local/api/v1/clusters/c1/images/import?filename=debian13.tar&type=container&alias=debian%2F13',
    )
    const init = fetchImpl.mock.calls[0]?.[1] as RequestInit
    expect(init.method).toBe('POST')
    expect(init.body).toBe(file)
    expect(fetchImpl.mock.calls[1]?.[0]).toBe(
      'https://fleet.local/api/v1/clusters/c1/images/abc123/copy',
    )
  })
})

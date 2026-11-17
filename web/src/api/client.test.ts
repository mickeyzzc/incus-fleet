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

  it('uploads an image import as multipart form data and copies images', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({ fingerprint: 'abc123' }))

    const file = new File(['tarball'], 'debian13.tar', { type: 'application/octet-stream' })
    const rootfs = new File(['rootfs'], 'debian13.root', { type: 'application/octet-stream' })
    const fingerprint = await client.importImage('c1', {
      file,
      rootfs,
      type: 'container',
      alias: 'debian/13',
    })
    await client.copyImage('c1', 'abc123', { target_cluster: 'c2', alias: 'debian/13' })

    expect(fingerprint).toBe('abc123')
    expect(fetchImpl.mock.calls[0]?.[0]).toBe(
      'https://fleet.local/api/v1/clusters/c1/images/import?type=container&alias=debian%2F13',
    )
    const init = fetchImpl.mock.calls[0]?.[1] as RequestInit
    expect(init.method).toBe('POST')
    const form = init.body as FormData
    expect(form).toBeInstanceOf(FormData)
    expect(form.get('image')).toEqual(file)
    expect(form.get('rootfs')).toEqual(rootfs)
    expect(fetchImpl.mock.calls[1]?.[0]).toBe(
      'https://fleet.local/api/v1/clusters/c1/images/abc123/copy',
    )
  })

  it('refreshes images from their origin', async () => {
    const { client, fetchImpl } = clientWith()

    await client.refreshImage('c1', 'abc123def456')

    expect(fetchImpl).toHaveBeenCalledWith(
      'https://fleet.local/api/v1/clusters/c1/images/abc123def456/refresh',
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('lists, uploads, and removes ISO volumes', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({ isos: [{ name: 'alpine', pool: 'default' }] }))

    const isos = await client.listISOs('c1')
    expect(isos).toEqual([{ name: 'alpine', pool: 'default' }])

    const file = new File(['iso'], 'alpine.iso')
    await client.importISO('c1', { file, pool: 'fast' })
    const init = fetchImpl.mock.calls[1]?.[1] as RequestInit
    expect(fetchImpl.mock.calls[1]?.[0]).toBe('https://fleet.local/api/v1/clusters/c1/iso?pool=fast')
    expect(init.body).toBeInstanceOf(FormData)
    expect((init.body as FormData).get('image')).toEqual(file)

    await client.deleteISO('c1', 'default', 'alpine')
    expect(fetchImpl).toHaveBeenCalledWith(
      'https://fleet.local/api/v1/clusters/c1/iso/default/alpine',
      expect.objectContaining({ method: 'DELETE' }),
    )
  })

  it('launches installer VMs, runs ops, rebuilds, and clones', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({ name: 'installer-01' }))

    await client.launchFromISO('c1', 'default', 'alpine', { name: 'installer-01' })
    expect(fetchImpl).toHaveBeenCalledWith(
      'https://fleet.local/api/v1/clusters/c1/iso/default/alpine/launch',
      expect.objectContaining({ method: 'POST' }),
    )

    const result = await client.runOps('c1', 'web', { action: 'run-script', script: 'echo hi' })
    expect(result).toEqual({ name: 'installer-01' })
    expect(fetchImpl).toHaveBeenCalledWith(
      'https://fleet.local/api/v1/clusters/c1/instances/web/ops',
      expect.objectContaining({ method: 'POST' }),
    )

    await client.rebuildInstance('c1', 'web', 'debian/13')
    expect(fetchImpl).toHaveBeenCalledWith(
      'https://fleet.local/api/v1/clusters/c1/instances/web/rebuild',
      expect.objectContaining({ method: 'POST' }),
    )

    await client.cloneInstance('c1', 'web', { target: 'web-clone' })
    expect(fetchImpl).toHaveBeenCalledWith(
      'https://fleet.local/api/v1/clusters/c1/instances/web/clone',
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('builds same-origin VGA websocket URLs', async () => {
    const { client } = clientWith()

    const url = client.vgaURL('c1', 'vm-01')

    expect(url).toBe('ws://localhost:3000/api/v1/clusters/c1/instances/vm-01/vga')
  })

  it('hits the documented endpoint for every admin and asset call', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({ metrics: { name: 'web' } }))

    const calls: Array<[() => Promise<unknown>, string, string]> = [
      [() => client.health(), 'GET', '/api/v1/healthz'],
      [() => client.version(), 'GET', '/api/v1/version'],
      [() => client.listClusters(), 'GET', '/api/v1/clusters'],
      [() => client.getCluster('c1'), 'GET', '/api/v1/clusters/c1'],
      [() => client.reconnectCluster('c1'), 'POST', '/api/v1/clusters/c1/reconnect'],
      [() => client.listClusterInstances('c1', 'container'), 'GET', '/api/v1/clusters/c1/instances?type=container'],
      [() => client.listInstances('virtual-machine'), 'GET', '/api/v1/instances?type=virtual-machine'],
      [() => client.getInstance('c1', 'web'), 'GET', '/api/v1/clusters/c1/instances/web'],
      [() => client.listImages('c1'), 'GET', '/api/v1/clusters/c1/images'],
      [() => client.deleteImage('c1', 'abc123'), 'DELETE', '/api/v1/clusters/c1/images/abc123'],
      [() => client.refreshImage('c1', 'abc123'), 'POST', '/api/v1/clusters/c1/images/abc123/refresh'],
      [() => client.copyImage('c1', 'abc123', { target_cluster: 'c2' }), 'POST', '/api/v1/clusters/c1/images/abc123/copy'],
      [() => client.listISOs('c1'), 'GET', '/api/v1/clusters/c1/iso'],
      [() => client.deleteISO('c1', 'default', 'alpine'), 'DELETE', '/api/v1/clusters/c1/iso/default/alpine'],
      [() => client.listSnapshots('c1', 'web'), 'GET', '/api/v1/clusters/c1/instances/web/snapshots'],
      [() => client.createSnapshot('c1', 'web', { name: 'snap0' }), 'POST', '/api/v1/clusters/c1/instances/web/snapshots'],
      [() => client.restoreSnapshot('c1', 'web', 'snap0'), 'POST', '/api/v1/clusters/c1/instances/web/snapshots/snap0/restore'],
      [() => client.deleteSnapshot('c1', 'web', 'snap0'), 'DELETE', '/api/v1/clusters/c1/instances/web/snapshots/snap0'],
      [() => client.attachDevice('c1', 'web', { name: 'data', type: 'disk' }), 'POST', '/api/v1/clusters/c1/instances/web/devices'],
      [() => client.detachDevice('c1', 'web', 'data'), 'DELETE', '/api/v1/clusters/c1/instances/web/devices/data'],
      [() => client.instanceMetrics('c1', 'web'), 'GET', '/api/v1/clusters/c1/instances/web/metrics'],
      [() => client.deleteInstance('c1', 'web', true), 'DELETE', '/api/v1/clusters/c1/instances/web?force=true'],
      [() => client.listVolumes('c1', 'default'), 'GET', '/api/v1/clusters/c1/storage-pools/default/volumes'],
      [() => client.createVolume('c1', 'default', { name: 'data', size: '10GiB' }), 'POST', '/api/v1/clusters/c1/storage-pools/default/volumes'],
      [() => client.deleteVolume('c1', 'default', 'data'), 'DELETE', '/api/v1/clusters/c1/storage-pools/default/volumes/data'],
      [() => client.createStoragePool('c1', { name: 'slow', driver: 'dir' }), 'POST', '/api/v1/clusters/c1/storage-pools'],
      [() => client.updateStoragePool('c1', 'slow', { description: 'cold' }), 'PATCH', '/api/v1/clusters/c1/storage-pools/slow'],
      [() => client.deleteStoragePool('c1', 'slow'), 'DELETE', '/api/v1/clusters/c1/storage-pools/slow'],
      [() => client.createNetwork('c1', { name: 'dmz' }), 'POST', '/api/v1/clusters/c1/networks'],
      [() => client.updateNetwork('c1', 'dmz', { description: 'edge' }), 'PATCH', '/api/v1/clusters/c1/networks/dmz'],
      [() => client.deleteNetwork('c1', 'dmz'), 'DELETE', '/api/v1/clusters/c1/networks/dmz'],
      [() => client.listProfiles('c1'), 'GET', '/api/v1/clusters/c1/profiles'],
      [() => client.createProfile('c1', { name: 'tiny' }), 'POST', '/api/v1/clusters/c1/profiles'],
      [() => client.updateProfile('c1', 'tiny', { description: 'small' }), 'PATCH', '/api/v1/clusters/c1/profiles/tiny'],
      [() => client.deleteProfile('c1', 'tiny'), 'DELETE', '/api/v1/clusters/c1/profiles/tiny'],
      [() => client.updateMember('c1', 'member-a', { description: 'rack1' }), 'PATCH', '/api/v1/clusters/c1/members/member-a'],
      [() => client.listKeys(), 'GET', '/api/v1/keys'],
      [() => client.createKey({ name: 'ci', public_key: 'ssh-ed25519 AAA' }), 'POST', '/api/v1/keys'],
      [() => client.updateKey('key-1', { name: 'ci', public_key: 'ssh-ed25519 AAA' }), 'PATCH', '/api/v1/keys/key-1'],
      [() => client.deleteKey('key-1'), 'DELETE', '/api/v1/keys/key-1'],
      [() => client.listScripts(), 'GET', '/api/v1/scripts'],
      [() => client.createScript({ name: 'init', body: 'echo' }), 'POST', '/api/v1/scripts'],
      [() => client.updateScript('script-1', { name: 'init', body: 'echo' }), 'PATCH', '/api/v1/scripts/script-1'],
      [() => client.deleteScript('script-1'), 'DELETE', '/api/v1/scripts/script-1'],
      [() => client.batchKeys({ targets: [], key_ids: [] }), 'POST', '/api/v1/batch/keys'],
      [() => client.batchScripts({ targets: [], script_id: 's' }), 'POST', '/api/v1/batch/scripts'],
      [() => client.batchPower({ targets: [], action: 'stop' }), 'POST', '/api/v1/batch/power'],
      [() => client.listInstanceFiles('c1', 'web', '/tmp'), 'GET', '/api/v1/clusters/c1/instances/web/files?action=list&path=%2Ftmp'],
      [() => client.mkdirInstanceDir('c1', 'web', '/tmp/x'), 'POST', '/api/v1/clusters/c1/instances/web/files?action=mkdir&path=%2Ftmp%2Fx'],
      [() => client.deleteInstanceFile('c1', 'web', '/tmp/x'), 'DELETE', '/api/v1/clusters/c1/instances/web/files?path=%2Ftmp%2Fx'],
      [() => client.listMigrations(10), 'GET', '/api/v1/migrations?limit=10'],
      [() => client.getMigration('m1'), 'GET', '/api/v1/migrations/m1'],
    ]

    for (const [call, method, suffix] of calls) {
      fetchImpl.mockClear()
      await call()
      expect(fetchImpl).toHaveBeenCalledTimes(1)
      const [url, init] = fetchImpl.mock.calls[0] as [string, RequestInit]
      expect(url, `${method} ${suffix}`).toBe(`https://fleet.local${suffix}`)
      expect(init.method, suffix).toBe(method)
    }
  })
})

describe('ApiClient fleet-wide inventory and uploads', () => {
  function routingClient(responses: Array<{ match: (url: string) => boolean; body: unknown }>) {
    const fetchImpl = vi.fn<typeof fetch>(async (input: RequestInfo | URL) => {
      const url = String(input)
      for (const route of responses) {
        if (route.match(url)) {
          return mockResponse(route.body)
        }
      }
      return mockResponse({}, { status: 404, statusText: 'not found' })
    })

    return {
      fetchImpl,
      client: new ApiClient({ baseURL: 'https://fleet.local/', fetchImpl }),
    }
  }

  it('aggregates images across clusters and tolerates offline clusters', async () => {
    const { client } = routingClient([
      { match: (u) => u.endsWith('/api/v1/clusters'), body: { clusters: [{ name: 'c1' }, { name: 'c2' }] } },
      { match: (u) => u.includes('/clusters/c1/images'), body: { images: [{ fingerprint: 'fp1', aliases: ['debian/13'], size: 100, cached: false, created_at: 't' }] } },
      { match: (u) => u.includes('/clusters/c2/images'), body: { images: [{ fingerprint: 'fp2', aliases: [], size: 5, cached: true, created_at: 't2' }] } },
    ])

    const images = await client.listAllImages()
    expect(images).toHaveLength(2)
    expect(images[0]).toMatchObject({ cluster: 'c1', fingerprint: 'fp1', alias: 'debian/13' })
    expect(images[1]).toMatchObject({ cluster: 'c2', alias: '', cached: true })
  })

  it('keeps clusters with failing image lists out of the result', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/clusters')) {
        return mockResponse({ clusters: [{ name: 'good' }, { name: 'bad' }] })
      }
      if (url.includes('/clusters/good/images')) {
        return mockResponse({ images: [{ fingerprint: 'fp', aliases: ['alpine'], size: 1, cached: true, created_at: 't' }] })
      }
      return mockResponse({}, { status: 503, statusText: 'down' })
    })
    const client = new ApiClient({ baseURL: 'https://fleet.local/', fetchImpl })

    const images = await client.listAllImages()
    expect(images).toHaveLength(1)
    expect(images[0]?.cluster).toBe('good')
  })

  it('tags every ISO volume with its cluster', async () => {
    const { client } = routingClient([
      { match: (u) => u.endsWith('/api/v1/clusters'), body: { clusters: [{ name: 'c9' }] } },
      { match: (u) => u.includes('/clusters/c9/iso'), body: { isos: [{ name: 'debian', pool: 'default', size: '10' }] } },
    ])

    const isos = await client.listAllISOs()
    expect(isos).toEqual([{ name: 'debian', pool: 'default', size: '10', cluster: 'c9' }])
  })

  it('reports upload progress and resolves the parsed body', async () => {
    class FakeXHR {
      status = 201
      response = { name: 'debian.iso', pool: 'default' }
      responseType = ''
      upload = { onprogress: null as ((event: ProgressEvent) => void) | null }
      onload: ((event: ProgressEvent) => void) | null = null
      open() {}
      send() {
        this.upload?.onprogress?.(new ProgressEvent('progress', { loaded: 500, total: 1000, lengthComputable: true }))
        this.onload?.(new ProgressEvent('load'))
      }
    }
    const OriginalXHR = globalThis.XMLHttpRequest
    ;(globalThis as { XMLHttpRequest: unknown }).XMLHttpRequest = FakeXHR

    try {
      const client = new ApiClient({ baseURL: 'https://fleet.local/', fetchImpl: vi.fn() })
      const seen: Array<[number, number]> = []
      const result = await client.importISOWithProgress(
        'c1',
        { file: new File(['data'], 'debian.iso'), pool: 'default' },
        (loaded, total) => seen.push([loaded, total]),
      )

      expect(result).toEqual({ name: 'debian.iso', pool: 'default' })
      expect(seen).toEqual([[500, 1000]])
    } finally {
      ;(globalThis as { XMLHttpRequest: unknown }).XMLHttpRequest = OriginalXHR
    }
  })
})

describe('ApiClient upload error handling', () => {
  it('rejects with an ApiError when the upload request errors', async () => {
    class FailingXHR {
      status = 0
      response = null
      responseType = ''
      upload = { onprogress: null as ((event: ProgressEvent) => void) | null }
      onerror: ((event: ProgressEvent) => void) | null = null
      open() {}
      send() {
        this.onerror?.(new ProgressEvent('error'))
      }
    }
    const OriginalXHR = globalThis.XMLHttpRequest
    ;(globalThis as { XMLHttpRequest: unknown }).XMLHttpRequest = FailingXHR

    try {
      const client = new ApiClient({ baseURL: 'https://fleet.local/', fetchImpl: vi.fn() })
      await expect(
        client.importISOWithProgress('c1', { file: new File(['x'], 'a.iso') }, () => {}),
      ).rejects.toMatchObject({ code: 'network_error' })
    } finally {
      ;(globalThis as { XMLHttpRequest: unknown }).XMLHttpRequest = OriginalXHR
    }
  })
})

describe('ApiClient remaining documented endpoints', () => {
  it('hits resources, instance mutation, and file upload endpoints', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({ resources: {} }))
    const file = new File(['x'], 'f.txt')

    await client.getResources('c1')
    await client.createInstance('c1', { name: 'web', image: 'debian/13' })
    await client.updateInstance('c1', 'web', { cpu: '2' })
    await client.instanceAction('c1', 'web', { action: 'start' })
    await client.uploadInstanceFile('c1', 'web', '/tmp/f.txt', file)
    await client.prepareSPICE('c1', 'web')

    const urls = fetchImpl.mock.calls.map((c) => String(c[0]))
    expect(urls).toContain('https://fleet.local/api/v1/clusters/c1/resources')
    expect(urls).toContain('https://fleet.local/api/v1/clusters/c1/instances')
    expect(urls).toContain('https://fleet.local/api/v1/clusters/c1/instances/web')
    expect(urls).toContain('https://fleet.local/api/v1/clusters/c1/instances/web/state')
    expect(urls).toContain('https://fleet.local/api/v1/clusters/c1/instances/web/files?path=%2Ftmp%2Ff.txt')
    expect(urls).toContain('https://fleet.local/api/v1/clusters/c1/instances/web/spice/prepare')
  })

  it('builds console, VGA, SPICE, and download URLs', () => {
    const { client } = clientWith()
    Object.defineProperty(globalThis.window, 'location', {
      writable: true,
      value: { protocol: 'https:', host: 'fleet.local' },
    })

    expect(client.terminalURL('c1', 'web', 'console')).toBe('wss://fleet.local/api/v1/clusters/c1/instances/web/console')
    expect(client.terminalURL('c1', 'web', 'exec')).toBe('wss://fleet.local/api/v1/clusters/c1/instances/web/exec')
    expect(client.vgaURL('c1', 'web')).toBe('wss://fleet.local/api/v1/clusters/c1/instances/web/vga')
    expect(client.spiceURL('c1', 'web', 5959)).toBe('wss://fleet.local/api/v1/clusters/c1/instances/web/spice?port=5959')
    expect(client.downloadFileURL('c1', 'web', '/tmp/f.txt')).toBe(
      '/api/v1/clusters/c1/instances/web/files?path=%2Ftmp%2Ff.txt',
    )
  })
})

describe('ApiClient node network and cluster registration endpoints', () => {
  it('hits every node network endpoint', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({ host: 'n1', nics: [] }))

    await client.nodeSnapshot('c1', 'n1')
    await client.nodeSnapshot('c1', 'n1', 'wlan0')
    await client.nodeNICState('c1', 'n1', 'eth0', false)
    await client.nodeApplyAddress('c1', 'n1', 'eth0', { dhcp: false, address: '10.0.0.50', persist: true })
    await client.nodeWifiConnect('c1', 'n1', 'wlan0', 'OfficeWiFi', 'secret')
    await client.nodeAddRoute('c1', 'n1', '10.0.1.0/24', '10.0.0.1', 'eth0')
    await client.nodeDeleteRoute('c1', 'n1', '10.0.1.0/24')
    await client.nodeAddFirewallRule('c1', 'n1', { port: 22, proto: 'tcp', action: 'accept', comment: 'ssh' })
    await client.nodeDeleteFirewallRule('c1', 'n1', '42')

    const auth = 'https://fleet.local/api/v1/clusters/c1/members/n1/node'
    const urls = fetchImpl.mock.calls.map((c) => String(c[0]))
    expect(urls).toContain(auth)
    expect(urls).toContain(`${auth}?wifi=wlan0`)
    expect(urls).toContain(`${auth}/networks/eth0/state`)
    expect(urls).toContain(`${auth}/networks/eth0`)
    expect(urls).toContain(`${auth}/networks/wlan0/wifi`)
    expect(urls).toContain(`${auth}/routes`)
    expect(urls).toContain(`${auth}/routes/10.0.1.0%2F24`)
    expect(urls).toContain(`${auth}/firewall`)
    expect(urls).toContain(`${auth}/firewall/42`)

    const methods = fetchImpl.mock.calls.map((c) => (c[1] as RequestInit | undefined)?.method)
    expect(methods).toContain('PATCH')
    expect(methods).toContain('DELETE')
  })

  it('loads host tasks and audit records', async () => {
    const { client, fetchImpl } = clientWith(
      mockResponse({
        tasks: [{ id: 'task-1' }],
        audit: [{ id: 'audit-1' }],
        task: { id: 'task-1' },
        events: [{ id: 'event-1' }],
      }),
    )

    expect(await client.nodeTasks('c1', 'n1')).toHaveLength(1)
    expect(await client.nodeAudit('c1', 'n1')).toHaveLength(1)
    expect(await client.nodeTask('task-1')).toEqual({
      task: { id: 'task-1' },
      events: [{ id: 'event-1' }],
    })

    const urls = fetchImpl.mock.calls.map((c) => String(c[0]))
    expect(urls).toContain('https://fleet.local/api/v1/tasks?cluster=c1&member=n1&limit=50')
    expect(urls).toContain('https://fleet.local/api/v1/audit?cluster=c1&member=n1&limit=50')
    expect(urls).toContain('https://fleet.local/api/v1/tasks/task-1')
  })

  it('lists agents, creates enrollment tokens, and bootstraps', async () => {
    const { client, fetchImpl } = clientWith(
      mockResponse({
        agents: [{ id: 'agent-1', cluster: 'c1', member: 'n1', status: 'online' }],
        token: 'enroll-token',
        result: { ok: true, task_id: 't1', mode: 'bootstrap', steps: ['install'] },
      }),
    )

    expect(await client.listAgents()).toHaveLength(1)
    expect((await client.createAgentEnrollmentToken('c1', 'n1')).token).toBe('enroll-token')
    expect((await client.bootstrapAgent('agent-1', { mode: 'join', address: '198.51.100.11' })).ok).toBe(true)

    const urls = fetchImpl.mock.calls.map((call) => String(call[0]))
    expect(urls).toContain('https://fleet.local/api/v1/agents')
    expect(urls).toContain('https://fleet.local/api/v1/agents/enrollment-tokens')
    expect(urls).toContain('https://fleet.local/api/v1/agents/agent-1/bootstrap')
  })

  it('queries the external PromQL proxy', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({
      status: 'success',
      data: { resultType: 'matrix', result: [{ metric: { __name__: 'fleet_host_load' }, values: [[1, '1.5']] }] },
    }))
    const series = await client.metricQueryRange('fleet_host_load', '1h', '30s')
    expect(series[0]?.name).toBe('fleet_host_load')
    expect(series[0]?.points[0]?.value).toBe(1.5)
    expect(String(fetchImpl.mock.calls[0]?.[0])).toContain('/api/v1/metrics/query_range?')
  })

  it('loads host metrics from the center buffer', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({ series: [{ name: 'fleet_host_load' }] }))
    const series = await client.hostMetrics('c1', 'n1')
    expect(series).toHaveLength(1)
    expect(String(fetchImpl.mock.calls[0]?.[0])).toContain(
      '/api/v1/metrics/query?cluster=c1&member=n1&from=1h',
    )
  })

  it('provisions a new cluster member', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({ member: { name: 'n2' }, steps: [] }))

    const result = await client.joinClusterMember('c2', {
      address: '198.51.100.11',
      install_incus: true,
      install_vms: false,
      ignore_host_key: true,
      ssh_user: 'node-admin',
    })

    expect(result.member.name).toBe('n2')
    const [url, init] = fetchImpl.mock.calls[0] ?? []
    expect(String(url)).toBe('https://fleet.local/api/v1/clusters/c2/members')
    expect((init as RequestInit).method).toBe('POST')
    expect(String((init as RequestInit).body)).toContain('198.51.100.11')
  })

  it('hits traffic and cluster registration endpoints', async () => {
    const { client, fetchImpl } = clientWith(mockResponse({ rx_bytes: 1, tx_bytes: 2 }))

    const traffic = await client.clusterTraffic('c1')
    expect(traffic).toMatchObject({ rx_bytes: 1, tx_bytes: 2 })

    await client.registerCluster({
      name: 'c64',
      endpoint: 'https://10.0.0.1:8443',
      tls_cert: 'cert',
      tls_key: 'key',
    })
    await client.unregisterCluster('c64')

    const urls = fetchImpl.mock.calls.map((c) => String(c[0]))
    expect(urls).toContain('https://fleet.local/api/v1/clusters/c1/traffic')
    expect(urls).toContain('https://fleet.local/api/v1/clusters')
    expect(urls).toContain('https://fleet.local/api/v1/clusters/c64')
  })
})

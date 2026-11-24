import { describe, expect, it, vi } from 'vitest'

import { ApiClient } from './client'

function mockResponse(body: unknown, status = 200): Response {
  const text = body === undefined ? '' : typeof body === 'string' ? body : JSON.stringify(body)
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status < 300 ? 'OK' : '',
    async text() {
      return text
    },
    async json() {
      return JSON.parse(text) as unknown
    },
    async blob() {
      return new Blob([text], { type: 'image/png' })
    },
  } as Response
}

function clientWith(response: Response) {
  const fetchImpl = vi.fn<typeof fetch>(async () => response)
  return {
    fetchImpl,
    client: new ApiClient({ baseURL: 'https://fleet.local/', fetchImpl }),
  }
}

// Thin wrappers are easy to leave untested because the dialogs that call them
// are covered separately. Exercise the remaining paths here so a renamed
// route or a dropped encodeURIComponent fails this suite instead of production.
describe('ApiClient endpoint wrappers', () => {
  it('reaches session, preview, desktop, overlay, member and migration routes', async () => {
    const { client, fetchImpl } = clientWith(
      mockResponse({
        sessions: [],
        revoked: 2,
        instances: [],
        unavailable_clusters: ['c2'],
        content: 'x',
        file_name: 'user-data',
        profile: 'debian-preseed',
        steps: [],
        overlays: [],
      }),
    )

    await client.listSessions('u 1')
    await client.revokeUserSessions('u 1')
    await client.listInstancesWithErrors('virtual-machine')
    await client.previewAnswerProfile({ name: 'vm', profile: 'debian-preseed' })
    await client.previewUnattendedAnswer('c1', 'default', 'debian', {
      name: 'vm',
      profile: 'debian-preseed',
    })
    await client.launchUnattended('c1', 'default', 'debian', {
      name: 'vm',
      profile: 'debian-preseed',
    })
    await client.prepareDesktop('c1', 'vm')
    await client.sendDesktopInput('c1', 'vm', {} as never)
    await client.installGuestAgent('c1', 'vm', {} as never)
    await client.renameInstance('c1', 'vm', 'vm-renamed')
    await client.listOverlays('c1')
    await client.getOverlay('c1', 'overlay 1')
    await client.enableOverlay('c1', {} as never)
    await client.updateOverlay('c1', 'overlay 1', {} as never)
    await client.reconcileOverlay('c1', 'overlay 1')
    await client.disableOverlay('c1', 'overlay 1')
    await client.deleteClusterMember('c1', 'node 1', true)
    await client.cancelMigration('m 1')
    await client.retryMigration('m 1')

    expect(fetchImpl).toHaveBeenCalledTimes(19)
    const urls = fetchImpl.mock.calls.map((call) => String(call[0]))
    expect(urls).toContain('https://fleet.local/api/v1/auth/sessions?user_id=u%201')
    expect(urls).toContain('https://fleet.local/api/v1/answer-profiles/preview')
    expect(urls).toContain('https://fleet.local/api/v1/clusters/c1/overlay/overlay%201/reconcile')
    expect(urls).toContain('https://fleet.local/api/v1/clusters/c1/members/node%201?force=true')
    expect(urls).toContain('https://fleet.local/api/v1/migrations/m%201/retry')
  })

  it('downloads a desktop screenshot as a blob', async () => {
    const { client, fetchImpl } = clientWith(mockResponse('png-bytes'))
    client.setBearerToken('token-1')

    const blob = await client.desktopScreenshot('c1', 'vm 1')

    expect(blob.size).toBeGreaterThan(0)
    const [url, init] = fetchImpl.mock.calls[0] ?? []
    expect(url).toBe(
      'https://fleet.local/api/v1/clusters/c1/instances/vm%201/desktop/screenshot',
    )
    expect((init as RequestInit).headers).toMatchObject({
      Accept: 'image/png',
      Authorization: 'Bearer token-1',
    })
  })
})

import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { clusterSummary } from '@/test/fixtures'
import { useClustersStore } from './clusters'

describe('clusters store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('fetches clusters and computes online counts', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([
      clusterSummary(),
      clusterSummary({ name: 'c2', display_name: 'Cluster 63', online: false }),
    ])
    const store = useClustersStore()

    await store.fetchAll()

    expect(store.items).toHaveLength(2)
    expect(store.onlineCount).toBe(1)
    expect(store.loading).toBe(false)
    expect(store.error).toBe('')
    expect(store.lastUpdated).toBeInstanceOf(Date)
    expect(store.byName('c2')?.display_name).toBe('Cluster 63')
    expect(store.byName('missing')).toBeUndefined()
  })

  it('records list errors', async () => {
    vi.mocked(api.listClusters).mockRejectedValue(new Error('control plane down'))
    const store = useClustersStore()

    await store.fetchAll()

    expect(store.error).toBe('control plane down')
    expect(store.loading).toBe(false)
  })

  it('replaces a cluster after reconnect', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([clusterSummary({ online: false })])
    vi.mocked(api.reconnectCluster).mockResolvedValue(
      clusterSummary({ online: true, server_name: 'reconnected' }),
    )
    const store = useClustersStore()
    await store.fetchAll()

    const summary = await store.reconnect('c1')

    expect(summary.online).toBe(true)
    expect(store.items).toHaveLength(1)
    expect(store.items[0]?.server_name).toBe('reconnected')
    expect(store.reconnecting).toBe('')
    expect(store.lastUpdated).toBeInstanceOf(Date)
  })

  it('appends a cluster returned by reconnect when it was not cached', async () => {
    vi.mocked(api.reconnectCluster).mockResolvedValue(clusterSummary({ name: 'c2' }))
    const store = useClustersStore()

    await store.reconnect('c2')

    expect(store.items.map((cluster) => cluster.name)).toEqual(['c2'])
  })

  it('records reconnect failures and resets the in-flight marker', async () => {
    vi.mocked(api.reconnectCluster).mockRejectedValue(new Error('refused'))
    const store = useClustersStore()

    await expect(store.reconnect('c1')).rejects.toThrow('refused')

    expect(store.error).toBe('refused')
    expect(store.reconnecting).toBe('')
  })
})

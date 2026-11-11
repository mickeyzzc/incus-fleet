import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { instanceDetail, instanceSummary } from '@/test/fixtures'
import { useInstancesStore } from './instances'

describe('instances store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('fetches all instances and computes counts', async () => {
    vi.mocked(api.listInstances).mockResolvedValue([
      instanceSummary(),
      instanceSummary({ name: 'vm1', type: 'virtual-machine' }),
    ])
    const store = useInstancesStore()

    await store.fetchAll()

    expect(api.listInstances).toHaveBeenCalledWith('any')
    expect(store.items).toHaveLength(2)
    expect(store.containers).toBe(1)
    expect(store.virtualMachines).toBe(1)
    expect(store.filter).toBe('any')
    expect(store.lastUpdated).toBeInstanceOf(Date)
    expect(store.byCluster('c1')).toHaveLength(2)
    expect(store.byCluster('c2')).toEqual([])
  })

  it('updates the active type filter and passes it to the API', async () => {
    vi.mocked(api.listInstances).mockResolvedValue([])
    const store = useInstancesStore()

    await store.fetchAll('virtual-machine')

    expect(api.listInstances).toHaveBeenCalledWith('virtual-machine')
    expect(store.filter).toBe('virtual-machine')
  })

  it('records list errors', async () => {
    vi.mocked(api.listInstances).mockRejectedValue(new Error('boom'))
    const store = useInstancesStore()

    await store.fetchAll()

    expect(store.error).toBe('boom')
    expect(store.loading).toBe(false)
  })

  it('fetches one cluster of instances', async () => {
    vi.mocked(api.listClusterInstances).mockResolvedValue([instanceSummary()])
    const store = useInstancesStore()

    await store.fetchCluster('c1', 'container')

    expect(api.listClusterInstances).toHaveBeenCalledWith('c1', 'container')
    expect(store.items).toHaveLength(1)
    expect(store.lastUpdated).toBeInstanceOf(Date)
  })

  it('records cluster list errors', async () => {
    vi.mocked(api.listClusterInstances).mockRejectedValue(new Error('cluster offline'))
    const store = useInstancesStore()

    await store.fetchCluster('c1')

    expect(store.error).toBe('cluster offline')
    expect(store.loading).toBe(false)
  })

  it('caches instance details', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    const store = useInstancesStore()

    const detail = await store.fetchDetail('c1', 'web')

    expect(api.getInstance).toHaveBeenCalledWith('c1', 'web')
    expect(detail?.hostname).toBe('web')
    expect(store.detail('c1', 'web')?.os).toBe('Debian GNU/Linux')
    expect(store.detail('c2', 'web')).toBeUndefined()
    expect(store.detailLoading).toBe(false)
  })

  it('returns null and records detail errors', async () => {
    vi.mocked(api.getInstance).mockRejectedValue(new Error('not found'))
    const store = useInstancesStore()

    await expect(store.fetchDetail('c1', 'missing')).resolves.toBeNull()

    expect(store.error).toBe('not found')
    expect(store.detailLoading).toBe(false)
  })
})

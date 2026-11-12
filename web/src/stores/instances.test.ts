import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { ClusterResources, InstanceImage } from '@/api/types'
import { instanceDetail, instanceSummary } from '@/test/fixtures'
import { useInstancesStore } from './instances'

function sampleImage(): InstanceImage {
  return {
    fingerprint: '053059ea4e28',
    aliases: ['debian'],
    type: 'container',
    architecture: 'x86_64',
    os: 'Debian',
    release: 'trixie',
    description: 'Debian trixie',
    size: 100,
    cached: false,
    created_at: '2026-11-01T00:00:00Z',
  }
}

function sampleResources(): ClusterResources {
  return {
    storage_pools: [
      {
        name: 'default',
        driver: 'dir',
        source: '/srv/incus',
        status: 'Created',
        used_by: ['/1.0/instances/web'],
        locations: [],
        total: 1000,
        used: 250,
      },
    ],
    networks: [
      {
        name: 'incusbr0',
        type: 'bridge',
        managed: true,
        ipv4: '10.0.0.0.1/24',
        ipv6: 'none',
        used_by: ['/1.0/instances/web'],
        locations: [],
      },
    ],
  }
}

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

  it('caches cluster images until a forced refresh', async () => {
    vi.mocked(api.listImages).mockResolvedValue([sampleImage()])
    const store = useInstancesStore()

    await store.fetchImages('c1')
    await store.fetchImages('c1')
    expect(api.listImages).toHaveBeenCalledTimes(1)
    expect(store.images('c1')).toHaveLength(1)
    expect(store.imagesLoading).toBe(false)

    await store.fetchImages('c1', true)
    expect(api.listImages).toHaveBeenCalledTimes(2)
  })

  it('records image load errors', async () => {
    vi.mocked(api.listImages).mockRejectedValue(new Error('images offline'))
    const store = useInstancesStore()

    await store.fetchImages('c1')

    expect(store.error).toBe('images offline')
    expect(store.images('c1')).toEqual([])
    expect(store.imagesLoading).toBe(false)
  })

  it('caches cluster resources until a forced refresh', async () => {
    vi.mocked(api.getResources).mockResolvedValue(sampleResources())
    const store = useInstancesStore()

    await store.fetchResources('c1')
    await store.fetchResources('c1')
    expect(api.getResources).toHaveBeenCalledTimes(1)
    expect(store.resources('c1')?.storage_pools).toHaveLength(1)
    expect(store.resourcesLoading).toBe(false)

    await store.fetchResources('c1', true)
    expect(api.getResources).toHaveBeenCalledTimes(2)
  })

  it('records resource load errors', async () => {
    vi.mocked(api.getResources).mockRejectedValue(new Error('resources offline'))
    const store = useInstancesStore()

    await store.fetchResources('c1')

    expect(store.error).toBe('resources offline')
    expect(store.resources('c1')).toBeUndefined()
    expect(store.resourcesLoading).toBe(false)
  })

  it('creates an instance and caches its detail', async () => {
    vi.mocked(api.createInstance).mockResolvedValue(instanceDetail({ name: 'newct' }))
    const store = useInstancesStore()

    const detail = await store.createInstance('c1', { name: 'newct', image: 'debian' })

    expect(api.createInstance).toHaveBeenCalledWith('c1', { name: 'newct', image: 'debian' })
    expect(detail.name).toBe('newct')
    expect(store.detail('c1', 'newct')?.name).toBe('newct')
    expect(store.saving).toBe(false)
  })

  it('records create errors', async () => {
    vi.mocked(api.createInstance).mockRejectedValue(new Error('create failed'))
    const store = useInstancesStore()

    await expect(store.createInstance('c1', { name: 'newct', image: 'debian' })).rejects.toThrow(
      'create failed',
    )

    expect(store.error).toBe('create failed')
    expect(store.saving).toBe(false)
  })

  it('updates an instance and refreshes cached detail', async () => {
    vi.mocked(api.updateInstance).mockResolvedValue(instanceDetail({ description: 'after' }))
    const store = useInstancesStore()

    const detail = await store.updateInstance('c1', 'web', { cpu: '4' })

    expect(api.updateInstance).toHaveBeenCalledWith('c1', 'web', { cpu: '4' })
    expect(detail?.description).toBe('after')
    expect(store.detail('c1', 'web')?.description).toBe('after')
    expect(store.saving).toBe(false)
  })

  it('records update errors', async () => {
    vi.mocked(api.updateInstance).mockRejectedValue(new Error('update failed'))
    const store = useInstancesStore()

    await expect(store.updateInstance('c1', 'web', { cpu: '4' })).rejects.toThrow('update failed')

    expect(store.error).toBe('update failed')
    expect(store.saving).toBe(false)
  })

  it('performs lifecycle actions and refreshes detail', async () => {
    vi.mocked(api.instanceAction).mockResolvedValue(undefined)
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail({ status: 'Running' }))
    const store = useInstancesStore()

    await store.instanceAction('c1', 'web', 'start')
    expect(api.instanceAction).toHaveBeenCalledWith('c1', 'web', { action: 'start', force: false })
    expect(api.getInstance).toHaveBeenCalledWith('c1', 'web')
    expect(store.detail('c1', 'web')?.status).toBe('Running')

    await store.instanceAction('c1', 'web', 'stop', true)
    expect(api.instanceAction).toHaveBeenLastCalledWith('c1', 'web', {
      action: 'stop',
      force: true,
    })
  })

  it('records lifecycle action errors', async () => {
    vi.mocked(api.instanceAction).mockRejectedValue(new Error('action failed'))
    const store = useInstancesStore()

    await expect(store.instanceAction('c1', 'web', 'restart')).rejects.toThrow('action failed')

    expect(store.error).toBe('action failed')
  })

  it('deletes an instance and removes cached state', async () => {
    vi.mocked(api.deleteInstance).mockResolvedValue(undefined)
    const store = useInstancesStore()
    store.items = [instanceSummary(), instanceSummary({ name: 'vm1' })]
    store.details = { 'c1/web': instanceDetail() }

    await store.deleteInstance('c1', 'web', true)

    expect(api.deleteInstance).toHaveBeenCalledWith('c1', 'web', true)
    expect(store.items.map((item) => item.name)).toEqual(['vm1'])
    expect(store.detail('c1', 'web')).toBeUndefined()
  })

  it('records delete errors', async () => {
    vi.mocked(api.deleteInstance).mockRejectedValue(new Error('delete failed'))
    const store = useInstancesStore()

    await expect(store.deleteInstance('c1', 'web')).rejects.toThrow('delete failed')

    expect(store.error).toBe('delete failed')
  })
})

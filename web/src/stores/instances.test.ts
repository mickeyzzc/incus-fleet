import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { ClusterResources, InstanceImage, Profile } from '@/api/types'
import {
  instanceDetail,
  instanceImage,
  instanceSnapshot,
  instanceSummary,
  storageVolume,
} from '@/test/fixtures'
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
        ipv4: '10.0.0.1/24',
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

    await store.deleteInstance('c1', 'web', true, true)

    expect(api.deleteInstance).toHaveBeenCalledWith('c1', 'web', true, true)
    expect(store.items.map((item) => item.name)).toEqual(['vm1'])
    expect(store.detail('c1', 'web')).toBeUndefined()
  })

  it('records delete errors', async () => {
    vi.mocked(api.deleteInstance).mockRejectedValue(new Error('delete failed'))
    const store = useInstancesStore()

    await expect(store.deleteInstance('c1', 'web')).rejects.toThrow('delete failed')

    expect(store.error).toBe('delete failed')
  })

  it('caches profiles until a forced refresh', async () => {
    const profile: Profile = {
      name: 'web',
      description: 'web tier',
      config: { 'limits.cpu': '2' },
      devices: { eth0: { type: 'nic', network: 'incusbr0' } },
      used_by: ['/1.0/instances/web'],
    }
    vi.mocked(api.listProfiles).mockResolvedValue([profile])
    const store = useInstancesStore()

    await store.fetchProfiles('c1')
    await store.fetchProfiles('c1')
    expect(api.listProfiles).toHaveBeenCalledTimes(1)
    expect(store.profiles('c1')).toHaveLength(1)
    expect(store.profiles('c2')).toEqual([])
    expect(store.profilesLoading).toBe(false)

    await store.fetchProfiles('c1', true)
    expect(api.listProfiles).toHaveBeenCalledTimes(2)
  })

  it('records profile load errors', async () => {
    vi.mocked(api.listProfiles).mockRejectedValue(new Error('profiles offline'))
    const store = useInstancesStore()

    await store.fetchProfiles('c1')

    expect(store.error).toBe('profiles offline')
    expect(store.profiles('c1')).toEqual([])
    expect(store.profilesLoading).toBe(false)
  })

  it('mutates storage pools and invalidates cached resources', async () => {
    const store = useInstancesStore()
    store.resourceCache = { c1: sampleResources() }

    await store.createStoragePool('c1', { name: 'fast', driver: 'zfs' })
    expect(api.createStoragePool).toHaveBeenCalledWith('c1', { name: 'fast', driver: 'zfs' })
    expect(store.resources('c1')).toBeUndefined()

    store.resourceCache = { c1: sampleResources() }
    await store.updateStoragePool('c1', 'default', { description: 'updated' })
    expect(api.updateStoragePool).toHaveBeenCalledWith('c1', 'default', {
      description: 'updated',
    })
    expect(store.resources('c1')).toBeUndefined()

    store.resourceCache = { c1: sampleResources() }
    await store.deleteStoragePool('c1', 'fast')
    expect(api.deleteStoragePool).toHaveBeenCalledWith('c1', 'fast')
    expect(store.resources('c1')).toBeUndefined()
    expect(store.saving).toBe(false)
  })

  it('records storage pool mutation errors', async () => {
    vi.mocked(api.createStoragePool).mockRejectedValue(new Error('pool failed'))
    vi.mocked(api.updateStoragePool).mockRejectedValue(new Error('update failed'))
    vi.mocked(api.deleteStoragePool).mockRejectedValue(new Error('delete failed'))
    const store = useInstancesStore()

    await expect(store.createStoragePool('c1', { name: 'fast' })).rejects.toThrow('pool failed')
    expect(store.error).toBe('pool failed')
    await expect(store.updateStoragePool('c1', 'default', {})).rejects.toThrow('update failed')
    expect(store.error).toBe('update failed')
    await expect(store.deleteStoragePool('c1', 'fast')).rejects.toThrow('delete failed')
    expect(store.error).toBe('delete failed')
    expect(store.saving).toBe(false)
  })

  it('mutates networks and invalidates cached resources', async () => {
    const store = useInstancesStore()
    store.resourceCache = { c1: sampleResources() }

    await store.createNetwork('c1', { name: 'br-test', type: 'bridge' })
    expect(api.createNetwork).toHaveBeenCalledWith('c1', { name: 'br-test', type: 'bridge' })
    expect(store.resources('c1')).toBeUndefined()

    store.resourceCache = { c1: sampleResources() }
    await store.updateNetwork('c1', 'incusbr0', { description: 'updated' })
    expect(api.updateNetwork).toHaveBeenCalledWith('c1', 'incusbr0', { description: 'updated' })
    expect(store.resources('c1')).toBeUndefined()

    store.resourceCache = { c1: sampleResources() }
    await store.deleteNetwork('c1', 'br-test')
    expect(api.deleteNetwork).toHaveBeenCalledWith('c1', 'br-test')
    expect(store.resources('c1')).toBeUndefined()
  })

  it('records network mutation errors', async () => {
    vi.mocked(api.createNetwork).mockRejectedValue(new Error('network failed'))
    vi.mocked(api.updateNetwork).mockRejectedValue(new Error('update failed'))
    vi.mocked(api.deleteNetwork).mockRejectedValue(new Error('delete failed'))
    const store = useInstancesStore()

    await expect(store.createNetwork('c1', { name: 'br-test' })).rejects.toThrow('network failed')
    expect(store.error).toBe('network failed')
    await expect(store.updateNetwork('c1', 'incusbr0', {})).rejects.toThrow('update failed')
    expect(store.error).toBe('update failed')
    await expect(store.deleteNetwork('c1', 'br-test')).rejects.toThrow('delete failed')
    expect(store.error).toBe('delete failed')
  })

  it('loads and mutates overlays', async () => {
    const store = useInstancesStore()
    const status = {
      cluster: 'c1',
      name: 'c1',
      bridge: 'fleetbr0',
      vni: 6200,
      subnet: '198.51.100.0/24',
      gateway_member: 'node-1',
      mtu: 1450,
      enabled: true,
      members: [],
      tunnels: [],
    }
    vi.mocked(api.listOverlays).mockResolvedValue([status])
    vi.mocked(api.enableOverlay).mockResolvedValue(status)
    vi.mocked(api.reconcileOverlay).mockResolvedValue(status)
    vi.mocked(api.disableOverlay).mockResolvedValue(undefined)

    const loaded = await store.fetchOverlays('c1')
    expect(loaded).toHaveLength(1)
    expect(api.listOverlays).toHaveBeenCalledWith('c1')
    expect(store.overlayLoading).toBe(false)

    // A cached read does not hit the API again.
    await store.fetchOverlays('c1')
    expect(api.listOverlays).toHaveBeenCalledTimes(1)

    await store.enableOverlay('c1', { name: 'c1', subnet: '198.51.100.0/24' })
    expect(api.enableOverlay).toHaveBeenCalledWith('c1', { name: 'c1', subnet: '198.51.100.0/24' })
    expect(store.overlayCache.c1).toBeUndefined()

    await store.reconcileOverlay('c1', 'c1')
    expect(api.reconcileOverlay).toHaveBeenCalledWith('c1', 'c1')

    await store.disableOverlay('c1', 'c1')
    expect(api.disableOverlay).toHaveBeenCalledWith('c1', 'c1')
  })

  it('records overlay errors', async () => {
    vi.mocked(api.listOverlays).mockRejectedValue(new Error('list failed'))
    vi.mocked(api.enableOverlay).mockRejectedValue(new Error('enable failed'))
    vi.mocked(api.reconcileOverlay).mockRejectedValue(new Error('reconcile failed'))
    vi.mocked(api.disableOverlay).mockRejectedValue(new Error('disable failed'))
    const store = useInstancesStore()

    await expect(store.fetchOverlays('c1', true)).rejects.toThrow('list failed')
    expect(store.error).toBe('list failed')
    await expect(store.enableOverlay('c1', {})).rejects.toThrow('enable failed')
    expect(store.error).toBe('enable failed')
    await expect(store.reconcileOverlay('c1', 'c1')).rejects.toThrow('reconcile failed')
    expect(store.error).toBe('reconcile failed')
    await expect(store.disableOverlay('c1', 'c1')).rejects.toThrow('disable failed')
    expect(store.error).toBe('disable failed')
  })

  it('mutates profiles and invalidates the profile cache', async () => {
    const store = useInstancesStore()
    store.profileCache = { c1: [] }

    await store.createProfile('c1', { name: 'web' })
    expect(api.createProfile).toHaveBeenCalledWith('c1', { name: 'web' })
    expect(api.listProfiles).not.toHaveBeenCalled()
    expect(store.profileCache.c1).toBeUndefined()

    store.profileCache = { c1: [] }
    await store.updateProfile('c1', 'web', { description: 'updated' })
    expect(api.updateProfile).toHaveBeenCalledWith('c1', 'web', { description: 'updated' })
    expect(store.profileCache.c1).toBeUndefined()

    store.profileCache = { c1: [] }
    await store.deleteProfile('c1', 'web')
    expect(api.deleteProfile).toHaveBeenCalledWith('c1', 'web')
    expect(store.profileCache.c1).toBeUndefined()
  })

  it('records profile mutation errors', async () => {
    vi.mocked(api.createProfile).mockRejectedValue(new Error('profile failed'))
    vi.mocked(api.updateProfile).mockRejectedValue(new Error('update failed'))
    vi.mocked(api.deleteProfile).mockRejectedValue(new Error('delete failed'))
    const store = useInstancesStore()

    await expect(store.createProfile('c1', { name: 'web' })).rejects.toThrow('profile failed')
    expect(store.error).toBe('profile failed')
    await expect(store.updateProfile('c1', 'web', {})).rejects.toThrow('update failed')
    expect(store.error).toBe('update failed')
    await expect(store.deleteProfile('c1', 'web')).rejects.toThrow('delete failed')
    expect(store.error).toBe('delete failed')
  })

  it('updates member metadata', async () => {
    vi.mocked(api.updateMember).mockResolvedValue(undefined)
    const store = useInstancesStore()

    await store.updateMember('c1', 'node-1', {
      description: 'primary',
      groups: ['site-a'],
    })

    expect(api.updateMember).toHaveBeenCalledWith('c1', 'node-1', {
      description: 'primary',
      groups: ['site-a'],
    })
    expect(store.saving).toBe(false)
  })

  it('records member update errors', async () => {
    vi.mocked(api.updateMember).mockRejectedValue(new Error('member failed'))
    const store = useInstancesStore()

    await expect(store.updateMember('c1', 'node-1', {})).rejects.toThrow('member failed')

    expect(store.error).toBe('member failed')
    expect(store.saving).toBe(false)
  })

  it('fetches, creates, restores, and deletes snapshots', async () => {
    vi.mocked(api.listSnapshots).mockResolvedValue([instanceSnapshot()])
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    vi.mocked(api.createSnapshot).mockResolvedValue(undefined)
    vi.mocked(api.restoreSnapshot).mockResolvedValue(undefined)
    vi.mocked(api.deleteSnapshot).mockResolvedValue(undefined)
    const store = useInstancesStore()

    await store.fetchSnapshots('c1', 'web')
    await store.fetchSnapshots('c1', 'web')
    expect(api.listSnapshots).toHaveBeenCalledTimes(1)
    expect(store.snapshots('c1', 'web')).toHaveLength(1)

    await store.createSnapshot('c1', 'web', { name: 'snap1', stateful: true })
    expect(api.createSnapshot).toHaveBeenCalledWith('c1', 'web', { name: 'snap1', stateful: true })
    expect(api.listSnapshots).toHaveBeenCalledTimes(2)

    await store.restoreSnapshot('c1', 'web', 'snap0')
    expect(api.restoreSnapshot).toHaveBeenCalledWith('c1', 'web', 'snap0')
    expect(api.getInstance).toHaveBeenCalledWith('c1', 'web')

    await store.deleteSnapshot('c1', 'web', 'snap0')
    expect(api.deleteSnapshot).toHaveBeenCalledWith('c1', 'web', 'snap0')
    expect(api.listSnapshots).toHaveBeenCalledTimes(3)
  })

  it('records snapshot errors and surfaces them on the store', async () => {
    vi.mocked(api.listSnapshots).mockRejectedValue(new Error('snapshots offline'))
    vi.mocked(api.createSnapshot).mockRejectedValue(new Error('snapshot failed'))
    const store = useInstancesStore()

    await store.fetchSnapshots('c1', 'web')
    expect(store.error).toBe('snapshots offline')
    expect(store.snapshots('c1', 'web')).toEqual([])

    await expect(store.createSnapshot('c1', 'web', { name: 'x' })).rejects.toThrow('snapshot failed')
    expect(store.error).toBe('snapshot failed')
  })

  it('fetches, creates, and deletes custom volumes', async () => {
    vi.mocked(api.listVolumes).mockResolvedValue([storageVolume()])
    vi.mocked(api.createVolume).mockResolvedValue(undefined)
    vi.mocked(api.deleteVolume).mockResolvedValue(undefined)
    const store = useInstancesStore()

    await store.fetchVolumes('c1', 'default')
    await store.fetchVolumes('c1', 'default')
    expect(api.listVolumes).toHaveBeenCalledTimes(1)
    expect(store.volumes('c1', 'default')).toHaveLength(1)

    await store.createVolume('c1', 'default', { name: 'assets', size: '5GiB' })
    expect(api.createVolume).toHaveBeenCalledWith('c1', 'default', { name: 'assets', size: '5GiB' })
    expect(api.listVolumes).toHaveBeenCalledTimes(2)

    await store.deleteVolume('c1', 'default', 'assets')
    expect(api.deleteVolume).toHaveBeenCalledWith('c1', 'default', 'assets')
    expect(api.listVolumes).toHaveBeenCalledTimes(3)

    vi.mocked(api.deleteVolume).mockRejectedValue(new Error('volume in use'))
    await expect(store.deleteVolume('c1', 'default', 'assets')).rejects.toThrow('volume in use')
  })

  it('attaches and detaches devices into the cached detail', async () => {
    vi.mocked(api.attachDevice).mockResolvedValue(instanceDetail({ devices: { data: { type: 'disk', path: '/data' } } }))
    vi.mocked(api.detachDevice).mockResolvedValue(instanceDetail())
    const store = useInstancesStore()

    const attached = await store.attachDevice('c1', 'web', {
      name: 'data',
      type: 'disk',
      path: '/data',
    })
    expect(api.attachDevice).toHaveBeenCalledWith('c1', 'web', { name: 'data', type: 'disk', path: '/data' })
    expect(store.detail('c1', 'web')?.devices.data).toBeDefined()

    const detached = await store.detachDevice('c1', 'web', 'data')
    expect(api.detachDevice).toHaveBeenCalledWith('c1', 'web', 'data')
    expect(detached.name).toBe('web')
    expect(attached.name).toBe('web')

    vi.mocked(api.attachDevice).mockRejectedValue(new Error('attach failed'))
    await expect(store.attachDevice('c1', 'web', { name: 'x', type: 'disk', path: '/x' })).rejects.toThrow('attach failed')
  })

  it('deletes an image and invalidates the image cache', async () => {
    vi.mocked(api.listImages).mockResolvedValue([instanceImage()])
    vi.mocked(api.deleteImage).mockResolvedValue(undefined)
    const store = useInstancesStore()

    await store.fetchImages('c1')
    await store.deleteImage('c1', '053059ea4e28')
    expect(api.deleteImage).toHaveBeenCalledWith('c1', '053059ea4e28')
    expect(store.imageCache.c1).toBeUndefined()
  })

  it('imports and distributes images with cache invalidation', async () => {
    vi.mocked(api.listImages).mockResolvedValue([])
    vi.mocked(api.importImage).mockResolvedValue('abc123')
    vi.mocked(api.copyImage).mockResolvedValue(undefined)
    const store = useInstancesStore()

    await store.fetchImages('c1')
    const fingerprint = await store.importImage('c1', {
      file: new File(['tarball'], 'debian13.tar'),
      type: 'container',
      alias: 'debian/13',
    })
    expect(fingerprint).toBe('abc123')
    expect(api.importImage).toHaveBeenCalledWith('c1', {
      file: expect.any(File),
      type: 'container',
      alias: 'debian/13',
    })
    expect(store.imageCache.c1).toBeUndefined()

    await store.fetchImages('c2')
    await store.copyImage('c1', 'abc123', { target_cluster: 'c2' })
    expect(api.copyImage).toHaveBeenCalledWith('c1', 'abc123', { target_cluster: 'c2' })
    expect(store.imageCache.c1).toBeUndefined()
    expect(store.imageCache.c2).toBeUndefined()

    vi.mocked(api.importImage).mockRejectedValue(new Error('quota'))
    await expect(
      store.importImage('c1', { file: new File(['x'], 'x.tar'), type: 'container' }),
    ).rejects.toThrow('quota')
  })

  it('manages ISO volumes, ops, rebuild, refresh, and clones', async () => {
    vi.mocked(api.listISOs).mockResolvedValue([
      { name: 'alpine', pool: 'default', description: 'installer' },
    ])
    vi.mocked(api.importISO).mockResolvedValue({ name: 'debian', pool: 'default' })
    vi.mocked(api.deleteISO).mockResolvedValue(undefined)
    vi.mocked(api.launchFromISO).mockResolvedValue({
      name: 'installer-01',
      cluster: 'c1',
      type: 'virtual-machine',
      status: 'Running',
    } as never)
    vi.mocked(api.runOps).mockResolvedValue({ exit_code: 0, stdout: 'ok', stderr: '' })
    vi.mocked(api.rebuildInstance).mockResolvedValue({ name: 'web' } as never)
    vi.mocked(api.getInstance).mockResolvedValue({ name: 'web' } as never)
    vi.mocked(api.refreshImage).mockResolvedValue(undefined)
    vi.mocked(api.cloneInstance).mockResolvedValue({
      name: 'web-clone',
      cluster: 'c1',
      type: 'container',
      status: 'Stopped',
    } as never)
    const store = useInstancesStore()

    await store.fetchISOs('c1')
    expect(store.isoCache.c1).toHaveLength(1)

    const name = await store.importISO('c1', { file: new File(['x'], 'debian.iso'), pool: 'default' })
    expect(name).toBe('debian')
    expect(store.isoCache.c1).toBeUndefined()

    await store.fetchISOs('c1')
    await store.deleteISO('c1', 'default', 'alpine')
    expect(store.isoCache.c1).toBeUndefined()

    const launched = await store.launchFromISO('c1', 'default', 'debian', { name: 'installer-01' })
    expect(launched?.name).toBe('installer-01')

    const result = await store.runOps('c1', 'web', { action: 'run-script', script: 'echo' })
    expect(result.stdout).toBe('ok')

    await store.fetchDetail('c1', 'web').catch(() => {})
    await store.rebuildInstance('c1', 'web', 'debian/13')
    expect(api.rebuildInstance).toHaveBeenCalledWith('c1', 'web', 'debian/13')

    await store.refreshImage('c1', 'abc123')
    expect(api.refreshImage).toHaveBeenCalledWith('c1', 'abc123')

    const cloned = await store.cloneInstance('c1', 'web', { target: 'web-clone' })
    expect(cloned?.name).toBe('web-clone')

    vi.mocked(api.runOps).mockRejectedValue(new Error('agent down'))
    await expect(store.runOps('c1', 'web', { action: 'run-script', script: 'x' })).rejects.toThrow(
      'agent down',
    )
  })
})

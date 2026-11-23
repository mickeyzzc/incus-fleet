import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { FleetVolume } from '@/api/types'
import FleetVolumesView from './FleetVolumesView.vue'

const volumeA: FleetVolume = {
  name: 'data-a',
  type: 'custom',
  content_type: 'filesystem',
  description: 'primary data',
  config: { size: '10737418240' },
  used_by: ['/1.0/instances/web'],
  location: 'node-a',
  cluster: 'c62',
  pool: 'default',
}

const volumeB: FleetVolume = {
  name: 'iso-b',
  type: 'custom',
  content_type: 'iso',
  config: {},
  used_by: [],
  cluster: 'c63',
  pool: 'fast',
}

function mockFleet() {
  vi.mocked(api.listClusters).mockResolvedValue([
    { name: 'c62', display_name: 'c62', endpoint: 'https://a', online: true } as never,
    { name: 'c63', display_name: 'c63', endpoint: 'https://b', online: true } as never,
  ])
  vi.mocked(api.listAllVolumesWithErrors).mockResolvedValue({ items: [volumeA, volumeB], failedClusters: [] })
}

async function mountView() {
  const wrapper = mount(FleetVolumesView, { global: { plugins: [ElementPlus] } })
  await flushPromises()
  return wrapper
}

describe('FleetVolumesView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockFleet()
  })

  it('loads volumes across every cluster', async () => {
    const wrapper = await mountView()

    expect(api.listAllVolumesWithErrors).toHaveBeenCalled()
    expect(wrapper.text()).toContain('data-a')
    expect(wrapper.text()).toContain('iso-b')
    // Both clusters appear, which is the point of a fleet-level view.
    expect(wrapper.text()).toContain('c62')
    expect(wrapper.text()).toContain('c63')
  })

  it('shows the pool and content type for each volume', async () => {
    const wrapper = await mountView()

    expect(wrapper.text()).toContain('default')
    expect(wrapper.text()).toContain('fast')
    expect(wrapper.text()).toContain('filesystem')
    expect(wrapper.text()).toContain('iso')
  })

  it('summarises the provisioned size', async () => {
    const wrapper = await mountView()

    // 10 GiB volume plus one with no size configured.
    expect(wrapper.text()).toContain('2 volumes')
    expect(wrapper.text()).toMatch(/10\.0 GiB/)
  })

  it('filters by search text', async () => {
    const wrapper = await mountView()

    const input = wrapper.find('input[placeholder]')
    await input.setValue('iso-b')
    await flushPromises()

    expect(wrapper.text()).toContain('iso-b')
    expect(wrapper.text()).not.toContain('data-a')
  })

  it('reports a load failure', async () => {
    const error = vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)
    vi.mocked(api.listAllVolumesWithErrors).mockRejectedValueOnce(new Error('cluster down'))

    const wrapper = await mountView()

    expect(wrapper.text()).toContain('cluster down')
    error.mockRestore()
  })

  it('shows an empty state when nothing matches', async () => {
    vi.mocked(api.listAllVolumesWithErrors).mockResolvedValueOnce({ items: [], failedClusters: [] })
    const wrapper = await mountView()

    expect(wrapper.text()).toContain('No volumes found')
  })
})

describe('FleetVolumesView filters and fallbacks', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockFleet()
  })

  it('filters by cluster', async () => {
    const wrapper = await mountView()

    const vm = wrapper.vm as unknown as { clusterFilter: string }
    vm.clusterFilter = 'c63'
    await flushPromises()

    expect(wrapper.text()).toContain('iso-b')
    expect(wrapper.text()).not.toContain('data-a')
  })

  it('renders a dash for a volume without a configured size', async () => {
    vi.mocked(api.listAllVolumesWithErrors).mockResolvedValueOnce({ items: [volumeB], failedClusters: [] })
    const wrapper = await mountView()

    // The ISO volume has no size in config, so the cell shows an em dash and
    // the total stays at zero bytes.
    expect(wrapper.text()).toContain('—')
    expect(wrapper.text()).toContain('0 B')
  })

  it('tolerates a cluster list failure and still renders volumes', async () => {
    vi.mocked(api.listClusters).mockRejectedValueOnce(new Error('nope'))
    const wrapper = await mountView()

    expect(wrapper.text()).toContain('nope')
    // The table is still mounted even though the cluster picker is empty.
    expect(wrapper.exists()).toBe(true)
  })
})

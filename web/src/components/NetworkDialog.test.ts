import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { useInstancesStore } from '@/stores/instances'
import NetworkDialog from './NetworkDialog.vue'

import type { NetworkResource } from '@/api/types'

function network(overrides: Partial<NetworkResource> = {}): NetworkResource {
  return {
    name: 'incusbr0',
    type: 'bridge',
    managed: true,
    description: 'main bridge',
    config: { 'ipv4.address': '10.0.0.1/24' },
    used_by: [],
    locations: [],
    ...overrides,
  }
}

function mountDialog(props: { network?: NetworkResource | null } = {}) {
  return mount(NetworkDialog, {
    props: { modelValue: true, cluster: 'c1', ...props },
    global: { plugins: [ElementPlus] },
  })
}

describe('NetworkDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.createNetwork).mockResolvedValue(undefined)
    vi.mocked(api.updateNetwork).mockResolvedValue(undefined)
  })

  it('creates a managed network', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const inputs = wrapper.findAll('input')
    await inputs[0]?.setValue('fleet0')
    wrapper.findComponent({ name: 'ElSelect' }).vm.$emit('update:modelValue', 'bridge')
    await flushPromises()
    await inputs[1]?.setValue('fleet internal')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.createNetwork).toHaveBeenCalledWith('c1', {
      name: 'fleet0',
      type: 'bridge',
      description: 'fleet internal',
      config: undefined,
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Network fleet0 saved.')
    expect(wrapper.emitted('saved')).toEqual([['fleet0']])
  })

  it('validates name and type on create', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')

    expect(api.createNetwork).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter a name.')
  })

  it('updates an existing managed network', async () => {
    vi.mocked(api.getResources).mockResolvedValue({
      storage_pools: [],
      networks: [network()],
    })
    const store = useInstancesStore()
    await store.fetchResources('c1')

    const wrapper = mountDialog({ network: network() })
    await flushPromises()

    const inputs = wrapper.findAll('input')
    expect((inputs[0]?.element as HTMLInputElement).value).toBe('incusbr0')
    await inputs[0]?.setValue('renamed')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.updateNetwork).toHaveBeenCalledWith('c1', 'incusbr0', {
      description: 'main bridge',
      config: { 'ipv4.address': '10.0.0.1/24' },
    })
  })

  it('reports save failures', async () => {
    vi.mocked(api.createNetwork).mockRejectedValue(new Error('subnet overlaps'))
    const wrapper = mountDialog()
    await flushPromises()

    const inputs = wrapper.findAll('input')
    await inputs[0]?.setValue('fleet0')
    wrapper.findComponent({ name: 'ElSelect' }).vm.$emit('update:modelValue', 'bridge')
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('subnet overlaps')
  })
})

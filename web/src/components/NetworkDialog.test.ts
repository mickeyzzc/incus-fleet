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

  it('persists structured IPv4, IPv6, NAT, MTU and DNS fields', async () => {
    const wrapper = mountDialog({ network: network() })
    await flushPromises()

    await wrapper.find('input[placeholder="10.0.0.1/24"]').setValue('10.63.0.1/24')
    await wrapper.find('input[placeholder="fd42::1/64"]').setValue('fd42::1/64')
    await wrapper.find('input[placeholder="1500"]').setValue('1400')
    await wrapper.find('input[placeholder="fleet.internal"]').setValue('lab.internal')
    const radios = wrapper.findAllComponents({ name: 'ElRadioGroup' })
    radios[0]!.vm.$emit('update:modelValue', 'false')
    radios[1]!.vm.$emit('update:modelValue', 'true')
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.updateNetwork).toHaveBeenCalledWith('c1', 'incusbr0', {
      description: 'main bridge',
      config: {
        'ipv4.address': '10.63.0.1/24',
        'ipv4.nat': 'false',
        'ipv6.address': 'fd42::1/64',
        'ipv6.nat': 'true',
        'bridge.mtu': '1400',
        'dns.domain': 'lab.internal',
      },
    })
  })

  it('clears structured fields and cancels without saving', async () => {
    const wrapper = mountDialog({ network: network({ config: {
      'ipv4.address': '', 'ipv4.nat': 'true', 'ipv6.address': '', 'ipv6.nat': 'false',
      'bridge.mtu': '', 'dns.domain': '',
    } }) })
    await flushPromises()

    const radios = wrapper.findAllComponents({ name: 'ElRadioGroup' })
    radios[0]!.vm.$emit('update:modelValue', '')
    radios[1]!.vm.$emit('update:modelValue', '')
    await flushPromises()

    const cancel = wrapper.findAll('button').find((b) => b.text().includes('Cancel'))
    await cancel!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toContainEqual([false])
    expect(api.updateNetwork).not.toHaveBeenCalled()
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

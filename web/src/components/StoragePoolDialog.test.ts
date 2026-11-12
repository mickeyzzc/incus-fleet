import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { clusterResources } from '@/test/fixtures'
import { useInstancesStore } from '@/stores/instances'
import StoragePoolDialog from './StoragePoolDialog.vue'

import type { StoragePoolResource } from '@/api/types'

function pool(overrides: Partial<StoragePoolResource> = {}): StoragePoolResource {
  return {
    name: 'default',
    driver: 'dir',
    description: 'local disk',
    config: { source: '/srv/incus' },
    used_by: [],
    locations: [],
    total: 1,
    used: 0,
    ...overrides,
  }
}

function mountDialog(props: { pool?: StoragePoolResource | null } = {}) {
  return mount(StoragePoolDialog, {
    props: { modelValue: true, cluster: 'c1', ...props },
    global: { plugins: [ElementPlus] },
  })
}

describe('StoragePoolDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.createStoragePool).mockResolvedValue(undefined)
    vi.mocked(api.updateStoragePool).mockResolvedValue(undefined)
  })

  it('creates a pool with name, driver, description, and config', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const inputs = wrapper.findAll('input')
    await inputs[0]?.setValue('fast')
    // driver uses the stubbed select; set the bound model directly.
    const select = wrapper.findComponent({ name: 'ElSelect' })
    select.vm.$emit('update:modelValue', 'zfs')
    await flushPromises()
    await inputs[1]?.setValue('fast local ssd')

    const buttons = wrapper.findAll('button')
    const addEntry = buttons.find((b) => b.text().includes('Add entry'))
    await addEntry?.trigger('click')
    const kvInputs = wrapper.findAll('.kv-editor input')
    await kvInputs[0]?.setValue('zfs.pool_name')
    await kvInputs[1]?.setValue('tank')

    await buttons.at(-1)?.trigger('click')
    await flushPromises()

    expect(api.createStoragePool).toHaveBeenCalledWith('c1', {
      name: 'fast',
      driver: 'zfs',
      description: 'fast local ssd',
      config: { 'zfs.pool_name': 'tank' },
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Storage pool fast saved.')
    expect(wrapper.emitted('saved')).toEqual([['fast']])
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
  })

  it('validates name and driver on create', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')

    expect(api.createStoragePool).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter a name.')
  })

  it('prefills and updates an existing pool without touching name or driver', async () => {
    vi.mocked(api.getResources).mockResolvedValue(clusterResources())
    const store = useInstancesStore()
    await store.fetchResources('c1')

    const wrapper = mountDialog({ pool: pool() })
    await flushPromises()

    const inputs = wrapper.findAll('input')
    expect((inputs[0]?.element as HTMLInputElement).value).toBe('default')
    expect((inputs[1]?.element as HTMLInputElement).value).toBe('local disk')
    await inputs[1]?.setValue('renamed pool')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.updateStoragePool).toHaveBeenCalledWith('c1', 'default', {
      description: 'renamed pool',
      config: { source: '/srv/incus' },
    })
    expect(wrapper.emitted('saved')).toEqual([['default']])
  })

  it('reports save failures', async () => {
    vi.mocked(api.createStoragePool).mockRejectedValue(new Error('driver not supported'))
    const wrapper = mountDialog()
    await flushPromises()

    const inputs = wrapper.findAll('input')
    await inputs[0]?.setValue('fast')
    wrapper.findComponent({ name: 'ElSelect' }).vm.$emit('update:modelValue', 'zfs')
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('driver not supported')
    expect(wrapper.emitted('saved')).toBeUndefined()
  })
})

import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { clusterResources, clusterSummary, instanceDetail, instanceImage } from '@/test/fixtures'
import InstanceFormDialog from './InstanceFormDialog.vue'

const clusters = [clusterSummary(), clusterSummary({ name: 'c2', display_name: 'Cluster 63' })]

function mountDialog(props: Record<string, unknown> = {}) {
  return mount(InstanceFormDialog, {
    props: {
      modelValue: true,
      mode: 'create',
      clusters,
      cluster: 'c1',
      ...props,
    },
    global: { plugins: [ElementPlus] },
  })
}

function buttonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`${text} button not found`)
  }

  return button
}

function selectAt(wrapper: ReturnType<typeof mount>, index: number, value: string) {
  const selects = wrapper.findAllComponents({ name: 'ElSelect' })
  const select = selects[index]
  if (!select) {
    throw new Error(`select ${index} not found`)
  }

  select.vm.$emit('update:modelValue', value)
}

describe('InstanceFormDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listImages).mockResolvedValue([
      instanceImage(),
      instanceImage({
        fingerprint: 'vmdigest',
        aliases: [],
        type: 'virtual-machine',
        os: 'Debian',
      }),
    ])
    vi.mocked(api.getResources).mockResolvedValue(clusterResources())
    vi.mocked(api.createInstance).mockResolvedValue(instanceDetail({ name: 'newct' }))
    vi.mocked(api.updateInstance).mockResolvedValue(instanceDetail({ description: 'after' }))
  })

  it('loads images and resources and creates an instance', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    expect(api.listImages).toHaveBeenCalledWith('c1')
    expect(api.getResources).toHaveBeenCalledWith('c1')

    await wrapper.find('input[placeholder="e.g. web-01"]').setValue('newct')
    selectAt(wrapper, 0, '053059ea4e28')
    await wrapper.find('input[placeholder="Optional note for operators"]').setValue('new note')
    await wrapper.find('input[placeholder="default, production"]').setValue('default, prod')
    await wrapper.find('input[placeholder="e.g. 2 or 0-3"]').setValue('2')
    await wrapper.find('input[placeholder="e.g. 512MiB or 2GiB"]').setValue('1GiB')
    await wrapper.find('input[placeholder="e.g. 10GiB"]').setValue('10GiB')
    selectAt(wrapper, 2, 'incusbr0')
    await wrapper.find('input[placeholder="e.g. 10.0.0.0.50"]').setValue('10.0.0.0.50')
    await wrapper.find('input[placeholder="e.g. fd42::50"]').setValue('fd42::50')

    await buttonByText(wrapper, 'Create instance').trigger('click')
    await flushPromises()

    expect(api.createInstance).toHaveBeenCalledWith('c1', {
      name: 'newct',
      type: 'container',
      image: '053059ea4e28',
      start: true,
      description: 'new note',
      profiles: ['default', 'prod'],
      cpu: '2',
      memory: '1GiB',
      root_disk_size: '10GiB',
      storage_pool: 'default',
      network: 'incusbr0',
      ipv4: '10.0.0.0.50',
      ipv6: 'fd42::50',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Created newct.')
    expect(wrapper.emitted('saved')).toHaveLength(1)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
  })

  it('validates required fields before submitting', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await buttonByText(wrapper, 'Create instance').trigger('click')
    expect(ElMessage.error).toHaveBeenCalledWith('Enter an instance name.')
    expect(api.createInstance).not.toHaveBeenCalled()

    await wrapper.find('input[placeholder="e.g. web-01"]').setValue('newct')
    await buttonByText(wrapper, 'Create instance').trigger('click')
    expect(ElMessage.error).toHaveBeenLastCalledWith('Select an image.')
    expect(api.createInstance).not.toHaveBeenCalled()
  })

  it('requires a cluster when none is fixed', async () => {
    const wrapper = mountDialog({
      cluster: '',
      clusters: [clusterSummary({ online: false })],
    })
    await flushPromises()

    await wrapper.find('input[placeholder="e.g. web-01"]').setValue('newct')
    await buttonByText(wrapper, 'Create instance').trigger('click')

    expect(ElMessage.error).toHaveBeenCalledWith('Select a cluster first.')
    expect(api.createInstance).not.toHaveBeenCalled()
  })

  it('clears a mismatched image when the type changes', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    selectAt(wrapper, 0, '053059ea4e28')
    await flushPromises()

    const typeGroup = wrapper.findComponent({ name: 'ElRadioGroup' })
    typeGroup.vm.$emit('update:modelValue', 'virtual-machine')
    await flushPromises()

    expect(wrapper.findAllComponents({ name: 'ElSelect' })[0]?.props('modelValue')).toBe('')
  })

  it('edits limits and networking for an instance with a NIC', async () => {
    const detail = instanceDetail({
      config: { 'limits.cpu': '2', 'limits.memory': '1GiB' },
      devices: {
        root: { type: 'disk', pool: 'default', path: '/', size: '5GiB' },
        eth0: {
          type: 'nic',
          network: 'incusbr0',
          'ipv4.address': '10.0.0.0.50',
          'ipv6.address': 'fd42::50',
        },
      },
    })

    const wrapper = mountDialog({ mode: 'edit', instance: detail })
    await flushPromises()

    expect(wrapper.find('input[placeholder="e.g. web-01"]').attributes('disabled')).toBeDefined()
    expect((wrapper.find('input[placeholder="e.g. 2 or 0-3"]').element as HTMLInputElement).value).toBe('2')

    await wrapper.find('input[placeholder="e.g. 512MiB or 2GiB"]').setValue('')
    await wrapper.find('input[placeholder="e.g. 10GiB"]').setValue('20GiB')
    await wrapper.find('input[placeholder="e.g. 10.0.0.0.50"]').setValue('10.0.0.0.60')

    await buttonByText(wrapper, 'Save changes').trigger('click')
    await flushPromises()

    expect(api.updateInstance).toHaveBeenCalledWith('c1', 'web', {
      description: 'web frontend',
      cpu: '2',
      memory: '',
      root_disk_size: '20GiB',
      device: 'eth0',
      network: 'incusbr0',
      ipv4: '10.0.0.0.60',
      ipv6: 'fd42::50',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Updated web.')
  })

  it('omits network fields when the instance has no NIC device', async () => {
    const wrapper = mountDialog({
      mode: 'edit',
      instance: instanceDetail({ devices: { root: { type: 'disk', pool: 'default' } } }),
    })
    await flushPromises()

    await buttonByText(wrapper, 'Save changes').trigger('click')
    await flushPromises()

    expect(api.updateInstance).toHaveBeenCalledWith(
      'c1',
      'web',
      expect.not.objectContaining({ device: expect.anything() }),
    )
  })

  it('surfaces save errors', async () => {
    vi.mocked(api.createInstance).mockRejectedValue(new Error('quota exceeded'))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('input[placeholder="e.g. web-01"]').setValue('newct')
    selectAt(wrapper, 0, '053059ea4e28')
    await buttonByText(wrapper, 'Create instance').trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('Could not save the instance: quota exceeded')
    expect(wrapper.emitted('saved')).toBeUndefined()
  })
})

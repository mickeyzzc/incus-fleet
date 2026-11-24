import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { useInstancesStore } from '@/stores/instances'
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

function selectAt(wrapper: ReturnType<typeof mount>, index: number, value: string | string[]) {
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
    vi.mocked(api.listProfiles).mockResolvedValue([])
    vi.mocked(api.createInstance).mockResolvedValue(instanceDetail({ name: 'newct' }))
    vi.mocked(api.updateInstance).mockResolvedValue(instanceDetail({ description: 'after' }))
  })

  it('loads images, resources, and profiles and creates an instance', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    expect(api.listImages).toHaveBeenCalledWith('c1')
    expect(api.getResources).toHaveBeenCalledWith('c1')
    expect(api.listProfiles).toHaveBeenCalledWith('c1')

    await wrapper.find('input[placeholder="e.g. web-01"]').setValue('newct')
    selectAt(wrapper, 0, '053059ea4e28')
    await wrapper.find('input[placeholder="Optional note for operators"]').setValue('new note')
    selectAt(wrapper, 1, ['default', 'prod'])
    await wrapper.find('input[placeholder="e.g. 2 or 0-3"]').setValue('2')
    await wrapper.find('input[placeholder="e.g. 512MiB or 2GiB"]').setValue('1')
    await wrapper.find('.size-input select').setValue('GiB')
    await wrapper.find('input[placeholder="e.g. 10GiB"]').setValue('10')
    selectAt(wrapper, 3, 'incusbr0')
    await wrapper.find('input[placeholder="e.g. 10.0.0.50"]').setValue('10.0.0.50')
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
      ipv4: '10.0.0.50',
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
          'ipv4.address': '10.0.0.50',
          'ipv6.address': 'fd42::50',
        },
      },
    })

    const wrapper = mountDialog({ mode: 'edit', instance: detail })
    await flushPromises()

    expect(wrapper.find('input[placeholder="e.g. web-01"]').attributes('disabled')).toBeDefined()
    expect((wrapper.find('input[placeholder="e.g. 2 or 0-3"]').element as HTMLInputElement).value).toBe('2')

    await wrapper.find('input[placeholder="e.g. 512MiB or 2GiB"]').setValue('')
    await wrapper.find('input[placeholder="e.g. 10GiB"]').setValue('20')
    await wrapper.find('input[placeholder="e.g. 10.0.0.50"]').setValue('10.0.0.60')

    await buttonByText(wrapper, 'Save changes').trigger('click')
    await flushPromises()

    // An unchanged network is not re-sent; only the edited fields travel.
    expect(api.updateInstance).toHaveBeenCalledWith('c1', 'web', {
      description: 'web frontend',
      cpu: '2',
      memory: '',
      root_disk_size: '20GiB',
      device: 'eth0',
      ipv4: '10.0.0.60',
      ipv6: 'fd42::50',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Updated web.')
  })

  it('sends the network when the NIC switches to another network', async () => {
    const detail = instanceDetail({
      devices: {
        root: { type: 'disk', pool: 'default', path: '/' },
        eth0: { type: 'nic', network: 'incusbr0' },
      },
    })

    const wrapper = mountDialog({ mode: 'edit', instance: detail })
    await flushPromises()

    selectAt(wrapper, 0, 'incusbr1')
    await flushPromises()

    await buttonByText(wrapper, 'Save changes').trigger('click')
    await flushPromises()

    expect(api.updateInstance).toHaveBeenCalledWith(
      'c1',
      'web',
      expect.objectContaining({ device: 'eth0', network: 'incusbr1' }),
    )
  })

  it('leaves physical NIC addresses alone in edit mode and says why', async () => {
    const detail = instanceDetail({
      devices: {
        root: { type: 'disk', pool: 'default', path: '/' },
        eth0: { type: 'nic', nictype: 'macvlan', parent: 'lan0' },
      },
    })

    const wrapper = mountDialog({ mode: 'edit', instance: detail })
    await flushPromises()

    await buttonByText(wrapper, 'Save changes').trigger('click')
    await flushPromises()

    expect(api.updateInstance).toHaveBeenCalledWith(
      'c1',
      'web',
      expect.objectContaining({ device: 'eth0' }),
    )
    expect(api.updateInstance).toHaveBeenCalledWith(
      'c1',
      'web',
      expect.not.objectContaining({ ipv4: expect.anything() }),
    )
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

  it('offers a fleet overlay bridge as a NIC choice', async () => {
    const store = useInstancesStore()
    store.overlayCache = {
      c1: [
        {
          cluster: 'c1',
          name: 'c1',
          bridge: 'fleetbr0',
          vni: 6200,
          subnet: '10.10.0.0/24',
          gateway_member: 'node-1',
          mtu: 1450,
          enabled: true,
          members: [],
          tunnels: [],
        },
      ],
    }
    const wrapper = mountDialog()
    await flushPromises()

    // The overlay bridge must appear among the NIC options even though Incus
    // does not report it as a managed network.
    const text = wrapper.html()
    expect(text).toContain('fleetbr0')
  })

  it('creates extra data disks and normalizes bare sizes', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('input[placeholder="e.g. web-01"]').setValue('newct')
    selectAt(wrapper, 0, '053059ea4e28')
    await wrapper.find('input[placeholder="e.g. 512MiB or 2GiB"]').setValue('512')
    await buttonByText(wrapper, 'Add data disk').trigger('click')
    await flushPromises()

    expect((wrapper.find('input[placeholder="e.g. data0"]').element as HTMLInputElement).value).toBe('data0')
    expect((wrapper.find('input[placeholder="e.g. /data"]').element as HTMLInputElement).value).toBe('/data')
    await wrapper.find('input[placeholder="e.g. 50GiB"]').setValue('50')

    await buttonByText(wrapper, 'Create instance').trigger('click')
    await flushPromises()

    expect(api.createInstance).toHaveBeenCalledWith(
      'c1',
      expect.objectContaining({
        memory: '512MiB',
        disks: [{ name: 'data0', path: '/data', size: '50GiB' }],
      }),
    )
  })

  it('rejects a relative data-disk path before calling the API', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('input[placeholder="e.g. web-01"]').setValue('newct')
    selectAt(wrapper, 0, '053059ea4e28')
    await buttonByText(wrapper, 'Add data disk').trigger('click')
    await flushPromises()
    await wrapper.find('input[placeholder="e.g. /data"]').setValue('data')

    await buttonByText(wrapper, 'Create instance').trigger('click')
    await flushPromises()

    expect(api.createInstance).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter an absolute path, for example /data.')
  })

  it('shows an inline error for a malformed memory limit', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    // The size control only accepts numbers, so drive the invalid value
    // through the component model to exercise the defensive validation.
    wrapper.findAllComponents({ name: 'SizeInput' })[0]?.vm.$emit('update:modelValue', 'abc')
    await flushPromises()

    expect(wrapper.text()).toContain('Enter a valid size, for example 512MiB, 2GiB or 10GB.')

    await buttonByText(wrapper, 'Create instance').trigger('click')
    await flushPromises()
    expect(api.createInstance).not.toHaveBeenCalled()
  })
})

import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { clusterResources, instanceDetail } from '@/test/fixtures'
import InstanceDevices from './InstanceDevices.vue'

function mountPanel(options: { devices?: Record<string, Record<string, string>> } = {}) {
  return mount(InstanceDevices, {
    props: {
      cluster: 'c1',
      instance: 'web',
      detail: instanceDetail({
        devices:
          options.devices ?? {
            root: { type: 'disk', pool: 'default', path: '/' },
            eth0: { type: 'nic', network: 'incusbr0' },
          },
      }),
    },
    global: { plugins: [ElementPlus] },
  })
}

describe('InstanceDevices', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.getResources).mockResolvedValue(clusterResources())
    vi.mocked(api.attachDevice).mockResolvedValue(instanceDetail())
    vi.mocked(api.detachDevice).mockResolvedValue(instanceDetail())
    vi.mocked(api.updateDevice).mockResolvedValue(instanceDetail())
    vi.mocked(ElMessageBox.confirm).mockResolvedValue('confirm' as never)
  })

  it('renders devices with a config summary', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('root')
    expect(text).toContain('eth0')
    expect(text).toContain('pool=default · path=/')
    expect(text).toContain('network=incusbr0')
  })

  it('attaches a disk device backed by an existing volume without a size', async () => {
    const wrapper = mountPanel()

    await wrapper.findAll('button').find((b) => b.text().includes('Add disk'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    const inputs = dialog.findAll('input[type="text"]')
    await inputs[0]?.setValue('data')
    const selects = dialog.findAllComponents({ name: 'ElSelect' })
    selects[0]?.vm.$emit('update:modelValue', 'default')
    selects[1]?.vm.$emit('update:modelValue', 'assets-vol')
    await inputs[1]?.setValue('/data')

    await dialog.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    // An existing volume cannot take a size quota; the request omits it (#13 F8).
    expect(api.attachDevice).toHaveBeenCalledWith('c1', 'web', {
      name: 'data',
      type: 'disk',
      pool: 'default',
      source: 'assets-vol',
      path: '/data',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Device data attached.')
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('disables the size field while a source volume is set', async () => {
    const wrapper = mountPanel()

    await wrapper.findAll('button').find((b) => b.text().includes('Add disk'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    const inputs = dialog.findAll('input[type="text"]')
    const source = dialog.findAllComponents({ name: 'ElSelect' })[1]
    expect(inputs[2]?.attributes('disabled')).toBeUndefined()

    source?.vm.$emit('update:modelValue', 'assets-vol')
    await flushPromises()
    expect(inputs[2]?.attributes('disabled')).toBeDefined()

    source?.vm.$emit('update:modelValue', '')
    await flushPromises()
    expect(inputs[2]?.attributes('disabled')).toBeUndefined()
  })

  it('attaches a fresh disk with a size quota', async () => {
    const wrapper = mountPanel()

    await wrapper.findAll('button').find((b) => b.text().includes('Add disk'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    const inputs = dialog.findAll('input[type="text"]')
    await inputs[0]?.setValue('scratch')
    await inputs[1]?.setValue('/scratch')
    await inputs[2]?.setValue('10GiB')

    await dialog.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.attachDevice).toHaveBeenCalledWith('c1', 'web', {
      name: 'scratch',
      type: 'disk',
      pool: 'default',
      path: '/scratch',
      size: '10GiB',
    })
  })

  it('attaches a NIC device through the nic branch', async () => {
    const wrapper = mountPanel()

    await wrapper.findAll('button').find((b) => b.text().includes('Add network card'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    const group = dialog.findComponent({ name: 'ElRadioGroup' })
    expect(group.props('modelValue')).toBe('nic')

    const inputs = dialog.findAll('input[type="text"]')
    await inputs[0]?.setValue('eth1')
    await inputs[1]?.setValue('10.0.0.9')
    await inputs[2]?.setValue('fd42::9')
    dialog.findComponent({ name: 'ElSelect' }).vm.$emit('update:modelValue', 'incusbr0')

    await dialog.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.attachDevice).toHaveBeenCalledWith('c1', 'web', {
      name: 'eth1',
      type: 'nic',
      network: 'incusbr0',
      ipv4: '10.0.0.9',
      ipv6: 'fd42::9',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Device eth1 attached.')
  })

  it('attaches a proxy port forward', async () => {
    const wrapper = mountPanel()

    await wrapper.findAll('button').find((b) => b.text().includes('Port forward'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    const group = dialog.findComponent({ name: 'ElRadioGroup' })
    expect(group.props('modelValue')).toBe('proxy')

    const inputs = dialog.findAll('input[type="text"]')
    await inputs[0]?.setValue('web80')
    await inputs[1]?.setValue('tcp:0.0.0.0:8080')
    await inputs[2]?.setValue('tcp:127.0.0.1:80')

    await dialog.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.attachDevice).toHaveBeenCalledWith('c1', 'web', {
      name: 'web80',
      type: 'proxy',
      listen: 'tcp:0.0.0.0:8080',
      connect: 'tcp:127.0.0.1:80',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Device web80 attached.')
  })

  it('labels unknown device types verbatim', async () => {
    const wrapper = mountPanel({
      devices: {
        root: { type: 'disk', pool: 'default', path: '/' },
        weird: { type: 'unix-block', path: '/dev/foo' },
      },
    })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('unix-block')
    expect(text).toContain('path=/dev/foo')
  })

  it('rejects an empty device name', async () => {
    const wrapper = mountPanel()

    await wrapper.findAll('button').find((b) => b.text().includes('Add disk'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    await dialog.find('input').setValue('')
    await dialog.findAll('button').at(-1)?.trigger('click')

    expect(api.attachDevice).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('This field is required.')
  })

  it('detaches a device after confirmation', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const row = wrapper.findAll('.el-table__row').find((item) => item.text().includes('root'))
    await row?.findAll('button').find((b) => b.text().includes('Detach'))?.trigger('click')
    await flushPromises()

    expect(api.detachDevice).toHaveBeenCalledWith('c1', 'web', 'root')
    expect(ElMessage.success).toHaveBeenCalledWith('Device root detached.')
  })

  it('expands a row to show the full device configuration', async () => {
    const wrapper = mountPanel({
      devices: {
        iso0: {
          type: 'disk',
          pool: 'default',
          source: 'installer-iso-x86_1.2.1011-very-long-name',
          'boot.priority': '1',
        },
      },
    })
    await flushPromises()

    await wrapper.find('.el-table__expand-icon').trigger('click')
    await flushPromises()

    // The expanded row renders every key on its own line, including values the
    // summary cell truncates with an ellipsis.
    const detail = wrapper.find('.device-detail')
    expect(detail.exists()).toBe(true)
    expect(detail.text()).toContain('very-long-name')
    expect(detail.text()).toContain('boot.priority')
  })

  it('edits a NIC device and clears the address when emptied', async () => {
    const wrapper = mountPanel({
      devices: { eth0: { type: 'nic', network: 'incusbr0', 'ipv4.address': '10.0.0.9' } },
    })
    await flushPromises()

    const row = wrapper.findAll('.el-table__row').find((item) => item.text().includes('eth0'))
    await row?.findAll('button').find((b) => b.text().includes('Edit'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findAllComponents({ name: 'ElDialog' })[1]
    const inputs = dialog!.findAll('input[type="text"]')
    await inputs[0]?.setValue('')
    await inputs[1]?.setValue('fd42::9')
    dialog!.findComponent({ name: 'ElSelect' }).vm.$emit('update:modelValue', 'incusbr0')

    await dialog!.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.updateDevice).toHaveBeenCalledWith('c1', 'web', 'eth0', {
      name: 'eth0',
      type: 'nic',
      ipv4: '',
      ipv6: 'fd42::9',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Device eth0 updated.')
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('keeps legacy nictype keys when the network is not switched', async () => {
    const wrapper = mountPanel({
      devices: {
        eth0: { type: 'nic', nictype: 'bridged', parent: 'incusbr0', 'ipv4.address': '10.0.0.9' },
      },
    })
    await flushPromises()

    const row = wrapper.findAll('.el-table__row').find((item) => item.text().includes('eth0'))
    await row?.findAll('button').find((b) => b.text().includes('Edit'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findAllComponents({ name: 'ElDialog' })[1]
    await dialog!.findAll('button').at(-2)?.trigger('click')
    await flushPromises()
    expect(api.updateDevice).not.toHaveBeenCalled()

    await row?.findAll('button').find((b) => b.text().includes('Edit'))?.trigger('click')
    await flushPromises()
    await dialog!.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.updateDevice).toHaveBeenCalledWith('c1', 'web', 'eth0', {
      name: 'eth0',
      type: 'nic',
      ipv4: '10.0.0.9',
      ipv6: '',
    })
  })

  it('edits a proxy device through the proxy branch', async () => {
    const wrapper = mountPanel({
      devices: { web80: { type: 'proxy', listen: 'tcp:0.0.0.0:8080', connect: 'tcp:127.0.0.1:80' } },
    })
    await flushPromises()

    const row = wrapper.findAll('.el-table__row').find((item) => item.text().includes('web80'))
    await row?.findAll('button').find((b) => b.text().includes('Edit'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findAllComponents({ name: 'ElDialog' })[1]
    const inputs = dialog!.findAll('input[type="text"]')
    await inputs[0]?.setValue('tcp:0.0.0.0:9090')
    await inputs[1]?.setValue('tcp:127.0.0.1:8080')

    await dialog!.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.updateDevice).toHaveBeenCalledWith('c1', 'web', 'web80', {
      name: 'web80',
      type: 'proxy',
      listen: 'tcp:0.0.0.0:9090',
      connect: 'tcp:127.0.0.1:8080',
    })
  })

  it('edits a disk device and preserves unmanaged keys on the backend', async () => {
    const wrapper = mountPanel({
      devices: {
        iso0: { type: 'disk', pool: 'default', source: 'installer', 'boot.priority': '1' },
      },
    })
    await flushPromises()

    const row = wrapper.findAll('.el-table__row').find((item) => item.text().includes('iso0'))
    await row?.findAll('button').find((b) => b.text().includes('Edit'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findAllComponents({ name: 'ElDialog' })[1]
    const inputs = dialog!.findAll('input[type="text"]')
    await inputs[0]?.setValue('installer-v2')
    await inputs[1]?.setValue('/mnt/iso')

    await dialog!.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    // boot.priority is not part of the form; the backend merge keeps it.
    // The size field is disabled for a source-backed volume and the request
    // omits the quota (#13 F8).
    expect(api.updateDevice).toHaveBeenCalledWith('c1', 'web', 'iso0', {
      name: 'iso0',
      type: 'disk',
      pool: 'default',
      source: 'installer-v2',
      path: '/mnt/iso',
    })
  })

  it('edits a root disk size while no source is set', async () => {
    const wrapper = mountPanel({
      devices: {
        root: { type: 'disk', pool: 'default', path: '/', size: '20GiB' },
      },
    })
    await flushPromises()

    const row = wrapper.findAll('.el-table__row').find((item) => item.text().includes('root'))
    await row?.findAll('button').find((b) => b.text().includes('Edit'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findAllComponents({ name: 'ElDialog' })[1]
    const sizeInput = dialog!.findAll('input[type="text"]')[2]
    expect(sizeInput?.attributes('disabled')).toBeUndefined()
    await sizeInput?.setValue('40GiB')

    await dialog!.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.updateDevice).toHaveBeenCalledWith('c1', 'web', 'root', {
      name: 'root',
      type: 'disk',
      pool: 'default',
      path: '/',
      size: '40GiB',
    })
  })

  it('reports edit failures', async () => {
    vi.mocked(api.updateDevice).mockRejectedValue(new Error('device busy'))
    const wrapper = mountPanel({
      devices: { eth0: { type: 'nic', network: 'incusbr0' } },
    })
    await flushPromises()

    const row = wrapper.findAll('.el-table__row').find((item) => item.text().includes('eth0'))
    await row?.findAll('button').find((b) => b.text().includes('Edit'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findAllComponents({ name: 'ElDialog' })[1]
    await dialog!.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('Device action failed: device busy')
  })

  it('reports failures', async () => {
    vi.mocked(api.detachDevice).mockRejectedValue(new Error('device busy'))
    const wrapper = mountPanel()
    await flushPromises()

    const row = wrapper.findAll('.el-table__row').find((item) => item.text().includes('root'))
    await row?.findAll('button').find((b) => b.text().includes('Detach'))?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('Device action failed: device busy')
  })
})

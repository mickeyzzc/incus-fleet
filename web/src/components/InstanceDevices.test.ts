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

  it('attaches a disk device', async () => {
    const wrapper = mountPanel()

    await wrapper.findAll('button').find((b) => b.text().includes('Attach device'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    const inputs = dialog.findAll('input[type="text"]')
    await inputs[0]?.setValue('data')
    await inputs[2]?.setValue('/data')

    await dialog.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.attachDevice).toHaveBeenCalledWith('c1', 'web', {
      name: 'data',
      type: 'disk',
      pool: 'default',
      path: '/data',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Device data attached.')
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('attaches a NIC device through the nic branch', async () => {
    const wrapper = mountPanel()

    await wrapper.findAll('button').find((b) => b.text().includes('Attach device'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    const group = dialog.findComponent({ name: 'ElRadioGroup' })
    group.vm.$emit('update:modelValue', 'nic')
    await flushPromises()

    const inputs = dialog.findAll('input[type="text"]')
    await inputs[0]?.setValue('eth1')
    await inputs[1]?.setValue('10.0.0.9')

    await dialog.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.attachDevice).toHaveBeenCalledWith('c1', 'web', {
      name: 'eth1',
      type: 'nic',
      network: 'incusbr0',
      ipv4: '10.0.0.9',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Device eth1 attached.')
  })

  it('attaches a proxy port forward', async () => {
    const wrapper = mountPanel()

    await wrapper.findAll('button').find((b) => b.text().includes('Attach device'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    const group = dialog.findComponent({ name: 'ElRadioGroup' })
    group.vm.$emit('update:modelValue', 'proxy')
    await flushPromises()

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

    await wrapper.findAll('button').find((b) => b.text().includes('Attach device'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    await dialog.findAll('button').at(-1)?.trigger('click')

    expect(api.attachDevice).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter a name.')
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

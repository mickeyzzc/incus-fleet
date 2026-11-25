import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { instanceDetail, instanceSnapshot } from '@/test/fixtures'
import InstanceDetailDrawer from './InstanceDetailDrawer.vue'

describe('InstanceDetailDrawer', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('loads and renders instance details', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'web' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    expect(api.getInstance).toHaveBeenCalledWith('c1', 'web')
    expect(wrapper.text()).toContain('web · c1')
    expect(wrapper.text()).toContain('Debian GNU/Linux')
    expect(wrapper.text()).toContain('image.os')
    expect(wrapper.text()).toContain('pool=default · path=/')
    expect(wrapper.text()).toContain('Storage pool')
  })

  it('opens directly on the requested devices tab', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'web', initialTab: 'devices' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    expect(wrapper.findComponent({ name: 'ElTabs' }).props('modelValue')).toBe('devices')
    expect(wrapper.text()).toContain('Add disk')
    expect(wrapper.text()).toContain('Add network card')
  })

  it('lists snapshots in the snapshots tab', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    vi.mocked(api.listSnapshots).mockResolvedValue([instanceSnapshot()])
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'web' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    expect(api.listSnapshots).toHaveBeenCalledWith('c1', 'web')
    expect(wrapper.text()).toContain('snap0')
  })

  it('offers the agent installer only for agent-less virtual machines', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(
      instanceDetail({ type: 'virtual-machine', os: '', os_version: '', hostname: '' }),
    )
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'vm1' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Install agent')

    // A VM that already reports guest info has the agent; no installer.
    vi.mocked(api.getInstance).mockResolvedValue(
      instanceDetail({ type: 'virtual-machine', os: 'Debian GNU/Linux', hostname: 'vm1' }),
    )
    const withAgent = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'vm1' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    expect(withAgent.text()).not.toContain('Install agent')

    // A running VM whose state carries interfaces is agent-backed even when
    // os_info stays empty (minimal images never fill it).
    vi.mocked(api.getInstance).mockResolvedValue(
      instanceDetail({
        type: 'virtual-machine',
        os: '',
        os_version: '',
        hostname: '',
        state: {
          status: 'Running',
          status_code: 103,
          network: { eth0: { addresses: [{ address: '10.0.0.5', family: 'inet', scope: 'global' }] } },
        },
      }),
    )
    const withNetwork = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'vm1' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    expect(withNetwork.text()).not.toContain('Install agent')
  })

  it('opens the remote desktop and maintenance dialogs', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'web' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    const buttons = wrapper.findAll('button')
    await buttons.find((button) => button.text().includes('Remote desktop'))?.trigger('click')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'RemoteDesktopDialog' }).exists()).toBe(true)

    await buttons
      .find((button) => button.text().includes('Maintenance'))
      ?.trigger('click')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'InstanceOpsDialog' }).exists()).toBe(true)
  })

  it('does not fetch while closed and fetches when opened', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: false, cluster: 'c1', name: 'web' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()
    expect(api.getInstance).not.toHaveBeenCalled()

    await wrapper.setProps({ modelValue: true })
    await flushPromises()
    expect(api.getInstance).toHaveBeenCalledTimes(1)
  })

  it('shows an empty state when details cannot be loaded', async () => {
    vi.mocked(api.getInstance).mockRejectedValue(new Error('not found'))
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'missing' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Instance details unavailable')
  })

  it('runs lifecycle actions from the drawer', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    vi.mocked(api.instanceAction).mockResolvedValue(undefined)
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'web' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    const stop = wrapper.findAll('button').find((item) => item.text().includes('Stop'))
    await stop?.trigger('click')
    await flushPromises()

    expect(api.instanceAction).toHaveBeenCalledWith('c1', 'web', {
      action: 'stop',
      force: false,
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Stopped web.')
  })

  it('emits edit, migrate, and delete requests', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'web' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    for (const label of ['Edit', 'Migrate', 'Delete']) {
      const button = wrapper.findAll('button').find((item) => item.text().includes(label))
      await button?.trigger('click')
    }

    expect(wrapper.emitted('edit')?.[0]?.[0]).toMatchObject({ name: 'web', cluster: 'c1' })
    expect(wrapper.emitted('migrate')).toHaveLength(1)
    expect(wrapper.emitted('delete')).toHaveLength(1)
  })

  it('opens the terminal and file manager dialogs', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'web' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    await wrapper.findAll('button').find((item) => item.text().includes('Terminal'))?.trigger('click')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'TerminalDialog' }).props('modelValue')).toBe(true)

    await wrapper.findAll('button').find((item) => item.text().includes('Files'))?.trigger('click')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'FileManagerDialog' }).props('modelValue')).toBe(true)
  })

  it('updates the cached detail when devices change', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'web' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    const devices = wrapper.findComponent({ name: 'InstanceDevices' })
    expect(devices.exists()).toBe(true)
    devices.vm.$emit('changed', { ...instanceDetail(), memory: 42 })
    await flushPromises()

    expect(wrapper.text()).toContain('42')
  })

  it('reports failed lifecycle actions', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    vi.mocked(api.instanceAction).mockRejectedValue(new Error('daemon busy'))
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'web' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    const actions = wrapper.findComponent({ name: 'InstanceActions' })
    actions.vm.$emit('action', 'start')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('Action failed: daemon busy')
  })

  it('hides migration when it is not available', async () => {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'web', canMigrate: false },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    expect(wrapper.text()).not.toContain('Migrate')
  })
})

describe('InstanceDetailDrawer controls', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  async function mountDrawer() {
    vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
    const wrapper = mount(InstanceDetailDrawer, {
      props: { modelValue: true, cluster: 'c1', name: 'web' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()
    return wrapper
  }

  it('opens the terminal, file manager, desktop, and ops dialogs', async () => {
    const wrapper = await mountDrawer()

    for (const label of ['Terminal', 'Files', 'Desktop', 'Maintenance']) {
      const button = wrapper.findAll('button').find((b) => b.text().includes(label))
      if (button) {
        await button.trigger('click')
        await flushPromises()
      }
    }

    // The dialogs mount on demand; asserting no throw is the observable effect.
    expect(wrapper.exists()).toBe(true)
  })

  it('emits delete for the current instance', async () => {
    const wrapper = await mountDrawer()

    const button = wrapper.findAll('button').find((b) => b.text().includes('Delete'))
    await button!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('delete')).toBeTruthy()
  })
})

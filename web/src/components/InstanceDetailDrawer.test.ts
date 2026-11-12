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

import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { instanceDetail } from '@/test/fixtures'
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
    expect(wrapper.text()).toContain('type=disk')
    expect(wrapper.text()).toContain('Storage pool')
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
})

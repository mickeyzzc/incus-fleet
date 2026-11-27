import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { PluginEntry } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import PluginPage from './PluginPage.vue'

function plugin(overrides: Partial<PluginEntry> = {}): PluginEntry {
  return {
    id: 'vmlogs',
    name: 'VictoriaLogs',
    version: '1.0.0',
    enabled: true,
    upstream: 'http://logs.example:9428',
    role: 'viewer',
    kind: 'external',
    status: 'healthy',
    ui: { path: '/plugins/vmlogs', icon: 'logs', title_key: 'plugins.text.vmlogs.title' },
    ...overrides,
  }
}

async function render(id = 'vmlogs', entry = plugin()) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.authenticated = true
  auth.initialized = true
  auth.role = 'admin'

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/plugins/:id', name: 'plugin-ui-vmlogs', component: PluginPage, props: true }],
  })
  await router.push(`/plugins/${id}`)
  await router.isReady()

  vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [entry], enabled: true })
  const wrapper = mount(PluginPage, {
    props: { id },
    global: { plugins: [pinia, router, ElementPlus] },
  })
  await flushPromises()
  return wrapper
}

describe('PluginPage', () => {
  beforeEach(() => {
    vi.mocked(api.listPluginCatalog).mockResolvedValue({ catalog: [] })
  })

  it('frames the same-origin proxy and shows the merged title', async () => {
    const wrapper = await render()
    const frame = wrapper.find('iframe')

    expect(frame.exists()).toBe(true)
    expect(frame.attributes('src')).toBe('/plugin-ui/vmlogs/')
    expect(wrapper.text()).toContain('VictoriaLogs')
    expect(wrapper.find('.plugin-icon').exists()).toBe(true)
  })

  it('shows a loading state before the descriptor arrives', async () => {
    vi.mocked(api.listPlugins).mockImplementation(() => new Promise(() => {}))
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.authenticated = true
    auth.initialized = true
    auth.role = 'admin'
    const wrapper = mount(PluginPage, {
      props: { id: 'vmlogs' },
      global: { plugins: [pinia, ElementPlus] },
    })
    await flushPromises()
    expect(wrapper.find('[data-state="loading"]').exists()).toBe(true)
  })

  it('shows an error state with a retry action', async () => {
    vi.mocked(api.listPlugins).mockRejectedValue(new Error('proxy down'))
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.authenticated = true
    auth.initialized = true
    auth.role = 'admin'
    const wrapper = mount(PluginPage, { props: { id: 'vmlogs' }, global: { plugins: [pinia, ElementPlus] } })
    await flushPromises()
    expect(wrapper.text()).toContain('proxy down')
    expect(wrapper.find('[data-state="error"]').exists()).toBe(true)
  })

  it('shows an empty state for an unknown plugin', async () => {
    const wrapper = await render('missing')
    expect(wrapper.find('[data-state="empty"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('not installed')
  })

  it('shows the permission-denied state for viewers', async () => {
    const wrapper = await render()
    const auth = useAuthStore()
    auth.role = 'viewer'
    await flushPromises()
    expect(wrapper.find('[data-state="forbidden"]').exists()).toBe(true)
  })
})

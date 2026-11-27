import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { PluginEntry } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import { pluginStatus } from '@/stores/plugins'
import PluginsView from './PluginsView.vue'

const Blank = { template: '<div />' }

function plugin(overrides: Partial<PluginEntry> = {}): PluginEntry {
  return {
    id: 'vm',
    name: 'VictoriaMetrics',
    version: '1.0.0',
    enabled: true,
    upstream: 'http://vm.example:8428',
    role: 'viewer',
    kind: 'external',
    cluster: 'c1',
    status: 'healthy',
    ui: { path: '/plugins/vm', title_key: 'plugins.text.vm.title' },
    ...overrides,
  }
}

async function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.authenticated = true
  auth.initialized = true
  auth.role = 'admin'

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/plugins', name: 'plugins', component: PluginsView },
      { path: '/plugins/:id', name: 'plugin-detail', component: Blank },
    ],
  })
  await router.push('/plugins')
  await router.isReady()

  const wrapper = mount(PluginsView, { global: { plugins: [pinia, router, ElementPlus] } })
  await flushPromises()
  return { wrapper, router }
}

async function openCatalog(wrapper: ReturnType<typeof mount>) {
  const tab = wrapper.findAll('.el-tabs__item').find((item) => item.text().includes('Catalog'))
  await tab!.trigger('click')
  await flushPromises()
}

describe('PluginsView information architecture', () => {
  beforeEach(() => {
    vi.mocked(api.listClusters).mockResolvedValue([])
    vi.mocked(api.listPluginCatalog).mockResolvedValue({
      catalog: [{
        id: 'vm', name: 'VictoriaMetrics', description: 'metrics',
        value: 'Long-term metrics storage', publisher: 'fleet-official',
        tier: 'official', permissions: 3, ui_path: '/plugin-ui/vm/', wiring: 'metrics.query_url',
      }],
    })
  })

  it('uses exactly five statuses with the shared i18n labels', () => {
    for (const status of ['installing', 'healthy', 'degraded', 'stopped', 'failed'] as const) {
      expect(pluginStatus(plugin({ status })).labelKey).toBe(`plugins.status.${status}`)
    }
  })

  it('shows the disabled state when the manager is off', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: null, enabled: false })
    const { wrapper } = await mountView()
    expect(wrapper.text()).toContain('disabled in configuration')
  })

  it('shows one empty state with a single next action', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    const { wrapper } = await mountView()

    expect(wrapper.text()).toContain('No plugins installed')
    const browse = wrapper.findAll('button').filter((button) => button.text().includes('Browse catalog'))
    expect(browse).toHaveLength(1)
  })

  it('shows the error state with the failure message', async () => {
    vi.mocked(api.listPlugins).mockRejectedValue(new Error('network down'))
    const { wrapper } = await mountView()

    expect(wrapper.text()).toContain('Could not load the plugin list')
    expect(wrapper.text()).toContain('network down')
  })

  it('shows the loading state while data is in flight', async () => {
    vi.mocked(api.listPlugins).mockImplementation(() => new Promise(() => {}))
    const { wrapper } = await mountView()
    expect(wrapper.find('[data-state="loading"]').exists()).toBe(true)
  })

  it('shows the permission-denied state for viewers', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [plugin()], enabled: true })
    const { wrapper } = await mountView()
    const auth = useAuthStore()
    auth.role = 'viewer'
    await flushPromises()
    expect(wrapper.find('[data-state="forbidden"]').exists()).toBe(true)
  })

  it('lists installed plugins with status, version, cluster and health', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [plugin()], enabled: true })
    const { wrapper } = await mountView()

    const table = wrapper.find('.plugin-table')
    expect(table.exists()).toBe(true)
    expect(wrapper.text()).toContain('VictoriaMetrics')
    expect(wrapper.text()).toContain('1.0.0')
    expect(wrapper.text()).toContain('Healthy')
    expect(wrapper.text()).toContain('c1')
  })

  it('offers exactly one primary action, in the header', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [plugin()], enabled: true })
    const { wrapper } = await mountView()

    const primaries = wrapper.findAll('.el-button--primary')
    expect(primaries).toHaveLength(1)
    expect(wrapper.find('.plugins-header .el-button--primary').exists()).toBe(true)
  })

  it('renders catalog cards with publisher, value, permission count and tier chip', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    const { wrapper } = await mountView()
    await openCatalog(wrapper)

    const card = wrapper.find('.catalog-card')
    expect(card.exists()).toBe(true)
    expect(card.text()).toContain('VictoriaMetrics')
    expect(card.text()).toContain('Long-term metrics storage')
    expect(card.text()).toContain('fleet-official')
    expect(card.text()).toContain('3 permissions')
    expect(card.text()).toContain('Official')
    expect(card.findAll('.el-button--primary')).toHaveLength(0)
  })

  it('opens the detail page from the installed table, never from the sidebar', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [plugin()], enabled: true })
    const { wrapper, router } = await mountView()

    await wrapper.find('.plugin-table tbody tr').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/plugins/vm')
  })

  it('refreshes from the header action', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [plugin()], enabled: true })
    const { wrapper } = await mountView()
    expect(api.listPlugins).toHaveBeenCalledTimes(1)

    const refresh = wrapper.findAll('button').find((button) => button.text().includes('Refresh'))
    await refresh!.trigger('click')
    await flushPromises()
    expect(api.listPlugins).toHaveBeenCalledTimes(2)
  })

  it('does not offer a self-referential action in the empty catalog', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.listPluginCatalog).mockResolvedValue({ catalog: [] })
    const { wrapper } = await mountView()

    expect(wrapper.findAll('button').filter((b) => b.text().includes('Browse catalog'))).toHaveLength(1)
    await openCatalog(wrapper)

    expect(wrapper.findAll('button').filter((b) => b.text().includes('Browse catalog'))).toHaveLength(0)
    expect(wrapper.text()).toContain('No plugins are registered on this instance')
  })
})

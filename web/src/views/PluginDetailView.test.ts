import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { PluginEntry } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import PluginDetailView from './PluginDetailView.vue'

const Blank = { template: '<div />' }

function plugin(overrides: Partial<PluginEntry> = {}): PluginEntry {
  return {
    id: 'vm',
    name: 'VictoriaMetrics',
    version: '1.2.0',
    enabled: true,
    upstream: 'http://vm.example:8428',
    role: 'viewer',
    kind: 'external',
    cluster: 'c1',
    instance: 'fleet-vm',
    status: 'healthy',
    ui: { path: '/plugins/vm', icon: 'metrics', title_key: 'plugins.text.vm.title' },
    health_history: [
      { at: '2026-11-27T10:00:00Z', status: 'healthy' },
      { at: '2026-11-27T09:45:00Z', status: 'degraded', detail: 'slow' },
    ],
    config: { retention: '30d', listen_port: '8428' },
    wiring: [{
      capability: 'Metrics ingestion',
      target: 'http://vm.example:8428',
      default_target: 'in-memory ring',
      key: 'metrics.remote_write_url',
    }],
    events: [
      { at: '2026-11-27T10:00:00Z', kind: 'delivery', message: 'agent.connected delivered' },
      { at: '2026-11-27T09:50:00Z', kind: 'error', message: 'webhook timeout' },
    ],
    ...overrides,
  }
}

async function render(id = 'vm', entry = plugin(), failLoad = false) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.authenticated = true
  auth.initialized = true
  auth.role = 'admin'

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/plugins', name: 'plugins', component: Blank },
      { path: '/plugins/:id', name: 'plugin-detail', component: PluginDetailView, props: true },
      { path: '/plugins/vm/ui', name: 'plugin-ui-vm', component: Blank },
    ],
  })
  await router.push(`/plugins/${id}`)
  await router.isReady()

  if (failLoad) {
    vi.mocked(api.listPlugins).mockRejectedValue(new Error('detail down'))
  } else {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [entry], enabled: true })
  }
  const wrapper = mount(PluginDetailView, { props: { id }, global: { plugins: [pinia, router, ElementPlus] } })
  await flushPromises()
  return { wrapper, router }
}

function tab(wrapper: ReturnType<typeof mount>, label: string) {
  const found = wrapper.findAll('.el-tabs__item').find((item) => item.text().includes(label))
  if (!found) throw new Error(`tab "${label}" not found`)
  return found
}

describe('PluginDetailView', () => {
  beforeEach(() => {
    vi.mocked(api.listPluginCatalog).mockResolvedValue({ catalog: [] })
    vi.mocked(api.togglePlugin).mockResolvedValue(plugin())
    vi.mocked(api.configurePlugin).mockResolvedValue(plugin())
    vi.mocked(api.restorePluginWiring).mockResolvedValue(undefined)
    vi.mocked(api.uninstallPlugin).mockResolvedValue(undefined)
    vi.mocked(api.installPlugin).mockResolvedValue({ token: 't', plugin: plugin() })
  })

  it('shows the five fixed tabs with real overview data', async () => {
    const { wrapper } = await render()
    const labels = wrapper.findAll('.el-tabs__item').map((item) => item.text())
    for (const expected of ['Overview', 'Configuration', 'Wiring', 'Events', 'Danger zone']) {
      expect(labels.some((label) => label.includes(expected))).toBe(true)
    }
    expect(wrapper.text()).toContain('1.2.0')
    expect(wrapper.text()).toContain('fleet-vm')
    expect(wrapper.text()).toContain('Health history')
    expect(wrapper.text()).toContain('Healthy')
  })

  it('starts and stops through the shared toggle action', async () => {
    const { wrapper } = await render()
    const stop = wrapper.findAll('button').find((button) => button.text().includes('Stop'))
    await stop!.trigger('click')
    await flushPromises()
    expect(api.togglePlugin).toHaveBeenCalledWith('vm', false)
  })

  it('saves only operator-editable configuration values', async () => {
    const { wrapper } = await render()
    await tab(wrapper, 'Configuration').trigger('click')
    const inputs = wrapper.findAll('.detail-config input')
    expect(inputs.length).toBe(2)
    await inputs[0]!.setValue('7d')

    const save = wrapper.findAll('button').find((button) => button.text().includes('Save configuration'))
    await save!.trigger('click')
    await flushPromises()

    expect(api.configurePlugin).toHaveBeenCalledWith('vm', { retention: '7d', listen_port: '8428' })
  })

  it('renders wiring targets and restores the static default', async () => {
    const { wrapper } = await render()
    await tab(wrapper, 'Wiring').trigger('click')
    expect(wrapper.text()).toContain('Metrics ingestion')
    expect(wrapper.text()).toContain('in-memory ring')

    const restore = wrapper.findAll('button').find((button) => button.text().includes('Restore default'))
    await restore!.trigger('click')
    await flushPromises()
    expect(api.restorePluginWiring).toHaveBeenCalledWith('vm', 'metrics.remote_write_url')
  })

  it('lists recent event deliveries and errors', async () => {
    const { wrapper } = await render()
    await tab(wrapper, 'Events').trigger('click')
    expect(wrapper.text()).toContain('agent.connected delivered')
    expect(wrapper.text()).toContain('webhook timeout')
  })

  it('requires the typed plugin name before uninstalling', async () => {
    const { wrapper, router } = await render()
    await tab(wrapper, 'Danger zone').trigger('click')

    const input = wrapper.find('.danger-confirm input')
    const button = wrapper.findAll('button').find((button) => button.text().includes('Uninstall plugin'))
    expect(button!.attributes('disabled')).toBeDefined()

    await input.setValue('not-the-name')
    expect(button!.attributes('disabled')).toBeDefined()

    await input.setValue('VictoriaMetrics')
    expect(button!.attributes('disabled')).toBeUndefined()
    await button!.trigger('click')
    await flushPromises()

    expect(api.uninstallPlugin).toHaveBeenCalledWith('vm')
    expect(router.currentRoute.value.path).toBe('/plugins')
  })

  it('reports a plugin that is not installed with a single next action', async () => {
    const { wrapper } = await render('missing')
    expect(wrapper.text()).toContain('not installed')
    expect(wrapper.text()).toContain('Back to plugins')
  })

  it('shows the error state when loading fails', async () => {
    const { wrapper } = await render('vm', plugin(), true)
    expect(wrapper.text()).toContain('Could not load plugin data')
    expect(wrapper.text()).toContain('detail down')
  })

  it('shows the permission-denied state for viewers', async () => {
    const { wrapper } = await render()
    const auth = useAuthStore()
    auth.role = 'viewer'
    await flushPromises()
    expect(wrapper.text()).toContain('do not have permission')
  })
})

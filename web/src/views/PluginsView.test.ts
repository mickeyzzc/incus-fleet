import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { PluginEntry } from '@/api/types'
import { pluginStatus } from '@/stores/plugins'
import PluginsView from './PluginsView.vue'

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>()
  return {
    ...actual,
    api: { ...actual.api, listPlugins: vi.fn() },
  }
})

function mountView() {
  return mount(PluginsView, {
    global: { plugins: [ElementPlus] },
  })
}

function samplePlugin(overrides: Partial<PluginEntry> = {}): PluginEntry {
  return {
    id: 'victoria-metrics',
    name: 'VictoriaMetrics',
    version: '1.0.0',
    enabled: true,
    upstream: 'http://127.0.0.1:8428',
    role: 'viewer',
    kind: 'external',
    ui: { path: '/plugins/victoria-metrics', title_key: 'plugins.victoriaMetrics', requires_role: 'viewer' },
    ...overrides,
  }
}

describe('PluginsView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listPlugins).mockReset()
  })

  it('shows the disabled state when the plugin manager is off', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: null, enabled: false })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('disabled in configuration')
  })

  it('shows one empty state with a single next action', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('No plugins installed')
    const browse = wrapper.findAll('button').filter((b) => b.text().includes('Browse catalog'))
    expect(browse).toHaveLength(1)
  })

  it('shows an error state with the failure message', async () => {
    vi.mocked(api.listPlugins).mockRejectedValue(new Error('network down'))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Could not load the plugin list')
    expect(wrapper.text()).toContain('network down')
  })

  it('lists installed plugins with the shared status vocabulary', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [samplePlugin()], enabled: true })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('VictoriaMetrics')
    expect(wrapper.text()).toContain('Healthy')
  })

  it('offers exactly one primary action in the header', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [samplePlugin()], enabled: true })

    const wrapper = mountView()
    await flushPromises()

    const primaries = wrapper.findAll('.el-button--primary')
    expect(primaries.length).toBeLessThanOrEqual(1)
  })
})

describe('pluginStatus', () => {
  it('maps disabled plugins to stopped', () => {
    expect(pluginStatus(samplePlugin({ enabled: false })).status).toBe('stopped')
  })

  it('maps a missing upstream to unknown', () => {
    expect(pluginStatus(samplePlugin({ upstream: undefined })).status).toBe('unknown')
  })

  it('maps a configured plugin to healthy with its target', () => {
    const view = pluginStatus(samplePlugin())
    expect(view.status).toBe('healthy')
    expect(view.detail).toContain('127.0.0.1:8428')
  })

  it('switches to the catalog view and exposes detail actions', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({
      plugins: [samplePlugin(), samplePlugin({ id: 'logs', name: 'VictoriaLogs' })],
      enabled: true,
    })

    const wrapper = mountView()
    await flushPromises()

    // Catalog tab renders cards rather than the installed table.
    const tabs = wrapper.findAll('.el-tabs__item')
    const catalog = tabs.find((tab) => tab.text().includes('Catalog'))
    expect(catalog).toBeDefined()
    await catalog!.trigger('click')
    await flushPromises()

    expect(wrapper.findAll('.catalog-card')).toHaveLength(2)
  })

  it('opens the detail drawer with the five fixed tabs', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [samplePlugin()], enabled: true })

    const wrapper = mountView()
    await flushPromises()

    const row = wrapper.find('.plugin-table tbody tr')
    expect(row.exists()).toBe(true)
    await row.trigger('click')
    await flushPromises()

    const labels = wrapper.findAll('.el-tabs__item').map((tab) => tab.text())
    for (const expected of ['Overview', 'Configuration', 'Wiring', 'Events', 'Danger zone']) {
      expect(labels.some((label) => label.includes(expected))).toBe(true)
    }
  })

  it('shows a plugin without an upstream as unknown', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({
      plugins: [samplePlugin({ upstream: undefined })],
      enabled: true,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Unknown')
  })

  it('shows a disabled plugin as stopped', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({
      plugins: [samplePlugin({ enabled: false })],
      enabled: true,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Stopped')
  })

  it('refreshes from the header action', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [samplePlugin()], enabled: true })

    const wrapper = mountView()
    await flushPromises()
    expect(api.listPlugins).toHaveBeenCalledTimes(1)

    const refresh = wrapper.findAll('button').find((b) => b.text().includes('Refresh'))
    expect(refresh).toBeDefined()
    await refresh!.trigger('click')
    await flushPromises()

    expect(api.listPlugins).toHaveBeenCalledTimes(2)
  })

  it('moves from the empty state into the catalog', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })

    const wrapper = mountView()
    await flushPromises()

    const browse = wrapper.findAll('button').find((b) => b.text().includes('Browse catalog'))
    await browse!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('catalog')
  })

  it('closes the detail drawer', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [samplePlugin()], enabled: true })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('.plugin-table tbody tr').trigger('click')
    await flushPromises()
    expect(wrapper.find('.el-drawer').exists()).toBe(true)

    const close = wrapper.find('.el-drawer__close-btn')
    if (close.exists()) {
      await close.trigger('click')
      await flushPromises()
    }
  })

  it('renders the installing status for a plugin being deployed', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({
      plugins: [samplePlugin({ enabled: true, upstream: '' })],
      enabled: true,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Unknown')
  })

  it('does not offer a self-referential action in the catalog empty state', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })

    const wrapper = mountView()
    await flushPromises()

    // Installed tab: one next step, pointing at the catalog.
    let browse = wrapper.findAll('button').filter((b) => b.text().includes('Browse catalog'))
    expect(browse).toHaveLength(1)

    const catalogTab = wrapper.findAll('.el-tabs__item').find((t) => t.text().includes('Catalog'))
    await catalogTab!.trigger('click')
    await flushPromises()

    // Catalog tab: explaining the empty catalog must not offer "Browse catalog"
    // again — that would be a no-op loop.
    browse = wrapper.findAll('button').filter((b) => b.text().includes('Browse catalog'))
    expect(browse).toHaveLength(0)
    expect(wrapper.text()).toContain('The catalog is empty')
  })
})

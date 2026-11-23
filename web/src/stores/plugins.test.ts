import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { PluginEntry } from '@/api/types'
import { usePluginsStore } from './plugins'

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>()
  return {
    ...actual,
    api: { ...actual.api, listPlugins: vi.fn() },
  }
})

function samplePlugin(overrides: Partial<PluginEntry> = {}): PluginEntry {
  return {
    id: 'vm',
    name: 'VictoriaMetrics',
    version: '1.0.0',
    enabled: true,
    upstream: 'http://127.0.0.1:8428',
    role: 'viewer',
    kind: 'external',
    ui: { path: '/plugins/vm', title_key: 'plugins.vm', requires_role: 'viewer' },
    ...overrides,
  }
}

describe('usePluginsStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listPlugins).mockReset()
  })

  it('caches results until a forced refresh', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [samplePlugin()], enabled: true })

    const store = usePluginsStore()
    await store.fetch()
    await store.fetch()

    expect(api.listPlugins).toHaveBeenCalledTimes(1)
    expect(store.loaded).toBe(true)
    expect(store.count).toBe(1)

    await store.fetch(true)
    expect(api.listPlugins).toHaveBeenCalledTimes(2)
  })

  it('records a failure and leaves the loaded flag false', async () => {
    vi.mocked(api.listPlugins).mockRejectedValue(new Error('boom'))

    const store = usePluginsStore()
    await store.fetch()

    expect(store.error).toContain('boom')
    expect(store.loaded).toBe(false)
    expect(store.loading).toBe(false)
  })

  it('builds navigation only for enabled plugins with a path', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({
      plugins: [
        samplePlugin(),
        samplePlugin({ id: 'off', enabled: false }),
        samplePlugin({ id: 'nopath', ui: {} }),
      ],
      enabled: true,
    })

    const store = usePluginsStore()
    await store.fetch()

    expect(store.navigation).toHaveLength(1)
    const nav = store.navigation[0]
    if (!nav) throw new Error('expected one navigation entry')
    expect(nav.path).toBe('/plugins/vm')
    expect(nav.titleKey).toBe('plugins.vm')
    expect(nav.requiresRole).toBe('viewer')
  })

  it('falls back to a stable title key when the manifest omits one', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({
      plugins: [samplePlugin({ id: 'bare', ui: { path: '/plugins/bare' } })],
      enabled: true,
    })

    const store = usePluginsStore()
    await store.fetch()

    const nav = store.navigation[0]
    if (!nav) throw new Error('expected one navigation entry')
    expect(nav.titleKey).toBe('plugins.bare')
    expect(nav.fallbackTitle).toBe('VictoriaMetrics')
  })

  it('resolves a plugin by id and reports its UI URL', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({
      plugins: [samplePlugin(), samplePlugin({ id: 'nolink', ui: {} })],
      enabled: true,
    })

    const store = usePluginsStore()
    await store.fetch()

    const found = store.byId('vm')
    expect(found?.id).toBe('vm')
    expect(store.uiURL(found as PluginEntry)).toBe('/plugin-ui/vm/')

    const bare = store.byId('nolink') as PluginEntry
    expect(store.uiURL(bare)).toBe('/plugin-ui/nolink/')

    expect(store.byId('missing')).toBeUndefined()
  })

  it('reset clears every field', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [samplePlugin()], enabled: true })

    const store = usePluginsStore()
    await store.fetch()
    expect(store.count).toBe(1)

    store.reset()

    expect(store.items).toEqual([])
    expect(store.enabled).toBe(false)
    expect(store.loaded).toBe(false)
    expect(store.error).toBe('')
    expect(store.hasPlugins).toBe(false)
  })

  it('handles a null plugin list from the disabled manager', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: null, enabled: false })

    const store = usePluginsStore()
    await store.fetch()

    expect(store.items).toEqual([])
    expect(store.enabled).toBe(false)
    expect(store.hasPlugins).toBe(false)
  })
})

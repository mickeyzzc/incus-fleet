import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '@/api'
import type { PluginEntry, PluginInstallTaskStatus } from '@/api/types'
import { usePluginsStore } from './plugins'

vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      listPlugins: vi.fn(),
      listPluginCatalog: vi.fn().mockResolvedValue({ catalog: [] }),
      getPlugin: vi.fn(),
      installPlugin: vi.fn(),
      submitPluginInstallTask: vi.fn(),
      getPluginInstallTask: vi.fn(),
      listPluginInstallTasks: vi.fn(),
      cancelPluginInstallTask: vi.fn(),
      resumePluginInstallTask: vi.fn(),
      togglePlugin: vi.fn(),
      uninstallPlugin: vi.fn(),
      configurePlugin: vi.fn(),
      restorePluginWiring: vi.fn(),
    },
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
    vi.mocked(api.listPluginCatalog).mockReset()
    vi.mocked(api.listPluginCatalog).mockResolvedValue({ catalog: [] })
    vi.mocked(api.getPlugin).mockReset()
    vi.mocked(api.installPlugin).mockReset()
    vi.mocked(api.submitPluginInstallTask).mockReset()
    vi.mocked(api.getPluginInstallTask).mockReset()
    vi.mocked(api.listPluginInstallTasks).mockReset()
    vi.mocked(api.cancelPluginInstallTask).mockReset()
    vi.mocked(api.resumePluginInstallTask).mockReset()
    vi.mocked(api.togglePlugin).mockReset()
    vi.mocked(api.uninstallPlugin).mockReset()
    vi.mocked(api.configurePlugin).mockReset()
    vi.mocked(api.restorePluginWiring).mockReset()
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

  it('falls back to a namespaced title key when the manifest omits one', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({
      plugins: [samplePlugin({ id: 'bare', ui: { path: '/plugins/bare' } })],
      enabled: true,
    })

    const store = usePluginsStore()
    await store.fetch()

    const nav = store.navigation[0]
    if (!nav) throw new Error('expected one navigation entry')
    expect(nav.titleKey).toBe('plugins.text.bare.title')
    expect(nav.title).toBe('VictoriaMetrics')
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

  it('installs, toggles and uninstalls a catalog plugin', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.listPluginCatalog).mockResolvedValue({
      catalog: [{
        id: 'vm', name: 'VictoriaMetrics', description: 'metrics',
        upstream_hint: 'http://127.0.0.1:8428', ui_path: '/plugin-ui/vm/', wiring: 'metrics.query_url',
      }],
    })
    vi.mocked(api.installPlugin).mockResolvedValue({ plugin: samplePlugin(), token: 'one-time-token' })
    vi.mocked(api.togglePlugin).mockResolvedValue(samplePlugin({ enabled: false }))
    vi.mocked(api.uninstallPlugin).mockResolvedValue(undefined)

    const store = usePluginsStore()
    await store.fetch()
    expect(store.catalog).toHaveLength(1)

    const token = await store.install({ id: 'vm', upstream: 'http://127.0.0.1:8428' })
    expect(token).toBe('one-time-token')
    expect(api.installPlugin).toHaveBeenCalledWith({ id: 'vm', upstream: 'http://127.0.0.1:8428' })

    await store.toggle('vm', false)
    expect(api.togglePlugin).toHaveBeenCalledWith('vm', false)

    await store.uninstall('vm')
    expect(api.uninstallPlugin).toHaveBeenCalledWith('vm')
    expect(store.error).toBe('')
  })

  it('reports install failures without throwing', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.installPlugin).mockRejectedValue(new Error('bad upstream'))
    const store = usePluginsStore()

    const token = await store.install({ id: 'vm', upstream: 'invalid' })

    expect(token).toBe('')
    expect(store.error).toContain('bad upstream')
  })

  it('counts one badge for plugins that need attention', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({
      plugins: [
        samplePlugin(),
        samplePlugin({ id: 'bad', status: 'failed' }),
        samplePlugin({ id: 'stale', update_available: true }),
        samplePlugin({ id: 'off', enabled: false }),
      ],
      enabled: true,
    })
    const store = usePluginsStore()
    await store.fetch()

    expect(store.attentionCount).toBe(2)
  })

  function status(overrides: Partial<PluginInstallTaskStatus> = {}): PluginInstallTaskStatus {
    return {
      id: 'task-1',
      plugin_id: 'vm',
      state: 'running',
      attempt: 1,
      created_at: '2026-11-27T10:00:00Z',
      updated_at: '2026-11-27T10:00:00Z',
      ...overrides,
    }
  }

  it('submits a task and drives the panel from its server status', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.submitPluginInstallTask).mockResolvedValue({ task: status({ state: 'queued' }) })
    vi.mocked(api.getPluginInstallTask).mockResolvedValue({
      task: status({ progress: JSON.stringify({ step: 6 }) }),
    })

    const store = usePluginsStore()
    await store.fetch()

    expect(await store.startInstall({ id: 'vm', cluster: 'c1' })).toBe(true)
    expect(api.submitPluginInstallTask).toHaveBeenCalledWith({ id: 'vm', cluster: 'c1' })
    expect(api.installPlugin).not.toHaveBeenCalled()
    expect(api.getPluginInstallTask).toHaveBeenCalledWith('task-1')
    expect(store.task?.taskId).toBe('task-1')
    expect(store.task?.progress).toBe(60)
    expect(store.task?.currentStep).toBe(2)
    expect(store.task?.state).toBe('running')
  })

  it('polls a running task by id and stops once it succeeds', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.submitPluginInstallTask).mockResolvedValue({ task: status() })
    vi.mocked(api.getPluginInstallTask)
      .mockResolvedValueOnce({ task: status({ progress: JSON.stringify({ step: 3 }) }) })
      .mockResolvedValueOnce({ task: status({ progress: JSON.stringify({ step: 4 }) }) })
      .mockResolvedValueOnce({ task: status({ state: 'succeeded' }) })

    const store = usePluginsStore()
    await store.startInstall({ id: 'vm' })
    expect(store.task?.progress).toBe(30)
    expect(store.task?.state).toBe('running')

    await store.refreshTask()
    expect(store.task?.progress).toBe(40)

    await store.refreshTask()
    expect(store.task?.state).toBe('succeeded')
    expect(store.task?.steps.every((step) => step.state === 'done')).toBe(true)
    const calls = vi.mocked(api.getPluginInstallTask).mock.calls.length
    await store.refreshTask()
    expect(vi.mocked(api.getPluginInstallTask).mock.calls.length).toBe(calls)
  })

  it('keeps a polling error as a warning instead of failing the task', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.submitPluginInstallTask).mockResolvedValue({ task: status() })
    vi.mocked(api.getPluginInstallTask).mockRejectedValue(new Error('poll down'))

    const store = usePluginsStore()
    await store.startInstall({ id: 'vm' })
    await store.refreshTask()

    expect(store.task?.state).toBe('running')
    expect(store.task?.pollError).toContain('poll down')
  })

  it('resumes a failed task by its id', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.submitPluginInstallTask).mockResolvedValue({ task: status() })
    vi.mocked(api.getPluginInstallTask)
      .mockResolvedValueOnce({ task: status() })
      .mockResolvedValueOnce({ task: status({ state: 'failed', error: 'health failed' }) })
      .mockResolvedValueOnce({ task: status({ state: 'running' }) })
    vi.mocked(api.resumePluginInstallTask).mockResolvedValue({ status: 'queued' })

    const store = usePluginsStore()
    await store.startInstall({ id: 'vm' })
    await store.refreshTask()
    expect(store.task?.state).toBe('failed')
    expect(store.task?.error).toBe('health failed')

    expect(await store.retryInstall()).toBe(true)
    expect(api.resumePluginInstallTask).toHaveBeenCalledWith('task-1')
    expect(store.task?.state).toBe('running')
    expect(store.task?.error).toBe('')

    store.dismissTask()
    expect(store.task).toBeNull()
    expect(await store.retryInstall()).toBe(false)
  })

  it('reports a failed resume and falls back only when resume is unavailable', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.submitPluginInstallTask).mockResolvedValue({ task: status() })
    vi.mocked(api.getPluginInstallTask)
      .mockResolvedValueOnce({ task: status() })
      .mockResolvedValueOnce({ task: status({ state: 'failed', error: 'broken' }) })
    vi.mocked(api.resumePluginInstallTask)
      .mockRejectedValueOnce(new ApiError(500, 'boom', 'resume failed'))
      .mockRejectedValueOnce(new ApiError(404, 'not_found', 'no resume route'))

    const store = usePluginsStore()
    await store.startInstall({ id: 'vm' })
    await store.refreshTask()

    expect(await store.retryInstall()).toBe(false)
    expect(store.task?.error).toContain('resume failed')

    expect(await store.retryInstall()).toBe(true)
    expect(api.submitPluginInstallTask).toHaveBeenCalledTimes(2)
  })

  it('cancels a running task through the task endpoint', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.submitPluginInstallTask).mockResolvedValue({ task: status() })
    vi.mocked(api.getPluginInstallTask).mockResolvedValue({ task: status() })
    vi.mocked(api.cancelPluginInstallTask).mockResolvedValue(undefined)

    const store = usePluginsStore()
    await store.startInstall({ id: 'vm' })
    expect(await store.cancelTask()).toBe(true)
    expect(api.cancelPluginInstallTask).toHaveBeenCalledWith('task-1')
    expect(store.task?.state).toBe('canceled')
    expect(await store.cancelTask()).toBe(false)
  })

  it('surfaces a cancellation failure and can resume a canceled task', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.submitPluginInstallTask).mockResolvedValue({ task: status() })
    vi.mocked(api.getPluginInstallTask).mockResolvedValue({ task: status() })
    vi.mocked(api.cancelPluginInstallTask).mockRejectedValue(new Error('cannot cancel'))
    vi.mocked(api.resumePluginInstallTask).mockResolvedValue({ status: 'queued' })

    const store = usePluginsStore()
    await store.startInstall({ id: 'vm' })
    expect(await store.cancelTask()).toBe(false)
    expect(store.error).toContain('cannot cancel')
    expect(store.task?.pollError).toContain('cannot cancel')

    // A task can still be resumed from a canceled checkpoint.
    store.task!.state = 'canceled'
    expect(await store.retryInstall()).toBe(true)
  })

  it('falls back to the legacy install endpoint only when tasks are unavailable', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.submitPluginInstallTask).mockRejectedValue(new ApiError(404, 'not_found', 'missing route'))
    vi.mocked(api.installPlugin).mockResolvedValue({ plugin: samplePlugin(), token: 'legacy-token' })

    const store = usePluginsStore()
    expect(await store.startInstall({ id: 'vm' })).toBe(true)
    expect(store.task?.token).toBe('legacy-token')
    expect(store.task?.taskId).toBe('')
    expect(api.installPlugin).toHaveBeenCalledTimes(1)
  })

  it('does not silently fall back for real task errors', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.submitPluginInstallTask).mockRejectedValue(new ApiError(500, 'boom', 'server exploded'))

    const store = usePluginsStore()
    expect(await store.startInstall({ id: 'vm' })).toBe(false)
    expect(store.task?.state).toBe('failed')
    expect(store.task?.error).toContain('server exploded')
    expect(api.installPlugin).not.toHaveBeenCalled()
  })

  it('retries a legacy task by resubmitting when no task id exists', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.submitPluginInstallTask).mockRejectedValue(new ApiError(405, 'method_not_allowed', 'unused'))
    vi.mocked(api.installPlugin)
      .mockRejectedValueOnce(new Error('import failed'))
      .mockResolvedValueOnce({ plugin: samplePlugin(), token: 'token' })

    const store = usePluginsStore()
    expect(await store.startInstall({ id: 'vm' })).toBe(false)
    expect(store.task?.steps[0]?.state).toBe('failed')

    expect(await store.retryInstall()).toBe(true)
    expect(store.task?.state).toBe('succeeded')
    expect(store.task?.steps.every((step) => step.state === 'done')).toBe(true)
  })

  it('loads the rich descriptor and merges it into the cached entry', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [samplePlugin()], enabled: true })
    vi.mocked(api.getPlugin).mockResolvedValue(samplePlugin({
      config: { retention: '7d' },
      wiring: [{ capability: 'Metrics', target: 'http://vm.example:8428', default_target: 'ring' }],
    }))

    const store = usePluginsStore()
    await store.fetch()
    const descriptor = await store.fetchDetail('vm')

    expect(api.getPlugin).toHaveBeenCalledWith('vm')
    expect(descriptor?.config).toEqual({ retention: '7d' })
    expect(store.byId('vm')?.wiring).toHaveLength(1)
    expect(store.byId('vm')?.upstream).toBe('http://127.0.0.1:8428')
  })

  it('falls back to the list entry when the descriptor cannot be loaded', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [samplePlugin()], enabled: true })
    vi.mocked(api.getPlugin).mockRejectedValue(new Error('descriptor down'))

    const store = usePluginsStore()
    await store.fetch()
    expect(await store.fetchDetail('vm')).toMatchObject({ id: 'vm' })
    expect(store.detailError).toContain('descriptor down')
    expect(await store.fetchDetail('missing')).toBeNull()
  })

  it('saves configuration and restores wiring through the store', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.configurePlugin).mockResolvedValue(samplePlugin())
    vi.mocked(api.restorePluginWiring).mockResolvedValue(undefined)
    const store = usePluginsStore()
    await store.fetch()

    expect(await store.saveConfig('vm', { retention: '7d' })).toBe(true)
    expect(api.configurePlugin).toHaveBeenCalledWith('vm', { retention: '7d' })

    expect(await store.restoreWiring('vm', 'metrics.query_url')).toBe(true)
    expect(api.restorePluginWiring).toHaveBeenCalledWith('vm', 'metrics.query_url')
    expect(store.error).toBe('')
  })

  it('reports config and wiring failures without throwing', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.configurePlugin).mockRejectedValue(new Error('config endpoint down'))
    vi.mocked(api.restorePluginWiring).mockRejectedValue(new Error('wiring endpoint down'))
    const store = usePluginsStore()
    await store.fetch()

    expect(await store.saveConfig('vm', {})).toBe(false)
    expect(store.error).toContain('config endpoint down')
    expect(await store.restoreWiring('vm', 'key')).toBe(false)
    expect(store.error).toContain('wiring endpoint down')
  })
})

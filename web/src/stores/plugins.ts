import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api, ApiError } from '@/api'
import type {
  PluginCatalogEntry,
  PluginEntry,
  PluginInstallRequest,
  PluginInstallTaskStatus,
} from '@/api/types'
import {
  applyPluginTaskStatus,
  INSTALL_STEP_DEFS,
  pluginStatus as describeStatus,
  type PluginInstallTask,
  type PluginNavEntry,
} from '@/plugins/model'
import { errorMessage } from '@/utils/errors'

export type { PluginStatus, PluginStatusView } from '@/plugins/model'
export { pluginStatus } from '@/plugins/model'

export const usePluginsStore = defineStore('plugins', () => {
  const items = ref<PluginEntry[]>([])
  const catalog = ref<PluginCatalogEntry[]>([])
  const enabled = ref(false)
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')
  /** Descriptor-specific failure, kept separate from the list error. */
  const detailError = ref('')
  const task = ref<PluginInstallTask | null>(null)

  const installed = computed(() => items.value)
  const count = computed(() => items.value.length)
  const hasPlugins = computed(() => items.value.length > 0)

  /** One badge on the Plugins entry, never one badge per plugin. */
  const attentionCount = computed(() => items.value.filter((entry) => {
    const status = describeStatus(entry).status
    return status === 'installing' || status === 'degraded' || status === 'failed'
      || entry.update_available === true
  }).length)

  /** Navigation descriptors consumed by the router, shell and icon map. */
  const navigation = computed<PluginNavEntry[]>(() =>
    items.value
      .filter((entry) => entry.enabled && entry.ui?.path)
      .map((entry) => ({
        id: entry.id,
        path: entry.ui.path as string,
        titleKey: entry.ui.title_key ?? `plugins.text.${entry.id}.title`,
        title: entry.name || entry.id,
        icon: entry.ui.icon ?? '',
        requiresRole: entry.ui.requires_role ?? entry.role,
        messages: entry.ui.messages,
      })),
  )

  async function fetch(force = false): Promise<void> {
    if (loaded.value && !force) {
      return
    }
    loading.value = true
    error.value = ''
    try {
      const [response, catalogResponse] = await Promise.all([
        api.listPlugins(),
        api.listPluginCatalog(),
      ])
      items.value = response.plugins ?? []
      catalog.value = catalogResponse.catalog ?? []
      enabled.value = response.enabled
      loaded.value = true
    } catch (err) {
      error.value = errorMessage(err)
    } finally {
      loading.value = false
    }
  }

  function byId(id: string): PluginEntry | undefined {
    return items.value.find((entry) => entry.id === id)
  }

  /**
   * Loads the rich descriptor for the detail page. The list endpoint may omit
   * config, wiring, events and health history, so a successful descriptor is
   * merged into the cached entry; a failure keeps the list data as a fallback.
   */
  async function fetchDetail(id: string): Promise<PluginEntry | null> {
    detailError.value = ''
    if (!id) return null
    try {
      const descriptor = await api.getPlugin(id)
      if (descriptor && typeof descriptor === 'object' && descriptor.id) {
        const index = items.value.findIndex((entry) => entry.id === descriptor.id)
        if (index >= 0) {
          items.value[index] = { ...items.value[index], ...descriptor }
        } else {
          items.value.push(descriptor)
        }
        return descriptor
      }
    } catch (err) {
      detailError.value = errorMessage(err)
    }
    return byId(id) ?? null
  }

  /** Deploys (or connects) a catalog plugin; returns its one-time token. */
  async function install(request: PluginInstallRequest): Promise<string> {
    loading.value = true
    error.value = ''
    try {
      const result = await api.installPlugin(request)
      await fetch(true)
      return result.token
    } catch (err) {
      error.value = errorMessage(err)
      return ''
    } finally {
      loading.value = false
    }
  }

  function makeTask(request: PluginInstallRequest): PluginInstallTask {
    const entry = byId(request.id)
    return {
      pluginId: request.id,
      pluginName: entry?.name ?? request.id,
      taskId: '',
      state: 'running',
      currentStep: 0,
      steps: INSTALL_STEP_DEFS.map((step) => ({ ...step, state: 'pending' })),
      error: '',
      token: '',
      request,
    }
  }

  function markFailed(current: PluginInstallTask, message: string): void {
    current.state = 'failed'
    current.error = message
    error.value = message
    const index = Math.min(current.currentStep, current.steps.length - 1)
    const step = current.steps[index]
    if (step) {
      step.state = 'failed'
      current.currentStep = index
    }
  }

  /**
   * The synchronous endpoint is only a compatibility path for older servers.
   * A real install failure must never be hidden by falling back to it.
   */
  function taskEndpointUnavailable(err: unknown): boolean {
    if (!(err instanceof ApiError)) return false
    if ([404, 405, 501].includes(err.status)) return true
    return ['not_found', 'not_implemented', 'unsupported', 'installer_unavailable'].includes(err.code)
  }

  /** Refreshes the active task from the server using its task id. */
  async function refreshTask(): Promise<void> {
    const current = task.value
    if (!current?.taskId || current.state === 'succeeded'
      || current.state === 'failed' || current.state === 'canceled') {
      return
    }
    current.pollError = ''
    try {
      const response: PluginInstallTaskStatus | undefined =
        (await api.getPluginInstallTask(current.taskId))?.task
      if (!response) return
      Object.assign(current, applyPluginTaskStatus(current, response))
      if (response.state === 'succeeded') {
        await fetch(true)
        await fetchDetail(current.pluginId)
      }
    } catch (err) {
      // Polling is advisory: a transient failure must not fail the install.
      current.pollError = errorMessage(err)
    }
  }

  /** Starts an install as a tracked task rather than a blocking dialog. */
  async function startInstall(request: PluginInstallRequest): Promise<boolean> {
    task.value = makeTask(request)
    // Mutate through the reactive ref, never the raw object, so the task panel
    // sees each transition.
    const current = task.value
    current.steps[0]!.state = 'running'
    loading.value = true
    error.value = ''

    try {
      const response = (await api.submitPluginInstallTask(request))?.task
      if (!response?.id) {
        throw new Error('install task response has no id')
      }
      current.taskId = response.id
      Object.assign(current, applyPluginTaskStatus(current, response))
      await refreshTask()
      // A submitted task is a successful start; completion is tracked by the
      // panel through subsequent refreshes.
      return true
    } catch (err) {
      if (!taskEndpointUnavailable(err)) {
        markFailed(current, errorMessage(err))
        return false
      }
      // Compatibility with a server that has no task endpoint yet.
      const token = await install(request)
      if (!token) {
        markFailed(current, error.value)
        return false
      }
      current.token = token
      current.state = 'succeeded'
      current.currentStep = current.steps.length
      for (const step of current.steps) {
        step.state = 'done'
      }
      return true
    } finally {
      loading.value = false
    }
  }

  /** Requests cooperative cancellation of an in-flight task. */
  async function cancelTask(): Promise<boolean> {
    const current = task.value
    if (!current?.taskId || current.state !== 'running') return false
    loading.value = true
    error.value = ''
    try {
      await api.cancelPluginInstallTask(current.taskId)
      current.state = 'canceled'
      current.pollError = ''
      return true
    } catch (err) {
      error.value = errorMessage(err)
      current.pollError = error.value
      return false
    } finally {
      loading.value = false
    }
  }

  /** Resumes a failed task at its last checkpoint (or resubmits legacy installs). */
  async function retryInstall(): Promise<boolean> {
    const current = task.value
    if (!current || (current.state !== 'failed' && current.state !== 'canceled')) {
      return false
    }
    if (!current.taskId) {
      return startInstall(current.request)
    }

    error.value = ''
    current.state = 'running'
    current.error = ''
    current.pollError = ''
    let armed = false
    current.steps = current.steps.map((step) => {
      if (step.state === 'done') return step
      if (!armed) {
        armed = true
        return { ...step, state: 'running' as const }
      }
      return { ...step, state: 'pending' as const }
    })
    try {
      await api.resumePluginInstallTask(current.taskId)
      await refreshTask()
      return true
    } catch (err) {
      if (taskEndpointUnavailable(err)) {
        return startInstall(current.request)
      }
      markFailed(current, errorMessage(err))
      return false
    }
  }

  function dismissTask(): void {
    task.value = null
  }

  async function toggle(id: string, nextEnabled: boolean): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      await api.togglePlugin(id, nextEnabled)
      await fetch(true)
    } catch (err) {
      error.value = errorMessage(err)
    } finally {
      loading.value = false
    }
  }

  async function uninstall(id: string): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      await api.uninstallPlugin(id)
      await fetch(true)
    } catch (err) {
      error.value = errorMessage(err)
    } finally {
      loading.value = false
    }
  }

  async function saveConfig(id: string, config: Record<string, string>): Promise<boolean> {
    loading.value = true
    error.value = ''
    try {
      await api.configurePlugin(id, config)
      await fetch(true)
      return true
    } catch (err) {
      error.value = errorMessage(err)
      return false
    } finally {
      loading.value = false
    }
  }

  async function restoreWiring(id: string, key: string): Promise<boolean> {
    loading.value = true
    error.value = ''
    try {
      await api.restorePluginWiring(id, key)
      await fetch(true)
      return true
    } catch (err) {
      error.value = errorMessage(err)
      return false
    } finally {
      loading.value = false
    }
  }

  /**
   * The same-origin URL a plugin's UI is proxied under. The declared `ui.path`
   * is the SPA route that hosts it; the proxied content is always served from
   * the dedicated /plugin-ui/<id>/ namespace so it cannot shadow /plugins.
   */
  function uiURL(entry: PluginEntry): string {
    return `/plugin-ui/${entry.id}/`
  }

  function reset(): void {
    items.value = []
    catalog.value = []
    enabled.value = false
    loaded.value = false
    error.value = ''
    detailError.value = ''
    task.value = null
  }

  return {
    items, catalog, enabled, loading, loaded, error, detailError, task,
    installed, count, hasPlugins, attentionCount, navigation,
    fetch, fetchDetail, byId, uiURL, install, startInstall, refreshTask,
    cancelTask, retryInstall, dismissTask,
    toggle, uninstall, saveConfig, restoreWiring, reset,
  }
})

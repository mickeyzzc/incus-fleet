import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api } from '@/api'
import type { PluginCatalogEntry, PluginEntry, PluginInstallRequest } from '@/api/types'
import {
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
      state: 'running',
      currentStep: 0,
      steps: INSTALL_STEP_DEFS.map((step) => ({ ...step, state: 'pending' })),
      error: '',
      token: '',
      request,
    }
  }

  /** Starts an install as a resumable task rather than a blocking dialog. */
  async function startInstall(request: PluginInstallRequest): Promise<boolean> {
    task.value = makeTask(request)
    // Mutate through the reactive ref, never the raw object, so the task panel
    // sees each transition.
    const current = task.value
    current.steps[0]!.state = 'running'

    const token = await install(request)
    if (!token) {
      current.state = 'failed'
      current.error = error.value
      current.steps[current.currentStep]!.state = 'failed'
      return false
    }

    current.token = token
    current.state = 'succeeded'
    current.currentStep = current.steps.length
    for (const step of current.steps) {
      step.state = 'done'
    }
    return true
  }

  /** Re-runs a failed install from the step that failed. */
  async function retryInstall(): Promise<boolean> {
    if (!task.value || task.value.state !== 'failed') {
      return false
    }
    return startInstall(task.value.request)
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

  async function saveConfig(id: string, config: Record<string, string>): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      await api.configurePlugin(id, config)
      await fetch(true)
    } catch (err) {
      error.value = errorMessage(err)
    } finally {
      loading.value = false
    }
  }

  async function restoreWiring(id: string, key: string): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      await api.restorePluginWiring(id, key)
      await fetch(true)
    } catch (err) {
      error.value = errorMessage(err)
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
    task.value = null
  }

  return {
    items, catalog, enabled, loading, loaded, error, task,
    installed, count, hasPlugins, attentionCount, navigation,
    fetch, byId, uiURL, install, startInstall, retryInstall, dismissTask,
    toggle, uninstall, saveConfig, restoreWiring, reset,
  }
})

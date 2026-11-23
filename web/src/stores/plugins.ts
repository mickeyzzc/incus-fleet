import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api } from '@/api'
import type { PluginEntry } from '@/api/types'
import { errorMessage } from '@/utils/errors'

/**
 * Status vocabulary shared by every plugin surface. One word and one colour per
 * state, product-wide.
 */
export type PluginStatus = 'installing' | 'healthy' | 'degraded' | 'stopped' | 'failed' | 'unknown'

export interface PluginStatusView {
  status: PluginStatus
  /** Short, human sentence explaining what the state means right now. */
  detail: string
}

/** Maps a descriptor to the single status vocabulary. */
export function pluginStatus(entry: PluginEntry): PluginStatusView {
  if (!entry.enabled) {
    return { status: 'stopped', detail: 'Disabled in configuration' }
  }
  if (!entry.upstream) {
    return { status: 'unknown', detail: 'No upstream configured' }
  }

  return { status: 'healthy', detail: `Proxied to ${entry.upstream}` }
}

export const usePluginsStore = defineStore('plugins', () => {
  const items = ref<PluginEntry[]>([])
  const enabled = ref(false)
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')

  const installed = computed(() => items.value)
  const count = computed(() => items.value.length)
  const hasPlugins = computed(() => items.value.length > 0)

  /** The navigation entries contributed by plugins, in manifest order. */
  const navigation = computed(() =>
    items.value
      .filter((entry) => entry.enabled && entry.ui?.path)
      .map((entry) => ({
        id: entry.id,
        path: entry.ui.path as string,
        titleKey: entry.ui.title_key ?? `plugins.${entry.id}`,
        fallbackTitle: entry.name || entry.id,
        icon: entry.ui.icon ?? '',
        requiresRole: entry.ui.requires_role ?? entry.role,
      })),
  )

  async function fetch(force = false): Promise<void> {
    if (loaded.value && !force) {
      return
    }
    loading.value = true
    error.value = ''
    try {
      const response = await api.listPlugins()
      items.value = response.plugins ?? []
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
   * The same-origin URL a plugin's UI is proxied under. The declared `ui.path`
   * is the SPA route that hosts it; the proxied content is always served from
   * the dedicated /plugin-ui/<id>/ namespace so it cannot shadow /plugins.
   */
  function uiURL(entry: PluginEntry): string {
    return `/plugin-ui/${entry.id}/`
  }

  function reset(): void {
    items.value = []
    enabled.value = false
    loaded.value = false
    error.value = ''
  }

  return {
    items, enabled, loading, loaded, error,
    installed, count, hasPlugins, navigation,
    fetch, byId, uiURL, reset,
  }
})

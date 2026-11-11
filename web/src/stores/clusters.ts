import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api } from '@/api'
import type { ClusterSummary } from '@/api/types'
import { errorMessage } from '@/utils/errors'

/** Cached cluster inventory and health information. */
export const useClustersStore = defineStore('clusters', () => {
  const items = ref<ClusterSummary[]>([])
  const loading = ref(false)
  const error = ref('')
  const lastUpdated = ref<Date | null>(null)
  const reconnecting = ref('')

  const onlineCount = computed(() => items.value.filter((cluster) => cluster.online).length)

  /** Fetches every configured cluster. */
  async function fetchAll(): Promise<void> {
    loading.value = true
    error.value = ''

    try {
      items.value = await api.listClusters()
      lastUpdated.value = new Date()
    } catch (err) {
      error.value = errorMessage(err)
    } finally {
      loading.value = false
    }
  }

  /** Finds a cached cluster by name. */
  function byName(name: string): ClusterSummary | undefined {
    return items.value.find((cluster) => cluster.name === name)
  }

  function replace(summary: ClusterSummary): void {
    const index = items.value.findIndex((cluster) => cluster.name === summary.name)
    if (index === -1) {
      items.value.push(summary)
      return
    }

    items.value[index] = summary
  }

  /** Re-dials one cluster and updates the cached summary. */
  async function reconnect(name: string): Promise<ClusterSummary> {
    reconnecting.value = name
    error.value = ''

    try {
      const summary = await api.reconnectCluster(name)
      replace(summary)
      lastUpdated.value = new Date()
      return summary
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      reconnecting.value = ''
    }
  }

  return {
    items,
    loading,
    error,
    lastUpdated,
    reconnecting,
    onlineCount,
    fetchAll,
    byName,
    reconnect,
  }
})

import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api } from '@/api'
import type { InstanceDetail, InstanceSummary, InstanceType } from '@/api/types'
import { errorMessage } from '@/utils/errors'

/** Instance inventory across clusters, with per-cluster filtering. */
export const useInstancesStore = defineStore('instances', () => {
  const items = ref<InstanceSummary[]>([])
  const details = ref<Record<string, InstanceDetail>>({})
  const loading = ref(false)
  const detailLoading = ref(false)
  const error = ref('')
  const filter = ref<InstanceType>('any')
  const lastUpdated = ref<Date | null>(null)

  const containers = computed(
    () => items.value.filter((instance) => instance.type === 'container').length,
  )
  const virtualMachines = computed(
    () => items.value.filter((instance) => instance.type === 'virtual-machine').length,
  )

  function detailKey(cluster: string, name: string): string {
    return `${cluster}/${name}`
  }

  /** Fetches instances from all clusters, optionally filtered by type. */
  async function fetchAll(type: InstanceType = filter.value): Promise<void> {
    filter.value = type
    loading.value = true
    error.value = ''

    try {
      items.value = await api.listInstances(type)
      lastUpdated.value = new Date()
    } catch (err) {
      error.value = errorMessage(err)
    } finally {
      loading.value = false
    }
  }

  /** Replaces the inventory with one cluster's instances. */
  async function fetchCluster(cluster: string, type: InstanceType = 'any'): Promise<void> {
    loading.value = true
    error.value = ''

    try {
      items.value = await api.listClusterInstances(cluster, type)
      lastUpdated.value = new Date()
    } catch (err) {
      error.value = errorMessage(err)
    } finally {
      loading.value = false
    }
  }

  /** Loads and caches full detail for one instance. */
  async function fetchDetail(cluster: string, name: string): Promise<InstanceDetail | null> {
    detailLoading.value = true
    error.value = ''

    try {
      const detail = await api.getInstance(cluster, name)
      details.value[detailKey(cluster, name)] = detail
      return detail
    } catch (err) {
      error.value = errorMessage(err)
      return null
    } finally {
      detailLoading.value = false
    }
  }

  function detail(cluster: string, name: string): InstanceDetail | undefined {
    return details.value[detailKey(cluster, name)]
  }

  function byCluster(cluster: string): InstanceSummary[] {
    return items.value.filter((instance) => instance.cluster === cluster)
  }

  return {
    items,
    details,
    loading,
    detailLoading,
    error,
    filter,
    lastUpdated,
    containers,
    virtualMachines,
    fetchAll,
    fetchCluster,
    fetchDetail,
    detail,
    byCluster,
  }
})

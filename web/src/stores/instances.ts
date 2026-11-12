import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api } from '@/api'
import type {
  ClusterResources,
  CreateInstanceRequest,
  InstanceAction,
  InstanceDetail,
  InstanceImage,
  InstanceSummary,
  InstanceType,
  UpdateInstanceRequest,
} from '@/api/types'
import { errorMessage } from '@/utils/errors'

/** Instance inventory across clusters, with cached detail and cluster resources. */
export const useInstancesStore = defineStore('instances', () => {
  const items = ref<InstanceSummary[]>([])
  const details = ref<Record<string, InstanceDetail>>({})
  const imageCache = ref<Record<string, InstanceImage[]>>({})
  const resourceCache = ref<Record<string, ClusterResources>>({})
  const loading = ref(false)
  const detailLoading = ref(false)
  const imagesLoading = ref(false)
  const resourcesLoading = ref(false)
  const saving = ref(false)
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

  /** Lists local images for a cluster, reusing the cache unless forced. */
  async function fetchImages(cluster: string, force = false): Promise<void> {
    if (!force && imageCache.value[cluster]) {
      return
    }

    imagesLoading.value = true
    error.value = ''

    try {
      imageCache.value[cluster] = await api.listImages(cluster)
    } catch (err) {
      error.value = errorMessage(err)
      imageCache.value[cluster] = []
    } finally {
      imagesLoading.value = false
    }
  }

  /** Loads storage pool and network details for a cluster. */
  async function fetchResources(cluster: string, force = false): Promise<void> {
    if (!force && resourceCache.value[cluster]) {
      return
    }

    resourcesLoading.value = true
    error.value = ''

    try {
      resourceCache.value[cluster] = await api.getResources(cluster)
    } catch (err) {
      error.value = errorMessage(err)
      delete resourceCache.value[cluster]
    } finally {
      resourcesLoading.value = false
    }
  }

  /** Creates an instance and caches the returned detail. */
  async function createInstance(
    cluster: string,
    request: CreateInstanceRequest,
  ): Promise<InstanceDetail> {
    saving.value = true
    error.value = ''

    try {
      const detail = await api.createInstance(cluster, request)
      details.value[detailKey(cluster, detail.name)] = detail
      return detail
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Updates instance settings and caches the returned detail. */
  async function updateInstance(
    cluster: string,
    name: string,
    request: UpdateInstanceRequest,
  ): Promise<InstanceDetail> {
    saving.value = true
    error.value = ''

    try {
      const detail = await api.updateInstance(cluster, name, request)
      details.value[detailKey(cluster, name)] = detail
      return detail
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Starts, stops, or restarts an instance and refreshes its cached detail. */
  async function instanceAction(
    cluster: string,
    name: string,
    action: InstanceAction,
    force = false,
  ): Promise<void> {
    error.value = ''

    try {
      await api.instanceAction(cluster, name, { action, force })
      await fetchDetail(cluster, name)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    }
  }

  /** Deletes an instance and removes it from cached state. */
  async function deleteInstance(cluster: string, name: string, force = false): Promise<void> {
    error.value = ''

    try {
      await api.deleteInstance(cluster, name, force)
      items.value = items.value.filter(
        (instance) => !(instance.cluster === cluster && instance.name === name),
      )
      delete details.value[detailKey(cluster, name)]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    }
  }

  function detail(cluster: string, name: string): InstanceDetail | undefined {
    return details.value[detailKey(cluster, name)]
  }

  function images(cluster: string): InstanceImage[] {
    return imageCache.value[cluster] ?? []
  }

  function resources(cluster: string): ClusterResources | undefined {
    return resourceCache.value[cluster]
  }

  function byCluster(cluster: string): InstanceSummary[] {
    return items.value.filter((instance) => instance.cluster === cluster)
  }

  return {
    items,
    details,
    imageCache,
    resourceCache,
    loading,
    detailLoading,
    imagesLoading,
    resourcesLoading,
    saving,
    error,
    filter,
    lastUpdated,
    containers,
    virtualMachines,
    fetchAll,
    fetchCluster,
    fetchDetail,
    fetchImages,
    fetchResources,
    createInstance,
    updateInstance,
    instanceAction,
    deleteInstance,
    detail,
    images,
    resources,
    byCluster,
  }
})

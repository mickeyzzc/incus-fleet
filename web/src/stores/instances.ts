import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api } from '@/api'
import type {
  ClusterResources,
  CreateInstanceRequest,
  DeviceRequest,
  FileEntry,
  ImportImageRequest,
  CloneRequest,
  CopyImageRequest,
  ImportISORequest,
  InstanceAction,
  InstanceDetail,
  InstanceImage,
  InstanceSnapshot,
  InstanceSummary,
  InstanceType,
  ISOSummary,
  LaunchISORequest,
  MemberUpdateRequest,
  NetworkRequest,
  OverlayRequest,
  OverlayStatus,
  OpsRequest,
  OpsResult,
  Profile,
  ProfileRequest,
  SnapshotRequest,
  StoragePoolRequest,
  StorageVolume,
  UnattendedLaunchRequest,
  UnattendedLaunchResult,
  UpdateInstanceRequest,
  VolumeRequest,
} from '@/api/types'
import { errorMessage } from '@/utils/errors'

/** Instance inventory across clusters, with cached detail and cluster resources. */
export const useInstancesStore = defineStore('instances', () => {
  const items = ref<InstanceSummary[]>([])
  const details = ref<Record<string, InstanceDetail>>({})
  const imageCache = ref<Record<string, InstanceImage[]>>({})
  const isoCache = ref<Record<string, ISOSummary[]>>({})
  const resourceCache = ref<Record<string, ClusterResources>>({})
  const profileCache = ref<Record<string, Profile[]>>({})
  const snapshotCache = ref<Record<string, InstanceSnapshot[]>>({})
  const volumeCache = ref<Record<string, StorageVolume[]>>({})
  const fileCache = ref<Record<string, FileEntry[]>>({})
  const overlayCache = ref<Record<string, OverlayStatus[]>>({})
  const overlayLoading = ref(false)
  const loading = ref(false)
  const detailLoading = ref(false)
  const imagesLoading = ref(false)
  const resourcesLoading = ref(false)
  const profilesLoading = ref(false)
  const snapshotsLoading = ref(false)
  const volumesLoading = ref(false)
  const filesLoading = ref(false)
  const saving = ref(false)
  const error = ref('')
  /** Clusters that could not be reached during the last fleet-wide fetch. */
  const unavailableClusters = ref<string[]>([])
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

  function snapshotKey(cluster: string, name: string): string {
    return `${cluster}/${name}`
  }

  function volumeKey(cluster: string, pool: string): string {
    return `${cluster}/${pool}`
  }

  function fileKey(cluster: string, instance: string, dir: string): string {
    return `${cluster}/${instance}/${dir}`
  }

  /** Fetches instances from all clusters, optionally filtered by type. */
  async function fetchAll(type: InstanceType = filter.value): Promise<void> {
    filter.value = type
    loading.value = true
    error.value = ''

    try {
      items.value = await api.listInstances(type)
      unavailableClusters.value = api.lastInventoryFailures ?? []
      lastUpdated.value = new Date()
    } catch (err) {
      error.value = errorMessage(err)
      unavailableClusters.value = []
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

  /** Lists instance profiles for a cluster, reusing the cache unless forced. */
  async function fetchProfiles(cluster: string, force = false): Promise<void> {
    if (!force && profileCache.value[cluster]) {
      return
    }

    profilesLoading.value = true
    error.value = ''

    try {
      profileCache.value[cluster] = await api.listProfiles(cluster)
    } catch (err) {
      error.value = errorMessage(err)
      delete profileCache.value[cluster]
    } finally {
      profilesLoading.value = false
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
  async function deleteInstance(cluster: string, name: string, force = false, volumes = false): Promise<void> {
    error.value = ''

    try {
      await api.deleteInstance(cluster, name, force, volumes)
      items.value = items.value.filter(
        (instance) => !(instance.cluster === cluster && instance.name === name),
      )
      delete details.value[detailKey(cluster, name)]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    }
  }

  /** Creates a storage pool and invalidates cached cluster resources. */
  async function createStoragePool(cluster: string, request: StoragePoolRequest): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.createStoragePool(cluster, request)
      delete resourceCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Updates a storage pool and invalidates cached cluster resources. */
  async function updateStoragePool(
    cluster: string,
    name: string,
    request: StoragePoolRequest,
  ): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.updateStoragePool(cluster, name, request)
      delete resourceCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Deletes a storage pool and invalidates cached cluster resources. */
  async function deleteStoragePool(cluster: string, name: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.deleteStoragePool(cluster, name)
      delete resourceCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Creates a managed network and invalidates cached cluster resources. */
  async function createNetwork(cluster: string, request: NetworkRequest): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.createNetwork(cluster, request)
      delete resourceCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Updates a managed network and invalidates cached cluster resources. */
  async function updateNetwork(
    cluster: string,
    name: string,
    request: NetworkRequest,
  ): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.updateNetwork(cluster, name, request)
      delete resourceCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Deletes a managed network and invalidates cached cluster resources. */
  async function deleteNetwork(cluster: string, name: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.deleteNetwork(cluster, name)
      delete resourceCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Loads the private overlay definitions of one cluster. */
  async function fetchOverlays(cluster: string, force = false): Promise<OverlayStatus[]> {
    if (!force && overlayCache.value[cluster]) {
      return overlayCache.value[cluster] ?? []
    }

    overlayLoading.value = true
    try {
      const overlays = await api.listOverlays(cluster)
      overlayCache.value[cluster] = overlays
      return overlays
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      overlayLoading.value = false
    }
  }

  /** Enables a private overlay and refreshes the cached list. */
  async function enableOverlay(cluster: string, request: OverlayRequest): Promise<OverlayStatus> {
    saving.value = true
    error.value = ''

    try {
      const status = await api.enableOverlay(cluster, request)
      delete overlayCache.value[cluster]
      return status
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Updates an existing overlay and re-applies it. */
  async function updateOverlay(
    cluster: string,
    name: string,
    request: OverlayRequest,
  ): Promise<OverlayStatus> {
    saving.value = true
    error.value = ''

    try {
      const status = await api.updateOverlay(cluster, name, request)
      delete overlayCache.value[cluster]
      return status
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Re-applies an overlay against the current member list. */
  async function reconcileOverlay(cluster: string, name: string): Promise<OverlayStatus> {
    saving.value = true
    error.value = ''

    try {
      const status = await api.reconcileOverlay(cluster, name)
      delete overlayCache.value[cluster]
      return status
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Tears a private overlay down. */
  async function disableOverlay(cluster: string, name: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.disableOverlay(cluster, name)
      delete overlayCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Creates a profile and invalidates the cached profile list. */
  async function createProfile(cluster: string, request: ProfileRequest): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.createProfile(cluster, request)
      delete profileCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Updates a profile and invalidates the cached profile list. */
  async function updateProfile(
    cluster: string,
    name: string,
    request: ProfileRequest,
  ): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.updateProfile(cluster, name, request)
      delete profileCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Deletes a profile and invalidates the cached profile list. */
  async function deleteProfile(cluster: string, name: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.deleteProfile(cluster, name)
      delete profileCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Uploads an image tarball into a cluster and refreshes the image list. */
  async function importImage(cluster: string, request: ImportImageRequest): Promise<string | null> {
    saving.value = true
    error.value = ''

    try {
      const fingerprint = await api.importImage(cluster, request)
      delete imageCache.value[cluster]
      return fingerprint
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Distributes an image to another cluster and refreshes both lists. */
  async function copyImage(
    cluster: string,
    fingerprint: string,
    request: CopyImageRequest,
  ): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.copyImage(cluster, fingerprint, request)
      delete imageCache.value[cluster]
      delete imageCache.value[request.target_cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Deletes an image from a cluster and invalidates the cached image list. */
  async function deleteImage(cluster: string, fingerprint: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.deleteImage(cluster, fingerprint)
      delete imageCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Lists installer ISO volumes, reusing the cache unless forced. */
  async function fetchISOs(cluster: string, force = false): Promise<void> {
    if (!force && isoCache.value[cluster]) {
      return
    }
    error.value = ''
    try {
      isoCache.value[cluster] = await api.listISOs(cluster)
    } catch (err) {
      error.value = errorMessage(err)
      delete isoCache.value[cluster]
      // Consistent with other list fetches: surface the error in store state
      // instead of rejecting, so callers composing Promise.all stay stable.
    }
  }

  /** Uploads an ISO installer image and refreshes the ISO list. */
  async function importISO(cluster: string, request: ImportISORequest): Promise<string> {
    saving.value = true
    error.value = ''

    try {
      const created = await api.importISO(cluster, request)
      delete isoCache.value[cluster]
      return created.name
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Deletes an ISO volume and invalidates the cached ISO list. */
  async function deleteISO(cluster: string, pool: string, name: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.deleteISO(cluster, pool, name)
      delete isoCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Launches an installer VM from an ISO volume and refreshes instances. */
  async function launchFromISO(
    cluster: string,
    pool: string,
    name: string,
    request: LaunchISORequest,
  ): Promise<InstanceSummary | null> {
    saving.value = true
    error.value = ''

    try {
      const detail = await api.launchFromISO(cluster, pool, name, request)
      delete imageCache.value[cluster]
      if (detail) {
        items.value = [detail, ...items.value.filter((it) => !(it.cluster === cluster && it.name === detail.name))]
      }
      return detail
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

    /** Launches an unattended-install VM from an ISO and refreshes instances. */
  async function launchUnattended(
    cluster: string,
    pool: string,
    isoName: string,
    request: UnattendedLaunchRequest,
  ): Promise<UnattendedLaunchResult | null> {
    saving.value = true
    error.value = ''

    try {
      const { instance, launch } = await api.launchUnattended(cluster, pool, isoName, request)
      delete imageCache.value[cluster]
      if (instance) {
        items.value = [instance, ...items.value.filter((it) => !(it.cluster === cluster && it.name === instance.name))]
      }
      return launch
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

/** Runs a guest-side maintenance action inside an instance. */
  async function runOps(cluster: string, instance: string, request: OpsRequest): Promise<OpsResult> {
    saving.value = true
    error.value = ''

    try {
      return await api.runOps(cluster, instance, request)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Rebuilds a stopped instance from another image and refreshes details. */
  async function rebuildInstance(cluster: string, instance: string, image: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.rebuildInstance(cluster, instance, image)
      delete details.value[detailKey(cluster, instance)]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Refreshes a cached image from its origin. */
  async function refreshImage(cluster: string, fingerprint: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.refreshImage(cluster, fingerprint)
      delete imageCache.value[cluster]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Clones an instance within its cluster and refreshes the list. */
  async function cloneInstance(cluster: string, instance: string, request: CloneRequest): Promise<InstanceSummary | null> {
    saving.value = true
    error.value = ''

    try {
      const detail = await api.cloneInstance(cluster, instance, request)
      if (detail) {
        items.value = [detail, ...items.value.filter((it) => !(it.cluster === cluster && it.name === detail.name))]
      }
      return detail
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Lists instance snapshots, reusing the cache unless forced. */
  async function fetchSnapshots(
    cluster: string,
    instance: string,
    force = false,
  ): Promise<void> {
    const key = snapshotKey(cluster, instance)
    if (!force && snapshotCache.value[key]) {
      return
    }

    snapshotsLoading.value = true
    error.value = ''

    try {
      snapshotCache.value[key] = await api.listSnapshots(cluster, instance)
    } catch (err) {
      error.value = errorMessage(err)
      delete snapshotCache.value[key]
    } finally {
      snapshotsLoading.value = false
    }
  }

  /** Creates a snapshot and refreshes the cached list. */
  async function createSnapshot(
    cluster: string,
    instance: string,
    request: SnapshotRequest,
  ): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.createSnapshot(cluster, instance, request)
      await fetchSnapshots(cluster, instance, true)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Restores a snapshot and refreshes the instance detail. */
  async function restoreSnapshot(cluster: string, instance: string, snapshot: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.restoreSnapshot(cluster, instance, snapshot)
      await fetchDetail(cluster, instance)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Deletes a snapshot and refreshes the cached list. */
  async function deleteSnapshot(cluster: string, instance: string, snapshot: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.deleteSnapshot(cluster, instance, snapshot)
      await fetchSnapshots(cluster, instance, true)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Lists custom volumes of one pool, reusing the cache unless forced. */
  async function fetchVolumes(cluster: string, pool: string, force = false): Promise<void> {
    const key = volumeKey(cluster, pool)
    if (!force && volumeCache.value[key]) {
      return
    }

    volumesLoading.value = true
    error.value = ''

    try {
      volumeCache.value[key] = await api.listVolumes(cluster, pool)
    } catch (err) {
      error.value = errorMessage(err)
      delete volumeCache.value[key]
    } finally {
      volumesLoading.value = false
    }
  }

  /** Creates a custom volume and refreshes the cached list. */
  async function createVolume(cluster: string, pool: string, request: VolumeRequest): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.createVolume(cluster, pool, request)
      await fetchVolumes(cluster, pool, true)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Deletes a custom volume and refreshes the cached list. */
  async function deleteVolume(cluster: string, pool: string, name: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.deleteVolume(cluster, pool, name)
      await fetchVolumes(cluster, pool, true)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Attaches a device and caches the returned detail. */
  async function attachDevice(
    cluster: string,
    instance: string,
    request: DeviceRequest,
  ): Promise<InstanceDetail> {
    saving.value = true
    error.value = ''

    try {
      const detail = await api.attachDevice(cluster, instance, request)
      details.value[detailKey(cluster, instance)] = detail
      return detail
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Detaches a device and caches the returned detail. */
  async function detachDevice(
    cluster: string,
    instance: string,
    device: string,
  ): Promise<InstanceDetail> {
    saving.value = true
    error.value = ''

    try {
      const detail = await api.detachDevice(cluster, instance, device)
      details.value[detailKey(cluster, instance)] = detail
      return detail
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Updates one device's configuration and caches the returned detail. */
  async function updateDevice(
    cluster: string,
    instance: string,
    device: string,
    request: DeviceRequest,
  ): Promise<InstanceDetail> {
    saving.value = true
    error.value = ''

    try {
      const detail = await api.updateDevice(cluster, instance, device, request)
      details.value[detailKey(cluster, instance)] = detail
      return detail
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Lists one directory inside an instance, caching per path. */
  async function fetchFiles(cluster: string, instance: string, dir: string): Promise<void> {
    const key = fileKey(cluster, instance, dir)
    if (fileCache.value[key]) {
      return
    }

    filesLoading.value = true
    error.value = ''

    try {
      fileCache.value[key] = await api.listInstanceFiles(cluster, instance, dir)
    } catch (err) {
      error.value = errorMessage(err)
      delete fileCache.value[key]
    } finally {
      filesLoading.value = false
    }
  }

  /** Uploads one file into dir and refreshes the directory listing. */
  async function uploadFile(
    cluster: string,
    instance: string,
    dir: string,
    file: File,
  ): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.uploadInstanceFile(cluster, instance, joinPath(dir, file.name), file)
      delete fileCache.value[fileKey(cluster, instance, dir)]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Creates a directory inside dir and refreshes the listing. */
  async function mkdirDir(cluster: string, instance: string, dir: string, name: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.mkdirInstanceDir(cluster, instance, joinPath(dir, name))
      delete fileCache.value[fileKey(cluster, instance, dir)]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Deletes one file and refreshes the listing. */
  async function deleteFile(cluster: string, instance: string, dir: string, name: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.deleteInstanceFile(cluster, instance, joinPath(dir, name))
      delete fileCache.value[fileKey(cluster, instance, dir)]
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  function joinPath(dir: string, name: string): string {
    return dir === '/' ? `/${name}` : `${dir}/${name}`
  }

  function files(cluster: string, instance: string, dir: string): FileEntry[] {
    return fileCache.value[fileKey(cluster, instance, dir)] ?? []
  }

  /** Updates safe cluster member metadata. */
  async function updateMember(
    cluster: string,
    name: string,
    request: MemberUpdateRequest,
  ): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.updateMember(cluster, name, request)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Renames one Incus cluster member and clears cached node detail. */
  async function renameMember(cluster: string, name: string, newName: string): Promise<void> {
    saving.value = true
    error.value = ''

    try {
      await api.renameClusterMember(cluster, name, newName)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
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

  function profiles(cluster: string): Profile[] {
    return profileCache.value[cluster] ?? []
  }

  function byCluster(cluster: string): InstanceSummary[] {
    return items.value.filter((instance) => instance.cluster === cluster)
  }

  function snapshots(cluster: string, instance: string): InstanceSnapshot[] {
    return snapshotCache.value[snapshotKey(cluster, instance)] ?? []
  }

  function volumes(cluster: string, pool: string): StorageVolume[] {
    return volumeCache.value[volumeKey(cluster, pool)] ?? []
  }

  return {
    items,
    details,
    imageCache,
    resourceCache,
    profileCache,
    snapshotCache,
    volumeCache,
    loading,
    detailLoading,
    imagesLoading,
    resourcesLoading,
    profilesLoading,
    snapshotsLoading,
    volumesLoading,
    filesLoading,
    saving,
    error,
    unavailableClusters,
    overlayCache,
    overlayLoading,
    filter,
    lastUpdated,
    containers,
    virtualMachines,
    fetchAll,
    fetchCluster,
    fetchDetail,
    fetchImages,
    fetchResources,
    fetchProfiles,
    fetchSnapshots,
    fetchVolumes,
    createInstance,
    updateInstance,
    instanceAction,
    deleteInstance,
    createStoragePool,
    updateStoragePool,
    deleteStoragePool,
    createNetwork,
    updateNetwork,
    deleteNetwork,
    fetchOverlays,
    enableOverlay,
    updateOverlay,
    reconcileOverlay,
    disableOverlay,
    createProfile,
    updateProfile,
    deleteProfile,
    updateMember,
    renameMember,
    deleteImage,
    importImage,
    copyImage,
    refreshImage,
    isoCache,
    fetchISOs,
    importISO,
    deleteISO,
    launchFromISO,
    launchUnattended,
    runOps,
    rebuildInstance,
    cloneInstance,
    createSnapshot,
    restoreSnapshot,
    deleteSnapshot,
    createVolume,
    deleteVolume,
    attachDevice,
    detachDevice,
    updateDevice,
    fetchFiles,
    uploadFile,
    mkdirDir,
    deleteFile,
    detail,
    images,
    resources,
    profiles,
    snapshots,
    volumes,
    files,
    byCluster,
  }
})

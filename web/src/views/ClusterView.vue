<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Box, Coin, Cpu, EditPen, Monitor, Plus, Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import InstanceActions from '@/components/InstanceActions.vue'
import CopyableText from '@/components/CopyableText.vue'
import AddClusterMemberDialog from '@/components/AddClusterMemberDialog.vue'
import InstanceDeleteDialog from '@/components/InstanceDeleteDialog.vue'
import InstanceDetailDrawer from '@/components/InstanceDetailDrawer.vue'
import InstanceFormDialog from '@/components/InstanceFormDialog.vue'
import ImageDistributeDialog from '@/components/ImageDistributeDialog.vue'
import ImageImportDialog from '@/components/ImageImportDialog.vue'
import IsoImportDialog from '@/components/IsoImportDialog.vue'
import IsoLaunchDialog from '@/components/IsoLaunchDialog.vue'
import MemberDialog from '@/components/MemberDialog.vue'
import MigrateDialog from '@/components/MigrateDialog.vue'
import ClusterResourceTrend from '@/components/ClusterResourceTrend.vue'
import RenameClusterDialog from '@/components/RenameClusterDialog.vue'
import RenameInstanceDialog from '@/components/RenameInstanceDialog.vue'
import NetworkDialog from '@/components/NetworkDialog.vue'
import OverlayDialog from '@/components/OverlayDialog.vue'
import NodeUpgradeDialog from '@/components/NodeUpgradeDialog.vue'
import ProfileDialog from '@/components/ProfileDialog.vue'
import StatusTag from '@/components/StatusTag.vue'
import StorageBar from '@/components/StorageBar.vue'
import StoragePoolDialog from '@/components/StoragePoolDialog.vue'
import StorageVolumesDialog from '@/components/StorageVolumesDialog.vue'
import type {
  ClusterMember,
  InstanceDetail,
  InstanceSummary,
  InstanceType,
  NetworkResource,
  OverlayStatus,
  Profile,
  StoragePoolResource,
} from '@/api/types'
import { api } from '@/api'
import { useInstanceActions, useVisiblePolling } from '@/composables/useInstanceActions'
import { useClustersStore } from '@/stores/clusters'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'
import { formatBytes, formatDateTime, primaryAddress } from '@/utils/format'

const props = defineProps<{ cluster: string }>()
const router = useRouter()
const clusters = useClustersStore()
const instances = useInstancesStore()
const { t, te } = useI18n()

const activeTab = ref('overview')
const typeFilter = ref<InstanceType>('any')
const instanceSearch = ref('')
const instancePage = ref(1)
const instancePageSize = ref(20)
const detailOpen = ref(false)
const detailName = ref('')
const detailTab = ref<'overview' | 'devices'>('overview')
const migrateOpen = ref(false)
const migrateInstance = ref<InstanceSummary | null>(null)
const renameOpen = ref(false)
const renameName = ref('')
const formOpen = ref(false)
const formMode = ref<'create' | 'edit'>('create')
const formInstance = ref<InstanceDetail | null>(null)
const poolDialogOpen = ref(false)
const poolEditing = ref<StoragePoolResource | null>(null)
const networkDialogOpen = ref(false)
const networkEditing = ref<NetworkResource | null>(null)
const overlayDialogOpen = ref(false)
const overlayEditing = ref<OverlayStatus | null>(null)
const overlayItems = ref<OverlayStatus[]>([])
const overlayLoading = ref(false)
const profileDialogOpen = ref(false)
const profileEditing = ref<Profile | null>(null)
const memberDialogOpen = ref(false)
const memberEditing = ref<ClusterMember | null>(null)
const renameClusterOpen = ref(false)
const addMemberOpen = ref(false)
const volumesDialogOpen = ref(false)
const volumesPool = ref<StoragePoolResource | null>(null)
const importDialogOpen = ref(false)
const distributeOpen = ref(false)
const distributeFingerprint = ref('')

// Node management
const nodeUpgradeDialogOpen = ref(false)
const nodeUpgradeMember = ref<{ name: string; version: string }>({ name: '', version: '' })

const summary = computed(() => clusters.byName(props.cluster))
const resources = computed(() => instances.resources(props.cluster))
const profiles = computed(() => instances.profiles(props.cluster))
const clusterImages = computed(() => instances.images(props.cluster))
const clusterISOs = computed(() => instances.isoCache[props.cluster] ?? [])
const poolNames = computed(() => (resources.value?.storage_pools ?? []).map((pool) => pool.name))
// Cluster scope only owns managed networks. Physical NICs, addresses, routes
// and firewall rules are per-node facts and live exclusively on the node page.
const networkRows = computed(() =>
  [...(resources.value?.networks ?? [])]
    .filter((network) => network.managed)
    .sort((a, b) => a.name.localeCompare(b.name)),
)
const isoImportOpen = ref(false)
const isoLaunchOpen = ref(false)
const isoTarget = ref({ name: '', pool: '' })
const cachedImageCount = computed(() => clusterImages.value.filter((image) => image.cached).length)
const filtered = computed(() => {
  const query = instanceSearch.value.trim().toLowerCase()
  return instances.items.filter((instance) => {
    if (typeFilter.value !== 'any' && instance.type !== typeFilter.value) {
      return false
    }
    if (!query) {
      return true
    }
    return (
      instance.name.toLowerCase().includes(query) ||
      instance.location?.toLowerCase().includes(query) === true ||
      (instance.description ?? '').toLowerCase().includes(query)
    )
  })
})

const pagedInstances = computed(() => {
  const start = (instancePage.value - 1) * instancePageSize.value
  return filtered.value.slice(start, start + instancePageSize.value)
})

watch([typeFilter, instanceSearch], () => {
  instancePage.value = 1
})
watch(filtered, (rows) => {
  if (instancePage.value > Math.max(1, Math.ceil(rows.length / instancePageSize.value))) {
    instancePage.value = Math.max(1, Math.ceil(rows.length / instancePageSize.value))
  }
})
const onlineTargets = computed(() =>
  clusters.items.filter((cluster) => cluster.online && cluster.name !== props.cluster),
)

/** Renders a cluster member status enum value in the active language. */
function memberStatusLabel(status: string): string {
  const key = `cluster.memberStatus.${status.toLowerCase()}`
  return te(key) ? t(key) : status
}

function resourcePercent(used = 0, total = 0): number {
  if (!Number.isFinite(total) || total <= 0) {
    return 0
  }

  return Math.min(100, Math.max(0, (used / total) * 100))
}

function resourceShare(used = 0, total = 0): string {
  if (!Number.isFinite(total) || total <= 0) {
    return t('common.none')
  }

  return `${formatBytes(used)} / ${formatBytes(total)}`
}

async function load(force = false): Promise<void> {
  // Store error flags surface failures in the UI. Rejections are left to
  // propagate: useVisiblePolling catches them for the polling loop (and backs
  // off while the member stays down); stray non-poll callers are logged by the
  // global unhandledrejection handler.
  await Promise.all([
    clusters.fetchAll(),
    instances.fetchCluster(props.cluster, typeFilter.value),
    instances.fetchResources(props.cluster, force),
    instances.fetchProfiles(props.cluster, force),
    instances.fetchImages(props.cluster, force),
    instances.fetchISOs(props.cluster, force),
    loadOverlays(force),
  ])
}

onMounted(() => load())
useVisiblePolling(load)

watch(
  () => props.cluster,
  async () => {
    await load()
  },
)

const { act, confirmDelete, isBusy, deleteTarget, deleting, cancelDelete, submitDelete } = useInstanceActions(() => load())

watch(typeFilter, async (type) => {
  await instances.fetchCluster(props.cluster, type)
})

async function reconnect(): Promise<void> {
  try {
    await clusters.reconnect(props.cluster)
    ElMessage.success(t('dashboard.reconnectSuccess', { name: props.cluster }))
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

function openDetail(instance: InstanceSummary): void {
  // A poll can replace the row between the click and this handler, leaving an
  // empty name. Ignore that click instead of opening an empty drawer.
  if (!instance?.name) {
    return
  }
  detailName.value = instance.name
  detailTab.value = 'overview'
  detailOpen.value = true
}

function openMigrate(instance: InstanceSummary): void {
  migrateInstance.value = instance
  migrateOpen.value = true
}

function openRename(instance: Pick<InstanceSummary, 'name'>): void {
  renameName.value = instance.name
  renameOpen.value = true
}

async function removeMember(member: ClusterMember): Promise<void> {
  try {
    await ElMessageBox.prompt(
      t('cluster.removeMemberPrompt', { name: member.name }),
      t('cluster.removeMember'),
      {
        type: 'warning',
        inputPlaceholder: member.name,
        inputValidator: (value) => value === member.name || t('common.typeNameMismatch'),
        confirmButtonText: t('cluster.removeMember'),
        confirmButtonClass: 'el-button--danger',
      },
    )
  } catch {
    return
  }

  try {
    await api.deleteClusterMember(props.cluster, member.name)
    ElMessage.success(t('cluster.memberRemoved', { name: member.name }))
    await clusters.fetchAll()
    await load()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

async function onRenamed(): Promise<void> {
  detailOpen.value = false
  await load()
}

function openCreate(): void {
  formMode.value = 'create'
  formInstance.value = null
  formOpen.value = true
}

async function openEdit(instance: InstanceSummary): Promise<void> {
  await openEditDetail(instance)
}

async function openEditDetail(instance: InstanceSummary | InstanceDetail): Promise<void> {
  const detail =
    'config' in instance ? instance : await instances.fetchDetail(instance.cluster, instance.name)
  if (!detail) {
    ElMessage.error(instances.error || t('detail.unavailable'))
    return
  }

  formMode.value = 'edit'
  formInstance.value = detail
  formOpen.value = true
}

async function onSaved(): Promise<void> {
  await load()
}

function openPoolDialog(pool: StoragePoolResource | null = null): void {
  poolEditing.value = pool
  poolDialogOpen.value = true
}

async function confirmDeletePool(pool: StoragePoolResource): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('admin.poolDeleteConfirm', { name: pool.name }),
      t('admin.editPool'),
      { type: 'warning', confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel') },
    )
  } catch {
    return
  }

  try {
    await instances.deleteStoragePool(props.cluster, pool.name)
    ElMessage.success(t('admin.poolDeleted', { name: pool.name }))
    await instances.fetchResources(props.cluster, true)
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

async function loadOverlays(force = false): Promise<void> {
  overlayLoading.value = true
  try {
    overlayItems.value = await instances.fetchOverlays(props.cluster, force)
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    overlayLoading.value = false
  }
}

function openOverlayDialog(): void {
  overlayEditing.value = null
  overlayDialogOpen.value = true
}

function openEditOverlay(overlay: OverlayStatus): void {
  overlayEditing.value = overlay
  overlayDialogOpen.value = true
}

async function reconcileOverlay(overlay: OverlayStatus): Promise<void> {
  try {
    await instances.reconcileOverlay(props.cluster, overlay.name)
    ElMessage.success(t('cluster.overlayReconciled', { name: overlay.name }))
    await loadOverlays(true)
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

async function confirmDisableOverlay(overlay: OverlayStatus): Promise<void> {
  try {
    await ElMessageBox.prompt(
      t('cluster.overlayDisablePrompt', { name: overlay.name }),
      t('cluster.overlayDisable'),
      {
        type: 'warning',
        inputPlaceholder: overlay.name,
        inputValidator: (value) => value === overlay.name || t('common.typeNameMismatch'),
        confirmButtonText: t('cluster.overlayDisable'),
        confirmButtonClass: 'el-button--danger',
        cancelButtonText: t('common.cancel'),
      },
    )
  } catch {
    return
  }

  try {
    await instances.disableOverlay(props.cluster, overlay.name)
    ElMessage.success(t('cluster.overlayRemoved', { name: overlay.name }))
    await loadOverlays(true)
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

function openNetworkDialog(network: NetworkResource | null = null): void {
  networkEditing.value = network
  networkDialogOpen.value = true
}

async function confirmDeleteNetwork(network: NetworkResource): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('admin.networkDeleteConfirm', { name: network.name }),
      t('admin.editNetwork'),
      { type: 'warning', confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel') },
    )
  } catch {
    return
  }

  try {
    await instances.deleteNetwork(props.cluster, network.name)
    ElMessage.success(t('admin.networkDeleted', { name: network.name }))
    await instances.fetchResources(props.cluster, true)
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

function openProfileDialog(profile: Profile | null = null): void {
  profileEditing.value = profile
  profileDialogOpen.value = true
}

async function confirmDeleteProfile(profile: Profile): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('admin.profileDeleteConfirm', { name: profile.name }),
      t('admin.editProfile'),
      { type: 'warning', confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel') },
    )
  } catch {
    return
  }

  try {
    await instances.deleteProfile(props.cluster, profile.name)
    ElMessage.success(t('admin.profileDeleted', { name: profile.name }))
    await instances.fetchProfiles(props.cluster, true)
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

function openMemberDialog(member: ClusterMember): void {
  memberEditing.value = member
  memberDialogOpen.value = true
}

/** Opens the node detail page where host networking is managed. */
function openNodeDetail(member: ClusterMember): void {
  void router.push({ name: 'node-detail', params: { cluster: props.cluster, member: member.name } })
}

function openDevices(instance: InstanceSummary): void {
  if (!instance?.name) {
    return
  }
  detailName.value = instance.name
  detailTab.value = 'devices'
  detailOpen.value = true
}

async function memberAdded(): Promise<void> {
  await clusters.fetchAll()
}

async function clusterRenamed(): Promise<void> {
  await clusters.fetchAll()
}

function openVolumesDialog(pool: StoragePoolResource): void {
  volumesPool.value = pool
  volumesDialogOpen.value = true
}

function openDistribute(fingerprint: string): void {
  distributeFingerprint.value = fingerprint
  distributeOpen.value = true
}

async function refreshImage(fingerprint: string): Promise<void> {
  try {
    await instances.refreshImage(props.cluster, fingerprint)
    ElMessage.success(t('images.refreshed'))
    await instances.fetchImages(props.cluster, true)
  } catch (err) {
    ElMessage.error(t('images.failed', { error: errorMessage(err) }))
  }
}

function openIsoLaunch(iso: { name: string; pool: string }): void {
  isoTarget.value = { name: iso.name, pool: iso.pool }
  isoLaunchOpen.value = true
}

async function deleteISO(iso: { name: string; pool: string }): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('iso.deleteConfirm', { name: iso.name }),
      t('common.delete'),
      { type: 'warning', confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel') },
    )
  } catch {
    return
  }

  try {
    await instances.deleteISO(props.cluster, iso.pool, iso.name)
    ElMessage.success(t('iso.deleted'))
    await instances.fetchISOs(props.cluster, true)
  } catch (err) {
    ElMessage.error(t('iso.failed', { error: errorMessage(err) }))
  }
}

async function deleteImage(fingerprint: string): Promise<void> {
  // Image deletion is instant and unrecoverable; every other destructive
  // action on this page confirms first (#12).
  try {
    await ElMessageBox.confirm(
      t('images.deleteConfirm', { fingerprint }),
      t('common.delete'),
      {
        type: 'warning',
        confirmButtonText: t('common.delete'),
        cancelButtonText: t('common.cancel'),
      },
    )
  } catch {
    return
  }

  try {
    await instances.deleteImage(props.cluster, fingerprint)
    ElMessage.success(t('images.deleted'))
    await instances.fetchImages(props.cluster, true)
  } catch (err) {
    ElMessage.error(t('images.failed', { error: errorMessage(err) }))
  }
}

async function cleanupCachedImages(): Promise<void> {
  const cached = instances.images(props.cluster).filter((image) => image.cached)
  if (cached.length === 0) {
    return
  }

  try {
    await ElMessageBox.confirm(
      t('images.cleanupConfirm', { count: cached.length }),
      t('images.cleanup'),
      { type: 'warning', confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel') },
    )
  } catch {
    return
  }

  let removed = 0
  for (const image of cached) {
    try {
      await instances.deleteImage(props.cluster, image.fingerprint)
      removed += 1
    } catch (err) {
      ElMessage.error(t('images.failed', { error: errorMessage(err) }))
    }
  }

  if (removed > 0) {
    ElMessage.success(t('images.cleanupDone', { count: removed }))
  }
  await instances.fetchImages(props.cluster, true)
}

function instanceTypeLabel(type: string): string {
  return type === 'virtual-machine' ? t('common.virtualMachine') : t('common.container')
}

// Node management: guided Incus upgrade per member.
function openNodeUpgrade(member: ClusterMember): void {
  nodeUpgradeMember.value = {
    name: member.name,
    version: summary.value?.server_version ?? '',
  }
  nodeUpgradeDialogOpen.value = true
}
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div class="title-with-back">
        <el-button
          :icon="ArrowLeft"
          circle
          plain
          :aria-label="t('cluster.back')"
          :title="t('cluster.back')"
          @click="router.push({ name: 'dashboard' })"
        />
        <div>
          <span class="page-eyebrow">{{ t('cluster.overview') }}</span>
          <div class="cluster-title-row">
            <h2>{{ summary?.display_name || cluster }}</h2>
            <el-button
              class="title-edit"
              text
              circle
              size="small"
              :icon="EditPen"
              :title="t('dashboard.renameCluster')"
              :aria-label="t('dashboard.renameCluster')"
              @click="renameClusterOpen = true"
            />
          </div>
          <p class="subtitle">
            {{ summary?.endpoint || t('cluster.endpointUnavailable') }} ·
            {{ t('cluster.refreshed', { time: formatDateTime(instances.lastUpdated?.toISOString()) }) }}
          </p>
        </div>
      </div>
      <div class="page-actions">
        <el-tag v-if="summary" :type="summary.online ? 'success' : 'danger'" effect="light" round>
          {{ summary.online ? t('cluster.online') : t('cluster.offline') }}
        </el-tag>
        <el-button :icon="Plus" type="primary" @click="openCreate">
          {{ t('cluster.createInstance') }}
        </el-button>
        <el-button
          :icon="Refresh"
          :loading="clusters.loading || instances.loading || instances.resourcesLoading"
          @click="load(true)"
        >
          {{ t('common.refresh') }}
        </el-button>
        <el-button :loading="clusters.reconnecting === cluster" @click="reconnect">
          {{ t('common.reconnect') }}
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="summary && !summary.online && summary.error"
      class="fleet-alert"
      type="error"
      :closable="false"
      show-icon
      :title="summary.error"
    />

    <el-alert
      v-if="instances.error"
      class="fleet-alert"
      type="error"
      :closable="false"
      show-icon
      :title="instances.error"
    />

    <el-tabs v-model="activeTab" class="page-tabs">
      <el-tab-pane :label="t('cluster.tabOverview')" name="overview">
      <section v-if="summary" class="panel cluster-summary-panel">
        <div class="panel-head cluster-summary-head">
          <div>
            <span class="panel-eyebrow">{{ t('cluster.resourceTotalEyebrow') }}</span>
            <h3 class="section-title">{{ t('cluster.resourceTotal') }}</h3>
            <p class="subtitle">{{ t('cluster.resourceTotalHint', { count: summary.members.length }) }}</p>
          </div>
          <div class="cluster-facts">
            <span>
              <small>{{ t('cluster.server') }}</small>
              <strong>{{ summary.server_name || t('common.none') }}</strong>
            </span>
            <span>
              <small>{{ t('cluster.incusVersion') }}</small>
              <strong>{{ summary.server_version || t('common.none') }}</strong>
            </span>
            <span>
              <small>{{ t('cluster.members') }}</small>
              <strong>{{ summary.members.length }}</strong>
            </span>
            <span>
              <small>{{ t('cluster.architectures') }}</small>
              <strong>{{ summary.architectures?.join(', ') || t('common.none') }}</strong>
            </span>
          </div>
        </div>

        <div class="panel-body">
          <div class="cluster-resource-grid">
            <article class="cluster-resource-card">
              <span class="resource-icon cpu"><el-icon><Cpu /></el-icon></span>
              <div class="resource-copy">
                <span class="resource-label">{{ t('cluster.cpuTotal') }}</span>
                <span class="resource-value">{{ summary.cpu?.total ?? '—' }} <small>vCPU</small></span>
                <span class="resource-hint">{{ summary.cpu?.architecture || t('common.none') }}</span>
              </div>
            </article>

            <article class="cluster-resource-card">
              <span class="resource-icon memory"><el-icon><Box /></el-icon></span>
              <div class="resource-copy">
                <span class="resource-label">{{ t('cluster.memoryTotal') }}</span>
                <span class="resource-value">
                  {{ formatBytes(summary.memory?.used ?? 0) }}
                  <small>/ {{ formatBytes(summary.memory?.total ?? 0) }}</small>
                </span>
                <div class="resource-bar">
                  <div
                    class="resource-bar-fill memory"
                    :style="{ width: `${resourcePercent(summary.memory?.used, summary.memory?.total)}%` }"
                  />
                </div>
              </div>
            </article>

            <article class="cluster-resource-card">
              <span class="resource-icon storage"><el-icon><Coin /></el-icon></span>
              <div class="resource-copy">
                <span class="resource-label">
                  {{ summary.storage.shared ? t('cluster.sharedStorage') : t('cluster.storageTotal') }}
                </span>
                <span class="resource-value">
                  {{ formatBytes(summary.storage.used) }}
                  <small>/ {{ formatBytes(summary.storage.total) }}</small>
                </span>
                <div class="resource-bar">
                  <div
                    class="resource-bar-fill storage"
                    :style="{ width: `${resourcePercent(summary.storage.used, summary.storage.total)}%` }"
                  />
                </div>
              </div>
            </article>

            <article class="cluster-resource-card">
              <span class="resource-icon instances"><el-icon><Monitor /></el-icon></span>
              <div class="resource-copy">
                <span class="resource-label">{{ t('cluster.instances') }}</span>
                <span class="resource-value">
                  {{ summary.instances.total }}
                  <small>{{ t('dashboard.running', { count: summary.instances.running }) }}</small>
                </span>
                <span class="resource-hint">
                  {{ summary.instances.containers }} {{ t('common.containers') }} ·
                  {{ summary.instances.virtual_machines }} {{ t('common.virtualMachines') }}
                </span>
              </div>
            </article>
          </div>

          <div v-if="summary.networks.length > 0" class="cluster-tags cluster-summary-tags">
            <span class="mini-label">{{ t('cluster.tabNetwork') }}</span>
            <el-tag v-for="network in summary.networks" :key="network" size="small" effect="plain">
              {{ network }}
            </el-tag>
          </div>
        </div>
      </section>

      <ClusterResourceTrend :cluster="cluster" />

      <section v-if="summary" class="panel">
        <div class="panel-body">
          <div class="page-header" style="margin-bottom: 14px">
            <div>
              <h3 class="section-title">{{ t('cluster.nodes') }}</h3>
              <p class="subtitle">{{ t('cluster.nodesHint') }}</p>
            </div>
            <div class="page-actions">
              <el-button :icon="Plus" type="primary" @click="addMemberOpen = true">
                {{ t('cluster.addNode') }}
              </el-button>
            </div>
          </div>

          <el-table :data="summary.members" size="small" stripe class="node-table">
            <el-table-column :label="t('common.name')" min-width="190">
              <template #default="{ row }">
                <div class="member-identity">
                  <el-button link type="primary" class="member-name-link" @click="openNodeDetail(row)">
                    {{ row.name }}
                  </el-button>
                  <span class="member-meta">
                    <span class="mono">{{ row.architecture || t('common.none') }}</span>
                    <span>{{ row.roles?.join(', ') || t('common.none') }}</span>
                  </span>
                  <span class="member-meta mono member-address" :title="row.url || summary.endpoint">
                    {{ row.url || summary.endpoint }}
                  </span>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.status')" width="80">
              <template #default="{ row }">
                <el-tag :type="row.status === 'Online' ? 'success' : 'info'" size="small" effect="plain">
                  {{ memberStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.cpuCores')" width="85">
              <template #default="{ row }">
                <strong class="node-value">{{ row.cpu?.total ?? '—' }}</strong>
                <small class="node-unit">vCPU</small>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.memory')" min-width="140">
              <template #default="{ row }">
                <div class="node-meter">
                  <span>{{ resourceShare(row.memory?.used, row.memory?.total) }}</span>
                  <div class="node-meter-bar">
                    <span
                      class="memory"
                      :style="{ width: `${resourcePercent(row.memory?.used, row.memory?.total)}%` }"
                    />
                  </div>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('nav.fleetVolumes')" min-width="140">
              <template #default="{ row }">
                <div class="node-meter">
                  <span>{{ resourceShare(row.storage?.used, row.storage?.total) }}</span>
                  <div class="node-meter-bar">
                    <span
                      class="storage"
                      :style="{ width: `${resourcePercent(row.storage?.used, row.storage?.total)}%` }"
                    />
                  </div>
                  <small v-if="row.storage?.shared">{{ t('cluster.sharedStorage') }}</small>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="245" fixed="right">
              <template #default="{ row }">
                <div class="member-actions">
                  <el-button link type="primary" @click="openNodeDetail(row)">
                    {{ t('cluster.manageNode') }}
                  </el-button>
                  <el-button link type="primary" @click="openMemberDialog(row)">
                    {{ t('common.edit') }}
                  </el-button>
                  <el-button
                    v-if="row.status === 'Online'"
                    link
                    type="warning"
                    @click="openNodeUpgrade(row)"
                  >
                    {{ t('node.upgrade') }}
                  </el-button>
                  <el-button link type="danger" @click="removeMember(row)">
                    {{ t('cluster.removeMember') }}
                  </el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </section>
      </el-tab-pane>

      <el-tab-pane :label="t('cluster.tabInstances')" name="instances">
      <section class="panel">
        <div class="panel-body">
          <div class="page-header" style="margin-bottom: 14px">
            <div>
              <h3 class="section-title">{{ t('cluster.instances') }}</h3>
              <p class="subtitle">
                {{ t('common.shown', { count: filtered.length }) }} ·
                {{ t('cluster.migrationTargets', { count: onlineTargets.length }) }}
              </p>
            </div>
            <div class="page-actions">
              <el-input
                v-model="instanceSearch"
                clearable
                :placeholder="t('instances.searchPlaceholder')"
                style="width: 200px"
              />
              <el-select v-model="typeFilter" aria-label="Instance type" style="width: 180px">
                <el-option :label="t('common.allTypes')" value="any" />
                <el-option :label="t('common.containers')" value="container" />
                <el-option :label="t('common.virtualMachines')" value="virtual-machine" />
              </el-select>
            </div>
          </div>

          <el-table v-loading="instances.loading" :data="pagedInstances" stripe>
            <el-table-column :label="t('common.name')" min-width="140">
              <template #default="{ row }">
                <div class="status-cell">
                  <div>
                    <el-button link type="primary" class="name-link" @click="openDetail(row)">
                      {{ row.name }}
                    </el-button>
                    <span class="name-meta">
                      <span v-if="row.description" class="mini-label">{{ row.description }}</span>
                      <el-tag v-if="row.live_migratable" size="small" type="success" effect="plain">
                        {{ t('common.live') }}
                      </el-tag>
                    </span>
                  </div>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.type')" width="75">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">{{ instanceTypeLabel(row.type) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.status')" width="80">
              <template #default="{ row }">
                <StatusTag :status="row.status" :status-code="row.status_code" />
              </template>
            </el-table-column>
            <el-table-column prop="location" :label="t('common.node')" width="100" />
            <el-table-column :label="t('common.address')" min-width="110">
              <template #default="{ row }">
                <span class="mono">{{ primaryAddress(row.ipv4, row.ipv6) }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.memory')" width="80">
              <template #default="{ row }">{{ formatBytes(row.memory) }}</template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="250" fixed="right">
              <template #default="{ row }">
                <InstanceActions
                  :instance="row"
                  :busy="isBusy(row)"
                  @action="act(row, $event)"
                  @edit="openEdit(row)"
                  @rename="openRename(row)"
                  @migrate="openMigrate(row)"
                  @delete="confirmDelete(row)"
                  @devices="openDevices(row)"
                />
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('cluster.noInstances')" />
            </template>
          </el-table>
          <el-pagination
            v-model:current-page="instancePage"
            v-model:page-size="instancePageSize"
            class="table-pagination"
            layout="total, sizes, prev, pager, next"
            :total="filtered.length"
            :page-sizes="[20, 50, 100]"
          />
        </div>
      </section>
      </el-tab-pane>

      <el-tab-pane :label="t('cluster.tabStorage')" name="storage">
      <section class="panel">
        <div class="panel-body">
          <div class="section-heading page-header">
            <div>
              <h3 class="section-title">{{ t('cluster.storagePools') }}</h3>
              <p class="subtitle">{{ t('cluster.storagePoolsHint') }}</p>
            </div>
            <div class="page-actions">
              <el-button :icon="Plus" @click="openPoolDialog()">{{ t('admin.addPool') }}</el-button>
            </div>
          </div>

          <el-table v-loading="instances.resourcesLoading" :data="resources?.storage_pools ?? []" stripe>
            <el-table-column :label="t('cluster.poolName')" min-width="120">
              <template #default="{ row }">
                <strong>{{ row.name }}</strong>
                <span v-if="row.description" class="mini-label">{{ row.description }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.driver')" width="90" prop="driver" />
            <el-table-column :label="t('cluster.source')" min-width="120">
              <template #default="{ row }">
                <span class="mono">{{ row.source || t('common.none') }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.usage')" min-width="180">
              <template #default="{ row }">
                <StorageBar v-if="row.total > 0" :used="row.used" :total="row.total" />
                <span v-else class="muted">{{ formatBytes(row.used) }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.usedBy')" width="110">
              <template #default="{ row }">
                {{ t('cluster.usedByCount', { count: row.used_by.length }) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="180" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openVolumesDialog(row)">
                  {{ t('volumes.manage') }}
                </el-button>
                <el-button link type="primary" @click="openPoolDialog(row)">
                  {{ t('common.edit') }}
                </el-button>
                <el-button link type="danger" @click="confirmDeletePool(row)">
                  {{ t('common.delete') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('cluster.noPools')" />
            </template>
          </el-table>
        </div>
      </section>
      </el-tab-pane>

      <el-tab-pane :label="t('cluster.tabMedia')" name="media">
      <section class="panel">
        <div class="panel-body">
          <div class="section-heading page-header">
            <div>
              <h3 class="section-title">{{ t('images.title') }}</h3>
              <p class="subtitle">{{ t('form.imageHint') }}</p>
            </div>
            <div class="page-actions">
              <el-button
                v-if="cachedImageCount > 0"
                :loading="instances.saving"
                @click="cleanupCachedImages"
              >
                {{ t('images.cleanup') }} ({{ cachedImageCount }})
              </el-button>
              <el-button :icon="Plus" type="primary" @click="importDialogOpen = true">
                {{ t('images.import') }}
              </el-button>
            </div>
          </div>

          <el-table v-loading="instances.imagesLoading" :data="clusterImages" size="small" stripe>
            <el-table-column :label="t('form.image')" min-width="180">
              <template #default="{ row }">
                <strong>{{ row.aliases[0] || row.os || row.fingerprint.slice(0, 12) }}</strong>
                <span v-if="row.release" class="mini-label">{{ row.release }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('images.fingerprint')" min-width="140">
              <template #default="{ row }">
                <CopyableText
                  :value="row.fingerprint"
                  :display="`${row.fingerprint.slice(0, 12)}…`"
                  :label="t('images.fingerprint')"
                />
              </template>
            </el-table-column>
            <el-table-column :label="t('common.type')" width="130">
              <template #default="{ row }">
                {{ instanceTypeLabel(row.type || 'container') }}
              </template>
            </el-table-column>
            <el-table-column :label="t('common.size')" width="100">
              <template #default="{ row }">
                {{ formatBytes(row.size) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('images.cached')" width="90">
              <template #default="{ row }">
                <el-tag v-if="row.cached" size="small" type="warning" effect="light">
                  {{ t('images.cached') }}
                </el-tag>
                <span v-else class="muted">{{ t('common.none') }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="170">
              <template #default="{ row }">
                <el-button link type="primary" @click="openDistribute(row.fingerprint)">
                  {{ t('images.distribute') }}
                </el-button>
                <el-button v-if="row.cached" link type="primary" @click="refreshImage(row.fingerprint)">
                  {{ t('images.refresh') }}
                </el-button>
                <el-button link type="danger" @click="deleteImage(row.fingerprint)">
                  {{ t('common.delete') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('images.empty')" />
            </template>
          </el-table>
        </div>
      </section>

      <section class="panel">
        <div class="panel-body">
          <div class="section-heading page-header">
            <div>
              <h3 class="section-title">{{ t('iso.title') }}</h3>
              <p class="subtitle">{{ t('iso.hint') }}</p>
            </div>
            <div class="page-actions">
              <el-button :icon="Plus" type="primary" @click="isoImportOpen = true">
                {{ t('iso.import') }}
              </el-button>
            </div>
          </div>

          <el-table v-loading="instances.saving" :data="clusterISOs" size="small" stripe>
            <el-table-column :label="t('iso.name')" min-width="180">
              <template #default="{ row }">
                <strong>{{ row.name }}</strong>
                <span v-if="row.description" class="mini-label">{{ row.description }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('iso.pool')" width="140">
              <template #default="{ row }">
                <span class="mono">{{ row.pool }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="200">
              <template #default="{ row }">
                <el-button link type="primary" @click="openIsoLaunch(row)">
                  {{ t('iso.launch') }}
                </el-button>
                <el-button link type="danger" @click="deleteISO(row)">
                  {{ t('common.delete') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('iso.empty')" />
            </template>
          </el-table>
        </div>
      </section>
      </el-tab-pane>

      <el-tab-pane :label="t('cluster.tabNetwork')" name="network">
      <section class="panel">
        <div class="panel-body">
          <div class="section-heading page-header">
            <div>
              <h3 class="section-title">{{ t('cluster.networks') }}</h3>
              <p class="subtitle">{{ t('cluster.networksHint') }}</p>
            </div>
            <div class="page-actions">
              <el-button :icon="Plus" @click="openNetworkDialog()">
                {{ t('admin.addNetwork') }}
              </el-button>
            </div>
          </div>

          <el-table v-loading="instances.resourcesLoading" :data="networkRows" stripe>
            <el-table-column :label="t('cluster.networkName')" min-width="150">
              <template #default="{ row }">
                <strong>{{ row.name }}</strong>
                <span v-if="row.description" class="mini-label">{{ row.description }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.networkType')" width="120" prop="type" />
            <el-table-column :label="t('cluster.ipv4')" min-width="150">
              <template #default="{ row }">
                <span class="mono">{{ row.ipv4 || t('common.none') }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.ipv6')" min-width="150">
              <template #default="{ row }">
                <span class="mono">{{ row.ipv6 || t('common.none') }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.usedBy')" width="130">
              <template #default="{ row }">
                {{ t('cluster.usedByCount', { count: row.used_by.length }) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="150" fixed="right">
              <template #default="{ row }">
                <template v-if="row.managed">
                  <el-button link type="primary" @click="openNetworkDialog(row)">
                    {{ t('common.edit') }}
                  </el-button>
                  <el-button link type="danger" @click="confirmDeleteNetwork(row)">
                    {{ t('common.delete') }}
                  </el-button>
                </template>
                <span v-else class="muted" :title="t('admin.unmanagedHint')">—</span>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('cluster.noNetworks')" />
            </template>
          </el-table>
        </div>
      </section>

      <section class="panel">
        <div class="panel-body">
          <div class="section-heading page-header">
            <div>
              <h3 class="section-title">{{ t('cluster.overlayTitle') }}</h3>
              <p class="subtitle">{{ t('cluster.overlayHint') }}</p>
            </div>
            <div class="page-actions">
              <el-button :icon="Plus" @click="openOverlayDialog">
                {{ t('cluster.overlayCreate') }}
              </el-button>
            </div>
          </div>

          <el-table v-loading="overlayLoading" :data="overlayItems" stripe>
            <el-table-column :label="t('cluster.overlaySubnet')" min-width="130">
              <template #default="{ row }">
                <strong>{{ row.name }}</strong>
                <span class="mini-label mono">{{ row.subnet }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.overlayBridge')" width="100" prop="bridge" />
            <el-table-column :label="t('cluster.overlayVNI')" width="70">
              <template #default="{ row }">
                <span class="mono">{{ row.vni }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.overlayGateway')" min-width="100">
              <template #default="{ row }">
                {{ row.gateway_member }}
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.overlayMTU')" width="60">
              <template #default="{ row }">
                <span class="mono">{{ row.mtu }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.overlayTunnels', { count: 0 })" width="90">
              <template #default="{ row }">
                {{ t('cluster.overlayTunnels', { count: row.tunnels.length }) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.overlayStatus')" width="90">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small" effect="light">
                  {{ row.enabled ? t('cluster.overlayEnabled') : t('cluster.overlayDisabled') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="200" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openEditOverlay(row)">
                  {{ t('common.edit') }}
                </el-button>
                <el-button link type="primary" @click="reconcileOverlay(row)">
                  {{ t('cluster.overlayReconcile') }}
                </el-button>
                <el-button link type="danger" @click="confirmDisableOverlay(row)">
                  {{ t('cluster.overlayDisable') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('cluster.overlayEmpty')" />
            </template>
          </el-table>

          <div v-for="overlay in overlayItems" :key="`${overlay.name}-members`" class="overlay-members">
            <h4 class="mini-title">{{ overlay.name }} · {{ t('cluster.overlayMember') }}</h4>
            <div class="overlay-member-grid">
              <div v-for="member in overlay.members" :key="member.member" class="overlay-member">
                <strong>{{ member.member }}</strong>
                <el-tag :type="member.gateway ? 'warning' : 'info'" size="small" effect="plain">
                  {{ member.gateway ? t('cluster.overlayRoleGateway') : t('cluster.overlayRoleMember') }}
                </el-tag>
                <span class="muted mono">{{ member.bridge_ip }}</span>
                <span class="muted mono">{{ member.underlay_ip }}</span>
              </div>
            </div>
          </div>
        </div>
      </section>
      </el-tab-pane>

      <el-tab-pane :label="t('cluster.tabProfiles')" name="profiles">
      <section class="panel">
        <div class="panel-body">
          <div class="section-heading page-header">
            <div>
              <h3 class="section-title">{{ t('cluster.profiles') }}</h3>
              <p class="subtitle">{{ t('cluster.profilesHint') }}</p>
            </div>
            <div class="page-actions">
              <el-button :icon="Plus" @click="openProfileDialog()">
                {{ t('admin.addProfile') }}
              </el-button>
            </div>
          </div>

          <el-table v-loading="instances.profilesLoading" :data="profiles" stripe>
            <el-table-column :label="t('common.name')" min-width="160">
              <template #default="{ row }">
                <strong>{{ row.name }}</strong>
                <span v-if="row.description" class="mini-label">{{ row.description }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('admin.config')" min-width="180">
              <template #default="{ row }">
                <CopyableText
                  v-if="Object.keys(row.config).length"
                  :value="Object.entries(row.config).map(([key, value]) => `${key}=${value}`).join('\n')"
                  :display="Object.keys(row.config).join(', ')"
                  :label="t('admin.config')"
                />
                <span v-else class="muted">{{ t('common.none') }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('admin.profileDevices')" width="110">
              <template #default="{ row }">
                {{ Object.keys(row.devices).length }}
              </template>
            </el-table-column>
            <el-table-column :label="t('cluster.usedBy')" width="130">
              <template #default="{ row }">
                {{ t('cluster.usedByCount', { count: row.used_by.length }) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="150" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openProfileDialog(row)">
                  {{ t('common.edit') }}
                </el-button>
                <el-button link type="danger" @click="confirmDeleteProfile(row)">
                  {{ t('common.delete') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('cluster.noProfiles')" />
            </template>
          </el-table>
        </div>
      </section>
      </el-tab-pane>
    </el-tabs>

    <InstanceDetailDrawer
      v-model="detailOpen"
      :cluster="cluster"
      :name="detailName"
      :initial-tab="detailTab"
      @edit="openEditDetail"
      @rename="openRename"
      @migrate="openMigrate"
      @delete="confirmDelete"
    />
    <RenameInstanceDialog
      v-model="renameOpen"
      :cluster="cluster"
      :instance="renameName"
      @renamed="onRenamed"
    />
    <InstanceFormDialog
      v-model="formOpen"
      :mode="formMode"
      :clusters="clusters.items"
      :cluster="cluster"
      :instance="formInstance"
      @saved="onSaved"
    />
    <MigrateDialog
      v-model="migrateOpen"
      :instance="migrateInstance"
      :clusters="clusters.items"
      @submitted="load()"
    />
    <StoragePoolDialog
      v-model="poolDialogOpen"
      :cluster="cluster"
      :pool="poolEditing"
      @saved="instances.fetchResources(cluster, true)"
    />
    <OverlayDialog
      v-model="overlayDialogOpen"
      :cluster="cluster"
      :members="summary?.members ?? []"
      :overlay="overlayEditing"
      @saved="loadOverlays(true)"
    />
    <NetworkDialog
      v-model="networkDialogOpen"
      :cluster="cluster"
      :network="networkEditing"
      :members="summary?.members ?? []"
      @saved="instances.fetchResources(cluster, true)"
    />
    <ProfileDialog
      v-model="profileDialogOpen"
      :cluster="cluster"
      :profile="profileEditing"
      @saved="instances.fetchProfiles(cluster, true)"
    />
    <AddClusterMemberDialog v-model="addMemberOpen" :cluster="cluster" @added="memberAdded" />
    <MemberDialog v-model="memberDialogOpen" :cluster="cluster" :member="memberEditing" />
    <RenameClusterDialog
      v-model="renameClusterOpen"
      :cluster="cluster"
      :current-name="summary?.display_name || cluster"
      @renamed="clusterRenamed"
    />
    <NodeUpgradeDialog
      v-model="nodeUpgradeDialogOpen"
      :member-name="nodeUpgradeMember.name"
      :current-version="nodeUpgradeMember.version"
    />
    <StorageVolumesDialog
      v-model="volumesDialogOpen"
      :cluster="cluster"
      :pool="volumesPool"
    />
    <ImageImportDialog
      v-model="importDialogOpen"
      :cluster="cluster"
      @imported="instances.fetchImages(cluster, true)"
    />
    <ImageDistributeDialog
      v-model="distributeOpen"
      :cluster="cluster"
      :fingerprint="distributeFingerprint"
      :clusters="clusters.items"
      @distributed="instances.fetchImages(cluster, true)"
    />
    <IsoImportDialog
      v-model="isoImportOpen"
      :cluster="cluster"
      :pools="poolNames"
      @imported="instances.fetchISOs(cluster, true)"
    />
    <IsoLaunchDialog
      v-model="isoLaunchOpen"
      :cluster="cluster"
      :iso-name="isoTarget.name"
      :iso-pool="isoTarget.pool"
      :networks="(resources?.networks ?? []).map((network) => network.name)"
      @launched="instances.fetchCluster(cluster, typeFilter)"
    />
    <InstanceDeleteDialog
      :target="deleteTarget"
      :busy="deleting"
      @cancel="cancelDelete"
      @confirm="submitDelete"
    />
  </div>
</template>

<style scoped>
.title-with-back {
  display: flex;
  align-items: center;
  gap: 14px;
}

.cluster-title-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

.title-edit {
  color: var(--fleet-ink-soft);
  opacity: 0.55;
  transition: opacity 0.15s ease;
}

.title-edit:hover {
  opacity: 1;
}

.cluster-detail {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.cluster-summary-panel {
  overflow: hidden;
}

.cluster-summary-head {
  align-items: flex-start;
}

.cluster-facts {
  display: flex;
  flex-wrap: wrap;
  gap: 16px 22px;
}

.cluster-facts span {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.cluster-facts small {
  color: var(--fleet-ink-soft);
  font-size: 10px;
  font-weight: 650;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.cluster-facts strong {
  font-size: 12.5px;
  font-weight: 650;
}

.cluster-resource-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 14px;
}

.cluster-resource-card {
  display: flex;
  align-items: flex-start;
  gap: 11px;
  padding: 13px 14px;
  border: 1px solid color-mix(in srgb, var(--fleet-line) 76%, transparent);
  border-radius: 12px;
  background: color-mix(in srgb, var(--fleet-panel-muted) 72%, transparent);
}

.resource-icon {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  flex: none;
  border-radius: 10px;
  font-size: 16px;
}

.resource-icon.cpu {
  background: #eef2ff;
  color: #4562e8;
}

.resource-icon.memory {
  background: #ecfeff;
  color: #0891b2;
}

.resource-icon.storage {
  background: #ecfdf5;
  color: #059669;
}

.resource-icon.instances {
  background: #f5f3ff;
  color: #7c3aed;
}

.resource-copy {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.resource-label {
  color: var(--fleet-ink-soft);
  font-size: 10.5px;
  font-weight: 650;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.resource-value {
  font-size: 20px;
  font-weight: 720;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.3px;
  line-height: 1.15;
}

.resource-value small {
  color: var(--fleet-ink-soft);
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0;
}

.resource-hint {
  color: var(--fleet-ink-soft);
  font-size: 11.5px;
}

.resource-bar {
  height: 5px;
  overflow: hidden;
  border-radius: 99px;
  background: var(--el-fill-color);
}

.resource-bar-fill {
  height: 100%;
  border-radius: inherit;
  transition: width 0.4s ease;
}

.resource-bar-fill.memory {
  background: linear-gradient(90deg, #3b82f6, #06b6d4);
}

.resource-bar-fill.storage {
  background: linear-gradient(90deg, #10b981, #22c55e);
}

.cluster-summary-tags {
  align-items: center;
  margin-top: 16px;
}

.node-table {
  --el-table-header-bg-color: var(--fleet-panel-muted);
}

.member-identity {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.member-name-link {
  align-self: flex-start;
  height: auto;
  padding: 0;
  font-weight: 650;
}

.member-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  color: var(--fleet-ink-soft);
  font-size: 11px;
}

.member-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 2px 6px;
}

.member-actions :deep(.el-button) {
  height: auto;
  margin-left: 0;
  padding: 0;
  font-size: 12px;
}

.node-value {
  font-size: 14px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.node-unit {
  margin-left: 2px;
  color: var(--fleet-ink-soft);
  font-size: 10.5px;
}

.node-meter {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.node-meter > span:first-child {
  font-size: 11.5px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.node-meter-bar {
  height: 4px;
  overflow: hidden;
  border-radius: 99px;
  background: var(--el-fill-color);
}

.node-meter-bar span {
  display: block;
  height: 100%;
  border-radius: inherit;
}

.node-meter-bar .memory {
  background: linear-gradient(90deg, #3b82f6, #06b6d4);
}

.node-meter-bar .storage {
  background: linear-gradient(90deg, #10b981, #22c55e);
}

.node-meter small {
  color: var(--fleet-ink-soft);
  font-size: 10px;
}

.cluster-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.mem-bar {
  margin-top: -6px;
}

.section-heading {
  margin-bottom: 12px;
}

.name-link {
  padding: 0;
  height: auto;
  font-weight: 600;
}

.name-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.name-meta .mini-label {
  display: inline;
}

.member-address {
  font-size: 11.5px;
  white-space: nowrap;
}

@media (prefers-color-scheme: dark) {
  .resource-icon.cpu {
    background: #1d2a4d;
    color: #8fa4ff;
  }

  .resource-icon.memory {
    background: #113640;
    color: #67d9e9;
  }

  .resource-icon.storage {
    background: #123b31;
    color: #67e0b0;
  }

  .resource-icon.instances {
    background: #2c2450;
    color: #b49cff;
  }
}

.member-tag {
  cursor: pointer;
}

.config-preview {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: block;
}
</style>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Plus, Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import InstanceActions from '@/components/InstanceActions.vue'
import InstanceDetailDrawer from '@/components/InstanceDetailDrawer.vue'
import InstanceFormDialog from '@/components/InstanceFormDialog.vue'
import MigrateDialog from '@/components/MigrateDialog.vue'
import StatusTag from '@/components/StatusTag.vue'
import StorageBar from '@/components/StorageBar.vue'
import type {
  InstanceAction,
  InstanceDetail,
  InstanceSummary,
  InstanceType,
} from '@/api/types'
import { useClustersStore } from '@/stores/clusters'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'
import { formatBytes, formatDateTime, primaryAddress } from '@/utils/format'

const props = defineProps<{ cluster: string }>()
const router = useRouter()
const clusters = useClustersStore()
const instances = useInstancesStore()
const { t, te } = useI18n()

const typeFilter = ref<InstanceType>('any')
const detailOpen = ref(false)
const detailName = ref('')
const migrateOpen = ref(false)
const migrateInstance = ref<InstanceSummary | null>(null)
const formOpen = ref(false)
const formMode = ref<'create' | 'edit'>('create')
const formInstance = ref<InstanceDetail | null>(null)

const summary = computed(() => clusters.byName(props.cluster))
const resources = computed(() => instances.resources(props.cluster))
const filtered = computed(() =>
  instances.items.filter(
    (instance) => typeFilter.value === 'any' || instance.type === typeFilter.value,
  ),
)
const onlineTargets = computed(() =>
  clusters.items.filter((cluster) => cluster.online && cluster.name !== props.cluster),
)

/** Renders a cluster member status enum value in the active language. */
function memberStatusLabel(status: string): string {
  const key = `cluster.memberStatus.${status.toLowerCase()}`
  return te(key) ? t(key) : status
}

async function load(force = false): Promise<void> {
  await Promise.all([
    clusters.fetchAll(),
    instances.fetchCluster(props.cluster, typeFilter.value),
    instances.fetchResources(props.cluster, force),
  ])
}

onMounted(() => load())

watch(
  () => props.cluster,
  async () => {
    await load()
  },
)

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
  detailName.value = instance.name
  detailOpen.value = true
}

function openMigrate(instance: InstanceSummary): void {
  migrateInstance.value = instance
  migrateOpen.value = true
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
    return
  }

  formMode.value = 'edit'
  formInstance.value = detail
  formOpen.value = true
}

async function act(
  instance: InstanceSummary | InstanceDetail,
  action: InstanceAction,
): Promise<void> {
  try {
    await instances.instanceAction(instance.cluster, instance.name, action)
    ElMessage.success(
      t('instances.actionSuccess', {
        action: t(`instances.actionDone.${action}`),
        name: instance.name,
      }),
    )
    await Promise.all([instances.fetchCluster(props.cluster, typeFilter.value), clusters.fetchAll()])
  } catch (err) {
    ElMessage.error(t('instances.actionFailed', { error: errorMessage(err) }))
  }
}

async function confirmDelete(instance: InstanceSummary | InstanceDetail): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('instances.deleteConfirm', { name: instance.name }),
      t('instances.deleteTitle'),
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
    await instances.deleteInstance(instance.cluster, instance.name, true)
    if (detailOpen.value && detailName.value === instance.name) {
      detailOpen.value = false
    }
    ElMessage.success(t('instances.deleteSuccess', { name: instance.name }))
    await Promise.all([instances.fetchCluster(props.cluster, typeFilter.value), clusters.fetchAll()])
  } catch (err) {
    ElMessage.error(t('instances.actionFailed', { error: errorMessage(err) }))
  }
}

async function onSaved(): Promise<void> {
  await Promise.all([instances.fetchCluster(props.cluster, typeFilter.value), clusters.fetchAll()])
}

function instanceTypeLabel(type: string): string {
  return type === 'virtual-machine' ? t('common.virtualMachine') : t('common.container')
}
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div class="title-with-back">
        <el-button :icon="ArrowLeft" circle plain @click="router.push({ name: 'dashboard' })" />
        <div>
          <h2>{{ summary?.display_name || cluster }}</h2>
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

    <section v-if="summary" class="panel">
      <div class="panel-body cluster-detail">
        <div class="detail-grid">
          <div class="detail-item">
            <div class="label">{{ t('cluster.server') }}</div>
            <div class="value">{{ summary.server_name || t('common.none') }}</div>
          </div>
          <div class="detail-item">
            <div class="label">{{ t('cluster.incusVersion') }}</div>
            <div class="value">{{ summary.server_version || t('common.none') }}</div>
          </div>
          <div class="detail-item">
            <div class="label">{{ t('cluster.members') }}</div>
            <div class="value">{{ summary.members.length }}</div>
          </div>
          <div class="detail-item">
            <div class="label">{{ t('cluster.instances') }}</div>
            <div class="value">
              {{
                t('cluster.instancesSummary', {
                  total: summary.instances.total,
                  running: summary.instances.running,
                })
              }}
            </div>
          </div>
          <div class="detail-item">
            <div class="label">{{ t('cluster.containersVms') }}</div>
            <div class="value">
              {{ summary.instances.containers }} / {{ summary.instances.virtual_machines }}
            </div>
          </div>
          <div class="detail-item">
            <div class="label">{{ t('cluster.architectures') }}</div>
            <div class="value">{{ summary.architectures?.join(', ') || t('common.none') }}</div>
          </div>
        </div>

        <StorageBar
          :used="summary.storage.used"
          :total="summary.storage.total"
          :pool="summary.storage.pool"
        />

        <div class="cluster-tags">
          <el-tag v-for="member in summary.members" :key="member.name" size="small" effect="plain">
            {{ member.name }} · {{ memberStatusLabel(member.status) }}
          </el-tag>
          <el-tag v-for="network in summary.networks" :key="network" size="small" effect="plain">
            {{ network }}
          </el-tag>
        </div>
      </div>
    </section>

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
            <el-select v-model="typeFilter" style="width: 180px">
              <el-option :label="t('common.allTypes')" value="any" />
              <el-option :label="t('common.containers')" value="container" />
              <el-option :label="t('common.virtualMachines')" value="virtual-machine" />
            </el-select>
          </div>
        </div>

        <el-table v-loading="instances.loading" :data="filtered" stripe>
          <el-table-column :label="t('common.name')" min-width="170">
            <template #default="{ row }">
              <div class="status-cell">
                <div>
                  <el-button link type="primary" class="name-link" @click="openDetail(row)">
                    {{ row.name }}
                  </el-button>
                  <span class="mini-label">{{ row.description || t('common.none') }}</span>
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.type')" width="130">
            <template #default="{ row }">
              <el-tag size="small" effect="plain">{{ instanceTypeLabel(row.type) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="120">
            <template #default="{ row }">
              <StatusTag :status="row.status" :status-code="row.status_code" />
            </template>
          </el-table-column>
          <el-table-column prop="location" :label="t('common.node')" width="140" />
          <el-table-column :label="t('common.address')" min-width="150">
            <template #default="{ row }">
              <span class="mono">{{ primaryAddress(row.ipv4, row.ipv6) }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.memory')" width="110">
            <template #default="{ row }">{{ formatBytes(row.memory) }}</template>
          </el-table-column>
          <el-table-column :label="t('common.live')" width="90">
            <template #default="{ row }">
              <el-tag v-if="row.live_migratable" size="small" type="success" effect="light">
                {{ t('common.yes') }}
              </el-tag>
              <span v-else class="muted">{{ t('common.none') }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="420" fixed="right">
            <template #default="{ row }">
              <InstanceActions
                :instance="row"
                @action="act(row, $event)"
                @edit="openEdit(row)"
                @migrate="openMigrate(row)"
                @delete="confirmDelete(row)"
              />
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="t('cluster.noInstances')" />
          </template>
        </el-table>
      </div>
    </section>

    <section class="panel">
      <div class="panel-body">
        <div class="section-heading">
          <h3 class="section-title">{{ t('cluster.storagePools') }}</h3>
          <p class="subtitle">{{ t('cluster.storagePoolsHint') }}</p>
        </div>

        <el-table v-loading="instances.resourcesLoading" :data="resources?.storage_pools ?? []" stripe>
          <el-table-column :label="t('cluster.poolName')" min-width="140">
            <template #default="{ row }">
              <strong>{{ row.name }}</strong>
              <span v-if="row.description" class="mini-label">{{ row.description }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('cluster.driver')" width="110" prop="driver" />
          <el-table-column :label="t('cluster.source')" min-width="150">
            <template #default="{ row }">
              <span class="mono">{{ row.source || t('common.none') }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('cluster.usage')" min-width="220">
            <template #default="{ row }">
              <StorageBar v-if="row.total > 0" :used="row.used" :total="row.total" />
              <span v-else class="muted">{{ formatBytes(row.used) }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('cluster.usedBy')" width="130">
            <template #default="{ row }">
              {{ t('cluster.usedByCount', { count: row.used_by.length }) }}
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="t('cluster.noPools')" />
          </template>
        </el-table>
      </div>
    </section>

    <section class="panel">
      <div class="panel-body">
        <div class="section-heading">
          <h3 class="section-title">{{ t('cluster.networks') }}</h3>
          <p class="subtitle">{{ t('cluster.networksHint') }}</p>
        </div>

        <el-table v-loading="instances.resourcesLoading" :data="resources?.networks ?? []" stripe>
          <el-table-column :label="t('cluster.networkName')" min-width="150">
            <template #default="{ row }">
              <strong>{{ row.name }}</strong>
              <span v-if="row.description" class="mini-label">{{ row.description }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('cluster.networkType')" width="120" prop="type" />
          <el-table-column :label="t('cluster.managed')" width="110">
            <template #default="{ row }">
              <el-tag :type="row.managed ? 'success' : 'info'" size="small" effect="light">
                {{ row.managed ? t('common.yes') : t('common.no') }}
              </el-tag>
            </template>
          </el-table-column>
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
          <template #empty>
            <el-empty :description="t('cluster.noNetworks')" />
          </template>
        </el-table>
      </div>
    </section>

    <InstanceDetailDrawer
      v-model="detailOpen"
      :cluster="cluster"
      :name="detailName"
      @edit="openEditDetail"
      @migrate="openMigrate"
      @delete="confirmDelete"
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
  </div>
</template>

<style scoped>
.title-with-back {
  display: flex;
  align-items: center;
  gap: 14px;
}

.cluster-detail {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.section-heading {
  margin-bottom: 12px;
}

.name-link {
  padding: 0;
  height: auto;
  font-weight: 600;
}
</style>

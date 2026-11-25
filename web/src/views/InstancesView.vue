<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { TableInstance } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import MigrationsView from '@/views/MigrationsView.vue'
import BatchOpsDialog from '@/components/BatchOpsDialog.vue'
import CloneInstanceDialog from '@/components/CloneInstanceDialog.vue'
import { useAssetsStore } from '@/stores/assets'
import InstanceActions from '@/components/InstanceActions.vue'
import InstanceDeleteDialog from '@/components/InstanceDeleteDialog.vue'
import InstanceDetailDrawer from '@/components/InstanceDetailDrawer.vue'
import InstanceFormDialog from '@/components/InstanceFormDialog.vue'
import MigrateDialog from '@/components/MigrateDialog.vue'
import RenameInstanceDialog from '@/components/RenameInstanceDialog.vue'
import StatusTag from '@/components/StatusTag.vue'
import type {
  InstanceDetail,
  InstanceSummary,
  InstanceType,
} from '@/api/types'
import { useVisiblePolling, useInstanceActions } from '@/composables/useInstanceActions'
import { errorMessage } from '@/utils/errors'
import { useClustersStore } from '@/stores/clusters'
import { useInstancesStore } from '@/stores/instances'
import { formatBytes, formatDateTime, primaryAddress } from '@/utils/format'

const clusters = useClustersStore()
const instances = useInstancesStore()
const { t } = useI18n()

type InstancesTab = 'list' | 'migrations'
const activeTab = ref<InstancesTab>(
  new URLSearchParams(window.location.search).get('view') === 'migrations' ? 'migrations' : 'list',
)

// Keep the tab shareable/bookmarkable without coupling the view to the router
// (the route redirect from /migrations already lands here with the query).
watch(activeTab, (tab) => {
  const url = new URL(window.location.href)
  if (tab === 'migrations') {
    url.searchParams.set('view', 'migrations')
  } else {
    url.searchParams.delete('view')
  }
  window.history.replaceState(null, '', url.toString())
})

const search = ref('')
const clusterFilter = ref('')
const typeFilter = ref<InstanceType>('any')
const page = ref(1)
const pageSize = ref(20)
const detailOpen = ref(false)
const detailCluster = ref('')
const detailName = ref('')
const migrateOpen = ref(false)
const migrateInstance = ref<InstanceSummary | null>(null)
const renameOpen = ref(false)
const renameCluster = ref('')
const renameName = ref('')
const formOpen = ref(false)
const formMode = ref<'create' | 'edit'>('create')
const formCluster = ref('')
const formInstance = ref<InstanceDetail | null>(null)

const filtered = computed(() => {
  const needle = search.value.trim().toLowerCase()

  return instances.items.filter((instance) => {
    if (clusterFilter.value && instance.cluster !== clusterFilter.value) {
      return false
    }
    if (typeFilter.value !== 'any' && instance.type !== typeFilter.value) {
      return false
    }
    if (!needle) {
      return true
    }

    return (
      instance.name.toLowerCase().includes(needle) ||
      instance.cluster.toLowerCase().includes(needle) ||
      instance.ipv4.some((address) => address.includes(needle))
    )
  })
})

const paged = computed(() => {
  const start = (page.value - 1) * pageSize.value

  return filtered.value.slice(start, start + pageSize.value)
})

const runningCount = computed(
  () => instances.items.filter((instance) => instance.status.toLowerCase() === 'running').length,
)
const vmCount = computed(
  () => instances.items.filter((instance) => instance.type === 'virtual-machine').length,
)
const containerCount = computed(() => instances.items.length - vmCount.value)

async function load(): Promise<void> {
  await Promise.all([clusters.fetchAll(), instances.fetchAll(typeFilter.value)])
}

onMounted(load)
useVisiblePolling(load)

watch([search, clusterFilter, typeFilter], () => {
  page.value = 1
})

// Deleting the last row of the last page must not strand the user on an empty
// page beyond the new range.
watch(
  () => filtered.value.length,
  (length) => {
    const last = Math.max(1, Math.ceil(length / pageSize.value))
    if (page.value > last) {
      page.value = last
    }
  },
)

watch(typeFilter, async (type) => {
  await instances.fetchAll(type)
})

const { act, confirmDelete, isBusy, deleteTarget, deleting, cancelDelete, submitDelete } = useInstanceActions(load)

const cloneOpen = ref(false)
const cloneTarget = ref({ cluster: '', name: '' })

function openClone(instance: { cluster: string; name: string }): void {
  cloneTarget.value = { cluster: instance.cluster, name: instance.name }
  cloneOpen.value = true
}

const assets = useAssetsStore()
const selected = ref<Array<{ cluster: string; name: string }>>([])
const batchOpen = ref(false)
const batchMode = ref<'keys' | 'script'>('keys')
const batchBusy = ref(false)
const batchTargets = computed(() =>
  selected.value.map((entry) => ({ cluster: entry.cluster, instance: entry.name })),
)

function handleSelectionChange(rows: Array<{ cluster: string; name: string }>): void {
  selected.value = rows.map((row) => ({ cluster: row.cluster, name: row.name }))
}

const tableRef = ref<TableInstance>()

function cardSelected(row: InstanceSummary): boolean {
  return selected.value.some((entry) => entry.cluster === row.cluster && entry.name === row.name)
}

function toggleCardSelection(row: InstanceSummary, checked: boolean): void {
  tableRef.value?.toggleRowSelection(row, checked)
}

/** Stable identity so reserve-selection survives polling refreshes. */
function instanceRowKey(row: { cluster: string; name: string }): string {
  return `${row.cluster}/${row.name}`
}

function openBatch(mode: 'keys' | 'script'): void {
  if (selected.value.length === 0) {
    return
  }
  batchMode.value = mode
  batchOpen.value = true
}

async function batchPower(action: 'start' | 'stop' | 'restart'): Promise<void> {
  if (selected.value.length === 0 || batchBusy.value) {
    return
  }

  const summary = selected.value.map((entry) => entry.name).join(', ')
  try {
    await ElMessageBox.confirm(
      t('batch.powerConfirm', { action: t(`batch.action.${action}`), names: summary }),
      t('batch.powerTitle'),
      { type: 'warning', confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel') },
    )
  } catch {
    return
  }

  batchBusy.value = true
  try {
    const items = await assets.batchPower(batchTargets.value, action)
    const failed = items.filter((item) => !item.ok)
    if (failed.length === 0) {
      ElMessage.success(t('batch.allOk', { count: items.length }))
    } else {
      ElMessage.warning(t('batch.partial', { ok: items.length - failed.length, failed: failed.length }))
    }
    await load()
  } catch (err) {
    ElMessage.error(t('batch.failed', { error: errorMessage(err) }))
  } finally {
    batchBusy.value = false
  }
}

watch(
  () => instances.items,
  (items) => {
    if (
      detailOpen.value &&
      !items.some(
        (instance) => instance.cluster === detailCluster.value && instance.name === detailName.value,
      )
    ) {
      detailOpen.value = false
    }
  },
  { deep: true },
)

function openDetail(instance: InstanceSummary): void {
  detailCluster.value = instance.cluster
  detailName.value = instance.name
  detailOpen.value = true
}

function openMigrate(instance: InstanceSummary): void {
  migrateInstance.value = instance
  migrateOpen.value = true
}

function openRename(instance: Pick<InstanceSummary, 'cluster' | 'name'>): void {
  renameCluster.value = instance.cluster
  renameName.value = instance.name
  renameOpen.value = true
}

async function onRenamed(): Promise<void> {
  detailOpen.value = false
  await load()
}

function openCreate(): void {
  formMode.value = 'create'
  formCluster.value = ''
  formInstance.value = null
  formOpen.value = true
}

function openEdit(instance: InstanceSummary): void {
  void openEditDetail(instance)
}

async function openEditDetail(instance: InstanceSummary | InstanceDetail): Promise<void> {
  const detail =
    'config' in instance
      ? instance
      : await instances.fetchDetail(instance.cluster, instance.name)
  if (!detail) {
    ElMessage.error(instances.error || t('detail.unavailable'))
    return
  }

  formMode.value = 'edit'
  formCluster.value = instance.cluster
  formInstance.value = detail
  formOpen.value = true
}

async function onSaved(): Promise<void> {
  await load()
}
</script>

<template>
  <div class="page">
    <el-tabs v-model="activeTab" class="page-tabs">
      <el-tab-pane :label="t('instances.tabList')" name="list" />
      <el-tab-pane :label="t('instances.tabMigrations')" name="migrations" />
    </el-tabs>

    <MigrationsView v-if="activeTab === 'migrations'" embedded />

    <template v-else>
    <div class="page-header">
      <div class="page-heading">
        <span class="page-eyebrow">{{ t('instances.eyebrow') }}</span>
        <h2>{{ t('instances.title') }}</h2>
        <p class="subtitle">
          {{
            t('instances.subtitle', {
              count: instances.items.length,
              clusters: clusters.items.length,
              time: formatDateTime(instances.lastUpdated?.toISOString()),
            })
          }}
        </p>
      </div>
      <div class="page-actions">
        <el-button type="primary" :icon="Plus" @click="openCreate">
          {{ t('instances.create') }}
        </el-button>
        <el-button :icon="Refresh" :loading="instances.loading || clusters.loading" @click="load">
          {{ t('common.refresh') }}
        </el-button>
      </div>
    </div>

    <div class="instance-summary">
      <span class="summary-chip">
        <strong>{{ instances.items.length }}</strong>
        {{ t('dashboard.instances') }}
      </span>
      <span class="summary-chip is-running">
        <strong>{{ runningCount }}</strong>
        {{ t('status.running') }}
      </span>
      <span class="summary-chip">
        <strong>{{ vmCount }}</strong>
        {{ t('common.virtualMachines') }}
      </span>
      <span class="summary-chip">
        <strong>{{ containerCount }}</strong>
        {{ t('common.containers') }}
      </span>
      <span class="summary-chip is-cluster">
        <strong>{{ clusters.items.length }}</strong>
        {{ t('instances.clustersLabel') }}
      </span>
    </div>

    <el-alert
      v-if="instances.error"
      class="fleet-alert"
      type="error"
      :closable="false"
      show-icon
      :title="instances.error"
    />

    <el-alert
      v-if="instances.unavailableClusters.length > 0"
      class="fleet-alert"
      type="warning"
      :closable="false"
      show-icon
      aria-live="polite"
      :title="t('assets.partialFailure', { clusters: instances.unavailableClusters.join(', ') })"
    />

    <section class="panel">
      <div class="panel-body">
        <div class="table-toolbar" style="margin-bottom: 16px">
          <el-input
            v-model="search"
            :prefix-icon="Search"
            :placeholder="t('instances.searchPlaceholder')"
            clearable
            style="width: 280px"
          />
          <el-select v-model="typeFilter" style="width: 170px" :aria-label="t('instances.filterType')">
            <el-option :label="t('common.allTypes')" value="any" />
            <el-option :label="t('common.containers')" value="container" />
            <el-option :label="t('common.virtualMachines')" value="virtual-machine" />
          </el-select>
          <el-select
            v-model="clusterFilter"
            :placeholder="t('common.allClusters')"
            clearable
            style="width: 180px"
            :aria-label="t('instances.filterCluster')"
          >
            <el-option
              v-for="cluster in clusters.items"
              :key="cluster.name"
              :label="cluster.display_name"
              :value="cluster.name"
            />
          </el-select>
          <span class="muted">{{ t('common.shown', { count: filtered.length }) }}</span>
        </div>

        <div v-if="selected.length > 0" class="batch-bar">
          <span class="batch-count">{{ t('batch.selected', { count: selected.length }) }}</span>
          <el-button size="small" type="primary" @click="openBatch('keys')">
            {{ t('batch.keysAction') }}
          </el-button>
          <el-button size="small" type="primary" @click="openBatch('script')">
            {{ t('batch.scriptAction') }}
          </el-button>
          <el-button size="small" :loading="batchBusy" @click="batchPower('start')">{{ t('batch.action.start') }}</el-button>
          <el-button size="small" :loading="batchBusy" @click="batchPower('stop')">{{ t('batch.action.stop') }}</el-button>
          <el-button size="small" :loading="batchBusy" @click="batchPower('restart')">{{ t('batch.action.restart') }}</el-button>
          <el-button size="small" type="danger" plain @click="selected = []">
            {{ t('batch.clearSelection') }}
          </el-button>
        </div>
        <!-- row-key + reserve-selection keep the user's batch picks through
             the 15s polling refresh instead of clearing them (#12). -->
        <div v-loading="instances.loading" class="instance-cards">
          <article v-for="row in paged" :key="instanceRowKey(row)" class="instance-card">
            <div class="instance-card-head">
              <el-checkbox
                :model-value="cardSelected(row)"
                :aria-label="t('batch.selectRow')"
                @change="toggleCardSelection(row, Boolean($event))"
              />
              <el-button link type="primary" class="name-link" @click="openDetail(row)">
                {{ row.name }}
              </el-button>
              <StatusTag :status="row.status" :status-code="row.status_code" />
            </div>
            <div class="instance-card-meta">
              <span>{{ row.cluster }} · {{ row.location || t('common.none') }}</span>
              <span class="mono">{{ primaryAddress(row.ipv4, row.ipv6) }}</span>
              <span>{{ formatBytes(row.memory) }}</span>
              <span>{{ row.type === 'virtual-machine' ? t('common.virtualMachine') : t('common.container') }}</span>
            </div>
            <div class="instance-card-actions">
              <InstanceActions
                :instance="row"
                :busy="isBusy(row)"
                show-clone
                @action="act(row, $event)"
                @edit="openEdit(row)"
                @rename="openRename(row)"
                @migrate="openMigrate(row)"
                @clone="openClone(row)"
                @delete="confirmDelete(row)"
              />
            </div>
          </article>
          <el-empty v-if="paged.length === 0" :description="t('instances.empty')" />
        </div>
        <el-table
          ref="tableRef"
          v-loading="instances.loading"
          :data="paged"
          :row-key="instanceRowKey"
          stripe
          class="instance-table"
          @selection-change="handleSelectionChange"
        >
          <el-table-column type="selection" width="42" reserve-selection />
          <el-table-column :label="t('common.name')" min-width="145">
            <template #default="{ row }">
              <el-button link type="primary" class="name-link" @click="openDetail(row)">
                {{ row.name }}
              </el-button>
              <span class="name-meta">
                <span class="mini-label">{{ row.location || t('common.none') }}</span>
                <el-tag v-if="row.live_migratable" size="small" type="success" effect="plain">
                  {{ t('common.live') }}
                </el-tag>
              </span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.cluster')" width="80">
            <template #default="{ row }">
              <el-tag size="small" effect="plain">{{ row.cluster }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.type')" width="90">
            <template #default="{ row }">
              {{ row.type === 'virtual-machine' ? t('common.virtualMachine') : t('common.container') }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="80">
            <template #default="{ row }">
              <StatusTag :status="row.status" :status-code="row.status_code" />
            </template>
          </el-table-column>
          <el-table-column :label="t('common.address')" min-width="115">
            <template #default="{ row }">
              <span class="mono">{{ primaryAddress(row.ipv4, row.ipv6) }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.memory')" width="80">
            <template #default="{ row }">{{ formatBytes(row.memory) }}</template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="250">
            <template #default="{ row }">
              <InstanceActions
                :instance="row"
                :busy="isBusy(row)"
                show-clone
                @action="act(row, $event)"
                @edit="openEdit(row)"
                @rename="openRename(row)"
                @migrate="openMigrate(row)"
                @clone="openClone(row)"
                @delete="confirmDelete(row)"
              />
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="t('instances.empty')" />
          </template>
        </el-table>

        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="filtered.length"
          :page-sizes="[20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          class="table-pagination"
        />
      </div>
    </section>

    <InstanceDetailDrawer
      v-model="detailOpen"
      :cluster="detailCluster"
      :name="detailName"
      @edit="openEditDetail"
      @rename="openRename"
      @migrate="openMigrate"
      @delete="confirmDelete"
    />
    <RenameInstanceDialog
      v-model="renameOpen"
      :cluster="renameCluster"
      :instance="renameName"
      @renamed="onRenamed"
    />
    <InstanceFormDialog
      v-model="formOpen"
      :mode="formMode"
      :clusters="clusters.items"
      :cluster="formCluster"
      :instance="formInstance"
      @saved="onSaved"
    />
    <BatchOpsDialog
      v-model="batchOpen"
      :mode="batchMode"
      :targets="batchTargets"
      @done="load()"
    />
    <CloneInstanceDialog
      v-model="cloneOpen"
      :cluster="cloneTarget.cluster"
      :instance="cloneTarget.name"
      @cloned="load()"
    />
    <MigrateDialog
      v-model="migrateOpen"
      :instance="migrateInstance"
      :clusters="clusters.items"
      @submitted="load"
    />
    <InstanceDeleteDialog
      :target="deleteTarget"
      :busy="deleting"
      @cancel="cancelDelete"
      @confirm="submitDelete"
    />
    </template>
  </div>
</template>

<style scoped>
.page-heading {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.instance-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.summary-chip {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  padding: 6px 11px;
  border: 1px solid var(--fleet-line);
  border-radius: 999px;
  background: var(--fleet-panel);
  color: var(--fleet-ink-soft);
  font-size: 11.5px;
  font-weight: 600;
  box-shadow: 0 1px 2px rgba(16, 24, 40, 0.03);
}

.summary-chip strong {
  color: var(--fleet-ink);
  font-size: 14px;
  font-variant-numeric: tabular-nums;
}

.summary-chip.is-running strong {
  color: var(--fleet-good);
}

.summary-chip.is-cluster strong {
  color: var(--el-color-primary);
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

.table-pagination {
  margin-top: 14px;
  justify-content: flex-end;
}
.batch-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
  padding: 10px 14px;
  border-radius: 8px;
  background: var(--el-color-primary-light-9);
}

.batch-count {
  font-size: 13px;
  font-weight: 600;
}

.instance-cards {
  display: none;
}

@media (max-width: 768px) {
  .instance-cards {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .instance-table {
    display: none;
  }

  .instance-card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 12px;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 10px;
  }

  .instance-card-head,
  .instance-card-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .instance-card-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
}
</style>

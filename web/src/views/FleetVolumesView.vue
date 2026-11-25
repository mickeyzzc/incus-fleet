<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { TableInstance } from 'element-plus'
import { Coin, Collection, Delete, Grid, Refresh, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { FleetVolume } from '@/api/types'
import { api } from '@/api'
import { errorMessage } from '@/utils/errors'
import { formatBytes } from '@/utils/format'

const { t } = useI18n()

const volumes = ref<FleetVolume[]>([])
const clusters = ref<string[]>([])
const loading = ref(false)
const bulkBusy = ref(false)
const error = ref('')
const search = ref('')
const clusterFilter = ref('')
const selected = ref<FleetVolume[]>([])
const tableRef = ref<TableInstance>()

/** Volumes matching the search and cluster filters. */
const filtered = computed(() => {
  const needle = search.value.trim().toLowerCase()

  return volumes.value.filter((volume) => {
    if (clusterFilter.value && volume.cluster !== clusterFilter.value) {
      return false
    }
    if (!needle) {
      return true
    }

    return (
      volume.name.toLowerCase().includes(needle) ||
      volume.cluster.toLowerCase().includes(needle) ||
      volume.pool.toLowerCase().includes(needle)
    )
  })
})

/** Totals across every visible volume, so the fleet view answers "how much". */
const totals = computed(() => {
  let used = 0
  let count = 0
  for (const volume of filtered.value) {
    count += 1
    const size = Number(volume.config?.size ?? 0)
    if (Number.isFinite(size)) {
      used += size
    }
  }

  return { count, used }
})

const clusterCount = computed(() => new Set(filtered.value.map((volume) => volume.cluster)).size)
const poolCount = computed(
  () => new Set(filtered.value.map((volume) => `${volume.cluster}/${volume.pool}`)).size,
)
const inUseCount = computed(
  () => filtered.value.filter((volume) => (volume.used_by?.length ?? 0) > 0).length,
)

function rowKey(volume: FleetVolume): string {
  return `${volume.cluster}/${volume.pool}/${volume.name}`
}

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    clusters.value = (await api.listClusters()).map((cluster) => cluster.name)
    const result = await api.listAllVolumesWithErrors()
    volumes.value = result.items
    selected.value = []
    tableRef.value?.clearSelection()
    if (result.failedClusters.length > 0) {
      error.value = t('assets.partialFailure', { clusters: result.failedClusters.join(', ') })
    }
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

onMounted(load)

watch([search, clusterFilter], () => {
  selected.value = []
  tableRef.value?.clearSelection()
})

function sizeLabel(volume: FleetVolume): string {
  const size = Number(volume.config?.size ?? 0)
  return Number.isFinite(size) && size > 0 ? formatBytes(size) : '—'
}

function handleSelectionChange(rows: FleetVolume[]): void {
  selected.value = rows
}

async function confirmDelete(volume: FleetVolume): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('fleetVolumes.deleteConfirm', { name: volume.name }),
      t('fleetVolumes.deleteTitle'),
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
    await api.deleteVolume(volume.cluster, volume.pool, volume.name)
    ElMessage.success(t('fleetVolumes.deleted', { name: volume.name }))
    await load()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

async function deleteSelected(): Promise<void> {
  const items = [...selected.value]
  if (items.length === 0) {
    return
  }

  try {
    await ElMessageBox.confirm(
      t('fleetVolumes.bulkDeleteConfirm', { count: items.length }),
      t('fleetVolumes.bulkDeleteTitle'),
      {
        type: 'warning',
        confirmButtonText: t('fleetVolumes.bulkDelete'),
        cancelButtonText: t('common.cancel'),
      },
    )
  } catch {
    return
  }

  bulkBusy.value = true
  try {
    const results = await Promise.allSettled(
      items.map((volume) => api.deleteVolume(volume.cluster, volume.pool, volume.name)),
    )
    const failed = results.filter((result) => result.status === 'rejected').length
    if (failed === 0) {
      ElMessage.success(t('fleetVolumes.bulkDeleteDone', { count: items.length }))
    } else {
      ElMessage.warning(
        t('fleetVolumes.bulkDeletePartial', {
          ok: items.length - failed,
          failed,
        }),
      )
    }
    await load()
  } finally {
    bulkBusy.value = false
  }
}
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div class="page-heading">
        <span class="page-eyebrow">{{ t('nav.resources') }} · {{ t('nav.fleetVolumes') }}</span>
        <h2>{{ t('fleetVolumes.title') }}</h2>
        <p class="subtitle">{{ t('fleetVolumes.subtitle') }}</p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" :loading="loading" @click="load">
          {{ t('common.refresh') }}
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="error"
      class="fleet-alert"
      type="error"
      :closable="false"
      show-icon
      :title="error"
    />

    <section class="stat-grid volume-stats">
      <article class="stat-card stat-card--volumes">
        <div class="stat-top">
          <span class="stat-icon"><el-icon><Collection /></el-icon></span>
          <span class="label">{{ t('fleetVolumes.totalVolumes') }}</span>
        </div>
        <div class="stat-value-row">
          <span class="value">{{ totals.count }}</span>
          <span class="stat-unit">{{ clusterCount }} {{ t('fleetVolumes.clusters') }}</span>
        </div>
        <span class="hint">{{ t('fleetVolumes.visibleHint', { count: volumes.length }) }}</span>
      </article>

      <article class="stat-card stat-card--allocated">
        <div class="stat-top">
          <span class="stat-icon"><el-icon><Coin /></el-icon></span>
          <span class="label">{{ t('fleetVolumes.allocated') }}</span>
        </div>
        <div class="stat-value-row">
          <span class="value">{{ formatBytes(totals.used) }}</span>
        </div>
        <span class="hint">{{ t('fleetVolumes.allocatedHint') }}</span>
      </article>

      <article class="stat-card stat-card--pools">
        <div class="stat-top">
          <span class="stat-icon"><el-icon><Grid /></el-icon></span>
          <span class="label">{{ t('fleetVolumes.pools') }}</span>
        </div>
        <div class="stat-value-row">
          <span class="value">{{ poolCount }}</span>
          <span class="stat-unit">{{ clusterCount }} {{ t('fleetVolumes.clusters') }}</span>
        </div>
        <span class="hint">{{ t('fleetVolumes.poolsHint') }}</span>
      </article>

      <article class="stat-card stat-card--inuse">
        <div class="stat-top">
          <span class="stat-icon"><el-icon><Coin /></el-icon></span>
          <span class="label">{{ t('fleetVolumes.usedBy') }}</span>
        </div>
        <div class="stat-value-row">
          <span class="value">{{ inUseCount }}</span>
          <span class="stat-unit">/ {{ totals.count }}</span>
        </div>
        <span class="hint">{{ t('fleetVolumes.inUseHint') }}</span>
      </article>
    </section>

    <section class="panel volume-panel">
      <div class="panel-head">
        <div>
          <h3 class="section-title">{{ t('fleetVolumes.tableTitle') }}</h3>
          <p class="subtitle">{{ t('fleetVolumes.tableHint') }}</p>
        </div>
        <span class="volume-summary">
          {{ t('fleetVolumes.summary', { count: totals.count, size: formatBytes(totals.used) }) }}
        </span>
      </div>

      <div class="panel-body">
        <div class="table-toolbar volume-toolbar">
          <el-input
            v-model="search"
            :prefix-icon="Search"
            :placeholder="t('fleetVolumes.searchPlaceholder')"
            clearable
            class="filter-input"
          />
          <el-select
            v-model="clusterFilter"
            :placeholder="t('common.allClusters')"
            clearable
            class="volume-cluster-filter"
          >
            <el-option v-for="cluster in clusters" :key="cluster" :label="cluster" :value="cluster" />
          </el-select>
          <div class="toolbar-spacer" />
          <div v-if="selected.length > 0" class="bulk-actions">
            <span class="bulk-count">{{ t('fleetVolumes.selected', { count: selected.length }) }}</span>
            <el-button
              type="danger"
              plain
              size="small"
              :icon="Delete"
              :loading="bulkBusy"
              @click="deleteSelected"
            >
              {{ t('fleetVolumes.bulkDelete') }}
            </el-button>
            <el-button size="small" text @click="tableRef?.clearSelection()">
              {{ t('fleetVolumes.clearSelection') }}
            </el-button>
          </div>
        </div>

        <el-table
          ref="tableRef"
          v-loading="loading"
          :data="filtered"
          :row-key="rowKey"
          stripe
          @selection-change="handleSelectionChange"
        >
          <el-table-column type="selection" width="42" />
          <el-table-column :label="t('common.name')" min-width="160">
            <template #default="{ row }">
              <strong class="mono volume-name" :title="row.name">{{ row.name }}</strong>
              <span v-if="row.description" class="mini-label">{{ row.description }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.cluster')" width="90">
            <template #default="{ row }">
              <el-tag size="small" effect="plain">{{ row.cluster }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('fleetVolumes.pool')" width="100">
            <template #default="{ row }">
              <span class="mono">{{ row.pool }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('fleetVolumes.contentType')" width="90">
            <template #default="{ row }">{{ row.content_type || row.type || '—' }}</template>
          </el-table-column>
          <el-table-column :label="t('fleetVolumes.size')" width="90">
            <template #default="{ row }">{{ sizeLabel(row) }}</template>
          </el-table-column>
          <el-table-column :label="t('fleetVolumes.usedBy')" width="75">
            <template #default="{ row }">
              <el-tag
                :type="(row.used_by?.length ?? 0) > 0 ? 'warning' : 'info'"
                size="small"
                effect="plain"
              >
                {{ row.used_by?.length ?? 0 }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('fleetVolumes.location')" width="100">
            <template #default="{ row }">{{ row.location || '—' }}</template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="80">
            <template #default="{ row }">
              <el-button link type="danger" @click="confirmDelete(row)">
                {{ t('common.delete') }}
              </el-button>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="t('fleetVolumes.empty')" />
          </template>
        </el-table>
      </div>
    </section>
  </div>
</template>

<style scoped>
.page-heading {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.volume-stats .stat-card {
  --stat-accent: #4562e8;
  position: relative;
  overflow: hidden;
  gap: 7px;
  padding: 15px 17px 14px;
}

.volume-stats .stat-card::after {
  content: '';
  position: absolute;
  inset: 0 0 auto;
  height: 3px;
  background: var(--stat-accent);
  opacity: 0.85;
}

.stat-card--allocated {
  --stat-accent: #0ea5a4;
}

.stat-card--pools {
  --stat-accent: #8b5cf6;
}

.stat-card--inuse {
  --stat-accent: #f59e0b;
}

.stat-top {
  display: flex;
  align-items: center;
  gap: 9px;
}

.stat-icon {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  flex: none;
  border-radius: 9px;
  background: color-mix(in srgb, var(--stat-accent) 12%, transparent);
  color: var(--stat-accent);
  font-size: 15px;
}

.stat-value-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.stat-unit {
  color: var(--fleet-ink-soft);
  font-size: 11.5px;
  font-weight: 600;
  white-space: nowrap;
}

.volume-panel {
  overflow: hidden;
}

.volume-summary {
  color: var(--fleet-ink-soft);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.volume-toolbar {
  margin-bottom: 14px;
}

.volume-cluster-filter {
  width: 170px;
}

.toolbar-spacer {
  flex: 1;
}

.bulk-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.bulk-count {
  color: var(--fleet-ink-soft);
  font-size: 12px;
  font-weight: 600;
}

.volume-name {
  display: block;
  overflow: hidden;
  font-size: 13px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 620px) {
  .toolbar-spacer {
    display: none;
  }

  .bulk-actions {
    width: 100%;
    flex-wrap: wrap;
  }
}
</style>

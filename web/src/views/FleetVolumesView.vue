<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { FleetVolume } from '@/api/types'
import { api } from '@/api'
import { errorMessage } from '@/utils/errors'
import { formatBytes } from '@/utils/format'

const { t } = useI18n()

const volumes = ref<FleetVolume[]>([])
const clusters = ref<string[]>([])
const loading = ref(false)
const error = ref('')
const search = ref('')
const clusterFilter = ref('')

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
  let count = 0.0
  for (const volume of filtered.value) {
    count += 1
    const size = Number(volume.config?.size ?? 0)
    if (Number.isFinite(size)) {
      used += size
    }
  }

  return { count, used }
})

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    clusters.value = (await api.listClusters()).map((cluster) => cluster.name)
    volumes.value = await api.listAllVolumes()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

onMounted(load)

function sizeLabel(volume: FleetVolume): string {
  const size = Number(volume.config?.size ?? 0)
  return Number.isFinite(size) && size > 0 ? formatBytes(size) : '—'
}
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
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

    <section class="panel">
      <div class="panel-body">
        <div class="table-toolbar" style="margin-bottom: 16px">
          <el-input
            v-model="search"
            :prefix-icon="Search"
            :placeholder="t('fleetVolumes.searchPlaceholder')"
            clearable
            style="width: 280px"
          />
          <el-select
            v-model="clusterFilter"
            :placeholder="t('common.allClusters')"
            clearable
            style="width: 180px"
          >
            <el-option v-for="cluster in clusters" :key="cluster" :label="cluster" :value="cluster" />
          </el-select>
          <span class="muted">
            {{ t('fleetVolumes.summary', { count: totals.count, size: formatBytes(totals.used) }) }}
          </span>
        </div>

        <el-table v-loading="loading" :data="filtered" stripe row-key="name">
          <el-table-column :label="t('common.name')" min-width="180">
            <template #default="{ row }">
              <strong class="mono">{{ row.name }}</strong>
              <span v-if="row.description" class="mini-label">{{ row.description }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.cluster')" width="150">
            <template #default="{ row }">
              <el-tag size="small" effect="plain">{{ row.cluster }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('fleetVolumes.pool')" width="140">
            <template #default="{ row }">
              <span class="mono">{{ row.pool }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('fleetVolumes.contentType')" width="130">
            <template #default="{ row }">{{ row.content_type || row.type || '—' }}</template>
          </el-table-column>
          <el-table-column :label="t('fleetVolumes.size')" width="120">
            <template #default="{ row }">{{ sizeLabel(row) }}</template>
          </el-table-column>
          <el-table-column :label="t('fleetVolumes.usedBy')" width="110">
            <template #default="{ row }">{{ row.used_by?.length ?? 0 }}</template>
          </el-table-column>
          <el-table-column :label="t('fleetVolumes.location')" width="150">
            <template #default="{ row }">{{ row.location || '—' }}</template>
          </el-table-column>
          <template #empty>
            <el-empty :description="t('fleetVolumes.empty')" />
          </template>
        </el-table>
      </div>
    </section>
  </div>
</template>

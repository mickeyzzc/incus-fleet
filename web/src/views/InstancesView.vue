<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'

import InstanceDetailDrawer from '@/components/InstanceDetailDrawer.vue'
import MigrateDialog from '@/components/MigrateDialog.vue'
import StatusTag from '@/components/StatusTag.vue'
import type { InstanceSummary, InstanceType } from '@/api/types'
import { useClustersStore } from '@/stores/clusters'
import { useInstancesStore } from '@/stores/instances'
import { formatBytes, formatDateTime, primaryAddress } from '@/utils/format'

const clusters = useClustersStore()
const instances = useInstancesStore()

const search = ref('')
const clusterFilter = ref('')
const typeFilter = ref<InstanceType>('any')
const detailOpen = ref(false)
const detailCluster = ref('')
const detailName = ref('')
const migrateOpen = ref(false)
const migrateInstance = ref<InstanceSummary | null>(null)

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

async function load(): Promise<void> {
  await Promise.all([clusters.fetchAll(), instances.fetchAll(typeFilter.value)])
}

onMounted(load)

watch(typeFilter, async (type) => {
  await instances.fetchAll(type)
})

function openDetail(instance: InstanceSummary): void {
  detailCluster.value = instance.cluster
  detailName.value = instance.name
  detailOpen.value = true
}

function openMigrate(instance: InstanceSummary): void {
  migrateInstance.value = instance
  migrateOpen.value = true
}
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>All instances</h2>
        <p class="subtitle">
          {{ instances.items.length }} instances across {{ clusters.items.length }} clusters ·
          {{ formatDateTime(instances.lastUpdated?.toISOString()) }}
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" :loading="instances.loading || clusters.loading" @click="load">
          Refresh
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="instances.error"
      class="fleet-alert"
      type="error"
      :closable="false"
      show-icon
      :title="instances.error"
    />

    <section class="panel">
      <div class="panel-body">
        <div class="table-toolbar" style="margin-bottom: 16px">
          <el-input
            v-model="search"
            :prefix-icon="Search"
            placeholder="Search name, cluster, or IP"
            clearable
            style="width: 280px"
          />
          <el-select v-model="typeFilter" style="width: 170px">
            <el-option label="All types" value="any" />
            <el-option label="Containers" value="container" />
            <el-option label="Virtual machines" value="virtual-machine" />
          </el-select>
          <el-select v-model="clusterFilter" placeholder="All clusters" clearable style="width: 180px">
            <el-option
              v-for="cluster in clusters.items"
              :key="cluster.name"
              :label="cluster.display_name"
              :value="cluster.name"
            />
          </el-select>
          <span class="muted">{{ filtered.length }} shown</span>
        </div>

        <el-table v-loading="instances.loading" :data="filtered" stripe>
          <el-table-column label="Name" min-width="170">
            <template #default="{ row }">
              <strong>{{ row.name }}</strong>
              <span class="mini-label">{{ row.location || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="Cluster" width="130">
            <template #default="{ row }">
              <el-tag size="small" effect="plain">{{ row.cluster }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="Type" width="110">
            <template #default="{ row }">
              {{ row.type === 'virtual-machine' ? 'VM' : 'Container' }}
            </template>
          </el-table-column>
          <el-table-column label="Status" width="120">
            <template #default="{ row }">
              <StatusTag :status="row.status" :status-code="row.status_code" />
            </template>
          </el-table-column>
          <el-table-column label="Address" min-width="150">
            <template #default="{ row }">
              <span class="mono">{{ primaryAddress(row.ipv4, row.ipv6) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="Memory" width="110">
            <template #default="{ row }">{{ formatBytes(row.memory) }}</template>
          </el-table-column>
          <el-table-column label="Live" width="90">
            <template #default="{ row }">
              <el-tag v-if="row.live_migratable" size="small" type="success" effect="light">
                Yes
              </el-tag>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="Actions" width="200" fixed="right">
            <template #default="{ row }">
              <el-button size="small" text @click="openDetail(row)">Details</el-button>
              <el-button size="small" type="primary" plain @click="openMigrate(row)">
                Migrate
              </el-button>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty description="No instances match the filters" />
          </template>
        </el-table>
      </div>
    </section>

    <InstanceDetailDrawer v-model="detailOpen" :cluster="detailCluster" :name="detailName" />
    <MigrateDialog
      v-model="migrateOpen"
      :instance="migrateInstance"
      :clusters="clusters.items"
      @submitted="load"
    />
  </div>
</template>

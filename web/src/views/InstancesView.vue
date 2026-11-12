<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import CloneInstanceDialog from '@/components/CloneInstanceDialog.vue'
import InstanceActions from '@/components/InstanceActions.vue'
import InstanceDetailDrawer from '@/components/InstanceDetailDrawer.vue'
import InstanceFormDialog from '@/components/InstanceFormDialog.vue'
import MigrateDialog from '@/components/MigrateDialog.vue'
import StatusTag from '@/components/StatusTag.vue'
import type {
  InstanceDetail,
  InstanceSummary,
  InstanceType,
} from '@/api/types'
import { useVisiblePolling, useInstanceActions } from '@/composables/useInstanceActions'
import { useClustersStore } from '@/stores/clusters'
import { useInstancesStore } from '@/stores/instances'
import { formatBytes, formatDateTime, primaryAddress } from '@/utils/format'

const clusters = useClustersStore()
const instances = useInstancesStore()
const { t } = useI18n()

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

async function load(): Promise<void> {
  await Promise.all([clusters.fetchAll(), instances.fetchAll(typeFilter.value)])
}

onMounted(load)
useVisiblePolling(load)

watch([search, clusterFilter, typeFilter], () => {
  page.value = 1
})

watch(typeFilter, async (type) => {
  await instances.fetchAll(type)
})

const { act, confirmDelete, isBusy } = useInstanceActions(load)

const cloneOpen = ref(false)
const cloneTarget = ref({ cluster: '', name: '' })

function openClone(instance: { cluster: string; name: string }): void {
  cloneTarget.value = { cluster: instance.cluster, name: instance.name }
  cloneOpen.value = true
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
    <div class="page-header">
      <div>
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

        <el-table v-loading="instances.loading" :data="paged" stripe>
          <el-table-column :label="t('common.name')" min-width="170">
            <template #default="{ row }">
              <el-button link type="primary" class="name-link" @click="openDetail(row)">
                {{ row.name }}
              </el-button>
              <span class="mini-label">{{ row.location || t('common.none') }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.cluster')" width="130">
            <template #default="{ row }">
              <el-tag size="small" effect="plain">{{ row.cluster }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.type')" width="120">
            <template #default="{ row }">
              {{ row.type === 'virtual-machine' ? t('common.virtualMachine') : t('common.container') }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="120">
            <template #default="{ row }">
              <StatusTag :status="row.status" :status-code="row.status_code" />
            </template>
          </el-table-column>
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
                :busy="isBusy(row)"
                @action="act(row, $event)"
                @edit="openEdit(row)"
                @migrate="openMigrate(row)"
                @delete="confirmDelete(row)"
              />
              <el-button link type="primary" @click="openClone(row)">
                {{ t('clone.action') }}
              </el-button>
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
      @migrate="openMigrate"
      @delete="confirmDelete"
    />
    <InstanceFormDialog
      v-model="formOpen"
      :mode="formMode"
      :clusters="clusters.items"
      :cluster="formCluster"
      :instance="formInstance"
      @saved="onSaved"
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
  </div>
</template>

<style scoped>
.name-link {
  padding: 0;
  height: auto;
  font-weight: 600;
}

.table-pagination {
  margin-top: 14px;
  justify-content: flex-end;
}
</style>

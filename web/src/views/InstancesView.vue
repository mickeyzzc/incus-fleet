<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import InstanceActions from '@/components/InstanceActions.vue'
import InstanceDetailDrawer from '@/components/InstanceDetailDrawer.vue'
import InstanceFormDialog from '@/components/InstanceFormDialog.vue'
import MigrateDialog from '@/components/MigrateDialog.vue'
import StatusTag from '@/components/StatusTag.vue'
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

const clusters = useClustersStore()
const instances = useInstancesStore()
const { t } = useI18n()

const search = ref('')
const clusterFilter = ref('')
const typeFilter = ref<InstanceType>('any')
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
    return
  }

  formMode.value = 'edit'
  formCluster.value = instance.cluster
  formInstance.value = detail
  formOpen.value = true
}

async function act(instance: InstanceSummary | InstanceDetail, action: InstanceAction): Promise<void> {
  try {
    await instances.instanceAction(instance.cluster, instance.name, action)
    ElMessage.success(
      t('instances.actionSuccess', {
        action: t(`instances.actionDone.${action}`),
        name: instance.name,
      }),
    )
    await Promise.all([instances.fetchAll(typeFilter.value), clusters.fetchAll()])
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
    await Promise.all([instances.fetchAll(typeFilter.value), clusters.fetchAll()])
  } catch (err) {
    ElMessage.error(t('instances.actionFailed', { error: errorMessage(err) }))
  }
}

async function onSaved(): Promise<void> {
  await Promise.all([instances.fetchAll(typeFilter.value), clusters.fetchAll()])
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

        <el-table v-loading="instances.loading" :data="filtered" stripe>
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
                @action="act(row, $event)"
                @edit="openEdit(row)"
                @migrate="openMigrate(row)"
                @delete="confirmDelete(row)"
              />
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="t('instances.empty')" />
          </template>
        </el-table>
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
</style>

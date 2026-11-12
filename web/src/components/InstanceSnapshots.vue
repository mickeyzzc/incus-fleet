<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { InstanceSummary } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'
import { formatBytes, formatDateTime } from '@/utils/format'

const props = defineProps<{
  cluster: string
  instance: string
  summary?: InstanceSummary | null
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const createOpen = ref(false)
const form = reactive({ name: '', stateful: false })

const snapshots = computed(() => instances.snapshots(props.cluster, props.instance))
const statefulCapable = computed(
  () => props.summary?.type === 'virtual-machine' && props.summary?.migration_stateful,
)

onMounted(() => {
  void instances.fetchSnapshots(props.cluster, props.instance)
})

async function refresh(): Promise<void> {
  await instances.fetchSnapshots(props.cluster, props.instance, true)
}

function openCreate(): void {
  form.name = ''
  form.stateful = false
  createOpen.value = true
}

async function submitCreate(): Promise<void> {
  const name = form.name.trim()
  if (!name) {
    ElMessage.error(t('admin.validationName'))
    return
  }

  try {
    await instances.createSnapshot(props.cluster, props.instance, {
      name,
      stateful: form.stateful,
    })
    ElMessage.success(t('snapshots.created', { name }))
    createOpen.value = false
  } catch (err) {
    ElMessage.error(t('snapshots.failed', { error: errorMessage(err) }))
  }
}

async function restore(name: string): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('snapshots.restoreConfirm', { instance: props.instance, name }),
      t('snapshots.restore'),
      { type: 'warning', confirmButtonText: t('snapshots.restore'), cancelButtonText: t('common.cancel') },
    )
  } catch {
    return
  }

  try {
    await instances.restoreSnapshot(props.cluster, props.instance, name)
    ElMessage.success(t('snapshots.restored', { instance: props.instance, name }))
  } catch (err) {
    ElMessage.error(t('snapshots.failed', { error: errorMessage(err) }))
  }
}

async function remove(name: string): Promise<void> {
  try {
    await ElMessageBox.confirm(t('snapshots.deleteConfirm', { name }), t('common.delete'), {
      type: 'warning',
      confirmButtonText: t('common.delete'),
      cancelButtonText: t('common.cancel'),
    })
  } catch {
    return
  }

  try {
    await instances.deleteSnapshot(props.cluster, props.instance, name)
    ElMessage.success(t('snapshots.deleted', { name }))
  } catch (err) {
    ElMessage.error(t('snapshots.failed', { error: errorMessage(err) }))
  }
}
</script>

<template>
  <div class="snapshot-panel">
    <div class="table-toolbar" style="margin-bottom: 12px">
      <el-button :icon="Plus" type="primary" size="small" @click="openCreate">
        {{ t('snapshots.create') }}
      </el-button>
      <el-button :icon="Refresh" size="small" :loading="instances.snapshotsLoading" @click="refresh">
        {{ t('common.refresh') }}
      </el-button>
    </div>

    <el-table v-loading="instances.snapshotsLoading" :data="snapshots" size="small" stripe>
      <el-table-column :label="t('common.name')" min-width="120">
        <template #default="{ row }">
          <strong>{{ row.name }}</strong>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.created')" min-width="150">
        <template #default="{ row }">
          {{ formatDateTime(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('common.size')" width="100">
        <template #default="{ row }">
          {{ formatBytes(row.size) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('snapshots.statefulLabel')" width="90">
        <template #default="{ row }">
          <el-tag v-if="row.stateful" size="small" type="success" effect="light">
            {{ t('common.yes') }}
          </el-tag>
          <span v-else class="muted">{{ t('common.none') }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.actions')" width="160">
        <template #default="{ row }">
          <el-button link type="primary" @click="restore(row.name)">
            {{ t('snapshots.restore') }}
          </el-button>
          <el-button link type="danger" @click="remove(row.name)">
            {{ t('common.delete') }}
          </el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty :description="t('snapshots.empty')" :image-size="60" />
      </template>
    </el-table>

    <el-dialog v-model="createOpen" :title="t('snapshots.create')" width="420px" destroy-on-close>
      <el-form label-position="top" @submit.prevent="submitCreate">
        <el-form-item :label="t('snapshots.name')">
          <el-input v-model="form.name" :placeholder="t('snapshots.namePlaceholder')" />
        </el-form-item>
        <el-form-item>
          <el-checkbox v-model="form.stateful" :disabled="!statefulCapable">
            {{ t('snapshots.stateful') }}
          </el-checkbox>
          <p class="field-hint">{{ t('snapshots.statefulHint') }}</p>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="instances.saving" @click="submitCreate">
          {{ t('common.create') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.snapshot-panel {
  display: flex;
  flex-direction: column;
}

.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}
</style>

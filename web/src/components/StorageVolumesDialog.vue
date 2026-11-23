<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { StoragePoolResource } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'
import { parseSize } from '@/utils/size'
import { formatBytes } from '@/utils/format'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  pool: StoragePoolResource | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const form = reactive({ name: '', description: '', size: '' })
const creating = ref(false)

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const volumes = computed(() => instances.volumes(props.cluster, props.pool?.name ?? ''))

/** Parses an Incus size string into bytes for display; returns 0 when unknown. */
function sizeBytes(value: string): number {
  const units: Record<string, number> = {
    KiB: 1024,
    MiB: 1024 ** 2,
    GiB: 1024 ** 3,
    TiB: 1024 ** 4,
    PiB: 1024 ** 5,
  }

  const match = /^([\d.]+)\s*(KiB|MiB|GiB|TiB|PiB|B)?$/.exec(value.trim())
  if (!match) {
    return 0
  }

  const amount = Number.parseFloat(match[1] ?? '0')
  return amount * (units[match[2] ?? 'B'] ?? 1)
}

watch(
  () => [props.modelValue, props.pool?.name] as const,
  ([open, pool]) => {
    creating.value = false
    form.name = ''
    form.description = ''
    form.size = ''
    if (open && pool) {
      void instances.fetchVolumes(props.cluster, pool, true)
    }
  },
  { immediate: true },
)

async function refresh(): Promise<void> {
  if (props.pool) {
    await instances.fetchVolumes(props.cluster, props.pool.name, true)
  }
}

function toggleCreate(): void {
  creating.value = !creating.value
  form.name = ''
  form.description = ''
  form.size = ''
}

async function submitCreate(): Promise<void> {
  const name = form.name.trim()
  if (!name) {
    ElMessage.error(t('admin.validationName'))
    return
  }
  if (form.size.trim() && !parseSize(form.size).valid) {
    ElMessage.error(t('validation.sizeFormat'))
    return
  }

  try {
    await instances.createVolume(props.cluster, props.pool?.name ?? '', {
      name,
      description: form.description.trim(),
      size: parseSize(form.size).value,
    })
    ElMessage.success(t('volumes.created', { name }))
    creating.value = false
    form.name = ''
    form.description = ''
    form.size = ''
  } catch (err) {
    ElMessage.error(t('volumes.failed', { error: errorMessage(err) }))
  }
}

async function remove(name: string): Promise<void> {
  try {
    await ElMessageBox.confirm(t('volumes.deleteConfirm', { name }), t('common.delete'), {
      type: 'warning',
      confirmButtonText: t('common.delete'),
      cancelButtonText: t('common.cancel'),
    })
  } catch {
    return
  }

  try {
    await instances.deleteVolume(props.cluster, props.pool?.name ?? '', name)
    ElMessage.success(t('volumes.deleted', { name }))
  } catch (err) {
    ElMessage.error(t('volumes.failed', { error: errorMessage(err) }))
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`${t('volumes.title')} · ${pool?.name ?? ''}`"
    width="620px"
    destroy-on-close
  >
    <div class="table-toolbar" style="margin-bottom: 12px">
      <el-button :icon="Plus" type="primary" size="small" @click="toggleCreate">
        {{ t('volumes.create') }}
      </el-button>
      <el-button :icon="Refresh" size="small" :loading="instances.volumesLoading" @click="refresh">
        {{ t('common.refresh') }}
      </el-button>
    </div>

    <el-form v-if="creating" label-position="top" class="volume-form" @submit.prevent="submitCreate">
      <div class="volume-grid">
        <el-form-item :label="t('common.name')">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('volumes.size')">
          <el-input v-model="form.size" :placeholder="t('form.diskSizePlaceholder')" />
          <p class="field-hint">{{ t('validation.sizeHint') }}</p>
          <p v-if="form.size.trim() && !parseSize(form.size).valid" class="field-error">
            {{ t('validation.sizeFormat') }}
          </p>
        </el-form-item>
        <el-form-item :label="t('admin.description')" class="volume-description">
          <el-input v-model="form.description" />
        </el-form-item>
      </div>
      <el-button type="primary" size="small" :loading="instances.saving" @click="submitCreate">
        {{ t('common.create') }}
      </el-button>
    </el-form>

    <el-table v-loading="instances.volumesLoading" :data="volumes" size="small" stripe>
      <el-table-column :label="t('common.name')" min-width="120">
        <template #default="{ row }">
          <strong>{{ row.name }}</strong>
          <span v-if="row.description" class="mini-label">{{ row.description }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('volumes.size')" width="100">
        <template #default="{ row }">
          {{ row.config?.size ? formatBytes(sizeBytes(row.config.size)) : t('common.none') }}
        </template>
      </el-table-column>
      <el-table-column :label="t('common.node')" width="110">
        <template #default="{ row }">
          {{ row.location || t('common.none') }}
        </template>
      </el-table-column>
      <el-table-column :label="t('cluster.usedBy')" width="110">
        <template #default="{ row }">
          {{ t('cluster.usedByCount', { count: row.used_by.length }) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('common.actions')" width="90">
        <template #default="{ row }">
          <el-button link type="danger" @click="remove(row.name)">
            {{ t('common.delete') }}
          </el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty :description="t('volumes.empty')" :image-size="60" />
      </template>
    </el-table>
  </el-dialog>
</template>

<style scoped>
.volume-form {
  margin-bottom: 14px;
  padding: 12px;
  border: 1px solid var(--fleet-line, #dcdfe6);
  border-radius: 6px;
}

.volume-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0 14px;
}

.volume-description {
  grid-column: 1 / -1;
}
</style>

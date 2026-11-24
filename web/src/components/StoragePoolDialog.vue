<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import KeyValueEditor from '@/components/KeyValueEditor.vue'
import type { StoragePoolResource } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    cluster: string
    pool?: StoragePoolResource | null
  }>(),
  {
    pool: null,
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: [name: string]
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const form = reactive({
  name: '',
  driver: '',
  description: '',
  config: {} as Record<string, string>,
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const isEdit = computed(() => Boolean(props.pool))
const title = computed(() => (isEdit.value ? t('admin.editPool') : t('admin.addPool')))
const drivers = ['dir', 'btrfs', 'zfs', 'lvm', 'ceph', 'cephfs']

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      return
    }

    form.name = props.pool?.name ?? ''
    form.driver = props.pool?.driver ?? ''
    form.description = props.pool?.description ?? ''
    form.config = { ...(props.pool?.config ?? {}) }
  },
  { immediate: true },
)

function request(): { name?: string; driver?: string; description?: string; config?: Record<string, string> } {
  const payload = {
    description: form.description.trim(),
    config: Object.keys(form.config).length > 0 ? { ...form.config } : undefined,
  }

  if (isEdit.value) {
    return payload
  }

  return { ...payload, name: form.name.trim(), driver: form.driver }
}

function validate(): string {
  if (!isEdit.value) {
    if (!form.name.trim()) {
      return t('admin.validationName')
    }
    if (!form.driver) {
      return t('admin.validationDriver')
    }
  }

  return ''
}

async function submit(): Promise<void> {
  const validation = validate()
  if (validation) {
    ElMessage.error(validation)
    return
  }

  const name = isEdit.value ? (props.pool?.name ?? '') : form.name.trim()
  try {
    if (isEdit.value) {
      await instances.updateStoragePool(props.cluster, name, request())
    } else {
      await instances.createStoragePool(props.cluster, request())
    }

    ElMessage.success(t('admin.poolSaved', { name }))
    emit('saved', name)
    visible.value = false
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="title" width="560px" destroy-on-close @submit.prevent="submit">
    <el-form label-position="top">
      <el-form-item :label="t('cluster.poolName')">
        <el-input v-model="form.name" :disabled="isEdit" />
      </el-form-item>

      <el-form-item :label="t('cluster.driver')">
        <el-select
          v-model="form.driver"
          :disabled="isEdit"
          filterable
          style="width: 100%"
        >
          <el-option v-for="driver in drivers" :key="driver" :label="driver" :value="driver" />
        </el-select>
      </el-form-item>

      <el-form-item :label="t('admin.description')">
        <el-input v-model="form.description" />
      </el-form-item>

      <el-form-item :label="t('admin.config')">
        <KeyValueEditor
          v-model="form.config"
          :key-placeholder="t('admin.keyPlaceholder')"
          :value-placeholder="t('admin.valuePlaceholder')"
          :add-label="t('admin.addRow')"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="instances.saving" @click="submit">
        {{ isEdit ? t('common.save') : t('common.create') }}
      </el-button>
    </template>
  </el-dialog>
</template>

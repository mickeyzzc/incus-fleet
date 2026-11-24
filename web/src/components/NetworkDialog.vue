<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import KeyValueEditor from '@/components/KeyValueEditor.vue'
import type { NetworkResource } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    cluster: string
    network?: NetworkResource | null
  }>(),
  {
    network: null,
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
  type: '',
  description: '',
  config: {} as Record<string, string>,
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const isEdit = computed(() => Boolean(props.network))
const title = computed(() => (isEdit.value ? t('admin.editNetwork') : t('admin.addNetwork')))
const types = ['bridge', 'ovn', 'macvlan', 'sriov', 'physical']

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      return
    }

    form.name = props.network?.name ?? ''
    form.type = props.network?.type ?? ''
    form.description = props.network?.description ?? ''
    form.config = { ...(props.network?.config ?? {}) }
  },
  { immediate: true },
)

function request(): { name?: string; type?: string; description?: string; config?: Record<string, string> } {
  const payload = {
    description: form.description.trim(),
    config: Object.keys(form.config).length > 0 ? { ...form.config } : undefined,
  }

  if (isEdit.value) {
    return payload
  }

  return { ...payload, name: form.name.trim(), type: form.type }
}

function validate(): string {
  if (!isEdit.value) {
    if (!form.name.trim()) {
      return t('admin.validationName')
    }
    if (!form.type) {
      return t('admin.validationType')
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

  const name = isEdit.value ? (props.network?.name ?? '') : form.name.trim()
  try {
    if (isEdit.value) {
      await instances.updateNetwork(props.cluster, name, request())
    } else {
      await instances.createNetwork(props.cluster, request())
    }

    ElMessage.success(t('admin.networkSaved', { name }))
    emit('saved', name)
    visible.value = false
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="title" width="560px" destroy-on-close>
    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('cluster.networkName')">
        <el-input v-model="form.name" :disabled="isEdit" />
      </el-form-item>

      <el-form-item :label="t('cluster.networkType')">
        <el-select
          v-model="form.type"
          :disabled="isEdit"
          filterable
          style="width: 100%"
        >
          <el-option v-for="type in types" :key="type" :label="type" :value="type" />
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

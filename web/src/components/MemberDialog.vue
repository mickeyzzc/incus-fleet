<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import type { ClusterMember } from '@/api/types'
import { useClustersStore } from '@/stores/clusters'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  member: ClusterMember | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: [name: string]
}>()

const clusters = useClustersStore()
const instances = useInstancesStore()
const { t } = useI18n()

const form = reactive({
  description: '',
  failureDomain: '',
  groups: '',
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      return
    }

    form.description = props.member?.description ?? ''
    form.failureDomain = props.member?.failure_domain ?? ''
    form.groups = (props.member?.groups ?? []).join(', ')
  },
  { immediate: true },
)

async function submit(): Promise<void> {
  if (!props.member) {
    return
  }

  const request: { description?: string; failure_domain?: string; groups?: string[] } = {
    description: form.description,
  }

  if (form.failureDomain.trim()) {
    request.failure_domain = form.failureDomain.trim()
  }

  const groups = form.groups
    .split(',')
    .map((group) => group.trim())
    .filter(Boolean)
  if (groups.length > 0) {
    request.groups = groups
  }

  try {
    await instances.updateMember(props.cluster, props.member.name, request)
    await clusters.fetchAll()
    ElMessage.success(t('admin.memberSaved', { name: props.member.name }))
    emit('saved', props.member.name)
    visible.value = false
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`${t('admin.editMember')} · ${member?.name ?? ''}`"
    width="480px"
    destroy-on-close
  >
    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('admin.description')">
        <el-input v-model="form.description" />
      </el-form-item>

      <el-form-item :label="t('admin.failureDomain')">
        <el-input v-model="form.failureDomain" />
      </el-form-item>

      <el-form-item :label="t('admin.groups')">
        <el-input v-model="form.groups" :placeholder="t('admin.groupsHint')" />
        <p class="field-hint">{{ t('admin.groupsHint') }}</p>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="instances.saving" @click="submit">
        {{ t('common.save') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}
</style>

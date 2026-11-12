<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import type { ClusterSummary } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  fingerprint: string
  clusters: ClusterSummary[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  distributed: []
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const form = reactive({
  target: '',
  alias: '',
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const targets = computed(() =>
  props.clusters.filter((cluster) => cluster.online && cluster.name !== props.cluster),
)

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      form.target = targets.value[0]?.name ?? ''
      form.alias = ''
    }
  },
  { immediate: true },
)

async function submit(): Promise<void> {
  if (!form.target) {
    ElMessage.error(t('migrate.targetRequired'))
    return
  }

  try {
    await instances.copyImage(props.cluster, props.fingerprint, {
      target_cluster: form.target,
      alias: form.alias.trim(),
    })
    ElMessage.success(t('images.distributed', { cluster: form.target }))
    emit('distributed')
    visible.value = false
  } catch (err) {
    ElMessage.error(t('images.failed', { error: errorMessage(err) }))
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="t('images.distributeTitle')"
    width="440px"
    destroy-on-close
  >
    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('images.distributeTo')">
        <el-select v-model="form.target" style="width: 100%">
          <el-option
            v-for="target in targets"
            :key="target.name"
            :label="target.display_name"
            :value="target.name"
          />
        </el-select>
        <p class="field-hint">{{ t('images.distributeHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('images.alias')">
        <el-input v-model="form.alias" :placeholder="t('images.aliasPlaceholder')" clearable />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="instances.saving" @click="submit">
        {{ t('images.distribute') }}
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

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  instance: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  cloned: [name: string]
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const form = reactive({
  target: '',
  start: false,
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      form.target = `${props.instance}-clone`
      form.start = false
    }
  },
  { immediate: true },
)

async function submit(): Promise<void> {
  if (!form.target.trim()) {
    ElMessage.error(t('clone.nameRequired'))
    return
  }

  try {
    await instances.cloneInstance(props.cluster, props.instance, {
      target: form.target.trim(),
      start: form.start,
    })
    ElMessage.success(t('clone.succeeded', { name: form.target.trim() }))
    emit('cloned', form.target.trim())
    visible.value = false
  } catch (err) {
    ElMessage.error(t('clone.failed', { error: errorMessage(err) }))
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`${t('clone.title')} · ${instance}`"
    width="440px"
    destroy-on-close
  >
    <p class="clone-hint">{{ t('clone.hint') }}</p>

    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('clone.target')">
        <el-input v-model="form.target" placeholder="web-clone" />
      </el-form-item>

      <el-form-item>
        <el-checkbox v-model="form.start">{{ t('clone.start') }}</el-checkbox>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="instances.saving" @click="submit">
        {{ t('clone.confirm') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.clone-hint {
  margin-bottom: 12px;
  color: var(--fleet-ink-soft);
  font-size: 13px;
}
</style>

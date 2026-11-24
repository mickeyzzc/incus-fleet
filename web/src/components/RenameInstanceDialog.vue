<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import { api } from '@/api'
import type { InstanceDetail } from '@/api/types'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  instance: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  renamed: [detail: InstanceDetail]
}>()

const { t } = useI18n()
const name = ref('')
const saving = ref(false)

const visible = ref(false)

watch(
  () => props.modelValue,
  (open) => {
    visible.value = open
    if (open) {
      name.value = ''
    }
  },
  { immediate: true },
)

watch(visible, (open) => {
  emit('update:modelValue', open)
})

async function submit(): Promise<void> {
  const next = name.value.trim()
  if (!next) {
    ElMessage.error(t('rename.required'))
    return
  }
  if (next === props.instance) {
    ElMessage.error(t('rename.same'))
    return
  }

  saving.value = true
  try {
    const detail = await api.renameInstance(props.cluster, props.instance, next)
    ElMessage.success(t('rename.done', { name: next }))
    emit('renamed', detail)
    visible.value = false
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="t('rename.title')" width="460px" destroy-on-close>
    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('rename.newName')">
        <el-input v-model="name" :placeholder="instance" @keyup.enter="submit" />
        <p class="field-hint">{{ t('rename.hint') }}</p>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="saving" :disabled="!name.trim()" @click="submit">
        {{ t('common.save') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.field-hint {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>

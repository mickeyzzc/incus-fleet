<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { EditPen } from '@element-plus/icons-vue'

import { useClustersStore } from '@/stores/clusters'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  currentName: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  renamed: [name: string]
}>()

const clusters = useClustersStore()
const { t } = useI18n()

const form = reactive({ name: '' })

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const trimmed = computed(() => form.name.trim())
const valid = computed(() => trimmed.value.length > 0 && trimmed.value.length <= 64)
const changed = computed(() => trimmed.value !== props.currentName)

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      form.name = props.currentName
    }
  },
  { immediate: true },
)

async function submit(): Promise<void> {
  if (!valid.value || !changed.value) {
    return
  }

  try {
    await clusters.rename(props.cluster, trimmed.value)
    ElMessage.success(t('dashboard.renameClusterSaved', { name: trimmed.value }))
    emit('renamed', trimmed.value)
    visible.value = false
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="t('dashboard.renameClusterTitle')" width="460px" destroy-on-close>
    <div class="rename-intro">
      <span class="rename-icon"><el-icon><EditPen /></el-icon></span>
      <div>
        <p>{{ t('dashboard.renameClusterHint', { name: cluster }) }}</p>
        <span class="rename-id mono">{{ cluster }}</span>
      </div>
    </div>

    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('clusterRegister.displayName')">
        <el-input
          v-model="form.name"
          :placeholder="t('dashboard.renameClusterPlaceholder')"
          maxlength="64"
          show-word-limit
          autofocus
          @keyup.enter="submit"
        />
        <p v-if="!valid" class="field-hint">{{ t('dashboard.renameClusterInvalid') }}</p>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button
        type="primary"
        :loading="clusters.renaming === cluster"
        :disabled="!valid || !changed"
        @click="submit"
      >
        {{ t('common.save') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.rename-intro {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  margin-bottom: 18px;
  padding: 12px 14px;
  border: 1px solid var(--fleet-line);
  border-radius: 12px;
  background: var(--fleet-panel-muted);
}

.rename-icon {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  flex: none;
  border-radius: 10px;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}

.rename-intro p {
  color: var(--fleet-ink-soft);
  font-size: 12.5px;
  line-height: 1.55;
}

.rename-id {
  display: inline-block;
  margin-top: 4px;
  color: var(--fleet-ink);
  font-size: 11.5px;
}
</style>

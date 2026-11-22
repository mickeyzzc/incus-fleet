<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  target: { cluster: string; name: string } | null
  busy: boolean
}>()

const emit = defineEmits<{
  cancel: []
  confirm: [volumes: boolean]
}>()

const { t } = useI18n()

const withVolumes = ref(false)

watch(
  () => props.target,
  (target) => {
    if (target) {
      withVolumes.value = false
    }
  },
)

function onVisibleChange(value: boolean): void {
  if (!value) {
    emit('cancel')
  }
}

function submit(): void {
  emit('confirm', withVolumes.value)
}
</script>

<template>
  <el-dialog
    :model-value="!!target"
    :title="t('instances.deleteTitle')"
    width="440px"
    @update:model-value="onVisibleChange"
  >
    <p v-if="target">{{ t('instances.deleteConfirm', { name: target.name }) }}</p>
    <el-checkbox v-model="withVolumes">
      {{ t('instances.deleteVolumes') }}
    </el-checkbox>
    <p class="hint">{{ t('instances.deleteVolumesHint') }}</p>
    <template #footer>
      <el-button @click="emit('cancel')">{{ t('common.cancel') }}</el-button>
      <el-button type="danger" :loading="busy" @click="submit">
        {{ t('common.delete') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.hint {
  margin: 8px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>

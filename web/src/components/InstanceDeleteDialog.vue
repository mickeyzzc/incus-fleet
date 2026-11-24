<script setup lang="ts">
import { computed, ref, watch } from 'vue'
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
const typed = ref('')
const canDelete = computed(
  () => Boolean(props.target) && typed.value.trim() === props.target?.name,
)

watch(
  () => props.target,
  (target) => {
    if (target) {
      withVolumes.value = false
      typed.value = ''
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
    <el-form label-position="top" class="typed-confirm" @submit.prevent="submit">
      <el-form-item :label="t('instances.typeToConfirm', { name: target?.name ?? '' })">
        <el-input v-model="typed" :placeholder="target?.name" autocomplete="off" />
      </el-form-item>
    </el-form>
    <el-checkbox v-model="withVolumes">
      {{ t('instances.deleteVolumes') }}
    </el-checkbox>
    <p class="hint">{{ t('instances.deleteVolumesHint') }}</p>
    <template #footer>
      <el-button @click="emit('cancel')">{{ t('common.cancel') }}</el-button>
      <el-button type="danger" :loading="busy" :disabled="!canDelete" @click="submit">
        {{ t('common.delete') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.typed-confirm {
  margin-top: 12px;
}

.hint {
  margin: 8px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>

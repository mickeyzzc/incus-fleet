<script setup lang="ts">
import { computed } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(
  defineProps<{
    value: string
    display?: string
    mono?: boolean
    label?: string
  }>(),
  {
    display: '',
    mono: true,
    label: '',
  },
)

const { t } = useI18n()

const text = computed(() => props.display || props.value)

async function copy(): Promise<void> {
  try {
    await navigator.clipboard.writeText(props.value)
    ElMessage.success(t('common.copied'))
  } catch {
    ElMessage.error(t('common.copyFailed'))
  }
}
</script>

<template>
  <span class="copyable-text" :title="value">
    <span :class="{ mono }" class="copyable-value">{{ text }}</span>
    <el-button
      class="copyable-button"
      link
      :icon="CopyDocument"
      :aria-label="label || t('common.copy')"
      @click.stop="copy"
    />
  </span>
</template>

<style scoped>
.copyable-text {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
}

.copyable-value {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mono {
  font-family: var(--fleet-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace);
  font-size: 12px;
}

.copyable-button {
  flex: none;
  padding: 0;
  height: auto;
}
</style>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { formatBytes, formatPercent } from '@/utils/format'

const props = defineProps<{
  used: number
  total: number
  pool?: string
}>()

const { t } = useI18n()

const percentage = computed(() => {
  if (!Number.isFinite(props.total) || props.total <= 0) {
    return 0
  }

  return Math.min(100, Math.max(0, (props.used / props.total) * 100))
})

const status = computed<'success' | 'warning' | 'exception'>(() => {
  if (percentage.value >= 90) {
    return 'exception'
  }
  if (percentage.value >= 75) {
    return 'warning'
  }

  return 'success'
})

const label = computed(() =>
  props.pool ? t('storage.label', { pool: props.pool }) : t('storage.labelGeneric'),
)
</script>

<template>
  <div class="storage-line">
    <div class="storage-label">
      <span>{{ label }}</span>
      <span>{{ formatBytes(used) }} / {{ formatBytes(total) }} ({{ formatPercent(used, total) }})</span>
    </div>
    <el-progress
      :percentage="Number(percentage.toFixed(1))"
      :status="status"
      :show-text="false"
      :stroke-width="8"
      :aria-label="`${label}: ${formatBytes(used)} / ${formatBytes(total)}`"
    />
  </div>
</template>

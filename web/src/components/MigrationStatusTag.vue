<script setup lang="ts">
import { computed } from 'vue'

import type { MigrationStatus } from '@/api/types'

type TagType = 'primary' | 'success' | 'info' | 'warning' | 'danger'

const props = defineProps<{
  status: MigrationStatus | string
}>()

const TYPES: Record<string, TagType> = {
  pending: 'info',
  preflight: 'info',
  stopping: 'warning',
  transferring: 'primary',
  starting: 'warning',
  verifying: 'warning',
  done: 'success',
  failed: 'danger',
}

const LABELS: Record<string, string> = {
  pending: 'Queued',
  preflight: 'Preflight',
  stopping: 'Stopping source',
  transferring: 'Transferring',
  starting: 'Starting',
  verifying: 'Verifying',
  done: 'Done',
  failed: 'Failed',
}

const tagType = computed<TagType>(() => TYPES[props.status] ?? 'info')
const label = computed(() => LABELS[props.status] ?? props.status)
</script>

<template>
  <el-tag :type="tagType" effect="light" round disable-transitions>{{ label }}</el-tag>
</template>

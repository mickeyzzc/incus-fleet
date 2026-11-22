<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type { MigrationStatus } from '@/api/types'

type TagType = 'primary' | 'success' | 'info' | 'warning' | 'danger'

const props = defineProps<{
  status: MigrationStatus | string
}>()

const { t, te } = useI18n()

const TYPES: Record<string, TagType> = {
  pending: 'info',
  preflight: 'info',
  stopping: 'warning',
  transferring: 'primary',
  starting: 'warning',
  verifying: 'warning',
  done: 'success',
  failed: 'danger',
  canceled: 'info',
}

const tagType = computed<TagType>(() => TYPES[props.status] ?? 'info')
const label = computed(() => {
  const key = `migrationStatus.${props.status}`
  return te(key) ? t(key) : props.status
})
</script>

<template>
  <el-tag :type="tagType" effect="light" round disable-transitions>{{ label }}</el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

type TagType = 'primary' | 'success' | 'info' | 'warning' | 'danger'

const props = defineProps<{
  status: string
  statusCode?: number
}>()

const { t, te } = useI18n()

const normalized = computed(() => (props.status || 'unknown').toLowerCase())

const tagType = computed<TagType>(() => {
  switch (normalized.value) {
    case 'running':
    case 'ready':
      return 'success'
    case 'frozen':
    case 'starting':
    case 'stopping':
    case 'freezing':
      return 'warning'
    case 'error':
    case 'failed':
      return 'danger'
    case 'stopped':
    case 'shutdown':
      return 'info'
    default:
      return 'primary'
  }
})

const label = computed(() => {
  const key = `status.${normalized.value}`
  if (te(key)) {
    return t(key)
  }

  return normalized.value.charAt(0).toUpperCase() + normalized.value.slice(1)
})
</script>

<template>
  <el-tag :type="tagType" effect="light" round class="status-tag" disable-transitions>
    {{ label }}
  </el-tag>
</template>

<style scoped>
.status-tag {
  font-weight: 600;
  letter-spacing: 0.2px;
}
</style>

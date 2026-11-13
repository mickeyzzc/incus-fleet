<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    values: number[]
    width?: number
    height?: number
    capacity?: number
  }>(),
  { width: 240, height: 48, capacity: 60 },
)

// Keep the newest `capacity` samples so the line scrolls instead of squeezing.
const window = computed(() => props.values.slice(-props.capacity))

const points = computed(() => {
  const data = window.value
  if (data.length === 0) {
    return ''
  }

  const max = Math.max(...data)
  const min = Math.min(...data)
  const span = max - min
  const step = data.length > 1 ? props.width / (data.length - 1) : props.width
  return data
    .map((value, index) => {
      const x = data.length > 1 ? (index * step).toFixed(2) : '0'
      // Equal values pin to the baseline; the general case scales top-down.
      const y = span === 0 ? props.height : props.height - ((value - min) / span) * props.height
      return `${x},${y.toFixed(2)}`
    })
    .join(' ')
})
</script>

<template>
  <svg
    :width="width"
    :height="height"
    :viewBox="`0 0 ${width} ${height}`"
    class="sparkline"
    preserveAspectRatio="none"
    aria-hidden="true"
  >
    <polyline
      v-if="points"
      :points="points"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linejoin="round"
      stroke-linecap="round"
    />
  </svg>
  <span v-if="!points" class="sparkline-empty" />
</template>

<style scoped>
.sparkline {
  display: block;
  color: var(--el-color-primary);
}
</style>

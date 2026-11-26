<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import type { TelemetrySeries } from '@/api/types'
import { api } from '@/api'
import { errorMessage } from '@/utils/errors'
import { formatBytes } from '@/utils/format'
import Sparkline from './Sparkline.vue'

const props = defineProps<{ cluster: string }>()
const { t } = useI18n()

const POLL_MS = 30000
const series = ref<TelemetrySeries[]>([])
const error = ref('')
const loaded = ref(false)
let timer: ReturnType<typeof setInterval> | null = null

function findSeries(name: string): TelemetrySeries | undefined {
  return series.value.find((item) => item.name === name)
}

function latest(name: string): number {
  return findSeries(name)?.points.at(-1)?.value ?? 0
}

function values(name: string): number[] {
  return (findSeries(name)?.points ?? []).map((point) => point.value)
}

const memoryUsed = computed(() => latest('fleet_cluster_memory_used_bytes'))
const memoryTotal = computed(() => latest('fleet_cluster_memory_total_bytes'))
const storageUsed = computed(() => latest('fleet_cluster_storage_used_bytes'))
const storageTotal = computed(() => latest('fleet_cluster_storage_total_bytes'))
const hasData = computed(() => series.value.some((item) => (item.points?.length ?? 0) > 0))

async function refresh(): Promise<void> {
  try {
    const selector = `{cluster="${props.cluster}",__name__=~"fleet_cluster_memory_used_bytes|fleet_cluster_memory_total_bytes|fleet_cluster_storage_used_bytes|fleet_cluster_storage_total_bytes"}`
    series.value = await api.metricQueryRange(selector, '1h', '30s')
    error.value = ''
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loaded.value = true
  }
}

onMounted(() => {
  void refresh()
  timer = setInterval(() => {
    if (document.visibilityState === 'visible') {
      void refresh()
    }
  }, POLL_MS)
})

onBeforeUnmount(() => {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
})
</script>

<template>
  <section v-if="hasData" class="panel trend-panel">
    <div class="panel-head">
      <div>
        <span class="panel-eyebrow">{{ t('cluster.trendEyebrow') }}</span>
        <h3 class="section-title">{{ t('cluster.trendTitle') }}</h3>
        <p class="subtitle">{{ t('cluster.trendHint') }}</p>
      </div>
    </div>
    <div class="panel-body trend-grid">
      <article class="trend-card">
        <div class="trend-copy">
          <span class="trend-label">{{ t('cluster.memoryTotal') }}</span>
          <strong>{{ formatBytes(memoryUsed) }} <small>/ {{ formatBytes(memoryTotal) }}</small></strong>
        </div>
        <Sparkline :values="values('fleet_cluster_memory_used_bytes')" :width="260" :height="46" />
      </article>
      <article class="trend-card">
        <div class="trend-copy">
          <span class="trend-label">{{ t('cluster.storageTotal') }}</span>
          <strong>{{ formatBytes(storageUsed) }} <small>/ {{ formatBytes(storageTotal) }}</small></strong>
        </div>
        <Sparkline :values="values('fleet_cluster_storage_used_bytes')" :width="260" :height="46" />
      </article>
    </div>
  </section>
  <p v-else-if="loaded && error" class="trend-error">{{ error }}</p>
</template>

<style scoped>
.trend-panel { margin-top: 16px; }
.trend-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
.trend-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 14px;
  border: 1px solid var(--el-border-color-lighter, #e5e7eb);
  border-radius: 10px;
}
.trend-copy { display: flex; flex-direction: column; gap: 4px; }
.trend-label { color: var(--fleet-ink-soft); font-size: 12px; }
.trend-copy strong { font-size: 18px; font-variant-numeric: tabular-nums; }
.trend-copy small { color: var(--fleet-ink-soft); font-size: 12px; font-weight: 400; }
.trend-error { margin-top: 12px; color: var(--el-color-warning); font-size: 12px; }
@media (max-width: 900px) {
  .trend-grid { grid-template-columns: 1fr; }
}
</style>

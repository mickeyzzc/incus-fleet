<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import type { TelemetrySeries } from '@/api/types'
import { api } from '@/api'
import { errorMessage } from '@/utils/errors'
import { formatBytes, formatDateTime } from '@/utils/format'
import Sparkline from './Sparkline.vue'

const props = defineProps<{
  cluster: string
  member: string
}>()

const { t } = useI18n()
const REFRESH_MS = 10000

const source = ref<'local' | 'external'>('local')
const series = ref<TelemetrySeries[]>([])
const loading = ref(false)
const error = ref('')
let timer: ReturnType<typeof setInterval> | null = null

function findSeries(name: string, labels: Record<string, string> = {}): TelemetrySeries | undefined {
  return series.value.find((item) =>
    item.name === name && Object.entries(labels).every(([key, value]) => item.labels[key] === value),
  )
}

function latest(name: string, labels: Record<string, string> = {}): number | null {
  const item = findSeries(name, labels)
  const point = item?.points.at(-1)
  return point ? point.value : null
}

function history(name: string, labels: Record<string, string> = {}, scale = 1): number[] {
  const item = findSeries(name, labels)
  return (item?.points ?? []).map((point) => point.value * scale)
}

const cpuHistory = computed(() => history('fleet_host_cpu_usage_ratio', {}, 100))
const cpuPercent = computed(() => latest('fleet_host_cpu_usage_ratio') !== null
  ? (latest('fleet_host_cpu_usage_ratio') as number) * 100
  : null)
const loadHistory = computed(() => history('fleet_host_load', { window: '1' }))
const load1 = computed(() => latest('fleet_host_load', { window: '1' }))
const memoryTotal = computed(() => latest('fleet_host_memory_bytes', { state: 'total' }) ?? 0)
const memoryUsed = computed(() => {
  const used = latest('fleet_host_memory_bytes', { state: 'used' })
  if (used !== null) {
    return used
  }
  const available = latest('fleet_host_memory_bytes', { state: 'available' })
  return memoryTotal.value > 0 && available !== null ? memoryTotal.value - available : 0
})
const memoryPercent = computed(() => memoryTotal.value > 0 ? (memoryUsed.value / memoryTotal.value) * 100 : 0)
const uptime = computed(() => latest('fleet_host_uptime_seconds'))
const lastSample = computed(() => {
  const points = (series.value ?? []).flatMap((item) => item.points)
  const latestPoint = points.sort((a, b) => a.at.localeCompare(b.at)).at(-1)
  return latestPoint?.at ?? ''
})

function uptimeLabel(seconds: number | null): string {
  if (seconds === null) {
    return '—'
  }
  const total = Math.floor(seconds)
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  return days > 0 ? `${days}d ${hours}h` : `${hours}h ${minutes}m`
}

async function refresh(): Promise<void> {
  loading.value = true
  try {
    if (source.value === 'external') {
      const selector = `{cluster="${props.cluster}",member="${props.member}",__name__=~"fleet_host_cpu_usage_ratio|fleet_host_memory_bytes|fleet_host_load|fleet_host_uptime_seconds"}`
      series.value = await api.metricQueryRange(selector, '1h', '30s')
    } else {
      series.value = (await api.hostMetrics(props.cluster, props.member)) ?? []
    }
    error.value = ''
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void refresh()
  timer = setInterval(() => {
    void refresh()
  }, REFRESH_MS)
})

onBeforeUnmount(() => {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
})

watch([() => props.cluster, () => props.member, source], () => {
  series.value = []
  void refresh()
})
</script>

<template>
  <section class="panel">
    <div class="panel-body">
      <div class="section-heading page-header">
        <div>
          <h3 class="section-title">{{ t('hostMetrics.title') }}</h3>
          <p class="subtitle">{{ t('hostMetrics.subtitle', { host: member }) }}</p>
        </div>
        <div class="page-actions">
          <label class="source-select">
            {{ t('hostMetrics.source') }}
            <select v-model="source">
              <option value="local">{{ t('hostMetrics.local') }}</option>
              <option value="external">{{ t('hostMetrics.external') }}</option>
            </select>
          </label>
          <span class="metric-time">{{ lastSample ? formatDateTime(lastSample) : t('common.none') }}</span>
          <el-button :loading="loading" @click="refresh">{{ t('common.refresh') }}</el-button>
        </div>
      </div>

      <el-alert v-if="error" :title="t('hostMetrics.failed', { error })" type="error" :closable="false" show-icon />
      <p v-else-if="loading && series.length === 0" class="metrics-empty">{{ t('hostMetrics.loading') }}</p>
      <p v-else-if="series.length === 0" class="metrics-empty">{{ t('hostMetrics.empty') }}</p>

      <template v-else>
        <div class="host-metric-grid">
          <div class="metric-block">
            <div class="metric-head">
              <h5>{{ t('hostMetrics.cpu') }}</h5>
              <span class="metric-value">{{ cpuPercent === null ? '—' : `${cpuPercent.toFixed(1)}%` }}</span>
            </div>
            <Sparkline :values="cpuHistory" :width="320" :height="40" />
          </div>
          <div class="metric-block">
            <div class="metric-head">
              <h5>{{ t('hostMetrics.load') }}</h5>
              <span class="metric-value">{{ load1 === null ? '—' : load1.toFixed(2) }}</span>
            </div>
            <Sparkline :values="loadHistory" :width="320" :height="40" />
          </div>
          <div class="metric-block">
            <div class="metric-head">
              <h5>{{ t('hostMetrics.memory') }}</h5>
              <span class="metric-value">{{ formatBytes(memoryUsed) }} / {{ formatBytes(memoryTotal) }}</span>
            </div>
            <el-progress :percentage="memoryPercent" :stroke-width="10" :show-text="false" />
          </div>
          <div class="metric-block">
            <div class="metric-head">
              <h5>{{ t('hostMetrics.uptime') }}</h5>
              <span class="metric-value">{{ uptimeLabel(uptime) }}</span>
            </div>
            <p class="metric-hint">{{ t('hostMetrics.retention') }}</p>
          </div>
        </div>
      </template>
    </div>
  </section>
</template>

<style scoped>
.host-metric-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 18px;
}

.metric-block {
  min-width: 0;
}

.metric-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 6px;
}

.metric-head h5 {
  margin: 0;
  font-size: 13px;
}

.metric-value {
  font-variant-numeric: tabular-nums;
  font-size: 13px;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
}

.metric-time {
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  font-size: 12px;
}

.source-select {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  font-size: 12px;
}

.source-select select {
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  background: transparent;
  color: inherit;
  padding: 3px 6px;
  font: inherit;
}

.metrics-empty {
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  font-size: 13px;
}

.metric-hint {
  margin: 4px 0 0;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  font-size: 12px;
}
</style>

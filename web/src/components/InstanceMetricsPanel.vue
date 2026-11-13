<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import type { InstanceMetrics } from '@/api/types'
import { api } from '@/api'
import { errorMessage } from '@/utils/errors'
import { formatBytes } from '@/utils/format'
import Sparkline from './Sparkline.vue'

const props = defineProps<{
  cluster: string
  instance: string
}>()

const { t } = useI18n()

const POLL_MS = 3000
const HISTORY = 60

const metrics = ref<InstanceMetrics | null>(null)
const previous = ref<InstanceMetrics | null>(null)
const error = ref('')
const loading = ref(false)

// History rings feed the sparklines: CPU percent and per-interface rates.
const cpuHistory = ref<number[]>([])
const rxHistory = ref<number[]>([])
const txHistory = ref<number[]>([])

let timer: ReturnType<typeof setInterval> | null = null

const cpuPercent = computed(() => {
  const current = metrics.value
  const last = previous.value
  if (!current || !last) {
    return cpuHistory.value.at(-1) ?? null
  }

  const dtNs = (Date.parse(current.at) - Date.parse(last.at)) * 1_000_000
  if (dtNs <= 0) {
    return null
  }

  return ((current.cpu.usage - last.cpu.usage) / dtNs) * 100
})

const memoryPercent = computed(() => {
  const memory = metrics.value?.memory
  if (!memory || memory.total <= 0) {
    return null
  }

  return (memory.usage / memory.total) * 100
})

interface NetRate {
  name: string
  rx: number
  tx: number
}

const netRates = computed<NetRate[]>(() => {
  const current = metrics.value
  const last = previous.value
  if (!current || !last) {
    return []
  }

  const seconds = (Date.parse(current.at) - Date.parse(last.at)) / 1000
  if (seconds <= 0) {
    return []
  }

  return current.network.map((net) => {
    const before = last.network.find((item) => item.interface === net.interface)
    return {
      name: net.interface,
      rx: before ? Math.max(0, (net.rx_bytes - before.rx_bytes) / seconds) : 0,
      tx: before ? Math.max(0, (net.tx_bytes - before.tx_bytes) / seconds) : 0,
    }
  })
})

async function poll(): Promise<void> {
  loading.value = true
  try {
    const sample = await api.instanceMetrics(props.cluster, props.instance)
    previous.value = metrics.value
    metrics.value = sample

    if (cpuPercent.value !== null) {
      cpuHistory.value = [...cpuHistory.value, cpuPercent.value].slice(-HISTORY)
    }
    if (netRates.value.length > 0) {
      rxHistory.value = [...rxHistory.value, netRates.value.reduce((sum, rate) => sum + rate.rx, 0)].slice(-HISTORY)
      txHistory.value = [...txHistory.value, netRates.value.reduce((sum, rate) => sum + rate.tx, 0)].slice(-HISTORY)
    }
    error.value = ''
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void poll()
  timer = setInterval(() => {
    void poll()
  }, POLL_MS)
})

onBeforeUnmount(() => {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
})

/** Renders the current rate for one interface direction. */
function rateFor(name: string, direction: 'rx' | 'tx'): string {
  const rate = netRates.value.find((item) => item.name === name)
  if (!rate) {
    return '—'
  }

  return `${formatBytes(rate[direction])}/s`
}

/** Incus reports usage -1 when the driver cannot measure a filesystem. */
function diskUsageLabel(disk: { usage: number; total: number }): string {
  if (disk.usage < 0) {
    return t('metrics.diskUnknown')
  }

  return `${formatBytes(disk.usage)} / ${formatBytes(disk.total)}`
}
</script>

<template>
  <div class="metrics-panel">
    <el-alert v-if="error" :title="t('metrics.failed', { error })" type="error" :closable="false" show-icon />
    <p v-else-if="!metrics" v-loading="loading" class="metrics-empty">
      {{ t('metrics.loading') }}
    </p>

    <template v-else>
      <div class="metrics-row">
        <span class="metric-pill">
          {{ t('metrics.status') }}: <strong>{{ metrics.status }}</strong>
        </span>
        <span class="metric-pill">
          {{ t('metrics.processes') }}: <strong>{{ metrics.processes }}</strong>
        </span>
        <span class="metric-pill">PID: <strong>{{ metrics.pid || '—' }}</strong></span>
      </div>

      <div class="metric-block">
        <div class="metric-head">
          <h5>{{ t('metrics.cpu') }}</h5>
          <span v-if="cpuPercent !== null" class="metric-value">{{ cpuPercent.toFixed(1) }}%</span>
          <span v-else class="metric-value">—</span>
        </div>
        <Sparkline :values="cpuHistory" :width="480" :height="40" />
        <p class="metric-hint">{{ t('metrics.cpuHint') }}</p>
      </div>

      <div class="metric-block">
        <div class="metric-head">
          <h5>{{ t('metrics.memory') }}</h5>
          <span v-if="memoryPercent !== null" class="metric-value">{{ memoryPercent.toFixed(1) }}%</span>
          <span v-else class="metric-value">—</span>
        </div>
        <el-progress
          :percentage="memoryPercent ?? 0"
          :stroke-width="10"
          :format="() => `${formatBytes(metrics?.memory.usage ?? 0)} / ${formatBytes(metrics?.memory.total ?? 0)}`"
        />
        <p class="metric-hint">
          {{ t('metrics.peak') }}: {{ formatBytes(metrics.memory.usage_peak) }}
          <template v-if="metrics.memory.swap_usage > 0">
            · {{ t('metrics.swap') }}: {{ formatBytes(metrics.memory.swap_usage) }}
          </template>
        </p>
      </div>

      <div v-if="metrics.disk.length > 0" class="metric-block">
        <h5>{{ t('metrics.disk') }}</h5>
        <div v-for="disk in metrics.disk" :key="disk.mountpoint" class="disk-row">
          <span class="disk-path">{{ disk.mountpoint }}</span>
          <el-progress
            class="disk-bar"
            :percentage="disk.usage >= 0 && disk.total > 0 ? Math.min(100, (disk.usage / disk.total) * 100) : 0"
            :stroke-width="8"
            :format="() => diskUsageLabel(disk)"
          />
        </div>
      </div>

      <div v-if="metrics.network.length > 0" class="metric-block">
        <div class="metric-head">
          <h5>{{ t('metrics.network') }}</h5>
        </div>
        <el-table :data="metrics.network" size="small" stripe>
          <el-table-column prop="interface" :label="t('metrics.interface')" min-width="90" />
          <el-table-column :label="t('metrics.rxRate')" min-width="110">
            <template #default="{ row }">
              {{ rateFor(row.interface, 'rx') }}
            </template>
          </el-table-column>
          <el-table-column :label="t('metrics.txRate')" min-width="110">
            <template #default="{ row }">
              {{ rateFor(row.interface, 'tx') }}
            </template>
          </el-table-column>
          <el-table-column :label="t('metrics.rxTotal')" min-width="100">
            <template #default="{ row }">
              {{ formatBytes(row.rx_bytes) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('metrics.txTotal')" min-width="100">
            <template #default="{ row }">
              {{ formatBytes(row.tx_bytes) }}
            </template>
          </el-table-column>
        </el-table>
        <div class="net-charts">
          <div>
            <p class="metric-hint">{{ t('metrics.rxRate') }}</p>
            <Sparkline :values="rxHistory" :width="220" :height="36" />
          </div>
          <div>
            <p class="metric-hint">{{ t('metrics.txRate') }}</p>
            <Sparkline :values="txHistory" :width="220" :height="36" />
          </div>
        </div>
      </div>

      <p class="metric-hint">{{ t('metrics.autoRefresh') }}</p>
    </template>
  </div>
</template>

<style scoped>
.metrics-panel {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.metrics-empty {
  min-height: 120px;
  color: var(--fleet-ink-soft);
}

.metrics-row {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.metric-pill {
  padding: 2px 10px;
  background: var(--el-fill-color-light);
  border-radius: 10px;
  font-size: 12px;
}

.metric-block h5 {
  margin: 0 0 6px;
  font-size: 13px;
}

.metric-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
}

.metric-value {
  font-size: 13px;
  font-weight: 600;
}

.metric-hint {
  margin: 4px 0 0;
  color: var(--fleet-ink-soft);
  font-size: 12px;
}

.disk-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}

.disk-path {
  min-width: 110px;
  font-size: 12px;
  font-family: var(--fleet-mono, monospace);
}

.disk-bar {
  flex: 1;
}

.net-charts {
  display: flex;
  gap: 20px;
  margin-top: 8px;
}
</style>

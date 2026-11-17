<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, QuestionFilled } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import ClusterRegisterDialog from '@/components/ClusterRegisterDialog.vue'
import HelpDialog from '@/components/HelpDialog.vue'
import StorageBar from '@/components/StorageBar.vue'
import type { ClusterSummary, ClusterTraffic } from '@/api/types'
import { api } from '@/api'
import { useClustersStore } from '@/stores/clusters'
import { useInstancesStore } from '@/stores/instances'
import { useMigrationsStore } from '@/stores/migrations'
import { errorMessage } from '@/utils/errors'
import { formatBytes, formatDateTime } from '@/utils/format'

const clusters = useClustersStore()
const instances = useInstancesStore()
const migrations = useMigrationsStore()
const { t } = useI18n()

const helpDialogOpen = ref(false)
const registerDialogOpen = ref(false)

// Fleet network traffic: per-cluster cumulative counters with derived rates.
interface TrafficState {
  rxRate: number
  txRate: number
  rxPacketsRate: number
  txPacketsRate: number
  last: ClusterTraffic | null
  lastAt: number
}

const traffic = reactive<Record<string, TrafficState>>({})
let trafficTimer: number | null = null
const prevSamples: Record<string, { at: number; traffic: ClusterTraffic }> = {}

async function pollTraffic(): Promise<void> {
  const now = performance.now()
  for (const cluster of clusters.items) {
    if (!cluster.online) {
      continue
    }

    try {
      const sample = await api.clusterTraffic(cluster.name)
      const previous = prevSamples[cluster.name]
      const state: TrafficState = traffic[cluster.name] ??= { rxRate: 0, txRate: 0, rxPacketsRate: 0, txPacketsRate: 0, last: sample, lastAt: now }

      if (previous) {
        const seconds = (now - previous.at) / 1000
        if (seconds >= 1) {
          state.rxRate = Math.max(0, (sample.total_rx_bytes - previous.traffic.total_rx_bytes) / seconds)
          state.txRate = Math.max(0, (sample.total_tx_bytes - previous.traffic.total_tx_bytes) / seconds)
          state.rxPacketsRate = Math.max(0, (sample.total_rx_packets - previous.traffic.total_rx_packets) / seconds)
          state.txPacketsRate = Math.max(0, (sample.total_tx_packets - previous.traffic.total_tx_packets) / seconds)
        }
      }
      prevSamples[cluster.name] = { at: now, traffic: sample }
      state.last = sample
      state.lastAt = now
    } catch {
      // A cluster going offline mid-poll is reported by the cards themselves.
    }
  }
}

function startTrafficPolling(): void {
  if (trafficTimer === null) {
    void pollTraffic()
    trafficTimer = window.setInterval(() => void pollTraffic(), 3000)
  }
}

function stopTrafficPolling(): void {
  if (trafficTimer !== null) {
    window.clearInterval(trafficTimer)
    trafficTimer = null
  }
}

onBeforeUnmount(stopTrafficPolling)

const totalInstances = computed(
  () => clusters.items.reduce((total, cluster) => total + cluster.instances.total, 0),
)
const runningInstances = computed(
  () => clusters.items.reduce((total, cluster) => total + cluster.instances.running, 0),
)
const totalVMs = computed(
  () => clusters.items.reduce((total, cluster) => total + cluster.instances.virtual_machines, 0),
)

/** Fleet-wide CPU capacity across online clusters. */
const fleetCPU = computed(() => {
  const online = clusters.items.filter((cluster) => cluster.online && cluster.cpu)
  return online.reduce((total, cluster) => total + (cluster.cpu?.total ?? 0), 0)
})

/** Fleet-wide memory usage and capacity across online clusters. */
const fleetMemory = computed(() => {
  const online = clusters.items.filter((cluster) => cluster.online && cluster.memory)
  return online.reduce(
    (acc, cluster) => ({
      used: acc.used + (cluster.memory?.used ?? 0),
      total: acc.total + (cluster.memory?.total ?? 0),
    }),
    { used: 0, total: 0 },
  )
})

const fleetMemoryPercent = computed(() => {
  if (fleetMemory.value.total <= 0) {
    return 0
  }

  return Math.round((fleetMemory.value.used / fleetMemory.value.total) * 100)
})

/** Fleet-wide storage usage and capacity across online clusters. */
const fleetStorage = computed(() => {
  const online = clusters.items.filter((cluster) => cluster.online)
  return online.reduce(
    (acc, cluster) => ({
      used: acc.used + cluster.storage.used,
      total: acc.total + cluster.storage.total,
    }),
    { used: 0, total: 0 },
  )
})

const fleetStoragePercent = computed(() => {
  if (fleetStorage.value.total <= 0) {
    return 0
  }

  return Math.round((fleetStorage.value.used / fleetStorage.value.total) * 100)
})

const fleetMembers = computed(() =>
  clusters.items.reduce((total, cluster) => total + cluster.members.length, 0),
)

/** Latest cumulative counters across all online clusters. */
const trafficTotals = computed(() => {
  const totals = { rxBytes: 0, txBytes: 0, rxPackets: 0, txPackets: 0 }
  for (const cluster of clusters.items) {
    const sample = traffic[cluster.name]?.last
    if (!sample) {
      continue
    }
    totals.rxBytes += sample.total_rx_bytes
    totals.txBytes += sample.total_tx_bytes
    totals.rxPackets += sample.total_rx_packets
    totals.txPackets += sample.total_tx_packets
  }

  return totals
})

onMounted(async () => {
  await Promise.all([
    clusters.fetchAll(),
    instances.fetchAll('any'),
    migrations.fetchAll(20),
  ])
  startTrafficPolling()
})

async function refresh(): Promise<void> {
  await Promise.all([
    clusters.fetchAll(),
    instances.fetchAll('any'),
    migrations.fetchAll(20),
  ])
  startTrafficPolling()
}

async function removeCluster(name: string): Promise<void> {
  try {
    await ElMessageBox.confirm(t('clusterRegister.removeConfirm', { name }), t('clusterRegister.add'), {
      type: 'warning',
      confirmButtonText: t('common.delete'),
      cancelButtonText: t('common.cancel'),
    })
  } catch {
    return
  }

  try {
    await api.unregisterCluster(name)
    ElMessage.success(t('clusterRegister.removed', { name }))
    await clusters.fetchAll()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

async function reconnect(name: string): Promise<void> {
  try {
    await clusters.reconnect(name)
    ElMessage.success(t('dashboard.reconnectSuccess', { name }))
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

function initials(name: string): string {
  return name.slice(0, 2).toUpperCase()
}

/** Compact memory share string for the cluster card, e.g. "3.2/16 GiB". */
function memoryShare(cluster: ClusterSummary): string {
  const memory = cluster.memory
  if (!memory || memory.total <= 0) {
    return '—'
  }

  return `${formatBytes(memory.used)} / ${formatBytes(memory.total)}`
}
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>{{ t('dashboard.title') }}</h2>
        <p class="subtitle">
          {{ t('dashboard.subtitle', { time: formatDateTime(clusters.lastUpdated?.toISOString()) }) }}
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" :loading="clusters.loading" @click="refresh">
          {{ t('common.refresh') }}
        </el-button>
        <el-button :icon="Plus" type="primary" @click="registerDialogOpen = true">
          {{ t('clusterRegister.add') }}
        </el-button>
        <el-button :icon="QuestionFilled" @click="helpDialogOpen = true">
          {{ t('common.help') }}
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="clusters.error"
      class="fleet-alert"
      type="error"
      :closable="false"
      show-icon
      :title="clusters.error"
    />

    <section class="stat-grid">
      <article class="stat-card">
        <span class="label">{{ t('dashboard.clustersOnline') }}</span>
        <span class="value">{{ clusters.onlineCount }} / {{ clusters.items.length }}</span>
        <span class="hint">
          {{ t('dashboard.unreachable', { count: clusters.items.length - clusters.onlineCount }) }}
        </span>
      </article>
      <article class="stat-card">
        <span class="label">{{ t('dashboard.instances') }}</span>
        <span class="value">{{ totalInstances }}</span>
        <span class="hint">{{ t('dashboard.running', { count: runningInstances }) }}</span>
      </article>
      <article class="stat-card">
        <span class="label">{{ t('dashboard.virtualMachines') }}</span>
        <span class="value">{{ totalVMs }}</span>
        <span class="hint">
          {{ t('dashboard.containersHint', { count: totalInstances - totalVMs }) }}
        </span>
      </article>
      <article class="stat-card">
        <span class="label">{{ t('dashboard.activeMigrations') }}</span>
        <span class="value">{{ migrations.active.length }}</span>
        <span class="hint">{{ t('dashboard.recentTasks', { count: migrations.items.length }) }}</span>
      </article>
    </section>

    <section v-loading="clusters.loading" class="cluster-grid">
      <article v-for="cluster in clusters.items" :key="cluster.name" class="cluster-card">
        <div class="cluster-card-head">
          <div class="cluster-title">
            <span class="cluster-avatar">{{ initials(cluster.display_name || cluster.name) }}</span>
            <div>
              <h3>{{ cluster.display_name }}</h3>
              <div class="endpoint">{{ cluster.endpoint }}</div>
            </div>
          </div>
          <el-tag :type="cluster.online ? 'success' : 'danger'" effect="light" round>
            {{ cluster.online ? t('cluster.online') : t('cluster.offline') }}
          </el-tag>
        </div>

        <el-alert
          v-if="!cluster.online && cluster.error"
          type="error"
          :closable="false"
          :title="cluster.error"
          class="fleet-alert"
        />

        <div class="cluster-metrics">
          <div class="metric">
            <span>{{ t('dashboard.instances') }}</span>
            <strong>{{ cluster.instances.total }}</strong>
          </div>
          <div class="metric">
            <span>{{ t('status.running') }}</span>
            <strong>{{ cluster.instances.running }}</strong>
          </div>
          <div class="metric">
            <span>{{ t('cluster.members') }}</span>
            <strong>{{ cluster.members.length }}</strong>
          </div>
          <div v-if="cluster.cpu" class="metric">
            <span>{{ t('cluster.cpuCores') }}</span>
            <strong>{{ cluster.cpu.total }}</strong>
          </div>
          <div v-if="cluster.memory && cluster.memory.total > 0" class="metric">
            <span>{{ t('cluster.memUsage') }}</span>
            <strong>{{ memoryShare(cluster) }}</strong>
          </div>
        </div>

        <StorageBar
          :used="cluster.storage.used"
          :total="cluster.storage.total"
          :pool="cluster.storage.pool"
        />

        <div class="cluster-tags">
          <el-tag v-if="cluster.clustered" size="small" effect="plain">
            {{ t('dashboard.clustered') }}
          </el-tag>
          <el-tag v-if="cluster.server_version" size="small" effect="plain">
            {{ t('dashboard.incusVersion', { version: cluster.server_version }) }}
          </el-tag>
          <el-tag v-if="cluster.server_name" size="small" effect="plain">
            {{ cluster.server_name }}
          </el-tag>
          <el-tag v-for="network in cluster.networks.slice(0, 3)" :key="network" size="small" effect="plain">
            {{ network }}
          </el-tag>
          <el-tag v-if="cluster.networks.length > 3" size="small" effect="plain">
            +{{ cluster.networks.length - 3 }}
          </el-tag>
        </div>

        <div v-if="traffic[cluster.name]" class="cluster-traffic">
          <span class="traffic-item">↓ {{ formatBytes(traffic[cluster.name]?.rxRate ?? 0) }}/s</span>
          <span class="traffic-item">↑ {{ formatBytes(traffic[cluster.name]?.txRate ?? 0) }}/s</span>
          <span class="traffic-item muted">{{ t('traffic.rxPackets') }} {{ (traffic[cluster.name]?.rxPacketsRate ?? 0).toFixed(0) }}/s · {{ t('traffic.txPackets') }} {{ (traffic[cluster.name]?.txPacketsRate ?? 0).toFixed(0) }}/s</span>
        </div>

        <div class="cluster-actions">
          <router-link :to="{ name: 'cluster', params: { cluster: cluster.name } }">
            <el-button type="primary" plain>{{ t('dashboard.openCluster') }}</el-button>
          </router-link>
          <el-button
            :loading="clusters.reconnecting === cluster.name"
            @click="reconnect(cluster.name)"
          >
            {{ t('common.reconnect') }}
          </el-button>
          <el-button link type="danger" @click="removeCluster(cluster.name)">
            {{ t('common.delete') }}
          </el-button>
        </div>
      </article>

      <el-empty
        v-if="!clusters.loading && clusters.items.length === 0"
        :description="t('dashboard.noClusters')"
        class="panel"
      />
    </section>

    <section class="panel resource-panel">
      <div class="panel-body">
        <div class="section-heading">
          <h3 class="section-title">{{ t('metrics.clusterResource') }}</h3>
        </div>
        <div class="resource-grid">
          <div class="resource-item">
            <span class="resource-label">{{ t('common.cpu') }}</span>
            <span class="resource-value">{{ fleetCPU }} <small>vCPU</small></span>
            <span class="resource-hint">{{ t('dashboard.clustered') }} · {{ fleetMembers }} {{ t('cluster.members') }}</span>
          </div>
          <div class="resource-item">
            <span class="resource-label">{{ t('common.memory') }}</span>
            <span class="resource-value">
              {{ formatBytes(fleetMemory.used) }} <small>/ {{ formatBytes(fleetMemory.total) }}</small>
            </span>
            <div class="resource-bar">
              <div class="resource-bar-fill memory" :style="{ width: `${fleetMemoryPercent}%` }"></div>
            </div>
          </div>
          <div class="resource-item">
            <span class="resource-label">{{ t('metrics.disk') }}</span>
            <span class="resource-value">
              {{ formatBytes(fleetStorage.used) }} <small>/ {{ formatBytes(fleetStorage.total) }}</small>
            </span>
            <div class="resource-bar">
              <div class="resource-bar-fill storage" :style="{ width: `${fleetStoragePercent}%` }"></div>
            </div>
          </div>
          <div class="resource-item">
            <span class="resource-label">{{ t('dashboard.instances') }}</span>
            <span class="resource-value">{{ runningInstances }} <small>/ {{ totalInstances }}</small></span>
            <span class="resource-hint">{{ t('dashboard.running', { count: runningInstances }) }}</span>
          </div>
        </div>

        <div v-if="trafficTotals.rxBytes > 0 || trafficTotals.txBytes > 0" class="traffic-totals">
          <span class="traffic-item">↓ {{ formatBytes(trafficTotals.rxBytes) }} · {{ trafficTotals.rxPackets.toLocaleString() }} {{ t('traffic.rxPackets') }}</span>
          <span class="traffic-item">↑ {{ formatBytes(trafficTotals.txBytes) }} · {{ trafficTotals.txPackets.toLocaleString() }} {{ t('traffic.txPackets') }}</span>
        </div>
      </div>
    </section>

    <ClusterRegisterDialog v-model="registerDialogOpen" @registered="clusters.fetchAll()" />
    <HelpDialog v-model="helpDialogOpen" />
  </div>
</template>

<style scoped>
.resource-panel {
  margin-top: 4px;
}

.resource-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 20px;
}

.resource-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.resource-label {
  font-size: 12px;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.resource-value {
  font-size: 22px;
  font-weight: 600;
}

.resource-value small {
  font-size: 12px;
  font-weight: 400;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
}

.resource-hint {
  font-size: 12px;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
}

.resource-bar {
  height: 6px;
  border-radius: 3px;
  background: var(--el-fill-color);
  overflow: hidden;
}

.resource-bar-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.3s ease;
}

.resource-bar-fill.memory {
  background: linear-gradient(90deg, #3b82f6, #06b6d4);
}

.resource-bar-fill.storage {
  background: linear-gradient(90deg, #10b981, #22c55e);
}
</style>

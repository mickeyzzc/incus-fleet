<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Box,
  Cpu,
  EditPen,
  Grid,
  Monitor,
  Plus,
  QuestionFilled,
  Refresh,
  RefreshRight,
} from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import ClusterRegisterDialog from '@/components/ClusterRegisterDialog.vue'
import HelpDialog from '@/components/HelpDialog.vue'
import RenameClusterDialog from '@/components/RenameClusterDialog.vue'
import StorageBar from '@/components/StorageBar.vue'
import TrafficSparkline from '@/components/TrafficSparkline.vue'
import type { ClusterSummary, ClusterTraffic } from '@/api/types'
import { api } from '@/api'
import { useClustersStore } from '@/stores/clusters'
import { useInstancesStore } from '@/stores/instances'
import { useMigrationsStore } from '@/stores/migrations'
import { useVisiblePolling } from '@/composables/useInstanceActions'
import { errorMessage } from '@/utils/errors'
import { formatBytes, formatDateTime } from '@/utils/format'

const clusters = useClustersStore()
const instances = useInstancesStore()
const migrations = useMigrationsStore()
const { t } = useI18n()

const helpDialogOpen = ref(false)
const registerDialogOpen = ref(false)
const renameDialogOpen = ref(false)
const renameTarget = ref<ClusterSummary | null>(null)

const HISTORY_LIMIT = 40

// Fleet network traffic: per-cluster cumulative counters with derived rates
// and a short rolling window for the live sparklines.
interface TrafficState {
  rxRate: number
  txRate: number
  rxPacketsRate: number
  txPacketsRate: number
  rxHistory: number[]
  txHistory: number[]
  rxPacketsHistory: number[]
  txPacketsHistory: number[]
  seeded: boolean
  last: ClusterTraffic | null
  lastAt: number
}

const traffic = reactive<Record<string, TrafficState>>({})
const fleetHistory = reactive({
  rx: Array<number>(HISTORY_LIMIT).fill(0),
  tx: Array<number>(HISTORY_LIMIT).fill(0),
  rxPackets: Array<number>(HISTORY_LIMIT).fill(0),
  txPackets: Array<number>(HISTORY_LIMIT).fill(0),
})
let trafficTimer: number | null = null
const prevSamples: Record<string, { at: number; traffic: ClusterTraffic }> = {}
let fleetSamples = 0
let fleetSeeded = false

let trafficPollBusy = false

async function pollTraffic(): Promise<void> {
  // Background tabs neither need live rates nor should they accumulate
  // requests; a still-running round skips the next tick.
  if (trafficPollBusy || (typeof document !== 'undefined' && document.visibilityState !== 'visible')) {
    return
  }
  trafficPollBusy = true
  try {
    await collectTrafficSamples()
  } finally {
    trafficPollBusy = false
  }
}

function pushHistory(history: number[], value: number): void {
  history.push(value)
  if (history.length > HISTORY_LIMIT) {
    history.shift()
  }
}

async function collectTrafficSamples(): Promise<void> {
  const now = performance.now()
  const fleetRates = { rx: 0, tx: 0, rxPackets: 0, txPackets: 0 }
  let sampled = 0

  for (const cluster of clusters.items) {
    if (!cluster.online) {
      continue
    }

    try {
      const sample = await api.clusterTraffic(cluster.name)
      const previous = prevSamples[cluster.name]
      const state: TrafficState = (traffic[cluster.name] ??= {
        rxRate: 0,
        txRate: 0,
        rxPacketsRate: 0,
        txPacketsRate: 0,
        rxHistory: Array<number>(HISTORY_LIMIT).fill(0),
        txHistory: Array<number>(HISTORY_LIMIT).fill(0),
        rxPacketsHistory: Array<number>(HISTORY_LIMIT).fill(0),
        txPacketsHistory: Array<number>(HISTORY_LIMIT).fill(0),
        seeded: false,
        last: sample,
        lastAt: now,
      })

      if (previous) {
        const seconds = (now - previous.at) / 1000
        if (seconds >= 1) {
          state.rxRate = Math.max(0, (sample.total_rx_bytes - previous.traffic.total_rx_bytes) / seconds)
          state.txRate = Math.max(0, (sample.total_tx_bytes - previous.traffic.total_tx_bytes) / seconds)
          state.rxPacketsRate = Math.max(
            0,
            (sample.total_rx_packets - previous.traffic.total_rx_packets) / seconds,
          )
          state.txPacketsRate = Math.max(
            0,
            (sample.total_tx_packets - previous.traffic.total_tx_packets) / seconds,
          )
        }
      }
      prevSamples[cluster.name] = { at: now, traffic: sample }
      state.last = sample
      state.lastAt = now
      sampled++

      // A live sparkline is easier to read when the first measured rate fills
      // the window instead of drawing a single cliff from a zero baseline.
      if (!state.seeded && previous) {
        state.rxHistory.fill(state.rxRate)
        state.txHistory.fill(state.txRate)
        state.rxPacketsHistory.fill(state.rxPacketsRate)
        state.txPacketsHistory.fill(state.txPacketsRate)
        state.seeded = true
      }

      pushHistory(state.rxHistory, state.rxRate)
      pushHistory(state.txHistory, state.txRate)
      pushHistory(state.rxPacketsHistory, state.rxPacketsRate)
      pushHistory(state.txPacketsHistory, state.txPacketsRate)

      fleetRates.rx += state.rxRate
      fleetRates.tx += state.txRate
      fleetRates.rxPackets += state.rxPacketsRate
      fleetRates.txPackets += state.txPacketsRate
    } catch {
      // A cluster going offline mid-poll is reported by the rows themselves.
    }
  }

  if (sampled > 0) {
    if (!fleetSeeded && fleetSamples > 0) {
      fleetHistory.rx.fill(fleetRates.rx)
      fleetHistory.tx.fill(fleetRates.tx)
      fleetHistory.rxPackets.fill(fleetRates.rxPackets)
      fleetHistory.txPackets.fill(fleetRates.txPackets)
      fleetSeeded = true
    }
    fleetSamples++
    pushHistory(fleetHistory.rx, fleetRates.rx)
    pushHistory(fleetHistory.tx, fleetRates.tx)
    pushHistory(fleetHistory.rxPackets, fleetRates.rxPackets)
    pushHistory(fleetHistory.txPackets, fleetRates.txPackets)
  }
}

function startTrafficPolling(): void {
  if (trafficTimer === null) {
    void pollTraffic()
    trafficTimer = window.setInterval(() => {
      if (document.visibilityState === 'visible') {
        void pollTraffic()
      }
    }, 3000)
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

const onlineRatio = computed(() => {
  if (clusters.items.length === 0) {
    return 0
  }

  return Math.round((clusters.onlineCount / clusters.items.length) * 100)
})
const runningRatio = computed(() => {
  if (totalInstances.value === 0) {
    return 0
  }

  return Math.round((runningInstances.value / totalInstances.value) * 100)
})
const vmRatio = computed(() => {
  if (totalInstances.value === 0) {
    return 0
  }

  return Math.round((totalVMs.value / totalInstances.value) * 100)
})

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
  await refresh()
  startTrafficPolling()
})

// Cluster rows used to freeze between manual refreshes while the traffic
// chart had its own timer; poll the fleet status on the shared
// visible-page loop so offline members come back on their own (#12).
useVisiblePolling(() => refresh(), 15000)

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
    await ElMessageBox.confirm(t('clusterRegister.removeConfirm', { name }), t('clusterRegister.removeTitle', { name }), {
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

function openRename(cluster: ClusterSummary): void {
  renameTarget.value = cluster
  renameDialogOpen.value = true
}

async function clusterRenamed(): Promise<void> {
  await clusters.fetchAll()
}

function initials(name: string): string {
  return name.slice(0, 2).toUpperCase()
}

/** Gives each cluster a stable accent hue so long lists stay scannable. */
function avatarHue(cluster: ClusterSummary): string {
  let hash = 0
  for (const char of cluster.name) {
    hash = (hash * 31 + char.charCodeAt(0)) % 360
  }

  return String(hash)
}

/** Compact memory share string for the cluster row, e.g. "3.2/16 GiB". */
function memoryShare(cluster: ClusterSummary): string {
  const memory = cluster.memory
  if (!memory || memory.total <= 0) {
    return '—'
  }

  return `${formatBytes(memory.used)} / ${formatBytes(memory.total)}`
}

function memoryPercent(cluster: ClusterSummary): number {
  const memory = cluster.memory
  if (!memory || memory.total <= 0) {
    return 0
  }

  return Math.min(100, Math.round((memory.used / memory.total) * 100))
}

function memberHint(cluster: ClusterSummary): string {
  return cluster.clustered ? t('dashboard.clustered') : t('cluster.server')
}
</script>

<template>
  <div class="page dashboard-page">
    <div class="page-header dashboard-header">
      <div class="page-heading">
        <span class="page-eyebrow">
          <span class="live-dot" :class="{ online: clusters.onlineCount > 0 }" />
          {{ t('app.tagline') }}
        </span>
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
      <article class="stat-card stat-card--clusters">
        <div class="stat-top">
          <span class="stat-icon"><el-icon><Grid /></el-icon></span>
          <span class="label">{{ t('dashboard.clustersOnline') }}</span>
        </div>
        <div class="stat-value-row">
          <span class="value">{{ clusters.onlineCount }} / {{ clusters.items.length }}</span>
          <span class="stat-unit">{{ onlineRatio }}%</span>
        </div>
        <div class="stat-meter"><span :style="{ width: `${onlineRatio}%` }" /></div>
        <span class="hint">
          {{ t('dashboard.unreachable', { count: clusters.items.length - clusters.onlineCount }) }}
        </span>
      </article>

      <article class="stat-card stat-card--instances">
        <div class="stat-top">
          <span class="stat-icon"><el-icon><Box /></el-icon></span>
          <span class="label">{{ t('dashboard.instances') }}</span>
        </div>
        <div class="stat-value-row">
          <span class="value">{{ totalInstances }}</span>
          <span class="stat-unit">{{ runningRatio }}% {{ t('status.running') }}</span>
        </div>
        <div class="stat-meter"><span :style="{ width: `${runningRatio}%` }" /></div>
        <span class="hint">{{ t('dashboard.running', { count: runningInstances }) }}</span>
      </article>

      <article class="stat-card stat-card--vms">
        <div class="stat-top">
          <span class="stat-icon"><el-icon><Monitor /></el-icon></span>
          <span class="label">{{ t('dashboard.virtualMachines') }}</span>
        </div>
        <div class="stat-value-row">
          <span class="value">{{ totalVMs }}</span>
          <span class="stat-unit">{{ vmRatio }}%</span>
        </div>
        <div class="stat-meter"><span :style="{ width: `${vmRatio}%` }" /></div>
        <span class="hint">{{ t('dashboard.containersHint', { count: totalInstances - totalVMs }) }}</span>
      </article>

      <article class="stat-card stat-card--migrations">
        <div class="stat-top">
          <span class="stat-icon"><el-icon><RefreshRight /></el-icon></span>
          <span class="label">{{ t('dashboard.activeMigrations') }}</span>
        </div>
        <div class="stat-value-row">
          <span class="value">{{ migrations.active.length }}</span>
          <span class="stat-unit">{{ t('dashboard.taskCount', { count: migrations.items.length }) }}</span>
        </div>
        <div class="stat-meter stat-meter--muted">
          <span :style="{ width: migrations.active.length > 0 ? '100%' : '0%' }" />
        </div>
        <span class="hint">{{ t('dashboard.recentTasks', { count: migrations.items.length }) }}</span>
      </article>
    </section>

    <section v-loading="clusters.loading" class="cluster-list">
      <article
        v-for="cluster in clusters.items"
        :key="cluster.name"
        class="cluster-row"
        :class="{ 'is-offline': !cluster.online }"
      >
        <div class="cluster-row-main">
          <div class="cluster-identity">
            <span
              class="cluster-avatar"
              :class="{ 'is-offline': !cluster.online }"
              :style="{ '--avatar-hue': avatarHue(cluster) }"
            >
              {{ initials(cluster.display_name || cluster.name) }}
            </span>
            <div class="cluster-title">
              <div class="cluster-name-line">
                <h3>{{ cluster.display_name || cluster.name }}</h3>
                <el-button
                  class="rename-trigger"
                  text
                  circle
                  size="small"
                  :icon="EditPen"
                  :title="t('dashboard.renameCluster')"
                  :aria-label="t('dashboard.renameCluster')"
                  @click="openRename(cluster)"
                />
                <el-tag
                  :type="cluster.online ? 'success' : 'danger'"
                  effect="light"
                  round
                  size="small"
                >
                  {{ cluster.online ? t('cluster.online') : t('cluster.offline') }}
                </el-tag>
              </div>
              <div class="cluster-endpoint">
                <span class="status-dot" :class="{ online: cluster.online }" />
                <span class="mono" :title="cluster.endpoint">{{ cluster.endpoint }}</span>
              </div>
            </div>
          </div>

          <div class="cluster-figures">
            <div class="figure">
              <span>{{ t('dashboard.instances') }}</span>
              <strong>{{ cluster.instances.total }}</strong>
              <small>{{ cluster.instances.running }} {{ t('status.running') }}</small>
            </div>
            <div class="figure">
              <span>{{ t('dashboard.nodes') }}</span>
              <strong>{{ cluster.members.length }}</strong>
              <small>{{ memberHint(cluster) }}</small>
            </div>
            <div class="figure">
              <span>{{ t('dashboard.cpuTotal') }}</span>
              <strong>{{ cluster.cpu?.total ?? '—' }}</strong>
              <small>vCPU</small>
            </div>
            <div class="figure figure--memory">
              <span>{{ t('dashboard.memoryTotal') }}</span>
              <strong>{{ memoryPercent(cluster) }}%</strong>
              <small :title="memoryShare(cluster)">{{ memoryShare(cluster) }}</small>
            </div>
          </div>

          <div class="cluster-live">
            <TrafficSparkline
              :rx="traffic[cluster.name]?.rxHistory ?? []"
              :tx="traffic[cluster.name]?.txHistory ?? []"
              :rx-packets="traffic[cluster.name]?.rxPacketsHistory ?? []"
              :tx-packets="traffic[cluster.name]?.txPacketsHistory ?? []"
            />
          </div>

          <div class="cluster-actions">
            <router-link :to="{ name: 'cluster', params: { cluster: cluster.name } }">
              <el-button type="primary" plain size="small">{{ t('dashboard.openCluster') }}</el-button>
            </router-link>
            <el-button
              size="small"
              :loading="clusters.reconnecting === cluster.name"
              @click="reconnect(cluster.name)"
            >
              {{ t('common.reconnect') }}
            </el-button>
            <el-button size="small" link type="danger" @click="removeCluster(cluster.name)">
              {{ t('common.delete') }}
            </el-button>
          </div>
        </div>

        <el-alert
          v-if="!cluster.online && cluster.error"
          type="error"
          :closable="false"
          :title="cluster.error"
          class="cluster-error"
        />

        <div class="cluster-row-foot">
          <StorageBar
            class="cluster-storage"
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
            <el-tag v-for="network in cluster.networks.slice(0, 2)" :key="network" size="small" effect="plain">
              {{ network }}
            </el-tag>
            <el-tag
              v-if="cluster.networks.length > 2"
              size="small"
              effect="plain"
              :title="cluster.networks.join(', ')"
            >
              +{{ cluster.networks.length - 2 }}
            </el-tag>
          </div>
        </div>
      </article>

      <el-empty
        v-if="!clusters.loading && clusters.items.length === 0"
        :description="t('dashboard.noClusters')"
        class="panel"
      />
    </section>

    <section class="panel resource-panel">
      <div class="panel-head resource-head">
        <div>
          <span class="panel-eyebrow">{{ t('dashboard.capacityEyebrow') }}</span>
          <h3 class="section-title">{{ t('metrics.clusterResource') }}</h3>
        </div>
        <span class="resource-meta">{{ fleetMembers }} {{ t('dashboard.nodes') }} · {{ clusters.onlineCount }} {{ t('dashboard.clustersOnline') }}</span>
      </div>
      <div class="panel-body">
        <div class="resource-grid">
          <article class="resource-item">
            <span class="resource-icon cpu"><el-icon><Cpu /></el-icon></span>
            <div class="resource-copy">
              <span class="resource-label">{{ t('common.cpu') }}</span>
              <span class="resource-value">{{ fleetCPU }} <small>vCPU</small></span>
              <span class="resource-hint">{{ t('dashboard.clustered') }} · {{ fleetMembers }} {{ t('dashboard.nodes') }}</span>
            </div>
          </article>
          <article class="resource-item">
            <span class="resource-icon memory"><el-icon><Box /></el-icon></span>
            <div class="resource-copy">
              <span class="resource-label">{{ t('common.memory') }}</span>
              <span class="resource-value">
                {{ formatBytes(fleetMemory.used) }} <small>/ {{ formatBytes(fleetMemory.total) }}</small>
              </span>
              <div class="resource-bar">
                <div class="resource-bar-fill memory" :style="{ width: `${fleetMemoryPercent}%` }" />
              </div>
            </div>
          </article>
          <article class="resource-item">
            <span class="resource-icon storage"><el-icon><Grid /></el-icon></span>
            <div class="resource-copy">
              <span class="resource-label">{{ t('metrics.disk') }}</span>
              <span class="resource-value">
                {{ formatBytes(fleetStorage.used) }} <small>/ {{ formatBytes(fleetStorage.total) }}</small>
              </span>
              <div class="resource-bar">
                <div class="resource-bar-fill storage" :style="{ width: `${fleetStoragePercent}%` }" />
              </div>
            </div>
          </article>
          <article class="resource-item">
            <span class="resource-icon instances"><el-icon><Monitor /></el-icon></span>
            <div class="resource-copy">
              <span class="resource-label">{{ t('dashboard.instances') }}</span>
              <span class="resource-value">{{ runningInstances }} <small>/ {{ totalInstances }}</small></span>
              <span class="resource-hint">{{ t('dashboard.running', { count: runningInstances }) }}</span>
            </div>
          </article>
        </div>

        <div class="fleet-traffic">
          <div class="fleet-traffic-chart">
            <TrafficSparkline
              :rx="fleetHistory.rx"
              :tx="fleetHistory.tx"
              :rx-packets="fleetHistory.rxPackets"
              :tx-packets="fleetHistory.txPackets"
              :title="t('traffic.title')"
            />
          </div>
          <div v-if="trafficTotals.rxBytes > 0 || trafficTotals.txBytes > 0" class="traffic-totals">
            <span class="traffic-item">
              ↓ {{ formatBytes(trafficTotals.rxBytes) }} · {{ trafficTotals.rxPackets.toLocaleString() }}
              {{ t('traffic.rxPackets') }}
            </span>
            <span class="traffic-item">
              ↑ {{ formatBytes(trafficTotals.txBytes) }} · {{ trafficTotals.txPackets.toLocaleString() }}
              {{ t('traffic.txPackets') }}
            </span>
          </div>
        </div>
      </div>
    </section>

    <RenameClusterDialog
      v-model="renameDialogOpen"
      :cluster="renameTarget?.name ?? ''"
      :current-name="renameTarget?.display_name ?? ''"
      @renamed="clusterRenamed"
    />
    <ClusterRegisterDialog v-model="registerDialogOpen" @registered="clusters.fetchAll()" />
    <HelpDialog v-model="helpDialogOpen" />
  </div>
</template>

<style scoped>
.dashboard-header {
  align-items: flex-end;
}

.page-heading {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.page-eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  color: var(--fleet-ink-soft);
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

.live-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--fleet-ink-soft);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--fleet-ink-soft) 14%, transparent);
}

.live-dot.online {
  background: var(--fleet-good);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--fleet-good) 18%, transparent);
}

.stat-card {
  position: relative;
  overflow: hidden;
  gap: 7px;
  padding: 15px 17px 14px;
}

.stat-card::after {
  content: '';
  position: absolute;
  inset: 0 0 auto;
  height: 3px;
  background: var(--stat-accent, var(--el-color-primary));
  opacity: 0.85;
}

.stat-card--clusters {
  --stat-accent: #4562e8;
}

.stat-card--instances {
  --stat-accent: #0ea5a4;
}

.stat-card--vms {
  --stat-accent: #8b5cf6;
}

.stat-card--migrations {
  --stat-accent: #f59e0b;
}

.stat-top {
  display: flex;
  align-items: center;
  gap: 9px;
}

.stat-icon {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  flex: none;
  border-radius: 9px;
  background: color-mix(in srgb, var(--stat-accent) 12%, transparent);
  color: var(--stat-accent);
  font-size: 15px;
}

.stat-card .label {
  letter-spacing: 0.06em;
  font-size: 11px;
}

.stat-value-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.stat-card .value {
  font-size: 27px;
  line-height: 1.05;
}

.stat-unit {
  color: var(--fleet-ink-soft);
  font-size: 11.5px;
  font-weight: 600;
  white-space: nowrap;
}

.stat-meter {
  height: 4px;
  overflow: hidden;
  border-radius: 99px;
  background: var(--fleet-panel-muted);
}

.stat-meter span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--stat-accent, var(--el-color-primary));
  transition: width 0.4s ease;
}

.stat-meter--muted span {
  opacity: 0.55;
}

.cluster-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 80px;
}

.cluster-row {
  position: relative;
  overflow: hidden;
  border: 1px solid var(--fleet-line);
  border-radius: var(--fleet-radius-lg);
  background:
    linear-gradient(90deg, color-mix(in srgb, var(--fleet-good) 5%, transparent), transparent 26%),
    var(--fleet-panel);
  box-shadow: var(--fleet-shadow);
  transition:
    border-color 0.18s ease,
    box-shadow 0.18s ease,
    transform 0.18s ease;
}

.cluster-row::before {
  content: '';
  position: absolute;
  inset: 0 auto 0 0;
  width: 3px;
  background: var(--fleet-good);
  opacity: 0.8;
}

.cluster-row.is-offline {
  background: var(--fleet-panel);
}

.cluster-row.is-offline::before {
  background: var(--fleet-bad);
}

.cluster-row:hover {
  transform: translateY(-1px);
  border-color: color-mix(in srgb, var(--el-color-primary) 30%, var(--fleet-line));
  box-shadow: 0 1px 2px rgba(16, 24, 40, 0.05), 0 18px 38px -26px rgba(16, 24, 40, 0.34);
}

.cluster-row-main {
  display: grid;
  grid-template-areas: 'identity figures live actions';
  grid-template-columns: minmax(220px, 1.25fr) minmax(300px, 1.15fr) minmax(210px, 0.85fr) auto;
  align-items: center;
  gap: 22px;
  padding: 16px 20px 12px 22px;
}

.cluster-identity {
  grid-area: identity;
  display: flex;
  align-items: flex-start;
  gap: 12px;
  min-width: 0;
}

.cluster-avatar {
  display: grid;
  place-items: center;
  width: 42px;
  height: 42px;
  flex: none;
  border-radius: 13px;
  background: linear-gradient(
    135deg,
    hsl(var(--avatar-hue, 245) 88% 96%),
    hsl(var(--avatar-hue, 245) 72% 90%)
  );
  color: hsl(var(--avatar-hue, 245) 58% 40%);
  font-size: 13px;
  font-weight: 800;
  letter-spacing: 0.02em;
}

.cluster-avatar.is-offline {
  background: var(--fleet-panel-muted);
  color: var(--fleet-ink-soft);
}

.cluster-title {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.cluster-name-line {
  display: flex;
  align-items: center;
  gap: 5px;
  min-width: 0;
}

.cluster-name-line h3 {
  overflow: hidden;
  font-size: 16px;
  font-weight: 720;
  letter-spacing: -0.22px;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rename-trigger {
  flex: none;
  color: var(--fleet-ink-soft);
  opacity: 0;
  transition: opacity 0.15s ease;
}

.cluster-row:hover .rename-trigger,
.rename-trigger:focus-visible {
  opacity: 1;
}

.cluster-endpoint {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  color: var(--fleet-ink-soft);
}

.cluster-endpoint .mono {
  overflow: hidden;
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.status-dot {
  width: 6px;
  height: 6px;
  flex: none;
  border-radius: 50%;
  background: var(--fleet-bad);
}

.status-dot.online {
  background: var(--fleet-good);
}

.cluster-figures {
  grid-area: figures;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}

.figure {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
  padding: 7px 10px;
  border: 1px solid color-mix(in srgb, var(--fleet-line) 76%, transparent);
  border-radius: 10px;
  background: color-mix(in srgb, var(--fleet-panel-muted) 78%, transparent);
}

.figure span {
  overflow: hidden;
  color: var(--fleet-ink-soft);
  font-size: 10px;
  font-weight: 650;
  letter-spacing: 0.06em;
  text-overflow: ellipsis;
  text-transform: uppercase;
  white-space: nowrap;
}

.figure strong {
  font-size: 18px;
  font-weight: 720;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.25px;
  line-height: 1.2;
}

.figure small {
  overflow: hidden;
  color: var(--fleet-ink-soft);
  font-size: 10.5px;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cluster-live {
  grid-area: live;
  min-width: 0;
  padding: 2px 0;
}

.cluster-actions {
  grid-area: actions;
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 0;
}

.cluster-actions a {
  display: inline-flex;
}

.cluster-actions :deep(.el-button) {
  min-height: 30px;
}

.cluster-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}

.cluster-error {
  margin: 0 20px 8px 22px;
  border-radius: 9px;
}

.cluster-row-foot {
  display: flex;
  align-items: center;
  gap: 22px;
  padding: 0 20px 13px 22px;
}

.cluster-storage {
  flex: 1;
  min-width: 220px;
  max-width: 440px;
}

.cluster-storage :deep(.storage-label) {
  font-size: 11px;
}

.cluster-tags {
  display: flex;
  flex: 1;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 5px;
  min-width: 0;
}

.resource-panel {
  overflow: hidden;
}

.resource-head {
  align-items: center;
}

.panel-eyebrow {
  display: block;
  margin-bottom: 2px;
  color: var(--fleet-ink-soft);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

.resource-head .section-title {
  margin-bottom: 0;
}

.resource-meta {
  color: var(--fleet-ink-soft);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.resource-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
  gap: 14px;
}

.resource-item {
  display: flex;
  align-items: flex-start;
  gap: 11px;
  padding: 13px 14px;
  border: 1px solid color-mix(in srgb, var(--fleet-line) 76%, transparent);
  border-radius: 12px;
  background: color-mix(in srgb, var(--fleet-panel-muted) 72%, transparent);
}

.resource-icon {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  flex: none;
  border-radius: 10px;
  font-size: 16px;
}

.resource-icon.cpu {
  background: #eef2ff;
  color: #4562e8;
}

.resource-icon.memory {
  background: #ecfeff;
  color: #0891b2;
}

.resource-icon.storage {
  background: #ecfdf5;
  color: #059669;
}

.resource-icon.instances {
  background: #f5f3ff;
  color: #7c3aed;
}

.resource-copy {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.resource-label {
  color: var(--fleet-ink-soft);
  font-size: 10.5px;
  font-weight: 650;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.resource-value {
  font-size: 20px;
  font-weight: 720;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.3px;
  line-height: 1.15;
}

.resource-value small {
  color: var(--fleet-ink-soft);
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0;
}

.resource-hint {
  color: var(--fleet-ink-soft);
  font-size: 11.5px;
}

.resource-bar {
  height: 5px;
  overflow: hidden;
  border-radius: 99px;
  background: var(--el-fill-color);
}

.resource-bar-fill {
  height: 100%;
  border-radius: inherit;
  transition: width 0.4s ease;
}

.resource-bar-fill.memory {
  background: linear-gradient(90deg, #3b82f6, #06b6d4);
}

.resource-bar-fill.storage {
  background: linear-gradient(90deg, #10b981, #22c55e);
}

.fleet-traffic {
  display: grid;
  grid-template-columns: minmax(0, 560px) auto;
  align-items: end;
  gap: 20px;
  margin-top: 18px;
  padding-top: 16px;
  border-top: 1px solid var(--fleet-line);
}

.fleet-traffic-chart {
  min-width: 0;
}

.traffic-totals {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px 18px;
  color: var(--fleet-ink-soft);
  font-size: 11.5px;
  font-variant-numeric: tabular-nums;
}

@media (max-width: 1180px) {
  .cluster-row-main {
    grid-template-areas: 'identity figures live actions';
    grid-template-columns: minmax(200px, 1.05fr) minmax(170px, 0.85fr) minmax(190px, 0.9fr) auto;
    gap: 14px;
  }

  .cluster-figures {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 6px;
  }

  .figure {
    padding: 5px 8px;
  }

  .figure strong {
    font-size: 16px;
  }

  .cluster-actions {
    max-width: 190px;
    flex-wrap: wrap;
  }

  .cluster-row-foot {
    flex-wrap: wrap;
  }

  .cluster-tags {
    justify-content: flex-start;
  }
}

@media (max-width: 860px) {
  .cluster-row-main {
    grid-template-areas:
      'identity actions'
      'figures figures'
      'live live';
    grid-template-columns: 1fr auto;
    gap: 14px;
  }

  .cluster-figures {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }

  .cluster-actions {
    max-width: none;
    align-self: start;
    flex-wrap: nowrap;
  }

  .fleet-traffic {
    grid-template-columns: 1fr;
  }

  .traffic-totals {
    justify-content: flex-start;
  }
}

@media (max-width: 620px) {
  .dashboard-header {
    align-items: flex-start;
  }

  .cluster-row-main {
    grid-template-areas:
      'identity'
      'figures'
      'live'
      'actions';
    grid-template-columns: 1fr;
    padding: 14px 14px 10px 17px;
  }

  .cluster-actions {
    justify-content: flex-start;
  }

  .cluster-figures {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .cluster-row-foot {
    flex-direction: column;
    align-items: stretch;
    padding: 0 14px 12px 17px;
  }

  .cluster-storage {
    min-width: 0;
    max-width: none;
  }

  .cluster-tags {
    justify-content: flex-start;
  }

  .resource-grid {
    grid-template-columns: 1fr;
  }
}

@media (prefers-color-scheme: dark) {
  .cluster-avatar {
    background: linear-gradient(
      135deg,
      hsl(var(--avatar-hue, 245) 32% 26%),
      hsl(var(--avatar-hue, 245) 38% 19%)
    );
    color: hsl(var(--avatar-hue, 245) 78% 78%);
  }

  .resource-icon.cpu {
    background: #1d2a4d;
    color: #8fa4ff;
  }

  .resource-icon.memory {
    background: #113640;
    color: #67d9e9;
  }

  .resource-icon.storage {
    background: #123b31;
    color: #67e0b0;
  }

  .resource-icon.instances {
    background: #2c2450;
    color: #b49cff;
  }
}
</style>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import StorageBar from '@/components/StorageBar.vue'
import type { ClusterSummary } from '@/api/types'
import { useClustersStore } from '@/stores/clusters'
import { useInstancesStore } from '@/stores/instances'
import { useMigrationsStore } from '@/stores/migrations'
import { errorMessage } from '@/utils/errors'
import { formatBytes, formatDateTime } from '@/utils/format'

const clusters = useClustersStore()
const instances = useInstancesStore()
const migrations = useMigrationsStore()
const { t } = useI18n()

const totalInstances = computed(
  () => clusters.items.reduce((total, cluster) => total + cluster.instances.total, 0),
)
const runningInstances = computed(
  () => clusters.items.reduce((total, cluster) => total + cluster.instances.running, 0),
)
const totalVMs = computed(
  () => clusters.items.reduce((total, cluster) => total + cluster.instances.virtual_machines, 0),
)

onMounted(async () => {
  await Promise.all([
    clusters.fetchAll(),
    instances.fetchAll('any'),
    migrations.fetchAll(20),
  ])
})

async function refresh(): Promise<void> {
  await Promise.all([
    clusters.fetchAll(),
    instances.fetchAll('any'),
    migrations.fetchAll(20),
  ])
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
        </div>
      </article>

      <el-empty
        v-if="!clusters.loading && clusters.items.length === 0"
        :description="t('dashboard.noClusters')"
        class="panel"
      />
    </section>
  </div>
</template>

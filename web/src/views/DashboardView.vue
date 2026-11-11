<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'

import StorageBar from '@/components/StorageBar.vue'
import { useClustersStore } from '@/stores/clusters'
import { useInstancesStore } from '@/stores/instances'
import { useMigrationsStore } from '@/stores/migrations'
import { errorMessage } from '@/utils/errors'
import { formatDateTime } from '@/utils/format'

const clusters = useClustersStore()
const instances = useInstancesStore()
const migrations = useMigrationsStore()

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
    ElMessage.success(`Reconnected ${name}.`)
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

function initials(name: string): string {
  return name.slice(0, 2).toUpperCase()
}
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>Fleet overview</h2>
        <p class="subtitle">
          Independent clusters, one control surface. Last refreshed
          {{ formatDateTime(clusters.lastUpdated?.toISOString()) }}.
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" :loading="clusters.loading" @click="refresh">Refresh</el-button>
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
        <span class="label">Clusters online</span>
        <span class="value">{{ clusters.onlineCount }} / {{ clusters.items.length }}</span>
        <span class="hint">{{ clusters.items.length - clusters.onlineCount }} unreachable</span>
      </article>
      <article class="stat-card">
        <span class="label">Instances</span>
        <span class="value">{{ totalInstances }}</span>
        <span class="hint">{{ runningInstances }} running</span>
      </article>
      <article class="stat-card">
        <span class="label">Virtual machines</span>
        <span class="value">{{ totalVMs }}</span>
        <span class="hint">{{ totalInstances - totalVMs }} containers</span>
      </article>
      <article class="stat-card">
        <span class="label">Active migrations</span>
        <span class="value">{{ migrations.active.length }}</span>
        <span class="hint">{{ migrations.items.length }} recent tasks</span>
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
            {{ cluster.online ? 'Online' : 'Offline' }}
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
            <span>Instances</span>
            <strong>{{ cluster.instances.total }}</strong>
          </div>
          <div class="metric">
            <span>Running</span>
            <strong>{{ cluster.instances.running }}</strong>
          </div>
          <div class="metric">
            <span>Members</span>
            <strong>{{ cluster.members.length }}</strong>
          </div>
        </div>

        <StorageBar
          :used="cluster.storage.used"
          :total="cluster.storage.total"
          :pool="cluster.storage.pool"
        />

        <div class="cluster-tags">
          <el-tag v-if="cluster.clustered" size="small" effect="plain">Clustered</el-tag>
          <el-tag v-if="cluster.server_version" size="small" effect="plain">
            Incus {{ cluster.server_version }}
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
            <el-button type="primary" plain>Open cluster</el-button>
          </router-link>
          <el-button
            :loading="clusters.reconnecting === cluster.name"
            @click="reconnect(cluster.name)"
          >
            Reconnect
          </el-button>
        </div>
      </article>

      <el-empty
        v-if="!clusters.loading && clusters.items.length === 0"
        description="No clusters configured"
        class="panel"
      />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Refresh } from '@element-plus/icons-vue'

import InstanceDetailDrawer from '@/components/InstanceDetailDrawer.vue'
import MigrateDialog from '@/components/MigrateDialog.vue'
import StatusTag from '@/components/StatusTag.vue'
import StorageBar from '@/components/StorageBar.vue'
import type { InstanceSummary, InstanceType } from '@/api/types'
import { useClustersStore } from '@/stores/clusters'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'
import { formatBytes, formatDateTime, primaryAddress } from '@/utils/format'

const props = defineProps<{ cluster: string }>()
const router = useRouter()
const clusters = useClustersStore()
const instances = useInstancesStore()

const typeFilter = ref<InstanceType>('any')
const detailOpen = ref(false)
const detailName = ref('')
const migrateOpen = ref(false)
const migrateInstance = ref<InstanceSummary | null>(null)

const summary = computed(() => clusters.byName(props.cluster))
const filtered = computed(() =>
  instances.items.filter((instance) => typeFilter.value === 'any' || instance.type === typeFilter.value),
)
const onlineTargets = computed(() =>
  clusters.items.filter((cluster) => cluster.online && cluster.name !== props.cluster),
)

async function load(): Promise<void> {
  await Promise.all([clusters.fetchAll(), instances.fetchCluster(props.cluster, typeFilter.value)])
}

onMounted(load)

watch(
  () => props.cluster,
  async () => {
    await load()
  },
)

watch(typeFilter, async (type) => {
  await instances.fetchCluster(props.cluster, type)
})

async function reconnect(): Promise<void> {
  try {
    await clusters.reconnect(props.cluster)
    ElMessage.success(`Reconnected ${props.cluster}.`)
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

function openDetail(instance: InstanceSummary): void {
  detailName.value = instance.name
  detailOpen.value = true
}

function openMigrate(instance: InstanceSummary): void {
  migrateInstance.value = instance
  migrateOpen.value = true
}

function instanceTypeLabel(type: string): string {
  return type === 'virtual-machine' ? 'VM' : 'Container'
}
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div class="title-with-back">
        <el-button :icon="ArrowLeft" circle plain @click="router.push({ name: 'dashboard' })" />
        <div>
          <h2>{{ summary?.display_name || cluster }}</h2>
          <p class="subtitle">
            {{ summary?.endpoint || 'Cluster endpoint unavailable' }} · refreshed
            {{ formatDateTime(instances.lastUpdated?.toISOString()) }}
          </p>
        </div>
      </div>
      <div class="page-actions">
        <el-tag v-if="summary" :type="summary.online ? 'success' : 'danger'" effect="light" round>
          {{ summary.online ? 'Online' : 'Offline' }}
        </el-tag>
        <el-button :icon="Refresh" :loading="clusters.loading || instances.loading" @click="load">
          Refresh
        </el-button>
        <el-button :loading="clusters.reconnecting === cluster" @click="reconnect">
          Reconnect
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="summary && !summary.online && summary.error"
      class="fleet-alert"
      type="error"
      :closable="false"
      show-icon
      :title="summary.error"
    />

    <el-alert
      v-if="instances.error"
      class="fleet-alert"
      type="error"
      :closable="false"
      show-icon
      :title="instances.error"
    />

    <section v-if="summary" class="panel">
      <div class="panel-body cluster-detail">
        <div class="detail-grid">
          <div class="detail-item">
            <div class="label">Server</div>
            <div class="value">{{ summary.server_name || '—' }}</div>
          </div>
          <div class="detail-item">
            <div class="label">Incus version</div>
            <div class="value">{{ summary.server_version || '—' }}</div>
          </div>
          <div class="detail-item">
            <div class="label">Members</div>
            <div class="value">{{ summary.members.length }}</div>
          </div>
          <div class="detail-item">
            <div class="label">Instances</div>
            <div class="value">
              {{ summary.instances.total }} total · {{ summary.instances.running }} running
            </div>
          </div>
          <div class="detail-item">
            <div class="label">Containers / VMs</div>
            <div class="value">
              {{ summary.instances.containers }} / {{ summary.instances.virtual_machines }}
            </div>
          </div>
          <div class="detail-item">
            <div class="label">Architectures</div>
            <div class="value">{{ summary.architectures?.join(', ') || '—' }}</div>
          </div>
        </div>

        <StorageBar
          :used="summary.storage.used"
          :total="summary.storage.total"
          :pool="summary.storage.pool"
        />

        <div class="cluster-tags">
          <el-tag v-for="member in summary.members" :key="member.name" size="small" effect="plain">
            {{ member.name }} · {{ member.status }}
          </el-tag>
          <el-tag v-for="network in summary.networks" :key="network" size="small" effect="plain">
            {{ network }}
          </el-tag>
        </div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-body">
        <div class="page-header" style="margin-bottom: 14px">
          <div>
            <h3 class="section-title">Instances</h3>
            <p class="subtitle">
              {{ filtered.length }} shown · {{ onlineTargets.length }} migration target(s) online
            </p>
          </div>
          <div class="page-actions">
            <el-select v-model="typeFilter" style="width: 180px">
              <el-option label="All types" value="any" />
              <el-option label="Containers" value="container" />
              <el-option label="Virtual machines" value="virtual-machine" />
            </el-select>
          </div>
        </div>

        <el-table v-loading="instances.loading" :data="filtered" stripe>
          <el-table-column label="Name" min-width="170">
            <template #default="{ row }">
              <div class="status-cell">
                <div>
                  <strong>{{ row.name }}</strong>
                  <span class="mini-label">{{ row.description || '—' }}</span>
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="Type" width="110">
            <template #default="{ row }">
              <el-tag size="small" effect="plain">{{ instanceTypeLabel(row.type) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="Status" width="120">
            <template #default="{ row }">
              <StatusTag :status="row.status" :status-code="row.status_code" />
            </template>
          </el-table-column>
          <el-table-column prop="location" label="Node" width="140" />
          <el-table-column label="Address" min-width="150">
            <template #default="{ row }">
              <span class="mono">{{ primaryAddress(row.ipv4, row.ipv6) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="Memory" width="110">
            <template #default="{ row }">{{ formatBytes(row.memory) }}</template>
          </el-table-column>
          <el-table-column label="Live" width="90">
            <template #default="{ row }">
              <el-tag v-if="row.live_migratable" size="small" type="success" effect="light">
                Yes
              </el-tag>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="Actions" width="200" fixed="right">
            <template #default="{ row }">
              <el-button size="small" text @click="openDetail(row)">Details</el-button>
              <el-button size="small" type="primary" plain @click="openMigrate(row)">
                Migrate
              </el-button>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty description="No instances on this cluster" />
          </template>
        </el-table>
      </div>
    </section>

    <InstanceDetailDrawer v-model="detailOpen" :cluster="cluster" :name="detailName" />
    <MigrateDialog
      v-model="migrateOpen"
      :instance="migrateInstance"
      :clusters="clusters.items"
      @submitted="load"
    />
  </div>
</template>

<style scoped>
.title-with-back {
  display: flex;
  align-items: center;
  gap: 14px;
}

.cluster-detail {
  display: flex;
  flex-direction: column;
  gap: 18px;
}
</style>

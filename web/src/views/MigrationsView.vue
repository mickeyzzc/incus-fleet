<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'

import MigrationStatusTag from '@/components/MigrationStatusTag.vue'
import type { Migration } from '@/api/types'
import { useMigrationsStore } from '@/stores/migrations'
import { formatDateTime, formatDuration, shortID } from '@/utils/format'

const migrations = useMigrationsStore()

const search = ref('')
const statusFilter = ref('')
const drawerOpen = ref(false)
const selectedId = ref('')

const filtered = computed(() => {
  const needle = search.value.trim().toLowerCase()

  return migrations.items.filter((migration) => {
    if (statusFilter.value && migration.status !== statusFilter.value) {
      return false
    }
    if (!needle) {
      return true
    }

    return (
      migration.instance.toLowerCase().includes(needle) ||
      migration.source_cluster.toLowerCase().includes(needle) ||
      migration.target_cluster.toLowerCase().includes(needle) ||
      migration.id.toLowerCase().includes(needle)
    )
  })
})

const selected = computed<Migration | undefined>(() =>
  migrations.items.find((migration) => migration.id === selectedId.value),
)

onMounted(async () => {
  await migrations.fetchAll()
  if (migrations.hasActive) {
    migrations.startPolling()
  }
})

onUnmounted(() => {
  migrations.stopPolling()
})

watch(
  () => migrations.hasActive,
  (active) => {
    if (active) {
      migrations.startPolling()
    } else {
      migrations.stopPolling()
    }
  },
)

async function refresh(): Promise<void> {
  await migrations.fetchAll()
}

function openDetail(migration: Migration): void {
  selectedId.value = migration.id
  drawerOpen.value = true
}

function statusLabel(status: string): string {
  return status.charAt(0).toUpperCase() + status.slice(1)
}
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>Migrations</h2>
        <p class="subtitle">
          {{ migrations.active.length }} active · last refreshed
          {{ formatDateTime(migrations.lastUpdated?.toISOString()) }}
        </p>
      </div>
      <div class="page-actions">
        <el-tag v-if="migrations.hasActive" type="primary" effect="light" round>
          Polling every 2s
        </el-tag>
        <el-button :icon="Refresh" :loading="migrations.loading" @click="refresh">Refresh</el-button>
      </div>
    </div>

    <el-alert
      v-if="migrations.error"
      class="fleet-alert"
      type="error"
      :closable="false"
      show-icon
      :title="migrations.error"
    />

    <section class="panel">
      <div class="panel-body">
        <div class="table-toolbar" style="margin-bottom: 16px">
          <el-input
            v-model="search"
            :prefix-icon="Search"
            placeholder="Search instance, cluster, or ID"
            clearable
            style="width: 300px"
          />
          <el-select v-model="statusFilter" placeholder="All statuses" clearable style="width: 180px">
            <el-option label="Queued" value="pending" />
            <el-option label="Preflight" value="preflight" />
            <el-option label="Stopping source" value="stopping" />
            <el-option label="Transferring" value="transferring" />
            <el-option label="Starting" value="starting" />
            <el-option label="Verifying" value="verifying" />
            <el-option label="Done" value="done" />
            <el-option label="Failed" value="failed" />
          </el-select>
          <span class="muted">{{ filtered.length }} shown</span>
        </div>

        <el-table
          v-loading="migrations.loading"
          :data="filtered"
          stripe
          row-key="id"
          @row-click="openDetail"
        >
          <el-table-column label="ID" width="110">
            <template #default="{ row }">
              <span class="mono">{{ shortID(row.id) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="Instance" min-width="150">
            <template #default="{ row }">
              <strong>{{ row.instance }}</strong>
              <span class="mini-label">{{ row.instance_type }}</span>
            </template>
          </el-table-column>
          <el-table-column label="Route" min-width="160">
            <template #default="{ row }">
              <el-tag size="small" effect="plain">{{ row.source_cluster }}</el-tag>
              <span class="arrow">→</span>
              <el-tag size="small" effect="plain" type="primary">{{ row.target_cluster }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="Mode" width="100">
            <template #default="{ row }">
              {{ row.mode }}
              <el-tag v-if="row.live" size="small" type="success" effect="light">live</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="Status" width="150">
            <template #default="{ row }">
              <MigrationStatusTag :status="row.status" />
            </template>
          </el-table-column>
          <el-table-column label="Progress" min-width="190">
            <template #default="{ row }">
              <div class="migration-progress">
                <el-progress
                  v-if="row.status === 'transferring'"
                  :percentage="100"
                  :indeterminate="true"
                  :show-text="false"
                  :stroke-width="6"
                />
                <small>{{ row.progress || statusLabel(row.status) }}</small>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="Duration" width="110">
            <template #default="{ row }">
              {{ formatDuration(row.created_at, row.status === 'done' || row.status === 'failed' ? row.updated_at : null) }}
            </template>
          </el-table-column>
          <el-table-column label="Created" width="180">
            <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
          </el-table-column>
          <template #empty>
            <el-empty description="No migrations yet. Start one from an instance." />
          </template>
        </el-table>
      </div>
    </section>

    <el-drawer v-model="drawerOpen" size="520px" title="Migration detail">
      <template v-if="selected">
        <div class="drawer-section">
          <h4>Task</h4>
          <div class="detail-grid">
            <div class="detail-item">
              <div class="label">ID</div>
              <div class="value mono">{{ selected.id }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Instance</div>
              <div class="value">{{ selected.instance }} ({{ selected.instance_type }})</div>
            </div>
            <div class="detail-item">
              <div class="label">Route</div>
              <div class="value">{{ selected.source_cluster }} → {{ selected.target_cluster }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Mode</div>
              <div class="value">{{ selected.mode }}{{ selected.live ? ' · live' : '' }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Status</div>
              <div class="value"><MigrationStatusTag :status="selected.status" /></div>
            </div>
            <div class="detail-item">
              <div class="label">Created</div>
              <div class="value">{{ formatDateTime(selected.created_at) }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Updated</div>
              <div class="value">{{ formatDateTime(selected.updated_at) }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Duration</div>
              <div class="value">
                {{ formatDuration(selected.created_at, selected.status === 'done' || selected.status === 'failed' ? selected.updated_at : null) }}
              </div>
            </div>
          </div>
        </div>

        <div class="drawer-section">
          <h4>Progress</h4>
          <el-progress
            v-if="selected.status === 'transferring'"
            :percentage="100"
            :indeterminate="true"
            :show-text="false"
          />
          <p>{{ selected.progress || 'No progress details reported.' }}</p>
        </div>

        <el-alert
          v-if="selected.error"
          type="error"
          :closable="false"
          show-icon
          :title="selected.error"
        />
      </template>

      <el-empty v-else description="Select a migration" />
    </el-drawer>
  </div>
</template>

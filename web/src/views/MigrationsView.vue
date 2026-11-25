<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'

import MigrationStatusTag from '@/components/MigrationStatusTag.vue'
import type { Migration } from '@/api/types'
import { useMigrationsStore } from '@/stores/migrations'
import { errorMessage } from '@/utils/errors'
import { formatDateTime, formatDuration, shortID } from '@/utils/format'

withDefaults(defineProps<{ embedded?: boolean }>(), { embedded: false })

const migrations = useMigrationsStore()
const { t, te } = useI18n()

const search = ref('')
const statusFilter = ref('')
const drawerOpen = ref(false)
const selectedId = ref('')
const actionBusy = ref('')

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
  const key = `migrationStatus.${status}`
  return te(key) ? t(key) : status
}

// Backend stage markers that merely duplicate the status column; the
// localized status label is the user-facing text for them. Every other
// progress value ("copying disks", Incus operation percentages, ...) is real
// stage information and renders as-is.
const progressMarkers: Record<string, string> = {
  complete: 'migrationStatus.done',
  validated: 'migrations.stage.validated',
  'live migration': 'migrations.stage.live',
  'stopping source': 'migrations.stage.stopping',
  'transferring instance': 'migrations.stage.transferring',
  'verifying target': 'migrations.stage.verifying',
  'starting target': 'migrations.stage.starting',
  'removing source': 'migrations.stage.removingSource',
  'restarting source': 'migrations.stage.restartingSource',
  'restoring guest network': 'migrations.stage.restoringNetwork',
  'stop source': 'migrations.stage.stopSource',
  'copy instance': 'migrations.stage.copyInstance',
  'transfer instance': 'migrations.stage.transferInstance',
  'start target': 'migrations.stage.startTarget',
  'delete source': 'migrations.stage.deleteSource',
  'restart source': 'migrations.stage.restartSource',
}

/** Renders the Progress column value, replacing raw terminal markers. */
/** Compact month-day + time for the narrow ID column. */
function shortDate(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '—'
  }

  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hour = String(date.getHours()).padStart(2, '0')
  const minute = String(date.getMinutes()).padStart(2, '0')
  return `${month}-${day} ${hour}:${minute}`
}

function progressLabel(progress: string, status: string): string {
  const marker = progressMarkers[progress]
  if (marker) {
    return t(marker)
  }

  return progress || statusLabel(status)
}

/** Renders an instance type enum value in the active language. */
function instanceTypeLabel(type: string): string {
  return t(type === 'virtual-machine' ? 'common.virtualMachine' : 'common.container')
}

/** Renders a migration mode enum value as a short localized label. */
function modeLabel(mode: string): string {
  return t(mode === 'copy' ? 'migrate.modeCopy' : 'migrate.modeMove')
}

const TERMINAL_STATUSES: readonly string[] = ['done', 'failed', 'canceled']

function canCancel(migration: Migration): boolean {
  return !TERMINAL_STATUSES.includes(migration.status)
}

function canRetry(migration: Migration): boolean {
  return TERMINAL_STATUSES.includes(migration.status)
}

/** Cancels a running migration after a confirmation. */
async function onCancel(migration: Migration): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('migrations.cancelConfirm', { instance: migration.instance }),
      t('migrations.cancelTitle'),
      { type: 'warning', confirmButtonText: t('migrations.cancel'), cancelButtonText: t('common.cancel') },
    )
  } catch {
    return
  }

  actionBusy.value = migration.id
  try {
    await migrations.cancel(migration.id)
    ElMessage.success(t('migrations.cancelRequested'))
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    actionBusy.value = ''
  }
}

/** Resubmits a terminal migration after a confirmation. */
async function onRetry(migration: Migration): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('migrations.retryConfirm', { instance: migration.instance }),
      t('migrations.retryTitle'),
      { type: 'info', confirmButtonText: t('migrations.retry'), cancelButtonText: t('common.cancel') },
    )
  } catch {
    return
  }

  actionBusy.value = migration.id
  try {
    await migrations.retry(migration.id)
    ElMessage.success(t('migrations.retrySubmitted'))
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    actionBusy.value = ''
  }
}
</script>

<template>
  <div class="page">
    <div v-if="!embedded" class="page-header">
      <div>
        <h2>{{ t('migrations.title') }}</h2>
        <p class="subtitle">
          {{
            t('migrations.subtitle', {
              active: migrations.active.length,
              time: formatDateTime(migrations.lastUpdated?.toISOString()),
            })
          }}
        </p>
      </div>
      <div class="page-actions">
        <el-tag v-if="migrations.hasActive" type="primary" effect="light" round>
          {{ t('migrations.polling') }}
        </el-tag>
        <el-button :icon="Refresh" :loading="migrations.loading" @click="refresh">
          {{ t('common.refresh') }}
        </el-button>
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
            :placeholder="t('migrations.searchPlaceholder')"
            clearable
            style="width: 300px"
          />
          <el-select
            v-model="statusFilter"
            :placeholder="t('common.allStatuses')"
            clearable
            style="width: 180px"
          >
            <el-option :label="t('migrationStatus.pending')" value="pending" />
            <el-option :label="t('migrationStatus.preflight')" value="preflight" />
            <el-option :label="t('migrationStatus.stopping')" value="stopping" />
            <el-option :label="t('migrationStatus.transferring')" value="transferring" />
            <el-option :label="t('migrationStatus.starting')" value="starting" />
            <el-option :label="t('migrationStatus.verifying')" value="verifying" />
            <el-option :label="t('migrationStatus.done')" value="done" />
            <el-option :label="t('migrationStatus.failed')" value="failed" />
            <el-option :label="t('migrationStatus.canceled')" value="canceled" />
          </el-select>
          <span class="muted">{{ t('common.shown', { count: filtered.length }) }}</span>
        </div>

        <el-table
          v-loading="migrations.loading"
          :data="filtered"
          stripe
          row-key="id"
          @row-click="openDetail"
        >
          <el-table-column :label="t('common.id')" width="105">
            <template #default="{ row }">
              <span class="mono">{{ shortID(row.id) }}</span>
              <span class="mini-label">{{ shortDate(row.created_at) }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.instance')" min-width="135">
            <template #default="{ row }">
              <strong>{{ row.instance }}</strong>
              <span class="mini-label">{{ instanceTypeLabel(row.instance_type) }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.route')" min-width="135">
            <template #default="{ row }">
              <el-tag size="small" effect="plain">{{ row.source_cluster }}</el-tag>
              <span class="arrow">→</span>
              <el-tag size="small" effect="plain" type="primary">{{ row.target_cluster }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.mode')" width="85">
            <template #default="{ row }">
              {{ modeLabel(row.mode) }}
              <el-tag v-if="row.live" size="small" type="success" effect="light">
                {{ t('common.live') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="120">
            <template #default="{ row }">
              <MigrationStatusTag :status="row.status" />
            </template>
          </el-table-column>
          <el-table-column :label="t('common.progress')" min-width="150">
            <template #default="{ row }">
              <div class="migration-progress">
                <el-progress
                  v-if="row.status === 'transferring'"
                  :percentage="100"
                  :indeterminate="true"
                  :show-text="false"
                  :stroke-width="6"
                />
                <small>{{ progressLabel(row.progress, row.status) }}</small>
                <small class="migration-duration">
                  {{ formatDuration(row.created_at, row.status === 'done' || row.status === 'failed' ? row.updated_at : null) }}
                </small>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="110">
            <template #default="{ row }">
              <el-button
                v-if="canCancel(row)"
                size="small"
                type="warning"
                plain
                :loading="actionBusy === row.id"
                @click.stop="onCancel(row)"
              >
                {{ t('migrations.cancel') }}
              </el-button>
              <el-button
                v-if="canRetry(row)"
                size="small"
                type="primary"
                plain
                :loading="actionBusy === row.id"
                @click.stop="onRetry(row)"
              >
                {{ t('migrations.retry') }}
              </el-button>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="t('migrations.empty')" />
          </template>
        </el-table>
      </div>
    </section>

    <el-drawer v-model="drawerOpen" size="520px" :title="t('migrations.detailTitle')">
      <template v-if="selected">
        <div class="drawer-section">
          <h4>{{ t('migrations.task') }}</h4>
          <div class="detail-grid">
            <div class="detail-item">
              <div class="label">{{ t('common.id') }}</div>
              <div class="value mono">{{ selected.id }}</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('common.instance') }}</div>
              <div class="value">{{ selected.instance }} ({{ instanceTypeLabel(selected.instance_type) }})</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('common.route') }}</div>
              <div class="value">{{ selected.source_cluster }} → {{ selected.target_cluster }}</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('common.mode') }}</div>
              <div class="value">
                {{ modeLabel(selected.mode) }}{{ selected.live ? ` · ${t('common.live')}` : '' }}
              </div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('common.status') }}</div>
              <div class="value"><MigrationStatusTag :status="selected.status" /></div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('common.created') }}</div>
              <div class="value">{{ formatDateTime(selected.created_at) }}</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('common.updated') }}</div>
              <div class="value">{{ formatDateTime(selected.updated_at) }}</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('common.duration') }}</div>
              <div class="value">
                {{ formatDuration(selected.created_at, selected.status === 'done' || selected.status === 'failed' ? selected.updated_at : null) }}
              </div>
            </div>
          </div>
        </div>

        <div class="drawer-section">
          <h4>{{ t('common.progress') }}</h4>
          <el-progress
            v-if="selected.status === 'transferring'"
            :percentage="100"
            :indeterminate="true"
            :show-text="false"
          />
          <p>{{ selected.progress ? progressLabel(selected.progress, selected.status) : t('migrations.noProgress') }}</p>
        </div>

        <el-alert
          v-if="selected.error"
          type="error"
          :closable="false"
          show-icon
          :title="selected.error"
        />
      </template>

      <el-empty v-else :description="t('migrations.select')" />
    </el-drawer>
  </div>
</template>

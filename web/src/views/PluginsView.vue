<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { PluginInstallRequest } from '@/api/types'
import PluginInstallWizard from '@/components/PluginInstallWizard.vue'
import PluginStateBlock from '@/components/PluginStateBlock.vue'
import PluginTaskPanel from '@/components/PluginTaskPanel.vue'
import { useVisiblePolling } from '@/composables/useInstanceActions'
import { pluginDot, pluginStatus } from '@/plugins/model'
import { useAuthStore } from '@/stores/auth'
import { useClustersStore } from '@/stores/clusters'
import { usePluginsStore } from '@/stores/plugins'

const { t } = useI18n()
const router = useRouter()
const plugins = usePluginsStore()
const clusters = useClustersStore()
const auth = useAuthStore()

type View = 'installed' | 'catalog'
const view = ref<View>('installed')
const wizardVisible = ref(false)
const wizardSelectedId = ref('')

const summary = computed(() => {
  if (plugins.loading && !plugins.loaded) return t('plugins.loading')
  if (!plugins.enabled) return t('plugins.disabled')
  if (plugins.count === 0) return t('plugins.emptySummary')
  return t('plugins.summary', { count: plugins.count })
})

function pageState(): 'forbidden' | 'loading' | 'error' | 'empty' | 'ready' {
  if (!auth.canAdmin) return 'forbidden'
  if (plugins.loading && !plugins.loaded) return 'loading'
  if (plugins.error) return 'error'
  if (view.value === 'installed' && !plugins.hasPlugins) return 'empty'
  if (view.value === 'catalog' && plugins.catalog.length === 0) return 'empty'
  return 'ready'
}

function openWizard(id = ''): void {
  wizardSelectedId.value = id || plugins.catalog[0]?.id || ''
  wizardVisible.value = true
}

async function install(request: PluginInstallRequest): Promise<void> {
  wizardVisible.value = false
  await plugins.startInstall(request)
}

function openDetail(id: string): void {
  void router.push({ name: 'plugin-detail', params: { id } })
}

const blockState = computed(() => pageState())
const stateBlock = computed<'loading' | 'error' | 'empty' | 'forbidden'>(() => {
  const value = pageState()
  return value === 'ready' ? 'empty' : value
})

onMounted(async () => {
  // Cluster data only drives the placement selector; a failure there must not
  // hide the catalog or the installed list.
  void clusters.fetchAll()
  await plugins.fetch(true)
})

// A submitted task id is the source of truth for progress. Polling is a no-op
// until a task exists and stops touching the server after a terminal state.
useVisiblePolling(() => plugins.refreshTask(), 3000)
</script>

<template>
  <section class="plugins">
    <!-- One status summary and exactly one primary action for the whole view. -->
    <header class="plugins-header">
      <div>
        <h2>{{ t('plugins.title') }}</h2>
        <p class="summary">{{ summary }}</p>
      </div>
      <div class="header-actions">
        <el-button :icon="Refresh" text :loading="plugins.loading" @click="plugins.fetch(true)">
          {{ t('common.refresh') }}
        </el-button>
        <el-button type="primary" @click="openWizard()">{{ t('plugins.installAction') }}</el-button>
      </div>
    </header>

    <el-tabs v-model="view" class="view-tabs">
      <el-tab-pane :label="t('plugins.installedTab')" name="installed" />
      <el-tab-pane :label="t('plugins.catalogTab')" name="catalog" />
    </el-tabs>

    <PluginTaskPanel
      v-if="plugins.task"
      :task="plugins.task"
      @retry="plugins.retryInstall()"
      @cancel="plugins.cancelTask()"
      @dismiss="plugins.dismissTask()"
    />

    <PluginStateBlock
      v-if="blockState !== 'ready'"
      :state="stateBlock"
      :heading="blockState === 'error' ? t('plugins.errorTitle') : ''"
      :message="blockState === 'error' ? plugins.error : blockState === 'empty' ? t(view === 'installed' ? 'plugins.emptyTitle' : 'plugins.catalogEmpty') : ''"
      :action-label="blockState === 'empty' && view === 'installed' ? t('plugins.browseCatalog') : blockState === 'error' ? t('plugins.state.retry') : ''"
      @action="blockState === 'error' ? plugins.fetch(true) : view = 'catalog'"
    />

    <el-table
      v-else-if="view === 'installed'"
      :data="plugins.installed"
      class="plugin-table"
      row-key="id"
      @row-click="(row: { id: string }) => openDetail(row.id)"
    >
      <el-table-column :label="t('plugins.columnStatus')" width="150">
        <template #default="{ row }">
          <span class="status-chip">
            <span class="dot" :class="pluginDot(pluginStatus(row).status)" />
            {{ t(pluginStatus(row).labelKey) }}
          </span>
        </template>
      </el-table-column>
      <el-table-column prop="name" :label="t('plugins.columnName')" min-width="170" />
      <el-table-column :label="t('plugins.columnVersion')" width="110">
        <template #default="{ row }">{{ row.version || '—' }}</template>
      </el-table-column>
      <el-table-column :label="t('plugins.columnCluster')" width="130">
        <template #default="{ row }">{{ row.cluster || '—' }}</template>
      </el-table-column>
      <el-table-column :label="t('plugins.columnHealth')" min-width="220">
        <template #default="{ row }">
          <span class="muted">{{ pluginStatus(row).detail }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('plugins.columnWiring')" min-width="180">
        <template #default="{ row }">
          <span class="muted">
            {{ row.wiring?.length ? t('plugins.wiringOverrides', { count: row.wiring.length }) : (row.upstream || '—') }}
          </span>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.actions')" width="110">
        <template #default="{ row }">
          <el-button link @click.stop="openDetail(row.id)">
            {{ t('plugins.details') }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <div v-else class="catalog-grid">
      <article
        v-for="entry in plugins.catalog"
        :key="entry.id"
        class="catalog-card"
        @click="openWizard(entry.id)"
      >
        <header>
          <h3>{{ entry.name }}</h3>
          <span class="tier-chip">{{ t(entry.tier === 'community' ? 'plugins.tierCommunity' : 'plugins.tierOfficial') }}</span>
        </header>
        <p class="card-value">{{ entry.value || entry.description }}</p>
        <p class="muted">{{ t('plugins.cardPublisher', { name: entry.publisher || t('plugins.tierCommunity') }) }}</p>
        <footer>
          <span class="muted">{{ t('plugins.cardPermissions', { count: entry.permissions ?? 0 }) }}</span>
        </footer>
      </article>
    </div>

    <PluginInstallWizard
      v-if="wizardVisible"
      :visible="wizardVisible"
      :catalog="plugins.catalog"
      :selected-id="wizardSelectedId"
      :clusters="clusters.items"
      :loading="plugins.loading"
      @close="wizardVisible = false"
      @submit="install"
    />
  </section>
</template>

<style scoped>
.plugins { display: flex; flex-direction: column; gap: 16px; }
.plugins-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.plugins-header h2 { margin: 0 0 4px; font-size: 18px; }
.summary { margin: 0; color: var(--el-text-color-secondary); font-size: 13px; }
.header-actions { display: flex; gap: 8px; }
.view-tabs { margin-bottom: -8px; }
.plugin-table { cursor: pointer; }
.status-chip { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: var(--el-text-color-placeholder); }
.dot.is-success { background: var(--el-color-success); }
.dot.is-warning { background: var(--el-color-warning); }
.dot.is-danger { background: var(--el-color-danger); }
.dot.is-info { background: var(--el-color-primary); }
.muted { color: var(--el-text-color-secondary); font-size: 13px; }
.catalog-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 12px; }
.catalog-card {
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  cursor: pointer;
}
.catalog-card:hover { border-color: var(--el-color-primary-light-5); }
.catalog-card header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.catalog-card h3 { margin: 0; font-size: 14px; }
.tier-chip { font-size: 11px; padding: 1px 6px; border-radius: 4px; background: var(--el-fill-color-light); color: var(--el-text-color-secondary); }
.card-value { margin: 0; font-size: 13px; color: var(--el-text-color-secondary); }
.catalog-card footer { margin-top: auto; }
</style>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Connection, Refresh, Setting, WarningFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import type { PluginCatalogEntry, PluginEntry } from '@/api/types'
import { pluginStatus, usePluginsStore } from '@/stores/plugins'
import type { PluginStatus } from '@/stores/plugins'

const plugins = usePluginsStore()
const { t } = useI18n()

/** Which top-level view is showing: the catalogue or what is already installed. */
type View = 'installed' | 'catalog'
const view = ref<View>('installed')
const selectedId = ref('')
const detailTab = ref('overview')

const selected = computed<PluginEntry | undefined>(() => plugins.byId(selectedId.value))
const installTarget = ref<PluginCatalogEntry | null>(null)
const upstream = ref('')
const installedIDs = computed(() => new Set(plugins.installed.map((entry) => entry.id)))

function openInstall(entry: PluginCatalogEntry): void {
  installTarget.value = entry
  upstream.value = entry.upstream_hint
}

async function confirmInstall(): Promise<void> {
  const target = installTarget.value
  if (!target || !upstream.value.trim()) {
    return
  }

  const token = await plugins.install(target.id, upstream.value.trim())
  if (token) {
    ElMessage.success(t('plugins.installed'))
    installTarget.value = null
  }
}

const statusLabels: Record<PluginStatus, string> = {
  installing: 'Installing',
  healthy: 'Healthy',
  degraded: 'Degraded',
  stopped: 'Stopped',
  failed: 'Failed',
  unknown: 'Unknown',
}

const statusDot: Record<PluginStatus, string> = {
  installing: 'is-info',
  healthy: 'is-success',
  degraded: 'is-warning',
  stopped: 'is-muted',
  failed: 'is-danger',
  unknown: 'is-muted',
}

/** Detail tabs, in the fixed order the specification requires. */
const detailTabs = [
  { key: 'overview', label: 'Overview' },
  { key: 'configuration', label: 'Configuration' },
  { key: 'wiring', label: 'Wiring' },
  { key: 'events', label: 'Events' },
  { key: 'danger', label: 'Danger zone' },
] as const

onMounted(async () => {
  await plugins.fetch(true)
})

function openDetail(entry: PluginEntry): void {
  selectedId.value = entry.id
  detailTab.value = 'overview'
}

function closeDetail(): void {
  selectedId.value = ''
}
</script>

<template>
  <section class="plugins">
    <!-- Header: one status summary and exactly one primary action. -->
    <header class="plugins-header">
      <div>
        <h2>{{ t('plugins.title') }}</h2>
        <p class="summary">
          <template v-if="plugins.loading">{{ t('plugins.loading') }}</template>
          <template v-else-if="!plugins.enabled">{{ t('plugins.disabled') }}</template>
          <template v-else-if="plugins.count === 0">{{ t('plugins.emptySummary') }}</template>
          <template v-else>
            {{ t('plugins.summary', { count: plugins.count }) }}
          </template>
        </p>
      </div>
      <div class="header-actions">
        <el-button :icon="Refresh" :loading="plugins.loading" @click="plugins.fetch(true)">
          {{ t('common.refresh') }}
        </el-button>
      </div>
    </header>

    <!-- Two top-level views, never one mixed list. -->
    <el-tabs v-model="view" class="view-tabs">
      <el-tab-pane :label="t('plugins.installedTab')" name="installed" />
      <el-tab-pane :label="t('plugins.catalogTab')" name="catalog" />
    </el-tabs>

    <!-- Error state: what failed, and what to do next. -->
    <el-alert
      v-if="plugins.error"
      type="error"
      :closable="false"
      show-icon
      class="state-block"
      :title="t('plugins.errorTitle')"
      :description="plugins.error"
    />

    <!-- Empty state: one next action. In the catalog the action would point at
         itself, so that view only explains how entries get registered. -->
    <div v-else-if="view === 'installed' && !plugins.loading && !plugins.hasPlugins" class="state-block empty">
      <el-icon class="empty-icon"><Connection /></el-icon>
      <h3>{{ t('plugins.emptyTitle') }}</h3>
      <p>{{ t('plugins.emptyBody') }}</p>
      <el-button type="primary" @click="view = 'catalog'">
        {{ t('plugins.browseCatalog') }}
      </el-button>
    </div>

    <!-- Content: table for installed, cards for the catalogue. -->
    <template v-else>
      <el-table
        v-if="view === 'installed'"
        :data="plugins.installed"
        class="plugin-table"
        row-key="id"
        @row-click="openDetail"
      >
        <el-table-column :label="t('plugins.columnStatus')" width="140">
          <template #default="{ row }">
            <span class="status-chip">
              <span class="dot" :class="statusDot[pluginStatus(row).status]" />
              {{ statusLabels[pluginStatus(row).status] }}
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="name" :label="t('plugins.columnName')" min-width="180" />
        <el-table-column prop="version" :label="t('plugins.columnVersion')" width="120">
          <template #default="{ row }">{{ row.version || '—' }}</template>
        </el-table-column>
        <el-table-column prop="role" :label="t('plugins.columnRole')" width="110" />
        <el-table-column :label="t('plugins.columnWiring')" min-width="200">
          <template #default="{ row }">
            <span class="muted">{{ pluginStatus(row).detail }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="190">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click.stop="plugins.toggle(row.id, !row.enabled)">
              {{ row.enabled ? t('plugins.disable') : t('plugins.enable') }}
            </el-button>
            <el-button link type="danger" size="small" @click.stop="plugins.uninstall(row.id)">
              {{ t('plugins.uninstall') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div v-else class="catalog-grid">
        <article v-for="entry in plugins.catalog" :key="entry.id" class="catalog-card">
          <header>
            <h3>{{ entry.name }}</h3>
            <span class="tier-chip">{{ entry.wiring }}</span>
          </header>
          <p class="card-body">{{ entry.description }}</p>
          <p class="muted">{{ t('plugins.upstream') }}: {{ entry.upstream_hint }}</p>
          <footer>
            <el-button
              size="small"
              type="primary"
              :disabled="installedIDs.has(entry.id)"
              @click="openInstall(entry)"
            >
              {{ installedIDs.has(entry.id) ? t('plugins.installed') : t('plugins.install') }}
            </el-button>
          </footer>
        </article>
        <p v-if="plugins.catalog.length === 0" class="muted">{{ t('plugins.catalogEmpty') }}</p>
      </div>
    </template>

    <!-- Detail drawer: fixed five-tab order. -->
    <el-drawer
      :model-value="selected !== undefined"
      :title="selected?.name || selected?.id"
      size="520px"
      @close="closeDetail"
    >
      <template v-if="selected">
        <el-tabs v-model="detailTab">
          <el-tab-pane
            v-for="tab in detailTabs"
            :key="tab.key"
            :label="tab.label"
            :name="tab.key"
          >
            <div v-if="tab.key === 'overview'" class="detail-block">
              <dl>
                <dt>{{ t('plugins.columnStatus') }}</dt>
                <dd>
                  <span class="status-chip">
                    <span class="dot" :class="statusDot[pluginStatus(selected).status]" />
                    {{ statusLabels[pluginStatus(selected).status] }}
                  </span>
                  <span class="muted"> — {{ pluginStatus(selected).detail }}</span>
                </dd>
                <dt>{{ t('plugins.columnVersion') }}</dt>
                <dd>{{ selected.version || '—' }}</dd>
                <dt>{{ t('plugins.columnRole') }}</dt>
                <dd>{{ selected.role }}</dd>
                <dt>{{ t('plugins.upstream') }}</dt>
                <dd>{{ selected.upstream || '—' }}</dd>
              </dl>
            </div>

            <div v-else-if="tab.key === 'configuration'" class="detail-block">
              <p class="muted">{{ t('plugins.configurationHint') }}</p>
            </div>

            <div v-else-if="tab.key === 'wiring'" class="detail-block">
              <p class="muted">{{ t('plugins.wiringHint') }}</p>
            </div>

            <div v-else-if="tab.key === 'events'" class="detail-block">
              <p class="muted">{{ t('plugins.eventsHint') }}</p>
            </div>

            <div v-else class="detail-block danger">
              <el-icon class="danger-icon"><WarningFilled /></el-icon>
              <p>{{ t('plugins.dangerHint') }}</p>
            </div>
          </el-tab-pane>
        </el-tabs>

        <footer class="drawer-footer">
          <el-button :icon="Setting">{{ t('plugins.configure') }}</el-button>
        </footer>
      </template>
    </el-drawer>

    <el-dialog
      :model-value="installTarget !== null"
      :title="t('plugins.installTitle', { name: installTarget?.name ?? '' })"
      width="460px"
      @update:model-value="installTarget = null"
    >
      <p class="muted">{{ t('plugins.installHint') }}</p>
      <el-form label-position="top" @submit.prevent="confirmInstall">
        <el-form-item :label="t('plugins.upstream')">
          <el-input v-model="upstream" :placeholder="installTarget?.upstream_hint" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="installTarget = null">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :disabled="!upstream.trim()" @click="confirmInstall">
          {{ t('plugins.install') }}
        </el-button>
      </template>
    </el-dialog>
  </section>
</template>

<style scoped>
.plugins { display: flex; flex-direction: column; gap: 16px; }
.plugins-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.plugins-header h2 { margin: 0 0 4px; font-size: 18px; }
.summary { margin: 0; color: var(--el-text-color-secondary); font-size: 13px; }
.header-actions { display: flex; gap: 8px; }
.view-tabs { margin-bottom: -8px; }
.state-block { margin-top: 8px; }
.empty { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 48px 16px; text-align: center; }
.empty-icon { font-size: 32px; color: var(--el-text-color-placeholder); }
.empty h3 { margin: 0; font-size: 15px; }
/* Cap the measure so the explanation stays readable instead of spanning the
   full content width. */
.empty p { margin: 0 0 8px; color: var(--el-text-color-secondary); font-size: 13px; max-width: 46ch; line-height: 1.55; }
.plugin-table { cursor: pointer; }
.status-chip { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: var(--el-text-color-placeholder); }
.dot.is-success { background: var(--el-color-success); }
.dot.is-warning { background: var(--el-color-warning); }
.dot.is-danger { background: var(--el-color-danger); }
.dot.is-info { background: var(--el-color-primary); }
.dot.is-muted { background: var(--el-text-color-placeholder); }
.muted { color: var(--el-text-color-secondary); font-size: 13px; }
.catalog-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 12px; }
.catalog-card { border: 1px solid var(--el-border-color-light); border-radius: 8px; padding: 14px; display: flex; flex-direction: column; gap: 8px; }
.catalog-card header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.catalog-card h3 { margin: 0; font-size: 14px; }
.tier-chip { font-size: 11px; padding: 1px 6px; border-radius: 4px; background: var(--el-fill-color-light); color: var(--el-text-color-secondary); }
.card-body { margin: 0; font-size: 13px; color: var(--el-text-color-secondary); word-break: break-all; }
.catalog-card footer { margin-top: auto; }
.detail-block dl { display: grid; grid-template-columns: 120px 1fr; gap: 8px 12px; margin: 0; font-size: 13px; }
.detail-block dt { color: var(--el-text-color-secondary); }
.detail-block dd { margin: 0; }
.danger { display: flex; gap: 8px; align-items: flex-start; }
.danger-icon { color: var(--el-color-danger); margin-top: 2px; }
.drawer-footer { margin-top: 16px; display: flex; justify-content: flex-end; }
</style>

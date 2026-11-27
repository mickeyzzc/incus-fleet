<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'

import PluginStateBlock from '@/components/PluginStateBlock.vue'
import PluginTaskPanel from '@/components/PluginTaskPanel.vue'
import { useVisiblePolling } from '@/composables/useInstanceActions'
import { pluginIcon } from '@/plugins/icons'
import { pluginDot, pluginStatus } from '@/plugins/model'
import { useAuthStore } from '@/stores/auth'
import { usePluginsStore } from '@/stores/plugins'

const props = defineProps<{ id: string }>()

const { t } = useI18n()
const router = useRouter()
const auth = useAuthStore()
const plugins = usePluginsStore()

const detailTab = ref('overview')
const configValues = reactive<Record<string, string>>({})
const typedName = ref('')
const configError = ref('')
const wiringError = ref('')

const entry = computed(() => plugins.byId(props.id))
const status = computed(() => entry.value ? pluginStatus(entry.value) : null)
/** Older descriptors omit these fields; the list entry is still shown. */
const partialDescriptor = computed(() => Boolean(entry.value) && (
  !entry.value?.config || !entry.value?.wiring
  || !entry.value?.events || !entry.value?.health_history
))
const icon = computed(() => pluginIcon(entry.value?.ui.icon))
const navPath = computed(() => entry.value?.ui.path ?? '')
const canUninstall = computed(() => Boolean(entry.value) && typedName.value === entry.value?.name)
const tabs = [
  { key: 'overview', labelKey: 'plugins.detail.tabOverview' },
  { key: 'configuration', labelKey: 'plugins.detail.tabConfiguration' },
  { key: 'wiring', labelKey: 'plugins.detail.tabWiring' },
  { key: 'events', labelKey: 'plugins.detail.tabEvents' },
  { key: 'danger', labelKey: 'plugins.detail.tabDanger' },
] as const

function state(): 'forbidden' | 'loading' | 'error' | 'empty' | 'ready' {
  if (!auth.canAdmin) return 'forbidden'
  if (plugins.loading && !plugins.loaded) return 'loading'
  // An action failure is shown next to its tab; only a load failure that
  // leaves no descriptor replaces the whole page.
  if ((plugins.error || plugins.detailError) && !entry.value) return 'error'
  if (!entry.value) return 'empty'
  return 'ready'
}

const blockState = computed(() => state())
const stateBlock = computed<'loading' | 'error' | 'empty' | 'forbidden'>(() => {
  const value = state()
  return value === 'ready' ? 'empty' : value
})

watch(entry, (value) => {
  typedName.value = ''
  configError.value = ''
  wiringError.value = ''
  for (const key of Object.keys(configValues)) {
    delete configValues[key]
  }
  if (value?.config) {
    Object.assign(configValues, value.config)
  }
}, { immediate: true })

watch(() => props.id, (id) => {
  void plugins.fetchDetail(id)
})

onMounted(async () => {
  await plugins.fetch()
  if (!plugins.error) {
    await plugins.fetchDetail(props.id)
  }
})

// Poll the active install task by id while this page is visible.
useVisiblePolling(() => plugins.refreshTask(), 3000)

function toggle(): void {
  if (entry.value) {
    void plugins.toggle(entry.value.id, !entry.value.enabled)
  }
}

function reinstall(): void {
  if (entry.value) {
    void plugins.startInstall({ id: entry.value.id })
    detailTab.value = 'overview'
  }
}

async function uninstall(): Promise<void> {
  if (!canUninstall.value || !entry.value) {
    return
  }
  await plugins.uninstall(entry.value.id)
  if (!plugins.error) {
    await router.push({ name: 'plugins' })
  }
}

async function saveConfig(): Promise<void> {
  if (!entry.value) return
  configError.value = ''
  const ok = await plugins.saveConfig(entry.value.id, { ...configValues })
  if (!ok) {
    configError.value = `${t('plugins.detail.configError')} · ${plugins.error}`
  } else {
    await plugins.fetchDetail(entry.value.id)
  }
}

async function restoreWiring(key: string): Promise<void> {
  if (!entry.value) return
  wiringError.value = ''
  const ok = await plugins.restoreWiring(entry.value.id, key)
  if (!ok) {
    wiringError.value = `${t('plugins.detail.wiringError')} · ${plugins.error}`
  } else {
    await plugins.fetchDetail(entry.value.id)
  }
}

function statusText(value: string): string {
  return t(`plugins.status.${value}`)
}
</script>

<template>
  <section class="plugin-detail">
    <header class="detail-header">
      <div class="detail-identity">
        <span class="plugin-icon"><el-icon><component :is="icon" /></el-icon></span>
        <div>
          <h2>{{ entry?.name ?? t('plugins.title') }}</h2>
          <p class="muted">
            {{ entry?.publisher ?? entry?.kind }} · {{ entry?.version || '—' }}
          </p>
        </div>
      </div>
      <router-link :to="{ name: 'plugins' }" class="back-link">
        {{ t('plugins.detail.backToList') }}
      </router-link>
    </header>

    <PluginStateBlock
      v-if="blockState !== 'ready'"
      :state="stateBlock"
      :message="blockState === 'error' ? (plugins.error || plugins.detailError) : blockState === 'empty' ? t('plugins.detail.notFound') : ''"
      :action-label="blockState === 'error' ? t('plugins.state.retry') : blockState === 'empty' ? t('plugins.detail.backToList') : ''"
      @action="blockState === 'error' ? plugins.fetch(true) : router.push({ name: 'plugins' })"
    />

    <template v-else-if="entry && status">
      <div class="detail-status">
        <span class="status-chip">
          <span class="dot" :class="pluginDot(status.status)" />
          {{ t(status.labelKey) }}
        </span>
        <span class="muted">— {{ status.detail }}</span>
      </div>
      <p v-if="partialDescriptor" class="muted descriptor-note" role="status">
        {{ t('plugins.detail.partialDescriptor') }}
      </p>

      <el-tabs v-model="detailTab" class="detail-tabs">
        <el-tab-pane
          v-for="tab in tabs"
          :key="tab.key"
          :label="t(tab.labelKey)"
          :name="tab.key"
        >
          <div v-if="tab.key === 'overview'" class="detail-block">
            <dl class="overview-list">
              <dt>{{ t('plugins.detail.status') }}</dt>
              <dd>{{ statusText(status.status) }}</dd>
              <dt>{{ t('plugins.detail.version') }}</dt>
              <dd>{{ entry.version || '—' }}</dd>
              <dt>{{ t('plugins.detail.cluster') }}</dt>
              <dd>{{ entry.cluster || '—' }}</dd>
              <dt>{{ t('plugins.detail.instance') }}</dt>
              <dd>
                <router-link
                  v-if="entry.instance"
                  :to="{ path: '/instances', query: { cluster: entry.cluster ?? '', instance: entry.instance } }"
                >
                  {{ entry.instance }}
                </router-link>
                <span v-else>—</span>
              </dd>
            </dl>

            <h4>{{ t('plugins.detail.healthHistory') }}</h4>
            <ul v-if="entry.health_history?.length" class="health-list">
              <li v-for="(sample, index) in entry.health_history" :key="index">
                <span class="dot" :class="pluginDot(sample.status)" />
                <span>{{ statusText(sample.status) }}</span>
                <span class="muted">{{ sample.at }}{{ sample.detail ? ` · ${sample.detail}` : '' }}</span>
              </li>
            </ul>
            <p v-else class="muted">{{ t('plugins.detail.noHealth') }}</p>

            <div class="overview-actions">
              <router-link v-if="navPath" :to="navPath" class="el-button el-button--primary">
                {{ t('plugins.detail.openUI') }}
              </router-link>
              <el-button @click="toggle">
                {{ entry.enabled ? t('plugins.detail.stop') : t('plugins.detail.start') }}
              </el-button>
              <el-button @click="reinstall">{{ t('plugins.detail.reinstall') }}</el-button>
            </div>

            <PluginTaskPanel
              v-if="plugins.task && plugins.task.pluginId === entry.id"
              :task="plugins.task"
              class="detail-task"
              @retry="plugins.retryInstall()"
              @cancel="plugins.cancelTask()"
              @dismiss="plugins.dismissTask()"
            />
          </div>

          <div v-else-if="tab.key === 'configuration'" class="detail-block detail-config">
            <p class="muted">{{ t('plugins.configurationHint') }}</p>
            <p v-if="configError" class="action-error" role="alert">{{ configError }}</p>
            <template v-if="Object.keys(configValues).length > 0">
              <label v-for="key in Object.keys(configValues)" :key="key" class="config-field">
                <span>{{ key }}</span>
                <el-input v-model="configValues[key]" />
              </label>
              <el-button type="primary" :loading="plugins.loading" @click="saveConfig">
                {{ t('plugins.detail.save') }}
              </el-button>
            </template>
            <p v-else class="muted">{{ t('plugins.detail.noConfig') }}</p>
          </div>

          <div v-else-if="tab.key === 'wiring'" class="detail-block">
            <p class="muted">{{ t('plugins.wiringHint') }}</p>
            <p v-if="wiringError" class="action-error" role="alert">{{ wiringError }}</p>
            <table v-if="entry.wiring?.length" class="wiring-table">
              <thead>
                <tr>
                  <th>{{ t('plugins.detail.capability') }}</th>
                  <th>{{ t('plugins.detail.currentTarget') }}</th>
                  <th>{{ t('plugins.detail.defaultTarget') }}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                <tr v-for="wire in entry.wiring" :key="wire.key ?? wire.capability">
                  <td>
                    <strong>{{ wire.capability }}</strong>
                    <small v-if="wire.key" class="muted">{{ wire.key }}</small>
                  </td>
                  <td>{{ wire.target }}</td>
                  <td class="muted">{{ wire.default_target }}</td>
                  <td>
                    <el-button link type="primary" @click="restoreWiring(wire.key ?? wire.capability)">
                      {{ t('plugins.detail.restoreDefault') }}
                    </el-button>
                  </td>
                </tr>
              </tbody>
            </table>
            <p v-else class="muted">{{ t('plugins.detail.noWiring') }}</p>
          </div>

          <div v-else-if="tab.key === 'events'" class="detail-block">
            <ul v-if="entry.events?.length" class="event-list">
              <li v-for="(event, index) in entry.events" :key="index">
                <span class="event-kind" :class="event.kind">
                  {{ t(event.kind === 'error' ? 'plugins.detail.errorEvent' : 'plugins.detail.delivery') }}
                </span>
                <span>{{ event.message }}</span>
                <span class="muted">{{ event.at }}</span>
              </li>
            </ul>
            <p v-else class="muted">{{ t('plugins.detail.noEvents') }}</p>
          </div>

          <div v-else class="detail-block danger-zone">
            <h4>{{ t('plugins.detail.dangerTitle', { name: entry.name }) }}</h4>
            <p>{{ t('plugins.detail.dangerBody') }}</p>
            <label class="danger-confirm">
              <span>{{ t('plugins.detail.typedLabel') }}</span>
              <el-input v-model="typedName" :placeholder="t('plugins.detail.typedHint', { name: entry.name })" />
              <small v-if="typedName && !canUninstall" class="danger-hint">{{ t('plugins.detail.typedMismatch') }}</small>
            </label>
            <el-button type="danger" :disabled="!canUninstall" @click="uninstall">
              {{ t('plugins.detail.uninstallButton') }}
            </el-button>
          </div>
        </el-tab-pane>
      </el-tabs>
    </template>
  </section>
</template>

<style scoped>
.plugin-detail { display: flex; flex-direction: column; gap: 12px; }
.detail-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.detail-identity { display: flex; align-items: center; gap: 12px; }
.plugin-icon { font-size: 24px; color: var(--el-color-primary); }
.detail-header h2 { margin: 0; font-size: 18px; }
.back-link { font-size: 13px; }
.detail-status { display: flex; align-items: center; gap: 6px; font-size: 13px; }
.status-chip { display: inline-flex; align-items: center; gap: 6px; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: var(--el-text-color-placeholder); }
.dot.is-success { background: var(--el-color-success); }
.dot.is-warning { background: var(--el-color-warning); }
.dot.is-danger { background: var(--el-color-danger); }
.dot.is-info { background: var(--el-color-primary); }
.muted { color: var(--el-text-color-secondary); font-size: 13px; }
.detail-block { padding: 8px 0; }
.overview-list { display: grid; grid-template-columns: 140px 1fr; gap: 8px 12px; margin: 0 0 16px; font-size: 13px; }
.overview-list dt { color: var(--el-text-color-secondary); }
.overview-list dd { margin: 0; }
.detail-block h4 { margin: 12px 0 8px; font-size: 14px; }
.descriptor-note { margin: 0; }
.action-error { margin: 8px 0; color: var(--el-color-danger); font-size: 13px; }
.health-list, .event-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; font-size: 13px; }
.health-list li, .event-list li { display: flex; align-items: center; gap: 8px; }
.overview-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 16px; }
.detail-task { margin-top: 16px; }
.config-field { display: grid; gap: 4px; margin-bottom: 10px; font-size: 13px; }
.wiring-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.wiring-table th, .wiring-table td { text-align: left; padding: 6px 8px; border-bottom: 1px solid var(--el-border-color-lighter); }
.wiring-table small { display: block; font-size: 11px; }
.event-kind { font-size: 11px; padding: 1px 6px; border-radius: 4px; background: var(--el-fill-color-light); }
.event-kind.error { color: var(--el-color-danger); }
.danger-zone { border: 1px solid var(--el-color-danger-light-4); border-radius: 8px; padding: 16px; }
.danger-zone h4 { color: var(--el-color-danger); margin-top: 0; }
.danger-confirm { display: grid; gap: 6px; max-width: 320px; margin: 12px 0; font-size: 13px; }
.danger-hint { color: var(--el-color-danger); font-size: 12px; }
</style>

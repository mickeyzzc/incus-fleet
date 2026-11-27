<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'

import PluginStateBlock from '@/components/PluginStateBlock.vue'
import { pluginIcon } from '@/plugins/icons'
import { useAuthStore } from '@/stores/auth'
import { usePluginsStore } from '@/stores/plugins'

const props = defineProps<{ id: string }>()

const { t } = useI18n()
const auth = useAuthStore()
const plugins = usePluginsStore()

const entry = computed(() => plugins.byId(props.id))
const icon = computed(() => pluginIcon(entry.value?.ui.icon))
const title = computed(() => {
  if (!entry.value) {
    return t('plugins.title')
  }
  const key = entry.value.ui.title_key
  if (key && t(key) !== key) {
    return t(key)
  }
  return entry.value.name || entry.value.id
})
const upstreamURL = computed(() => `/plugin-ui/${encodeURIComponent(props.id)}/`)

function state(): 'forbidden' | 'loading' | 'error' | 'empty' | 'ready' {
  if (!auth.canAdmin) return 'forbidden'
  if (plugins.loading && !plugins.loaded) return 'loading'
  if (plugins.error) return 'error'
  if (!entry.value) return 'empty'
  return 'ready'
}

const blockState = computed(() => state())
const stateBlock = computed<'loading' | 'error' | 'empty' | 'forbidden'>(() => {
  const value = state()
  return value === 'ready' ? 'empty' : value
})

onMounted(() => {
  void plugins.fetch()
})
</script>

<template>
  <section class="plugin-page">
    <template v-if="blockState !== 'ready'">
      <PluginStateBlock
        :state="stateBlock"
        :message="blockState === 'error' ? plugins.error : blockState === 'empty' ? t('plugins.detail.notFound') : ''"
        :action-label="t('plugins.state.retry')"
        @action="plugins.fetch(true)"
      />
    </template>
    <template v-else>
      <header class="plugin-page-header">
        <span class="plugin-icon"><el-icon><component :is="icon" /></el-icon></span>
        <h2>{{ title }}</h2>
      </header>
      <!-- Plugin UIs are served by fleet's same-origin proxy. An iframe keeps
           the fleet shell (and its navigation) available while the upstream
           document renders. -->
      <iframe :src="upstreamURL" class="plugin-frame" :title="title" />
    </template>
  </section>
</template>

<style scoped>
.plugin-page { display: flex; flex-direction: column; height: calc(100vh - 120px); gap: 12px; }
.plugin-page-header { display: flex; align-items: center; gap: 8px; }
.plugin-page-header h2 { margin: 0; font-size: 18px; }
.plugin-icon { font-size: 20px; color: var(--el-color-primary); }
.plugin-frame {
  flex: 1;
  width: 100%;
  min-height: 400px;
  border: 1px solid var(--el-border-color-lighter, #e5e7eb);
  border-radius: 10px;
  background: #fff;
}
</style>

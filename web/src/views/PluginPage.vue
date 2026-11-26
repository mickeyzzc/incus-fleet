<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

import { useI18n } from 'vue-i18n'

const route = useRoute()
const { t } = useI18n()

// Plugin UIs are served by fleet's same-origin proxy. Rendering them in an
// iframe keeps the fleet shell (and its navigation) available instead of
// replacing the whole document with the upstream response.
const pluginId = computed(() => String(route.params.id ?? ''))
const upstreamURL = computed(() => `/plugin-ui/${encodeURIComponent(pluginId.value)}/`)
</script>

<template>
  <section class="plugin-page">
    <header class="plugin-page-header">
      <h2>{{ t('plugins.title') }} · {{ pluginId }}</h2>
    </header>
    <iframe :src="upstreamURL" class="plugin-frame" :title="pluginId" />
  </section>
</template>

<style scoped>
.plugin-page { display: flex; flex-direction: column; height: calc(100vh - 120px); gap: 12px; }
.plugin-page-header h2 { margin: 0; font-size: 18px; }
.plugin-frame {
  flex: 1;
  width: 100%;
  min-height: 400px;
  border: 1px solid var(--el-border-color-lighter, #e5e7eb);
  border-radius: 10px;
  background: #fff;
}
</style>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Back, Box, Coin, Cpu, EditPen } from '@element-plus/icons-vue'

import HostMetricsPanel from '@/components/HostMetricsPanel.vue'
import MemberDialog from '@/components/MemberDialog.vue'
import NodeNetworkPanel from '@/components/NodeNetworkPanel.vue'
import type { ClusterMember } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import { useClustersStore } from '@/stores/clusters'
import { formatBytes } from '@/utils/format'

const props = defineProps<{
  cluster: string
  member: string
}>()

const router = useRouter()
const clusters = useClustersStore()
const auth = useAuthStore()
const { t } = useI18n()

const summary = computed(() => clusters.byName(props.cluster))
const memberInfo = computed<ClusterMember | undefined>(() =>
  summary.value?.members.find((item) => item.name === props.member),
)
const editOpen = ref(false)

/** Renders a cluster member status enum value in the active language. */
function memberStatusLabel(status: string): string {
  const key = `cluster.memberStatus.${status.toLowerCase()}`
  return t(key) === key ? status : t(key)
}

function backToCluster(): void {
  void router.push({ name: 'cluster', params: { cluster: props.cluster } })
}

function resourcePercent(used = 0, total = 0): number {
  if (!Number.isFinite(total) || total <= 0) {
    return 0
  }

  return Math.min(100, Math.max(0, (used / total) * 100))
}



function ensureCluster(): void {
  if (!clusters.byName(props.cluster)) {
    void clusters.fetchAll()
  }
}

async function memberSaved(name: string): Promise<void> {
  await clusters.fetchAll()
  if (name !== props.member) {
    await router.replace({
      name: 'node-detail',
      params: { cluster: props.cluster, member: name },
    })
  }
}

onMounted(ensureCluster)
watch(() => props.cluster, ensureCluster)
</script>

<template>
  <div class="page node-detail">
    <header class="node-header">
      <div class="node-heading">
        <el-button class="back-link" :icon="Back" text @click="backToCluster">
          {{ t('cluster.backToCluster') }}
        </el-button>
        <span class="page-eyebrow">{{ t('nodeDetail.eyebrow') }}</span>
        <div class="node-title-row">
          <h2>{{ memberInfo?.name || member }}</h2>
          <el-tag
            v-if="memberInfo"
            :type="memberInfo.status === 'Online' ? 'success' : 'info'"
            effect="plain"
            round
          >
            {{ memberStatusLabel(memberInfo.status) }}
          </el-tag>
        </div>
        <p class="subtitle">
          {{ t('nodeDetail.subtitle', { cluster: summary?.display_name || cluster }) }}
        </p>
        <p class="node-hint">{{ t('nodeDetail.hint') }}</p>
      </div>
      <div class="page-actions">
        <el-button v-if="auth.canWrite" :icon="EditPen" :disabled="!memberInfo" @click="editOpen = true">
          {{ t('admin.editMember') }}
        </el-button>
        <el-button :loading="clusters.loading" @click="clusters.fetchAll()">
          {{ t('common.refresh') }}
        </el-button>
      </div>
    </header>

    <el-alert
      v-if="clusters.error"
      class="fleet-alert"
      type="error"
      :closable="false"
      show-icon
      :title="clusters.error"
    />

    <el-alert
      v-if="!auth.canWrite"
      class="fleet-alert"
      type="warning"
      :closable="false"
      show-icon
      :title="t('nodeDetail.readOnlyHint')"
    />

    <el-alert
      v-if="!clusters.loading && !memberInfo"
      class="fleet-alert"
      type="warning"
      :closable="false"
      show-icon
      :title="t('nodeDetail.notFound', { member })"
    />

    <template v-if="memberInfo">
      <section class="panel node-facts">
        <div class="node-fact">
          <span class="label">{{ t('cluster.memberRoles') }}</span>
          <span class="value">{{ memberInfo.roles?.join(', ') || t('common.none') }}</span>
        </div>
        <div class="node-fact">
          <span class="label">{{ t('cluster.architectures') }}</span>
          <span class="value">{{ memberInfo.architecture || t('common.none') }}</span>
        </div>
        <div class="node-fact">
          <span class="label">{{ t('cluster.memberAddress') }}</span>
          <span class="value mono" :title="memberInfo.url">{{ memberInfo.url || t('common.none') }}</span>
        </div>
        <div class="node-fact">
          <span class="label">{{ t('cluster.memberMessages') }}</span>
          <span class="value">{{ memberInfo.message || t('common.none') }}</span>
        </div>
      </section>

      <section class="panel node-resource-panel">
        <div class="panel-head">
          <div>
            <span class="panel-eyebrow">{{ t('nodeDetail.resourceEyebrow') }}</span>
            <h3 class="section-title">{{ t('nodeDetail.resourceTitle') }}</h3>
            <p class="subtitle">{{ t('nodeDetail.resourceHint') }}</p>
          </div>
        </div>
        <div class="panel-body node-resource-grid">
          <article class="node-resource-item">
            <span class="resource-icon cpu"><el-icon><Cpu /></el-icon></span>
            <div class="resource-copy">
              <span class="resource-label">{{ t('common.cpu') }}</span>
              <span class="resource-value">
                {{ memberInfo.cpu?.total ?? '—' }} <small>vCPU</small>
              </span>
              <span class="resource-hint">{{ memberInfo.cpu?.architecture || t('common.none') }}</span>
            </div>
          </article>

          <article class="node-resource-item">
            <span class="resource-icon memory"><el-icon><Box /></el-icon></span>
            <div class="resource-copy">
              <span class="resource-label">{{ t('common.memory') }}</span>
              <span v-if="memberInfo.memory?.total" class="resource-value">
                {{ formatBytes(memberInfo.memory.used) }}
                <small>/ {{ formatBytes(memberInfo.memory.total) }}</small>
              </span>
              <span v-else class="resource-value">{{ t('common.none') }}</span>
              <div class="resource-bar">
                <div
                  class="resource-bar-fill memory"
                  :style="{ width: `${resourcePercent(memberInfo.memory?.used, memberInfo.memory?.total)}%` }"
                />
              </div>
            </div>
          </article>

          <article class="node-resource-item">
            <span class="resource-icon storage"><el-icon><Coin /></el-icon></span>
            <div class="resource-copy">
              <span class="resource-label">
                {{ memberInfo.storage?.shared ? t('nodeDetail.storageShared') : t('nav.fleetVolumes') }}
              </span>
              <span v-if="memberInfo.storage?.total" class="resource-value">
                {{ formatBytes(memberInfo.storage.used) }}
                <small>/ {{ formatBytes(memberInfo.storage.total) }}</small>
              </span>
              <span v-else class="resource-value">{{ t('common.none') }}</span>
              <div class="resource-bar">
                <div
                  class="resource-bar-fill storage"
                  :style="{ width: `${resourcePercent(memberInfo.storage?.used, memberInfo.storage?.total)}%` }"
                />
              </div>
            </div>
          </article>
        </div>
      </section>

      <HostMetricsPanel :cluster="cluster" :member="memberInfo.name" />

      <NodeNetworkPanel :cluster="cluster" :member="memberInfo.name" :can-write="auth.canWrite" />
    </template>
    <el-skeleton v-else-if="clusters.loading" :rows="6" animated />

    <MemberDialog
      v-model="editOpen"
      :cluster="cluster"
      :member="memberInfo ?? null"
      @saved="memberSaved"
    />
  </div>
</template>

<style scoped>
.node-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  flex-wrap: wrap;
}

.node-heading {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.back-link {
  align-self: flex-start;
  height: auto;
  padding: 0;
  color: var(--fleet-ink-soft);
}

.node-title-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.node-title-row h2 {
  font-size: 24px;
  line-height: 1.25;
  letter-spacing: -0.4px;
}

.subtitle {
  color: var(--fleet-ink-soft);
  font-size: 13px;
}

.node-hint {
  color: var(--fleet-ink-soft);
  font-size: 12.5px;
}

.node-facts {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  overflow: hidden;
}

.node-fact {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  padding: 14px 20px;
}

.node-fact + .node-fact {
  border-left: 1px solid var(--fleet-line);
}

.node-fact .label {
  color: var(--fleet-ink-soft);
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.5px;
  text-transform: uppercase;
}

.node-fact .value {
  font-size: 13.5px;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.node-fact .mono {
  font-size: 12px;
  font-weight: 500;
}

.node-fact .value.mono {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.node-resource-panel {
  overflow: hidden;
}

.node-resource-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
  gap: 14px;
}

.node-resource-item {
  display: flex;
  align-items: flex-start;
  gap: 11px;
  padding: 13px 14px;
  border: 1px solid color-mix(in srgb, var(--fleet-line) 76%, transparent);
  border-radius: 12px;
  background: color-mix(in srgb, var(--fleet-panel-muted) 72%, transparent);
}

.resource-icon {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  flex: none;
  border-radius: 10px;
  font-size: 16px;
}

.resource-icon.cpu {
  background: #eef2ff;
  color: #4562e8;
}

.resource-icon.memory {
  background: #ecfeff;
  color: #0891b2;
}

.resource-icon.storage {
  background: #ecfdf5;
  color: #059669;
}

.resource-copy {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.resource-label {
  color: var(--fleet-ink-soft);
  font-size: 10.5px;
  font-weight: 650;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.resource-value {
  font-size: 20px;
  font-weight: 720;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.3px;
  line-height: 1.15;
}

.resource-value small {
  color: var(--fleet-ink-soft);
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0;
}

.resource-hint {
  color: var(--fleet-ink-soft);
  font-size: 11.5px;
}

.resource-bar {
  height: 5px;
  overflow: hidden;
  border-radius: 99px;
  background: var(--el-fill-color);
}

.resource-bar-fill {
  height: 100%;
  border-radius: inherit;
  transition: width 0.4s ease;
}

.resource-bar-fill.memory {
  background: linear-gradient(90deg, #3b82f6, #06b6d4);
}

.resource-bar-fill.storage {
  background: linear-gradient(90deg, #10b981, #22c55e);
}

@media (prefers-color-scheme: dark) {
  .resource-icon.cpu {
    background: #1d2a4d;
    color: #8fa4ff;
  }

  .resource-icon.memory {
    background: #113640;
    color: #67d9e9;
  }

  .resource-icon.storage {
    background: #123b31;
    color: #67e0b0;
  }
}

@media (max-width: 720px) {
  .node-title-row h2 {
    font-size: 21px;
  }

  .node-facts {
    grid-template-columns: 1fr;
  }

  .node-fact + .node-fact {
    border-top: 1px solid var(--fleet-line);
    border-left: 0;
  }
}
</style>

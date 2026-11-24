<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Back } from '@element-plus/icons-vue'

import HostMetricsPanel from '@/components/HostMetricsPanel.vue'
import NodeNetworkPanel from '@/components/NodeNetworkPanel.vue'
import type { ClusterMember } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import { useClustersStore } from '@/stores/clusters'

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

/** Renders a cluster member status enum value in the active language. */
function memberStatusLabel(status: string): string {
  const key = `cluster.memberStatus.${status.toLowerCase()}`
  return t(key) === key ? status : t(key)
}

function backToCluster(): void {
  void router.push({ name: 'cluster', params: { cluster: props.cluster } })
}

function ensureCluster(): void {
  if (!clusters.byName(props.cluster)) {
    void clusters.fetchAll()
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
      <el-button :loading="clusters.loading" @click="clusters.fetchAll()">
        {{ t('common.refresh') }}
      </el-button>
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
          <span class="value mono">{{ memberInfo.url || t('common.none') }}</span>
        </div>
        <div class="node-fact">
          <span class="label">{{ t('cluster.memberMessages') }}</span>
          <span class="value">{{ memberInfo.message || t('common.none') }}</span>
        </div>
      </section>

      <HostMetricsPanel :cluster="cluster" :member="memberInfo.name" />

      <NodeNetworkPanel :cluster="cluster" :member="memberInfo.name" :can-write="auth.canWrite" />
    </template>
    <el-skeleton v-else-if="clusters.loading" :rows="6" animated />
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
  font-weight: 500;
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

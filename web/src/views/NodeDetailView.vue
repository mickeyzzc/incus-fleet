<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Back } from '@element-plus/icons-vue'

import HostMetricsPanel from '@/components/HostMetricsPanel.vue'
import NodeNetworkPanel from '@/components/NodeNetworkPanel.vue'
import type { ClusterMember } from '@/api/types'
import { useClustersStore } from '@/stores/clusters'

const props = defineProps<{
  cluster: string
  member: string
}>()

const router = useRouter()
const clusters = useClustersStore()
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
  <div class="page">
    <div class="page-header">
      <div class="title-row">
        <el-button :icon="Back" text @click="backToCluster">{{ t('cluster.backToCluster') }}</el-button>
        <div>
          <h2>{{ memberInfo?.name || member }}</h2>
          <p class="subtitle">{{ t('nodeDetail.subtitle', { cluster: summary?.display_name || cluster }) }}</p>
        </div>
      </div>
      <div class="page-actions">
        <el-tag v-if="memberInfo" :type="memberInfo.status === 'Online' ? 'success' : 'info'" effect="plain">
          {{ memberStatusLabel(memberInfo.status) }}
        </el-tag>
        <el-button :loading="clusters.loading" @click="clusters.fetchAll()">
          {{ t('common.refresh') }}
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="!memberInfo"
      class="fleet-alert"
      type="warning"
      :closable="false"
      show-icon
      :title="t('nodeDetail.notFound', { member })"
    />

    <template v-else-if="memberInfo">
      <section class="panel">
        <div class="panel-body member-grid">
          <div class="detail-item">
            <div class="label">{{ t('cluster.memberRoles') }}</div>
            <div class="value">{{ memberInfo.roles?.join(', ') || t('common.none') }}</div>
          </div>
          <div class="detail-item">
            <div class="label">{{ t('cluster.architectures') }}</div>
            <div class="value">{{ memberInfo.architecture || t('common.none') }}</div>
          </div>
          <div class="detail-item">
            <div class="label">{{ t('cluster.memberAddress') }}</div>
            <div class="value mono">{{ memberInfo.url || t('common.none') }}</div>
          </div>
          <div class="detail-item">
            <div class="label">{{ t('cluster.memberMessages') }}</div>
            <div class="value">{{ memberInfo.message || t('common.none') }}</div>
          </div>
        </div>
      </section>

      <HostMetricsPanel :cluster="cluster" :member="memberInfo.name" />

      <NodeNetworkPanel :cluster="cluster" :member="memberInfo.name" />
    </template>
  </div>
</template>

<style scoped>
.title-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.member-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 14px;
}

.detail-item .label {
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  font-size: 12px;
  margin-bottom: 2px;
}

.detail-item .value {
  font-size: 13px;
  word-break: break-all;
}
</style>

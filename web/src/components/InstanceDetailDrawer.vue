<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import InstanceActions from '@/components/InstanceActions.vue'
import StatusTag from '@/components/StatusTag.vue'
import type { InstanceAction, InstanceDetail } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'
import { formatBytes, formatDateTime } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    cluster: string
    name: string
    canMigrate?: boolean
  }>(),
  { canMigrate: true },
)

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  edit: [detail: InstanceDetail]
  migrate: [detail: InstanceDetail]
  delete: [detail: InstanceDetail]
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const actionLoading = ref(false)

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const detail = computed(() => instances.detail(props.cluster, props.name))

watch(
  () => [props.modelValue, props.cluster, props.name] as const,
  ([open, cluster, name]) => {
    if (open && cluster && name && !detail.value) {
      void instances.fetchDetail(cluster, name)
    }
  },
  { immediate: true },
)

async function handleAction(action: InstanceAction): Promise<void> {
  if (!detail.value) {
    return
  }

  actionLoading.value = true
  try {
    await instances.instanceAction(props.cluster, props.name, action)
    ElMessage.success(
      t('instances.actionSuccess', {
        action: t(`instances.actionDone.${action}`),
        name: props.name,
      }),
    )
  } catch (err) {
    ElMessage.error(t('instances.actionFailed', { error: errorMessage(err) }))
  } finally {
    actionLoading.value = false
  }
}
</script>

<template>
  <el-drawer v-model="visible" size="560px" :title="`${name} · ${cluster}`">
    <div v-loading="instances.detailLoading">
      <template v-if="detail">
        <div class="drawer-section">
          <h4>{{ t('detail.actions') }}</h4>
          <InstanceActions
            :instance="detail"
            size="default"
            :busy="actionLoading"
            :show-migrate="canMigrate"
            @action="handleAction"
            @edit="emit('edit', detail)"
            @migrate="emit('migrate', detail)"
            @delete="emit('delete', detail)"
          />
        </div>

        <div class="drawer-section">
          <h4>{{ t('detail.instance') }}</h4>
          <div class="detail-grid">
            <div class="detail-item">
              <div class="label">{{ t('detail.type') }}</div>
              <div class="value">
                {{
                  detail.type === 'virtual-machine'
                    ? t('common.virtualMachine')
                    : t('common.container')
                }}
              </div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('detail.status') }}</div>
              <div class="value">
                <StatusTag :status="detail.status" :status-code="detail.status_code" />
              </div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('detail.node') }}</div>
              <div class="value">{{ detail.location || t('common.none') }}</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('detail.architecture') }}</div>
              <div class="value">{{ detail.architecture || t('common.none') }}</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('detail.memory') }}</div>
              <div class="value">{{ formatBytes(detail.memory) }}</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('detail.processes') }}</div>
              <div class="value">{{ detail.processes }}</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('detail.storagePool') }}</div>
              <div class="value">{{ detail.storage_pool || t('common.none') }}</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('detail.created') }}</div>
              <div class="value">{{ formatDateTime(detail.created_at) }}</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('detail.started') }}</div>
              <div class="value">{{ formatDateTime(detail.started_at) }}</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('detail.liveMigration') }}</div>
              <div class="value">
                {{
                  detail.live_migratable ? t('detail.statefulVM') : t('detail.notAvailable')
                }}
              </div>
            </div>
          </div>
        </div>

        <div v-if="detail.os || detail.hostname" class="drawer-section">
          <h4>{{ t('detail.guest') }}</h4>
          <div class="detail-grid">
            <div class="detail-item">
              <div class="label">{{ t('detail.os') }}</div>
              <div class="value">
                {{ detail.os || t('common.none') }} {{ detail.os_version || '' }}
              </div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('detail.kernel') }}</div>
              <div class="value">{{ detail.kernel_version || t('common.none') }}</div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('detail.hostname') }}</div>
              <div class="value">{{ detail.hostname || t('common.none') }}</div>
            </div>
          </div>
        </div>

        <div class="drawer-section">
          <h4>{{ t('detail.networking') }}</h4>
          <div class="detail-grid">
            <div class="detail-item">
              <div class="label">{{ t('detail.ipv4') }}</div>
              <div class="value mono">
                {{ detail.ipv4.join(', ') || t('common.none') }}
              </div>
            </div>
            <div class="detail-item">
              <div class="label">{{ t('detail.ipv6') }}</div>
              <div class="value mono">
                {{ detail.ipv6.join(', ') || t('common.none') }}
              </div>
            </div>
          </div>
        </div>

        <div class="drawer-section">
          <h4>{{ t('detail.configuration') }}</h4>
          <div class="config-list">
            <div v-for="(value, key) in detail.config" :key="key" class="config-row">
              <span class="muted">{{ key }}</span>
              <code>{{ value }}</code>
            </div>
            <div v-if="Object.keys(detail.config).length === 0" class="muted">
              {{ t('detail.noConfig') }}
            </div>
          </div>
        </div>

        <div class="drawer-section">
          <h4>{{ t('detail.devices') }}</h4>
          <div class="config-list">
            <div v-for="(device, key) in detail.devices" :key="key" class="config-row">
              <span class="muted">{{ key }}</span>
              <code>{{ Object.entries(device).map(([k, v]) => `${k}=${v}`).join(' ') }}</code>
            </div>
            <div v-if="Object.keys(detail.devices).length === 0" class="muted">
              {{ t('detail.noDevices') }}
            </div>
          </div>
        </div>
      </template>

      <el-empty v-else-if="!instances.detailLoading" :description="t('detail.unavailable')" />
    </div>
  </el-drawer>
</template>

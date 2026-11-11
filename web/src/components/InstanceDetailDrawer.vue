<script setup lang="ts">
import { computed, watch } from 'vue'

import { useInstancesStore } from '@/stores/instances'
import { formatBytes, formatDateTime } from '@/utils/format'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  name: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

const instances = useInstancesStore()

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
</script>

<template>
  <el-drawer v-model="visible" size="540px" :title="`${name} · ${cluster}`">
    <div v-loading="instances.detailLoading">
      <template v-if="detail">
        <div class="drawer-section">
          <h4>Instance</h4>
          <div class="detail-grid">
            <div class="detail-item">
              <div class="label">Type</div>
              <div class="value">
                {{ detail.type === 'virtual-machine' ? 'Virtual machine' : 'Container' }}
              </div>
            </div>
            <div class="detail-item">
              <div class="label">Status</div>
              <div class="value">{{ detail.status }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Node</div>
              <div class="value">{{ detail.location || '—' }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Architecture</div>
              <div class="value">{{ detail.architecture || '—' }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Memory</div>
              <div class="value">{{ formatBytes(detail.memory) }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Processes</div>
              <div class="value">{{ detail.processes }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Storage pool</div>
              <div class="value">{{ detail.storage_pool || '—' }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Created</div>
              <div class="value">{{ formatDateTime(detail.created_at) }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Started</div>
              <div class="value">{{ formatDateTime(detail.started_at) }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Live migration</div>
              <div class="value">
                {{ detail.live_migratable ? 'Stateful VM' : 'Not available' }}
              </div>
            </div>
          </div>
        </div>

        <div v-if="detail.os || detail.hostname" class="drawer-section">
          <h4>Guest</h4>
          <div class="detail-grid">
            <div class="detail-item">
              <div class="label">OS</div>
              <div class="value">{{ detail.os || '—' }} {{ detail.os_version || '' }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Kernel</div>
              <div class="value">{{ detail.kernel_version || '—' }}</div>
            </div>
            <div class="detail-item">
              <div class="label">Hostname</div>
              <div class="value">{{ detail.hostname || '—' }}</div>
            </div>
          </div>
        </div>

        <div class="drawer-section">
          <h4>Networking</h4>
          <div class="detail-grid">
            <div class="detail-item">
              <div class="label">IPv4</div>
              <div class="value mono">{{ detail.ipv4.join(', ') || '—' }}</div>
            </div>
            <div class="detail-item">
              <div class="label">IPv6</div>
              <div class="value mono">{{ detail.ipv6.join(', ') || '—' }}</div>
            </div>
          </div>
        </div>

        <div class="drawer-section">
          <h4>Selected configuration</h4>
          <div class="config-list">
            <div v-for="(value, key) in detail.config" :key="key" class="config-row">
              <span class="muted">{{ key }}</span>
              <code>{{ value }}</code>
            </div>
            <div v-if="Object.keys(detail.config).length === 0" class="muted">
              No selected configuration.
            </div>
          </div>
        </div>

        <div class="drawer-section">
          <h4>Devices</h4>
          <div class="config-list">
            <div v-for="(device, key) in detail.devices" :key="key" class="config-row">
              <span class="muted">{{ key }}</span>
              <code>{{ Object.entries(device).map(([k, v]) => `${k}=${v}`).join(' ') }}</code>
            </div>
          </div>
        </div>
      </template>

      <el-empty v-else-if="!instances.detailLoading" description="Instance details unavailable" />
    </div>
  </el-drawer>
</template>

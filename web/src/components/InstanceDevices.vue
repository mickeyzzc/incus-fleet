<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { DeviceRequest, InstanceDetail } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  cluster: string
  instance: string
  detail: InstanceDetail | null
}>()

const emit = defineEmits<{
  changed: [detail: InstanceDetail]
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const attachOpen = ref(false)
const form = reactive({
  name: '',
  type: 'disk' as 'disk' | 'nic' | 'proxy',
  pool: '',
  source: '',
  path: '',
  size: '',
  network: '',
  ipv4: '',
  ipv6: '',
  listen: '',
  connect: '',
})

// Rows are populated after mount so el-table renders them once its columns
// have registered; a static first render would leave the cells empty in tests.
const rows = ref<Array<{ name: string; device: Record<string, string> }>>([])

function syncRows(): void {
  rows.value = Object.entries(props.detail?.devices ?? {}).map(([name, device]) => ({
    name,
    device: { ...device },
  }))
}

onMounted(syncRows)
watch(
  () => props.detail?.devices,
  () => syncRows(),
  { deep: true },
)

const pools = computed(() => instances.resources(props.cluster)?.storage_pools ?? [])
const networks = computed(() => instances.resources(props.cluster)?.networks ?? [])

watch(attachOpen, (open) => {
  if (!open) {
    return
  }

  form.name = ''
  form.type = 'disk'
  form.pool = pools.value[0]?.name ?? ''
  form.source = ''
  form.path = ''
  form.size = ''
  form.network = networks.value[0]?.name ?? ''
  form.ipv4 = ''
  form.ipv6 = ''
  form.listen = ''
  form.connect = ''
})

function deviceLabel(type: string): string {
  return type === 'nic'
    ? t('devices.nic')
    : type === 'disk'
      ? t('devices.disk')
      : type === 'proxy'
        ? t('devices.proxy')
        : type
}

async function openAttach(): Promise<void> {
  await instances.fetchResources(props.cluster)
  attachOpen.value = true
}

function request(): DeviceRequest {
  const payload: DeviceRequest = { name: form.name.trim(), type: form.type }

  if (form.type === 'disk') {
    if (form.pool.trim()) payload.pool = form.pool.trim()
    if (form.source.trim()) payload.source = form.source.trim()
    if (form.path.trim()) payload.path = form.path.trim()
    if (form.size.trim()) payload.size = form.size.trim()
  } else if (form.type === 'proxy') {
    if (form.listen.trim()) payload.listen = form.listen.trim()
    if (form.connect.trim()) payload.connect = form.connect.trim()
  } else {
    if (form.network) payload.network = form.network
    if (form.ipv4.trim()) payload.ipv4 = form.ipv4.trim()
    if (form.ipv6.trim()) payload.ipv6 = form.ipv6.trim()
  }

  return payload
}

async function submitAttach(): Promise<void> {
  if (!form.name.trim()) {
    ElMessage.error(t('admin.validationName'))
    return
  }

  try {
    const detail = await instances.attachDevice(props.cluster, props.instance, request())
    ElMessage.success(t('devices.attached', { name: form.name.trim() }))
    attachOpen.value = false
    emit('changed', detail)
  } catch (err) {
    ElMessage.error(t('devices.failed', { error: errorMessage(err) }))
  }
}

async function detach(name: string): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('devices.detachConfirm', { name, instance: props.instance }),
      t('devices.detach'),
      { type: 'warning', confirmButtonText: t('devices.detach'), cancelButtonText: t('common.cancel') },
    )
  } catch {
    return
  }

  try {
    const detail = await instances.detachDevice(props.cluster, props.instance, name)
    ElMessage.success(t('devices.detached', { name }))
    emit('changed', detail)
  } catch (err) {
    ElMessage.error(t('devices.failed', { error: errorMessage(err) }))
  }
}
</script>

<template>
  <div class="device-panel">
    <div class="table-toolbar" style="margin-bottom: 12px">
      <el-button :icon="Plus" type="primary" size="small" @click="openAttach">
        {{ t('devices.attach') }}
      </el-button>
    </div>

    <el-table :data="rows" size="small" stripe>
      <el-table-column :label="t('common.name')" min-width="110">
        <template #default="{ row }">
          <strong>{{ row.name }}</strong>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.type')" width="100">
        <template #default="{ row }">
          <el-tag size="small" effect="plain">{{ deviceLabel(row.device.type) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('admin.config')" min-width="200">
        <template #default="{ row }">
          <span class="mono device-config">
            {{ Object.entries(row.device).filter(([key]) => key !== 'type').map(([key, value]) => `${key}=${value}`).join(' · ') }}
          </span>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.actions')" width="90">
        <template #default="{ row }">
          <el-button link type="danger" @click="detach(row.name)">
            {{ t('devices.detach') }}
          </el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty :description="t('detail.noDevices')" :image-size="60" />
      </template>
    </el-table>

    <el-dialog v-model="attachOpen" :title="t('devices.attach')" width="480px" destroy-on-close>
      <el-form label-position="top" @submit.prevent="submitAttach">
        <el-form-item :label="t('admin.deviceName')">
          <el-input v-model="form.name" />
        </el-form-item>

        <el-form-item :label="t('devices.deviceType')">
          <el-radio-group v-model="form.type">
            <el-radio-button value="disk">{{ t('devices.disk') }}</el-radio-button>
            <el-radio-button value="nic">{{ t('devices.nic') }}</el-radio-button>
            <el-radio-button value="proxy">{{ t('devices.proxy') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <template v-if="form.type === 'disk'">
          <el-form-item :label="t('form.storagePool')">
            <el-select v-model="form.pool" style="width: 100%">
              <el-option
                v-for="pool in pools"
                :key="pool.name"
                :label="`${pool.name} · ${pool.driver}`"
                :value="pool.name"
              />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('devices.source')">
            <el-input v-model="form.source" />
          </el-form-item>
          <el-form-item :label="t('devices.path')">
            <el-input v-model="form.path" />
          </el-form-item>
          <el-form-item :label="t('devices.size')">
            <el-input v-model="form.size" :placeholder="t('form.rootDiskPlaceholder')" />
          </el-form-item>
        </template>

        <template v-else-if="form.type === 'proxy'">
          <el-form-item :label="t('devices.listen')">
            <el-input v-model="form.listen" placeholder="tcp:0.0.0.0:8080" />
            <p class="field-hint">{{ t('devices.listenHint') }}</p>
          </el-form-item>
          <el-form-item :label="t('devices.connect')">
            <el-input v-model="form.connect" placeholder="tcp:127.0.0.1:80" />
            <p class="field-hint">{{ t('devices.connectHint') }}</p>
          </el-form-item>
        </template>

        <template v-else>
          <el-form-item :label="t('form.network')">
            <el-select v-model="form.network" style="width: 100%">
              <el-option
                v-for="network in networks"
                :key="network.name"
                :label="`${network.name} · ${network.type}`"
                :value="network.name"
              />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('form.ipv4')">
            <el-input v-model="form.ipv4" :placeholder="t('form.ipv4Placeholder')" />
          </el-form-item>
          <el-form-item :label="t('form.ipv6')">
            <el-input v-model="form.ipv6" :placeholder="t('form.ipv6Placeholder')" />
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="attachOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="instances.saving" @click="submitAttach">
          {{ t('common.create') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.device-panel {
  display: flex;
  flex-direction: column;
}

.device-config {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: block;
}
.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}
</style>

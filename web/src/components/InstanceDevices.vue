<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { DeviceRequest, InstanceDetail } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'
import { parseSize, validDeviceName, validGuestPath } from '@/utils/size'

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

// Edit dialog state. editingDevice keeps the pre-edit map so legacy NICs
// (nictype+parent) can keep those keys when the network is not switched.
const editOpen = ref(false)
const editName = ref('')
const editingDevice = ref<Record<string, string>>({})
const editForm = reactive({
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

// Grouped NIC options: managed networks cluster-wide, physical NICs under
// one group per member so cluster-level lists stay readable (#13 F6/F7).
const networkGroups = computed(() => {
  const managed = networks.value.filter((network) => network.managed)
  const groups: { label: string; options: typeof networks.value }[] = []

  if (managed.length > 0) {
    groups.push({ label: t('form.managedNetworks'), options: managed })
  }

  const byLocation = new Map<string, typeof networks.value>()
  for (const network of networks.value) {
    if (network.managed) {
      continue
    }
    const location =
      network.locations.length > 0 ? network.locations.join(', ') : t('cluster.unknownMember')
    const bucket = byLocation.get(location)
    if (bucket) {
      bucket.push(network)
    } else {
      byLocation.set(location, [network])
    }
  }
  const physical = [...byLocation.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  for (const [location, options] of physical) {
    groups.push({ label: t('form.physicalNetworksOn', { member: location }), options })
  }

  return groups
})

// A legacy NIC has no "network" key; its parent is offered as a selectable
// option so the edit form can also migrate it onto a managed network.
const editLegacyParent = computed(() =>
  editingDevice.value.type === 'nic' && !editingDevice.value.network && editingDevice.value.parent
    ? editingDevice.value.parent
    : '',
)

watch(attachOpen, (open) => {
  if (!open) {
    return
  }

  form.name = 'data0'
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
    // A size quota only applies to freshly created disks; sending one for an
    // existing volume fails with "Only root or tmpfs disks can have a size
    // quota".
    if (form.size.trim() && !form.source.trim()) payload.size = parseSize(form.size).value
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

function attachError(): string {
  const name = form.name.trim()
  if (!name) {
    return t('validation.required')
  }
  if (!validDeviceName(name)) {
    return t('validation.deviceNameFormat')
  }
  if (rows.value.some((row) => row.name === name)) {
    return t('validation.duplicateDevice')
  }

  if (form.type === 'disk') {
    if (!form.path.trim()) {
      return t('validation.required')
    }
    if (!validGuestPath(form.path.trim())) {
      return t('validation.pathFormat')
    }
    if (form.size.trim() && !form.source.trim() && !parseSize(form.size).valid) {
      return t('validation.sizeFormat')
    }
  }
  if (form.type === 'nic' && !form.network) {
    return t('validation.required')
  }
  if (form.type === 'proxy' && (!form.listen.trim() || !form.connect.trim())) {
    return t('validation.required')
  }

  return ''
}

async function submitAttach(): Promise<void> {
  const error = attachError()
  if (error) {
    ElMessage.error(error)
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

async function openEdit(row: { name: string; device: Record<string, string> }): Promise<void> {
  await instances.fetchResources(props.cluster)
  editName.value = row.name
  editingDevice.value = { ...row.device }
  editForm.pool = row.device.pool ?? ''
  editForm.source = row.device.source ?? ''
  editForm.path = row.device.path ?? ''
  editForm.size = row.device.size ?? ''
  editForm.network = row.device.network ?? row.device.parent ?? ''
  editForm.ipv4 = row.device['ipv4.address'] ?? ''
  editForm.ipv6 = row.device['ipv6.address'] ?? ''
  editForm.listen = row.device.listen ?? ''
  editForm.connect = row.device.connect ?? ''
  editOpen.value = true
}

/** Builds the edit request; empty NIC addresses clear back to DHCP. */
function editRequest(): DeviceRequest {
  const device = editingDevice.value
  const payload: DeviceRequest = { name: editName.value, type: 'nic' }

  if (device.type === 'nic') {
    // Only send a network when it differs from the legacy parent: a legacy
    // nictype+parent NIC keeps its keys unless the user switches networks.
    const current = device.network ?? device.parent ?? ''
    if (editForm.network && editForm.network !== current) {
      payload.network = editForm.network
    }
    payload.ipv4 = editForm.ipv4.trim()
    payload.ipv6 = editForm.ipv6.trim()
    return payload
  }

  payload.type = 'disk'
  if (editForm.pool.trim()) payload.pool = editForm.pool.trim()
  if (editForm.source.trim()) payload.source = editForm.source.trim()
  if (editForm.path.trim()) payload.path = editForm.path.trim()
  if (editForm.size.trim() && !editForm.source.trim()) payload.size = parseSize(editForm.size).value
  if (device.type === 'proxy') {
    payload.type = 'proxy'
    if (editForm.listen.trim()) payload.listen = editForm.listen.trim()
    if (editForm.connect.trim()) payload.connect = editForm.connect.trim()
  }

  return payload
}

async function submitEdit(): Promise<void> {
  try {
    const detail = await instances.updateDevice(props.cluster, props.instance, editName.value, editRequest())
    ElMessage.success(t('devices.updated', { name: editName.value }))
    editOpen.value = false
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
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="device-detail">
            <div v-for="(value, key) in row.device" :key="key" class="device-detail-row">
              <span class="device-detail-key">{{ key }}</span>
              <code>{{ value === '' ? '—' : value }}</code>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.name')" min-width="110">
        <template #default="{ row }">
          <strong>{{ row.name }}</strong>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.type')" width="90">
        <template #default="{ row }">
          <el-tag size="small" effect="plain">{{ deviceLabel(row.device.type) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('admin.config')" min-width="220">
        <template #default="{ row }">
          <span class="mono device-config">
            {{ Object.entries(row.device).filter(([key]) => key !== 'type').map(([key, value]) => `${key}=${value}`).join(' · ') }}
          </span>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.actions')" width="130" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">
            {{ t('common.edit') }}
          </el-button>
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
          <el-input v-model="form.name" :placeholder="t('form.diskNamePlaceholder')" />
          <p class="field-hint">{{ t('validation.deviceNameFormat') }}</p>
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
            <el-input v-model="form.source" :placeholder="t('devices.sourcePlaceholder')" />
            <p class="field-hint">{{ t('devices.sourceHint') }}</p>
          </el-form-item>
          <el-form-item :label="t('devices.path')">
            <el-input v-model="form.path" :placeholder="t('form.diskPathPlaceholder')" />
            <p class="field-hint">{{ t('devices.pathHint') }}</p>
          </el-form-item>
          <el-form-item :label="t('devices.size')">
            <el-input
              v-model="form.size"
              :placeholder="t('form.diskSizePlaceholder')"
              :disabled="!!form.source.trim()"
            />
            <p class="field-hint">{{ t('devices.sizeNewHint') }}</p>
            <p v-if="form.size.trim() && !form.source.trim() && !parseSize(form.size).valid" class="field-error">
              {{ t('validation.sizeFormat') }}
            </p>
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
              <el-option-group v-for="group in networkGroups" :key="group.label" :label="group.label">
                <el-option
                  v-for="network in group.options"
                  :key="network.name"
                  :label="`${network.name} · ${network.type}`"
                  :value="network.name"
                />
              </el-option-group>
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

    <el-dialog
      v-model="editOpen"
      :title="t('devices.editTitle', { name: editName })"
      width="480px"
      destroy-on-close
    >
      <el-form label-position="top" @submit.prevent="submitEdit">
        <template v-if="editingDevice.type === 'nic'">
          <el-form-item :label="t('form.network')">
            <el-select v-model="editForm.network" style="width: 100%">
              <el-option-group v-for="group in networkGroups" :key="group.label" :label="group.label">
                <el-option
                  v-for="network in group.options"
                  :key="network.name"
                  :label="`${network.name} · ${network.type}`"
                  :value="network.name"
                />
              </el-option-group>
              <el-option
                v-if="editLegacyParent"
                :label="`${editLegacyParent} · ${t('devices.customParent')}`"
                :value="editLegacyParent"
              />
            </el-select>
            <p class="field-hint">{{ t('devices.networkHint') }}</p>
          </el-form-item>
          <el-form-item :label="t('form.ipv4')">
            <el-input v-model="editForm.ipv4" :placeholder="t('form.ipv4Placeholder')" />
            <p class="field-hint">{{ t('devices.addressHint') }}</p>
          </el-form-item>
          <el-form-item :label="t('form.ipv6')">
            <el-input v-model="editForm.ipv6" :placeholder="t('form.ipv6Placeholder')" />
          </el-form-item>
        </template>

        <template v-else-if="editingDevice.type === 'proxy'">
          <el-form-item :label="t('devices.listen')">
            <el-input v-model="editForm.listen" placeholder="tcp:0.0.0.0:8080" />
          </el-form-item>
          <el-form-item :label="t('devices.connect')">
            <el-input v-model="editForm.connect" placeholder="tcp:127.0.0.1:80" />
          </el-form-item>
        </template>

        <template v-else>
          <el-form-item :label="t('devices.source')">
            <el-input v-model="editForm.source" />
            <p class="field-hint">{{ t('devices.sourceHint') }}</p>
          </el-form-item>
          <el-form-item :label="t('devices.path')">
            <el-input v-model="editForm.path" :placeholder="t('form.diskPathPlaceholder')" />
            <p class="field-hint">{{ t('devices.pathHint') }}</p>
          </el-form-item>
          <el-form-item :label="t('devices.size')">
            <el-input
              v-model="editForm.size"
              :placeholder="t('form.diskSizePlaceholder')"
              :disabled="!!editForm.source.trim()"
            />
            <p class="field-hint">{{ t('devices.sizeHint') }}</p>
            <p v-if="editForm.size.trim() && !editForm.source.trim() && !parseSize(editForm.size).valid" class="field-error">
              {{ t('validation.sizeFormat') }}
            </p>
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="editOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="instances.saving" @click="submitEdit">
          {{ t('common.save') }}
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

.device-detail {
  padding: 4px 12px 8px 48px;
}

.device-detail-row {
  display: flex;
  gap: 12px;
  padding: 2px 0;
  align-items: baseline;
}

.device-detail-key {
  color: var(--fleet-ink-soft);
  font-size: 12px;
  min-width: 120px;
}

.device-detail-row code {
  word-break: break-all;
  white-space: pre-wrap;
}

.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}
</style>

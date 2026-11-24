<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { NodeAudit, NodeNIC, NodeTask } from '@/api/types'
import { api } from '@/api'
import { errorMessage } from '@/utils/errors'
import { formatDateTime } from '@/utils/format'

const props = withDefaults(defineProps<{
  cluster: string
  member: string
  /** Read-only roles cannot apply host network changes. */
  canWrite?: boolean
}>(), { canWrite: true })

const { t } = useI18n()

const loading = ref(false)
const loadError = ref('')
const addressBusy = ref(false)
const routeBusy = ref(false)
const firewallBusy = ref(false)
const wifiBusy = ref(false)
const wifiScanning = ref(false)
const networkTab = ref<'nics' | 'routes' | 'firewall' | 'wifi'>('nics')
const nicBusy = ref('')

/** Only a positively known read-only role hides mutations; the backend is authoritative. */
const mayWrite = computed(() => props.canWrite)

function guardWrite(): boolean {
  if (mayWrite.value) {
    return true
  }
  ElMessage.error(t('common.readOnly'))
  return false
}

/** Accepts plain IPv4 addresses and CIDR notation. */
function validIPv4(value: string): boolean {
  const [address, prefix, ...rest] = value.split('/')
  if (!address || rest.length > 0) {
    return false
  }
  if (prefix !== undefined && (!/^\d{1,2}$/.test(prefix) || Number(prefix) > 32)) {
    return false
  }
  const parts = address.split('.')
  return parts.length === 4 && parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255)
}

function validDestination(value: string): boolean {
  if (value === 'default') {
    return true
  }
  const [address, prefix] = value.split('/')
  if (!address) {
    return false
  }
  return /^\d{1,2}$/.test(prefix ?? '') && Number(prefix) <= 32 && validIPv4(address)
}
const snapshot = ref<{ host: string; nics: NodeNIC[]; routes: { destination: string; gateway?: string; device?: string; metric?: string }[]; firewall: { handle: string; rule: string }[]; wifi_scan?: { ssid: string; signal: number; secure: boolean; frequency: number }[]; wifi_device?: string; backend?: string } | null>(null)

const historyOpen = ref(false)
const historyError = ref('')
const historyLoading = ref(false)
const historyTasks = ref<NodeTask[]>([])
const historyAudit = ref<NodeAudit[]>([])

const addressDialogOpen = ref(false)
const addressTarget = ref<NodeNIC | null>(null)
const addressForm = reactive({ dhcp: false, address: '', gateway: '', persist: true })

const routeDialogOpen = ref(false)
const routeForm = reactive({ destination: '', gateway: '', device: '' })

const firewallDialogOpen = ref(false)
const firewallForm = reactive({ port: 22, proto: 'tcp' as 'tcp' | 'udp', action: 'accept' as 'accept' | 'drop', comment: '' })

const wifiDialogOpen = ref(false)
const wifiForm = reactive({ ssid: '', password: '' })

const wifiDevice = computed(() => snapshot.value?.wifi_device ?? '')
const displayHost = computed(() => snapshot.value?.host || props.member)

watch(
  [() => props.cluster, () => props.member],
  () => {
    snapshot.value = null
    void refresh()
  },
  { immediate: true },
)

async function refresh(): Promise<void> {
  loading.value = true
  try {
    snapshot.value = await api.nodeSnapshot(props.cluster, props.member, wifiDevice.value || undefined)
    loadError.value = ''
  } catch (err) {
    loadError.value = errorMessage(err)
    ElMessage.error(loadError.value)
  } finally {
    loading.value = false
  }
}

// scanWifi refreshes only the Wi-Fi inventory; the NICs/routes/firewall
// sections stay untouched and the section shows its own progress.
async function scanWifi(): Promise<void> {
  if (!wifiDevice.value || wifiScanning.value) {
    return
  }
  wifiScanning.value = true
  try {
    const next = await api.nodeSnapshot(props.cluster, props.member, wifiDevice.value)
    if (snapshot.value) {
      snapshot.value.wifi_scan = next.wifi_scan
      snapshot.value.wifi_device = next.wifi_device
    } else {
      snapshot.value = next
    }
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    wifiScanning.value = false
  }
}

async function openHistory(): Promise<void> {
  historyOpen.value = true
  historyLoading.value = true
  try {
    const [tasks, audit] = await Promise.all([
      api.nodeTasks(props.cluster, props.member, 50),
      api.nodeAudit(props.cluster, props.member, 50),
    ])
    historyTasks.value = tasks
    historyAudit.value = audit
    historyError.value = ''
  } catch (err) {
    historyError.value = errorMessage(err)
    ElMessage.error(historyError.value)
  } finally {
    historyLoading.value = false
  }
}

function stateTag(state: string): 'success' | 'info' | 'warning' {
  if (state === 'up') {
    return 'success'
  }
  if (state === 'down') {
    return 'info'
  }
  return 'warning'
}

async function toggleNIC(nic: NodeNIC): Promise<void> {
  if (!guardWrite() || nicBusy.value) {
    return
  }
  const up = nic.state !== 'up'
  const label = up ? t('nodeActions.bringUp') : t('nodeActions.bringDown')
  try {
    await ElMessageBox.confirm(t('nodeActions.stateConfirm', { name: nic.name, action: label }), label, {
      type: 'warning',
      confirmButtonText: label,
      cancelButtonText: t('common.cancel'),
    })
  } catch {
    return
  }

  nicBusy.value = nic.name
  try {
    await api.nodeNICState(props.cluster, props.member, nic.name, up)
    ElMessage.success(t('nodeActions.stateChanged', { name: nic.name }))
    await refresh()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    nicBusy.value = ''
  }
}

function openAddressDialog(nic: NodeNIC): void {
  addressTarget.value = nic
  addressForm.dhcp = (nic.addresses.find((a) => a.family === 'inet')?.address ?? '') === ''
  addressForm.address = nic.addresses.find((a) => a.family === 'inet')?.address?.split('/')[0] ?? ''
  addressForm.gateway = ''
  addressForm.persist = true
  addressDialogOpen.value = true
}

async function submitAddress(): Promise<void> {
  if (!guardWrite()) {
    return
  }
  const target = addressTarget.value
  if (!target) {
    return
  }
  const address = addressForm.address.trim()
  const gateway = addressForm.gateway.trim()
  if (!addressForm.dhcp && (!validIPv4(address) || (gateway !== '' && !validIPv4(gateway)))) {
    ElMessage.error(t('nodePage.addressInvalid'))
    return
  }

  try {
    await ElMessageBox.confirm(t('nodePage.addressConfirm', { name: target.name }), t('common.confirm'), {
      type: 'warning',
      confirmButtonText: t('common.confirm'),
      cancelButtonText: t('common.cancel'),
    })
  } catch {
    return
  }

  addressBusy.value = true
  try {
    await api.nodeApplyAddress(props.cluster, props.member, target.name, {
      dhcp: addressForm.dhcp,
      address: addressForm.dhcp ? undefined : address,
      gateway: addressForm.dhcp ? undefined : gateway,
      persist: addressForm.persist,
    })
    ElMessage.success(t('nodeActions.addressApplied', { name: target.name }))
    addressDialogOpen.value = false
    await refresh()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    addressBusy.value = false
  }
}

async function submitRoute(): Promise<void> {
  if (!guardWrite()) {
    return
  }
  const destination = routeForm.destination.trim()
  if (!validDestination(destination)) {
    ElMessage.error(t('nodePage.routeInvalid'))
    return
  }
  const gateway = routeForm.gateway.trim()
  if (gateway && !/^(\d{1,3}\.){3}\d{1,3}$/.test(gateway)) {
    ElMessage.error(t('validation.ipv4Format'))
    return
  }

  routeBusy.value = true
  try {
    await api.nodeAddRoute(
      props.cluster,
      props.member,
      destination,
      gateway,
      routeForm.device.trim(),
    )
    ElMessage.success(t('nodeActions.routeAdded'))
    routeDialogOpen.value = false
    await refresh()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    routeBusy.value = false
  }
}

async function confirmDeleteRoute(destination: string): Promise<void> {
  if (!guardWrite()) {
    return
  }
  try {
    await ElMessageBox.confirm(t('nodeActions.routeDeleteConfirm', { destination }), t('common.delete'), {
      type: 'warning',
      confirmButtonText: t('common.delete'),
      cancelButtonText: t('common.cancel'),
    })
  } catch {
    return
  }

  try {
    await api.nodeDeleteRoute(props.cluster, props.member, destination)
    ElMessage.success(t('nodeActions.routeDeleted'))
    await refresh()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

async function submitFirewall(): Promise<void> {
  if (!guardWrite()) {
    return
  }
  firewallBusy.value = true
  try {
    await api.nodeAddFirewallRule(props.cluster, props.member, {
      port: firewallForm.port,
      proto: firewallForm.proto,
      action: firewallForm.action,
      comment: firewallForm.comment.trim(),
    })
    ElMessage.success(t('nodeActions.firewallRuleAdded'))
    firewallDialogOpen.value = false
    await refresh()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    firewallBusy.value = false
  }
}

async function confirmDeleteFirewall(handle: string): Promise<void> {
  if (!guardWrite()) {
    return
  }
  try {
    await ElMessageBox.confirm(t('nodeActions.firewallDeleteConfirm', { handle }), t('common.delete'), {
      type: 'warning',
      confirmButtonText: t('common.delete'),
      cancelButtonText: t('common.cancel'),
    })
  } catch {
    return
  }

  try {
    await api.nodeDeleteFirewallRule(props.cluster, props.member, handle)
    ElMessage.success(t('nodeActions.firewallRuleDeleted'))
    await refresh()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

async function connectWiFi(ssid: string): Promise<void> {
  if (!guardWrite() || wifiBusy.value) {
    return
  }
  const password = wifiForm.password
  wifiBusy.value = true
  try {
    await api.nodeWifiConnect(props.cluster, props.member, wifiDevice.value, ssid, password)
    ElMessage.success(t('nodeActions.wifiConnected', { ssid }))
    wifiDialogOpen.value = false
    wifiForm.password = ''
    await refresh()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    wifiBusy.value = false
  }
}

async function connectSavedNetwork(ssid: string): Promise<void> {
  try {
    await ElMessageBox.confirm(t('nodeActions.wifiConnectConfirm', { ssid }), t('nodeActions.wifiConnect'), {
      type: 'info',
      confirmButtonText: t('nodeActions.wifiConnect'),
      cancelButtonText: t('common.cancel'),
    })
  } catch {
    return
  }

  await connectWiFi(ssid)
}
</script>

<template>
  <section class="panel node-network">
    <div class="panel-head node-network-head">
      <div>
        <h3 class="section-title">{{ t('nodePage.title') }}</h3>
        <p class="subtitle">
          {{ t('nodePage.subtitle', { host: displayHost }) }}
        </p>
        <el-tag v-if="snapshot?.backend" size="small" type="info">
          {{ t('nodePage.backend', { backend: snapshot.backend }) }}
        </el-tag>
      </div>
      <div class="page-actions">
        <el-button :loading="historyLoading" @click="openHistory">
          {{ t('nodePage.history') }}
        </el-button>
        <el-button :icon="Refresh" :loading="loading" @click="refresh">
          {{ t('common.refresh') }}
        </el-button>
      </div>
    </div>

    <div class="panel-body">
    <el-alert
      v-if="loadError"
      class="fleet-alert"
      type="error"
      :closable="false"
      show-icon
      :title="t('common.loadFailed', { error: loadError })"
    />

    <el-alert
      v-if="!mayWrite"
      class="fleet-alert"
      type="info"
      :closable="false"
      show-icon
      :title="t('common.readOnly')"
    />

    <el-alert
      class="fleet-alert"
      type="warning"
      :closable="false"
      show-icon
      :title="t('nodePage.warning', { host: displayHost })"
    />

    <el-tabs v-model="networkTab" class="network-tabs">
      <el-tab-pane :label="t('nodePage.nicsTitle')" name="nics">
        <el-table v-loading="loading" :data="snapshot?.nics ?? []" size="small" stripe>
          <el-table-column :label="t('common.name')" min-width="120">
            <template #default="{ row }">
              <strong>{{ row.name }}</strong>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.type')" width="100">
            <template #default="{ row }">
              {{ row.kind }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="100">
            <template #default="{ row }">
              <el-tag :type="stateTag(row.state)" size="small">{{ row.state }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.address')" min-width="180">
            <template #default="{ row }">
              <div v-for="addr in row.addresses" :key="addr.address" class="mono addr-line">
                {{ addr.address }}/{{ addr.prefixlen }}
              </div>
              <span v-if="row.addresses.length === 0" class="muted">{{ t('common.none') }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('nodePage.wifi')" min-width="140">
            <template #default="{ row }">
              <span v-if="row.wifi?.connected">
                {{ row.wifi.ssid || t('nodeActions.wifiAssociated') }}<template v-if="row.wifi.signal"> · {{ row.wifi.signal }} dBm</template>
              </span>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="170" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" :disabled="!mayWrite" @click="openAddressDialog(row)">
                {{ t('nodePage.configure') }}
              </el-button>
              <el-button
                link
                :type="row.state === 'up' ? 'danger' : 'success'"
                :disabled="!mayWrite"
                :loading="nicBusy === row.name"
                @click="toggleNIC(row)"
              >
                {{ row.state === 'up' ? t('nodeActions.bringDown') : t('nodeActions.bringUp') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane :label="t('nodePage.routesTitle')" name="routes">
        <div class="tab-toolbar">
          <el-button :icon="Plus" type="primary" :disabled="!mayWrite" @click="routeDialogOpen = true">
            {{ t('nodePage.addRoute') }}
          </el-button>
        </div>

        <el-table v-loading="loading" :data="snapshot?.routes ?? []" size="small" stripe>
          <el-table-column :label="t('nodePage.destination')" min-width="160">
            <template #default="{ row }">
              <span class="mono">{{ row.destination }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('network.networkGateway')" min-width="130">
            <template #default="{ row }">
              <span class="mono">{{ row.gateway || t('common.none') }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="device" :label="t('nodePage.device')" width="120" />
          <el-table-column :label="t('common.actions')" width="100">
            <template #default="{ row }">
              <el-button v-if="row.destination !== 'default'" link type="danger" :disabled="!mayWrite" @click="confirmDeleteRoute(row.destination)">
                {{ t('common.delete') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane :label="t('nodePage.firewallTitle')" name="firewall">
        <div class="tab-toolbar">
          <p class="subtitle">{{ t('nodePage.firewallHint') }}</p>
          <el-button :icon="Plus" type="primary" :disabled="!mayWrite" @click="firewallDialogOpen = true">
            {{ t('nodePage.addRule') }}
          </el-button>
        </div>

        <el-table v-loading="loading" :data="snapshot?.firewall ?? []" size="small" stripe>
          <el-table-column :label="t('nodePage.rule')" min-width="240">
            <template #default="{ row }">
              <span class="mono">{{ row.rule }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="100">
            <template #default="{ row }">
              <el-button link type="danger" :disabled="!mayWrite" @click="confirmDeleteFirewall(row.handle)">
                {{ t('common.delete') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane v-if="wifiDevice" :label="t('nodePage.wifiTitle')" name="wifi">
        <div class="tab-toolbar">
          <p class="subtitle">{{ t('nodePage.wifiHint', { device: wifiDevice }) }}</p>
          <el-button :icon="Refresh" :loading="wifiScanning" @click="scanWifi">
            {{ t('nodePage.wifiScan') }}
          </el-button>
        </div>

        <el-table v-loading="loading || wifiScanning" :data="snapshot?.wifi_scan ?? []" size="small" stripe>
          <el-table-column :label="t('nodePage.ssid')" min-width="160">
            <template #default="{ row }">
              <strong>{{ row.ssid }}</strong>
            </template>
          </el-table-column>
          <el-table-column :label="t('nodePage.signal')" width="110">
            <template #default="{ row }">
              {{ row.signal }} dBm
            </template>
          </el-table-column>
          <el-table-column :label="t('nodePage.secure')" width="100">
            <template #default="{ row }">
              {{ row.secure ? t('common.yes') : t('common.no') }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="130">
            <template #default="{ row }">
              <el-button
                link
                type="primary"
                :disabled="!mayWrite || wifiBusy"
                @click="row.secure ? (wifiForm.ssid = row.ssid, wifiDialogOpen = true) : connectSavedNetwork(row.ssid)"
              >
                {{ t('nodeActions.wifiConnect') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>
    </div>

    <el-dialog v-model="addressDialogOpen" :title="t('nodePage.configureTitle', { name: addressTarget?.name })" width="520px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item>
          <el-radio-group v-model="addressForm.dhcp">
            <el-radio-button :value="false">{{ t('nodePage.static') }}</el-radio-button>
            <el-radio-button :value="true">DHCP</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <template v-if="!addressForm.dhcp">
          <el-form-item :label="t('nodePage.addressCidr')">
            <el-input v-model="addressForm.address" placeholder="10.0.0.50" />
          </el-form-item>
          <el-form-item :label="t('network.networkGateway')">
            <el-input v-model="addressForm.gateway" placeholder="10.0.0.1" />
          </el-form-item>
        </template>
        <el-form-item :label="t('nodePage.persist')">
          <el-switch v-model="addressForm.persist" />
          <span class="field-hint">{{ t('nodePage.persistHint') }}</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addressDialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="addressBusy" @click="submitAddress">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="routeDialogOpen" :title="t('nodePage.addRoute')" width="520px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item :label="t('nodePage.destination')">
          <el-input v-model="routeForm.destination" placeholder="10.0.1.0/24" />
        </el-form-item>
        <el-form-item :label="t('network.networkGateway')">
          <el-input v-model="routeForm.gateway" placeholder="10.0.0.1" />
        </el-form-item>
        <el-form-item :label="t('nodePage.device')">
          <el-select
            v-model="routeForm.device"
            filterable
            allow-create
            default-first-option
            placeholder="eth0"
            style="width: 100%"
          >
            <el-option
              v-for="nic in snapshot?.nics ?? []"
              :key="nic.name"
              :label="nic.name"
              :value="nic.name"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="routeDialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="routeBusy" @click="submitRoute">{{ t('common.create') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="firewallDialogOpen" :title="t('nodePage.addRule')" width="520px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item :label="t('nodePage.port')">
          <el-input-number v-model="firewallForm.port" :min="1" :max="65535" style="width: 100%" />
        </el-form-item>
        <el-form-item :label="t('common.type')">
          <el-radio-group v-model="firewallForm.proto">
            <el-radio-button value="tcp">TCP</el-radio-button>
            <el-radio-button value="udp">UDP</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="t('nodePage.action')">
          <el-radio-group v-model="firewallForm.action">
            <el-radio-button value="accept">{{ t('nodePage.accept') }}</el-radio-button>
            <el-radio-button value="drop">{{ t('nodePage.drop') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="t('assets.comment')">
          <el-input v-model="firewallForm.comment" :placeholder="t('nodePage.ruleComment')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="firewallDialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="firewallBusy" @click="submitFirewall">{{ t('common.create') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="wifiDialogOpen" :title="t('nodeActions.wifiConnectTitle', { ssid: wifiForm.ssid })" width="480px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item :label="t('nodeActions.wifiPassword')">
          <el-input v-model="wifiForm.password" type="password" show-password />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="wifiDialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="wifiBusy" :disabled="!wifiForm.password" @click="connectWiFi(wifiForm.ssid)">{{ t('nodeActions.wifiConnect') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="historyOpen" :title="t('nodePage.historyTitle', { host: displayHost })" width="860px">
      <template #header>
        <div class="history-header">
          <span>{{ t('nodePage.historyTitle', { host: displayHost }) }}</span>
          <el-button :icon="Refresh" :loading="historyLoading" @click="openHistory">
            {{ t('common.refresh') }}
          </el-button>
        </div>
      </template>
      <div v-loading="historyLoading">
        <el-alert
          v-if="historyError"
          class="fleet-alert"
          type="error"
          :closable="false"
          show-icon
          :title="historyError"
        >
          <el-button size="small" @click="openHistory">{{ t('common.retry') }}</el-button>
        </el-alert>
        <h4 class="section-title">{{ t('nodePage.tasks') }}</h4>
        <el-table :data="historyTasks" size="small" stripe :empty-text="t('common.none')">
          <el-table-column type="expand">
            <template #default="{ row }">
              <div class="history-detail">
                <div v-for="(value, key) in row" :key="key" class="history-detail-row">
                  <span class="history-detail-key">{{ key }}</span>
                  <pre>{{ typeof value === 'object' ? JSON.stringify(value, null, 2) : value }}</pre>
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('nodePage.taskType')" min-width="160">
            <template #default="{ row }">{{ row.type }}</template>
          </el-table-column>
          <el-table-column :label="t('nodePage.state')" width="110">
            <template #default="{ row }">{{ row.state }}</template>
          </el-table-column>
          <el-table-column :label="t('nodePage.error')" min-width="220">
            <template #default="{ row }">
              <span class="mono history-error">{{ row.error || t('common.none') }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('nodePage.time')" min-width="170">
            <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
          </el-table-column>
        </el-table>

        <h4 class="section-title history-heading">{{ t('nodePage.audit') }}</h4>
        <el-table :data="historyAudit" size="small" stripe :empty-text="t('common.none')">
          <el-table-column type="expand">
            <template #default="{ row }">
              <div class="history-detail">
                <div v-for="(value, key) in row" :key="key" class="history-detail-row">
                  <span class="history-detail-key">{{ key }}</span>
                  <pre>{{ typeof value === 'object' ? JSON.stringify(value, null, 2) : value }}</pre>
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column :label="t('nodePage.action')" min-width="160">
            <template #default="{ row }">{{ row.action }}</template>
          </el-table-column>
          <el-table-column :label="t('nodePage.backendLabel')" width="100">
            <template #default="{ row }">{{ row.backend }}</template>
          </el-table-column>
          <el-table-column :label="t('nodePage.result')" width="100">
            <template #default="{ row }">{{ row.result }}</template>
          </el-table-column>
          <el-table-column :label="t('nodePage.detail')" min-width="240">
            <template #default="{ row }">
              <span class="mono history-error">{{ row.detail || t('common.none') }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('nodePage.time')" min-width="170">
            <template #default="{ row }">{{ formatDateTime(row.at) }}</template>
          </el-table-column>
        </el-table>
      </div>
    </el-dialog>
  </section>
</template>

<style scoped>
.node-network {
  width: 100%;
  min-width: 0;
}

.node-network-head {
  align-items: flex-start;
}

.node-network-head .subtitle {
  margin-top: 4px;
}

.node-network-head .el-tag {
  margin-top: 6px;
}

.node-network .panel-body {
  padding-top: 14px;
}

.tab-toolbar {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}

.tab-toolbar .subtitle {
  margin-right: auto;
  color: var(--fleet-ink-soft);
  font-size: 13px;
}

.network-tabs :deep(.el-tabs__header) {
  margin-bottom: 14px;
}

.addr-line {
  font-size: 12px;
  line-height: 1.45;
}

.field-hint {
  margin-left: 10px;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  font-size: 12px;
}

.history-heading {
  margin-top: 20px;
}

.history-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 16px;
  font-weight: 600;
}

.history-detail {
  padding: 8px 12px 8px 48px;
  display: grid;
  gap: 6px;
}

.history-detail-row {
  display: grid;
  grid-template-columns: 160px 1fr;
  gap: 8px;
  align-items: start;
}

.history-detail-key {
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  font-size: 12px;
}

.history-detail-row pre {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
  font-size: 12px;
}

.history-error {
  display: block;
  white-space: pre-wrap;
  word-break: break-all;
  font-size: 12px;
}
</style>

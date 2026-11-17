<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { NodeNIC } from '@/api/types'
import { api } from '@/api'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  cluster: string
  member: string
}>()

const { t } = useI18n()

const loading = ref(false)
const snapshot = ref<{ host: string; nics: NodeNIC[]; routes: { destination: string; gateway?: string; device?: string; metric?: string }[]; firewall: { handle: string; rule: string }[]; wifi_scan?: { ssid: string; signal: number; secure: boolean; frequency: number }[]; wifi_device?: string; backend?: string } | null>(null)

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
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    loading.value = false
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

  try {
    await api.nodeNICState(props.cluster, props.member, nic.name, up)
    ElMessage.success(t('nodeActions.stateChanged', { name: nic.name }))
    await refresh()
  } catch (err) {
    ElMessage.error(errorMessage(err))
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
  const target = addressTarget.value
  if (!target) {
    return
  }

  try {
    await api.nodeApplyAddress(props.cluster, props.member, target.name, {
      dhcp: addressForm.dhcp,
      address: addressForm.dhcp ? undefined : addressForm.address.trim(),
      gateway: addressForm.dhcp ? undefined : addressForm.gateway.trim(),
      persist: addressForm.persist,
    })
    ElMessage.success(t('nodeActions.addressApplied', { name: target.name }))
    addressDialogOpen.value = false
    await refresh()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

async function submitRoute(): Promise<void> {
  try {
    await api.nodeAddRoute(
      props.cluster,
      props.member,
      routeForm.destination.trim(),
      routeForm.gateway.trim(),
      routeForm.device.trim(),
    )
    ElMessage.success(t('nodeActions.routeAdded'))
    routeDialogOpen.value = false
    await refresh()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

async function confirmDeleteRoute(destination: string): Promise<void> {
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
  }
}

async function confirmDeleteFirewall(handle: string): Promise<void> {
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
  try {
    await api.nodeWifiConnect(props.cluster, props.member, wifiDevice.value, ssid, wifiForm.password)
    ElMessage.success(t('nodeActions.wifiConnected', { ssid }))
    wifiDialogOpen.value = false
    wifiForm.password = ''
    await refresh()
  } catch (err) {
    ElMessage.error(errorMessage(err))
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
  <div class="page">
    <div class="page-header">
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
        <el-button :icon="Refresh" :loading="loading" @click="refresh">
          {{ t('common.refresh') }}
        </el-button>
      </div>
    </div>

    <el-alert
      class="fleet-alert"
      type="warning"
      :closable="false"
      show-icon
      :title="t('nodePage.warning', { host: displayHost })"
    />

    <section class="panel">
      <div class="panel-body">
        <div class="section-heading">
          <h3 class="section-title">{{ t('nodePage.nicsTitle') }}</h3>
        </div>

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
              <el-button link type="primary" @click="openAddressDialog(row)">
                {{ t('nodePage.configure') }}
              </el-button>
              <el-button link :type="row.state === 'up' ? 'danger' : 'success'" @click="toggleNIC(row)">
                {{ row.state === 'up' ? t('nodeActions.bringDown') : t('nodeActions.bringUp') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </section>

    <section class="panel">
      <div class="panel-body">
        <div class="section-heading page-header">
          <div>
            <h3 class="section-title">{{ t('nodePage.routesTitle') }}</h3>
          </div>
          <div class="page-actions">
            <el-button :icon="Plus" type="primary" @click="routeDialogOpen = true">
              {{ t('nodePage.addRoute') }}
            </el-button>
          </div>
        </div>

        <el-table :data="snapshot?.routes ?? []" size="small" stripe>
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
              <el-button v-if="row.destination !== 'default'" link type="danger" @click="confirmDeleteRoute(row.destination)">
                {{ t('common.delete') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </section>

    <section class="panel">
      <div class="panel-body">
        <div class="section-heading page-header">
          <div>
            <h3 class="section-title">{{ t('nodePage.firewallTitle') }}</h3>
            <p class="subtitle">{{ t('nodePage.firewallHint') }}</p>
          </div>
          <div class="page-actions">
            <el-button :icon="Plus" type="primary" @click="firewallDialogOpen = true">
              {{ t('nodePage.addRule') }}
            </el-button>
          </div>
        </div>

        <el-table :data="snapshot?.firewall ?? []" size="small" stripe>
          <el-table-column :label="t('nodePage.rule')" min-width="240">
            <template #default="{ row }">
              <span class="mono">{{ row.rule }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="100">
            <template #default="{ row }">
              <el-button link type="danger" @click="confirmDeleteFirewall(row.handle)">
                {{ t('common.delete') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </section>

    <section v-if="wifiDevice" class="panel">
      <div class="panel-body">
        <div class="section-heading page-header">
          <div>
            <h3 class="section-title">{{ t('nodePage.wifiTitle') }}</h3>
            <p class="subtitle">{{ t('nodePage.wifiHint', { device: wifiDevice }) }}</p>
          </div>
        </div>

        <el-table :data="snapshot?.wifi_scan ?? []" size="small" stripe>
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
                @click="row.secure ? (wifiForm.ssid = row.ssid, wifiDialogOpen = true) : connectSavedNetwork(row.ssid)"
              >
                {{ t('nodeActions.wifiConnect') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </section>

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
        <el-button type="primary" @click="submitAddress">{{ t('common.save') }}</el-button>
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
          <el-input v-model="routeForm.device" placeholder="eth0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="routeDialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" @click="submitRoute">{{ t('common.create') }}</el-button>
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
        <el-button type="primary" @click="submitFirewall">{{ t('common.create') }}</el-button>
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
        <el-button type="primary" @click="connectWiFi(wifiForm.ssid)">{{ t('nodeActions.wifiConnect') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.addr-line {
  font-size: 12px;
}

.field-hint {
  margin-left: 10px;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  font-size: 12px;
}
</style>

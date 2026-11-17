<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import type {
  AgentEnrollmentToken,
  AgentSummary,
  ClusterMemberJoinRequest,
  ClusterMemberJoinResult,
} from '@/api/types'
import { api } from '@/api'
import { errorMessage } from '@/utils/errors'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{
  modelValue: boolean
  cluster: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  added: [result: ClusterMemberJoinResult]
}>()

const { t } = useI18n()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const submitting = ref(false)
const result = ref<ClusterMemberJoinResult | null>(null)
const mode = ref<'agent' | 'ssh'>('agent')
const agents = ref<AgentSummary[]>([])
const agentLoading = ref(false)
const agentError = ref('')
const enrollment = ref<AgentEnrollmentToken | null>(null)
const polling = ref(false)
const bootstrapping = ref(false)
const agentInstallIncus = ref(false)
const agentVersionPin = ref('')
let pollTimer: ReturnType<typeof setTimeout> | null = null

const memberName = computed(() => form.name.trim())
const agent = computed(() =>
  agents.value.find((item) => item.cluster === props.cluster && item.member === memberName.value),
)
const agentOnline = computed(() => agent.value?.status === 'online')
const enrollCommand = computed(() => {
  if (!enrollment.value) {
    return ''
  }
  return `printf '%s' '${enrollment.value.token}' | sudo incus-fleet-agent enroll \
  --center https://<center>:8444 \
  --ca /etc/incus-fleet/agent-ca.crt \
  --cluster ${props.cluster} --member ${memberName.value}`
})

const form = reactive({
  name: '',
  address: '',
  ssh_user: '',
  ssh_port: 22,
  ssh_password: '',
  sudo_password: '',
  install_incus: true,
  install_vms: true,
  ignore_host_key: true,
  storage_source: '/srv/incus',
  incus_repo: '',
  incus_repo_key: '',
})

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      stopPolling()
      return
    }

    result.value = null
    mode.value = 'agent'
    enrollment.value = null
    agentError.value = ''
    agentInstallIncus.value = false
    agentVersionPin.value = ''
    stopPolling()
    void refreshAgents().then(() => {
      if (agent.value && !agentOnline.value) {
        startPolling()
      }
    })
    form.name = ''
    form.address = ''
    form.ssh_user = ''
    form.ssh_port = 22
    form.ssh_password = ''
    form.sudo_password = ''
    form.install_incus = true
    form.install_vms = true
    form.ignore_host_key = true
    form.storage_source = '/srv/incus'
    form.incus_repo = ''
    form.incus_repo_key = ''
  },
  { immediate: true },
)

watch(memberName, () => {
  if (mode.value === 'agent' && enrollment.value) {
    enrollment.value = null
    stopPolling()
  }
})

async function refreshAgents(): Promise<void> {
  agentLoading.value = true
  try {
    agents.value = (await api.listAgents(props.cluster)) ?? []
    agentError.value = ''
  } catch (err) {
    agentError.value = errorMessage(err)
  } finally {
    agentLoading.value = false
  }
}

function stopPolling(): void {
  polling.value = false
  if (pollTimer) {
    clearTimeout(pollTimer)
    pollTimer = null
  }
}

function startPolling(): void {
  stopPolling()
  polling.value = true
  const tick = async (): Promise<void> => {
    await refreshAgents()
    if (agentOnline.value) {
      polling.value = false
      return
    }
    if (visible.value && mode.value === 'agent') {
      pollTimer = setTimeout(() => {
        void tick()
      }, 3000)
    }
  }
  void tick()
}

async function createEnrollmentToken(): Promise<void> {
  if (!memberName.value) {
    ElMessage.error(t('cluster.agentNameRequired'))
    return
  }
  try {
    enrollment.value = await api.createAgentEnrollmentToken(props.cluster, memberName.value)
    startPolling()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

function switchMode(next: 'agent' | 'ssh'): void {
  mode.value = next
  if (next === 'ssh') {
    stopPolling()
    return
  }
  agentError.value = ''
  void refreshAgents()
  if (enrollment.value) {
    startPolling()
  }
}

async function bootstrapWithAgent(): Promise<void> {
  if (!form.address.trim()) {
    ElMessage.error(t('cluster.addNodeAddressRequired'))
    return
  }
  if (!agentOnline.value || !agent.value) {
    ElMessage.error(t('cluster.agentNotOnline'))
    return
  }

  bootstrapping.value = true
  try {
    const bootstrapped = await api.bootstrapAgent(agent.value.id, {
      mode: 'join',
      address: form.address.trim(),
      storage_source: form.storage_source.trim() || undefined,
      install_mode: agentInstallIncus.value ? 'package' : 'existing',
      version_pin: agentVersionPin.value.trim() || undefined,
    })
    const joined: ClusterMemberJoinResult = {
      member: { name: memberName.value, status: 'Online', database: false, roles: [] },
      steps: bootstrapped.steps.map((step) => ({ name: step, status: 'ok' })),
    }
    result.value = joined
    emit('added', joined)
    stopPolling()
    ElMessage.success(t('cluster.agentBootstrapSuccess', { name: memberName.value }))
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    bootstrapping.value = false
  }
}

onBeforeUnmount(stopPolling)

function stepTagType(status: string): 'success' | 'warning' | 'info' | 'danger' {
  switch (status) {
    case 'ok':
      return 'success'
    case 'warning':
      return 'warning'
    case 'skipped':
      return 'info'
    default:
      return 'danger'
  }
}

async function submit(): Promise<void> {
  if (!form.address.trim()) {
    ElMessage.error(t('cluster.addNodeAddressRequired'))
    return
  }

  const request: ClusterMemberJoinRequest = {
    address: form.address.trim(),
    install_incus: form.install_incus,
    install_vms: form.install_vms,
    ignore_host_key: form.ignore_host_key,
  }

  if (form.name.trim()) {
    request.name = form.name.trim()
  }
  if (form.ssh_user.trim()) {
    request.ssh_user = form.ssh_user.trim()
  }
  if (form.ssh_port > 0) {
    request.ssh_port = form.ssh_port
  }
  if (form.ssh_password) {
    request.ssh_password = form.ssh_password
  }
  if (form.sudo_password) {
    request.sudo_password = form.sudo_password
  }
  if (form.storage_source.trim()) {
    request.storage_source = form.storage_source.trim()
  }
  if (form.incus_repo.trim()) {
    request.incus_repo = form.incus_repo.trim()
  }
  if (form.incus_repo_key.trim()) {
    request.incus_repo_key = form.incus_repo_key.trim()
  }

  submitting.value = true
  try {
    const joined = await api.joinClusterMember(props.cluster, request)
    result.value = joined
    emit('added', joined)
    ElMessage.success(t('cluster.addNodeSuccess', { name: joined.member.name }))
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    submitting.value = false
  }
}

function close(): void {
  visible.value = false
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="t('cluster.addNodeTitle', { cluster })"
    width="660px"
    destroy-on-close
    :close-on-click-modal="!submitting"
    :close-on-press-escape="!submitting"
  >
    <template v-if="!result">
      <el-alert
        class="join-alert"
        type="warning"
        :closable="false"
        show-icon
        :title="t('cluster.addNodeWarning')"
      />

      <div class="mode-switch">
        <button
          type="button"
          :class="{ active: mode === 'agent' }"
          :disabled="submitting || bootstrapping"
          @click="switchMode('agent')"
        >
          {{ t('cluster.agentMode') }}
        </button>
        <button
          type="button"
          :class="{ active: mode === 'ssh' }"
          :disabled="submitting || bootstrapping"
          @click="switchMode('ssh')"
        >
          {{ t('cluster.sshMode') }}
        </button>
      </div>

      <div v-if="mode === 'agent'" class="join-form">
        <el-alert
          class="join-alert"
          type="info"
          :closable="false"
          show-icon
          :title="t('cluster.agentHint')"
        />

        <section class="form-section">
          <h4>{{ t('cluster.addNodeTarget') }}</h4>
          <div class="field-grid">
            <div class="field">
              <label class="required">{{ t('cluster.addNodeName') }}</label>
              <el-input v-model="form.name" :placeholder="t('cluster.agentNamePlaceholder')" />
              <span class="hint">{{ t('cluster.agentNameHint') }}</span>
            </div>
            <div class="field">
              <label class="required">{{ t('cluster.addNodeAddress') }}</label>
              <el-input v-model="form.address" placeholder="198.51.100.11" />
            </div>
          </div>
        </section>

        <section class="form-section">
          <h4>{{ t('cluster.agentTitle') }}</h4>
          <el-alert
            v-if="agentError"
            class="join-alert"
            type="warning"
            :closable="false"
            show-icon
            :title="agentError"
          />

          <div v-if="agent" class="agent-status">
            <el-tag :type="agentOnline ? 'success' : 'warning'" size="small">
              {{ agent.status }}
            </el-tag>
            <span>{{ agent.hostname || agent.member }}</span>
            <span v-if="agent.version" class="hint">{{ agent.version }}</span>
            <el-button size="small" :loading="agentLoading" @click="refreshAgents">
              {{ t('common.refresh') }}
            </el-button>
          </div>
          <p v-else class="hint">{{ t('cluster.agentNone') }}</p>

          <p v-if="agent && !agentOnline" class="hint">{{ t('cluster.agentOfflineHint') }}</p>

          <el-button
            v-if="!agent"
            type="primary"
            plain
            :loading="agentLoading"
            @click="createEnrollmentToken"
          >
            {{ t('cluster.agentCreateToken') }}
          </el-button>

          <div v-if="enrollment && !agentOnline" class="token-box">
            <label>{{ t('cluster.agentToken') }}</label>
            <el-input :model-value="enrollment.token" readonly />
            <span class="hint">
              {{ t('cluster.agentTokenExpires', { at: formatDateTime(enrollment.expires_at) }) }}
            </span>
            <pre class="agent-command">{{ enrollCommand }}</pre>
            <span class="hint">
              {{ t('cluster.agentWaiting') }}
              <el-tag v-if="polling" size="small" type="info">{{ t('cluster.agentPolling') }}</el-tag>
            </span>
          </div>

          <el-button link type="primary" @click="switchMode('ssh')">
            {{ t('cluster.useSSH') }}
          </el-button>
        </section>

        <section class="form-section">
          <h4>{{ t('cluster.addNodeInstall') }}</h4>
          <div class="switch-row">
            <div>
              <strong>{{ t('cluster.agentInstallIncus') }}</strong>
              <p class="hint">{{ t('cluster.agentInstallIncusHint') }}</p>
            </div>
            <el-switch v-model="agentInstallIncus" />
          </div>
          <div class="field">
            <label>{{ t('cluster.addNodeStorageSource') }}</label>
            <el-input v-model="form.storage_source" placeholder="/srv/incus" />
            <span class="hint">{{ t('cluster.addNodeStorageSourceHint') }}</span>
          </div>
          <div class="field">
            <label>{{ t('cluster.agentVersionPin') }}</label>
            <el-input v-model="agentVersionPin" :placeholder="t('cluster.agentVersionPinPlaceholder')" />
            <span class="hint">{{ t('cluster.agentVersionPinHint') }}</span>
          </div>
        </section>
      </div>

      <div v-else class="join-form">
        <section class="form-section">
          <h4>{{ t('cluster.addNodeTarget') }}</h4>
          <div class="field-grid">
            <div class="field">
              <label>{{ t('cluster.addNodeName') }}</label>
              <el-input v-model="form.name" :placeholder="t('cluster.addNodeNamePlaceholder')" />
              <span class="hint">{{ t('cluster.addNodeNameHint') }}</span>
            </div>
            <div class="field">
              <label class="required">{{ t('cluster.addNodeAddress') }}</label>
              <el-input v-model="form.address" placeholder="198.51.100.11" />
            </div>
          </div>
        </section>

        <section class="form-section">
          <h4>{{ t('cluster.addNodeSSH') }}</h4>
          <div class="field-grid">
            <div class="field">
              <label>{{ t('cluster.addNodeSSHUser') }}</label>
              <el-input v-model="form.ssh_user" :placeholder="t('cluster.addNodeSSHUserHint')" />
            </div>
            <div class="field">
              <label>{{ t('cluster.addNodeSSHPort') }}</label>
              <el-input-number v-model="form.ssh_port" :min="1" :max="65535" style="width: 100%" />
            </div>
            <div class="field">
              <label>{{ t('cluster.addNodeSSHPassword') }}</label>
              <el-input v-model="form.ssh_password" type="password" show-password autocomplete="new-password" />
              <span class="hint">{{ t('cluster.addNodeSSHPasswordHint') }}</span>
            </div>
            <div class="field">
              <label>{{ t('cluster.addNodeSudoPassword') }}</label>
              <el-input v-model="form.sudo_password" type="password" show-password autocomplete="new-password" />
              <span class="hint">{{ t('cluster.addNodeSudoPasswordHint') }}</span>
            </div>
          </div>
        </section>

        <section class="form-section">
          <h4>{{ t('cluster.addNodeInstall') }}</h4>
          <div class="switch-row">
            <div>
              <strong>{{ t('cluster.addNodeInstallIncus') }}</strong>
              <p class="hint">{{ t('cluster.addNodeInstallIncusHint') }}</p>
            </div>
            <el-switch v-model="form.install_incus" />
          </div>
          <div class="switch-row">
            <div>
              <strong>{{ t('cluster.addNodeInstallVMs') }}</strong>
              <p class="hint">{{ t('cluster.addNodeInstallVMsHint') }}</p>
            </div>
            <el-switch v-model="form.install_vms" />
          </div>
          <div class="switch-row">
            <div>
              <strong>{{ t('cluster.addNodeIgnoreHostKey') }}</strong>
              <p class="hint">{{ t('cluster.addNodeIgnoreHostKeyHint') }}</p>
            </div>
            <el-switch v-model="form.ignore_host_key" />
          </div>

          <div class="field">
            <label>{{ t('cluster.addNodeStorageSource') }}</label>
            <el-input v-model="form.storage_source" placeholder="/srv/incus" />
            <span class="hint">{{ t('cluster.addNodeStorageSourceHint') }}</span>
          </div>
        </section>

        <el-collapse class="advanced">
          <el-collapse-item :title="t('cluster.addNodeAdvanced')" name="advanced">
            <div class="field">
              <label>{{ t('cluster.addNodeRepo') }}</label>
              <el-input v-model="form.incus_repo" :placeholder="t('cluster.addNodeRepoPlaceholder')" />
            </div>
            <div class="field">
              <label>{{ t('cluster.addNodeRepoKey') }}</label>
              <el-input v-model="form.incus_repo_key" :placeholder="t('cluster.addNodeRepoKeyPlaceholder')" />
            </div>
          </el-collapse-item>
        </el-collapse>
      </div>

      <el-alert
        v-if="submitting || bootstrapping"
        class="join-alert"
        type="info"
        :closable="false"
        show-icon
        :title="mode === 'agent' ? t('cluster.agentBootstrapRunning') : t('cluster.addNodeRunning')"
      />
    </template>

    <template v-else>
      <el-alert
        class="join-alert"
        type="success"
        :closable="false"
        show-icon
        :title="t('cluster.addNodeSuccessTitle', { name: result.member.name })"
      />

      <ul class="step-list">
        <li v-for="step in result.steps" :key="`${step.name}-${step.detail}`">
          <el-tag :type="stepTagType(step.status)" size="small" effect="plain">
            {{ t(`cluster.addNodeStepStatus.${step.status}`) }}
          </el-tag>
          <span class="step-name">{{ t(`cluster.addNodeSteps.${step.name}`) }}</span>
          <span v-if="step.detail" class="step-detail">{{ step.detail }}</span>
        </li>
      </ul>
    </template>

    <template #footer>
      <el-button :disabled="submitting || bootstrapping" @click="close">
        {{ result ? t('common.close') : t('common.cancel') }}
      </el-button>
      <el-button
        v-if="!result"
        type="primary"
        :loading="mode === 'agent' ? bootstrapping : submitting"
        :disabled="submitting || bootstrapping || (mode === 'agent' && !agentOnline)"
        @click="mode === 'agent' ? bootstrapWithAgent() : submit()"
      >
        {{ mode === 'agent' ? t('cluster.agentBootstrap') : t('cluster.addNodeSubmit') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.join-alert {
  margin-bottom: 14px;
}

.join-form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.mode-switch {
  display: inline-flex;
  gap: 4px;
  padding: 3px;
  margin-bottom: 14px;
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
}

.mode-switch button {
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  padding: 6px 14px;
  font: inherit;
  font-size: 13px;
  cursor: pointer;
}

.mode-switch button.active {
  background: var(--el-fill-color);
  color: var(--el-text-color-primary);
  font-weight: 600;
}

.agent-status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  font-size: 13px;
}

.token-box {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 12px 0;
}

.agent-command {
  margin: 4px 0;
  padding: 10px;
  border-radius: 6px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-regular);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
}

.form-section h4 {
  margin: 0 0 10px;
  font-size: 13px;
  font-weight: 600;
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px 16px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: 10px;
}

.field label {
  font-size: 12px;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
}

.field label.required::after {
  content: ' *';
  color: var(--el-color-danger);
}

.hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.45;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
}

.switch-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 8px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.switch-row strong {
  font-size: 13px;
}

.advanced {
  margin-top: 4px;
}

.step-list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.step-list li {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: 13px;
}

.step-name {
  font-weight: 600;
}

.step-detail {
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  font-size: 12px;
  word-break: break-word;
}
</style>

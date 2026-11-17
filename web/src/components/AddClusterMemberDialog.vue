<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import type { ClusterMemberJoinRequest, ClusterMemberJoinResult } from '@/api/types'
import { api } from '@/api'
import { errorMessage } from '@/utils/errors'

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
      return
    }

    result.value = null
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

      <div class="join-form">
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
        v-if="submitting"
        class="join-alert"
        type="info"
        :closable="false"
        show-icon
        :title="t('cluster.addNodeRunning')"
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
      <el-button :disabled="submitting" @click="close">
        {{ result ? t('common.close') : t('common.cancel') }}
      </el-button>
      <el-button
        v-if="!result"
        type="primary"
        :loading="submitting"
        :disabled="submitting"
        @click="submit"
      >
        {{ t('cluster.addNodeSubmit') }}
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

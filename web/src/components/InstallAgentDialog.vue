<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import { api } from '@/api'
import type { GuestAgentInstallRequest, GuestAgentStep } from '@/api/types'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  instance: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  done: []
}>()

const { t } = useI18n()

// Backend step names map onto i18n keys; hyphenated names cannot be message
// keys directly.
const STEP_LABELS: Record<string, string> = {
  validate: 'guestAgent.steps.validate',
  'resolve-address': 'guestAgent.steps.resolveAddress',
  connect: 'guestAgent.steps.connect',
  'check-guest': 'guestAgent.steps.check',
  'mount-config': 'guestAgent.steps.mount',
  install: 'guestAgent.steps.install',
  cleanup: 'guestAgent.steps.cleanup',
  verify: 'guestAgent.steps.verify',
}

const STEP_TAG_TYPES: Record<string, 'success' | 'danger' | 'warning' | 'info'> = {
  ok: 'success',
  failed: 'danger',
  warn: 'warning',
}

const form = reactive({
  address: '',
  ssh_user: 'root',
  ssh_password: '',
  sudo_password: '',
})
const submitting = ref(false)
const result = ref<GuestAgentStep[] | null>(null)

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      return
    }

    form.address = ''
    form.ssh_user = 'root'
    form.ssh_password = ''
    form.sudo_password = ''
    submitting.value = false
    result.value = null
  },
  { immediate: true },
)

function stepLabel(name: string): string {
  const key = STEP_LABELS[name]
  return key ? t(key) : name
}

function stepTagType(status: string): 'success' | 'danger' | 'warning' | 'info' {
  return STEP_TAG_TYPES[status] ?? 'info'
}

async function submit(): Promise<void> {
  if (!form.ssh_user.trim()) {
    ElMessage.error(t('guestAgent.userRequired'))
    return
  }
  if (!form.ssh_password && !form.sudo_password) {
    ElMessage.error(t('guestAgent.passwordRequired'))
    return
  }

  submitting.value = true
  try {
    const request: GuestAgentInstallRequest = {
      ssh_user: form.ssh_user.trim(),
    }
    if (form.ssh_password) {
      request.ssh_password = form.ssh_password
    }
    if (form.sudo_password) {
      request.sudo_password = form.sudo_password
    }
    if (form.address.trim()) {
      request.address = form.address.trim()
    }

    const outcome = await api.installGuestAgent(props.cluster, props.instance, request)
    result.value = outcome.steps
    // The agent may already be reporting state; refresh so the guest section
    // and the button visibility follow.
    emit('done')
  } catch (err) {
    ElMessage.error(t('guestAgent.failed', { error: errorMessage(err) }))
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="t('guestAgent.title')"
    width="520px"
    destroy-on-close
    :close-on-click-modal="!submitting"
    :close-on-press-escape="!submitting"
  >
    <template v-if="!result">
      <el-alert
        class="agent-alert"
        :title="t('guestAgent.hint')"
        type="info"
        :closable="false"
        show-icon
      />

      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item :label="t('guestAgent.address')">
          <el-input v-model="form.address" :placeholder="t('guestAgent.addressPlaceholder')" />
        </el-form-item>

        <el-form-item :label="t('guestAgent.sshUser')">
          <el-input v-model="form.ssh_user" />
        </el-form-item>

        <el-form-item :label="t('guestAgent.sshPassword')">
          <el-input
            v-model="form.ssh_password"
            type="password"
            show-password
            autocomplete="new-password"
          />
        </el-form-item>

        <el-form-item :label="t('guestAgent.sudoPassword')">
          <el-input
            v-model="form.sudo_password"
            type="password"
            show-password
            autocomplete="new-password"
          />
          <p class="field-hint">{{ t('guestAgent.sudoHint') }}</p>
        </el-form-item>
      </el-form>

      <el-alert
        v-if="submitting"
        class="agent-alert"
        :title="t('guestAgent.running')"
        type="info"
        :closable="false"
        show-icon
      />
    </template>

    <template v-else>
      <ul class="step-list">
        <li v-for="step in result" :key="`${step.name}-${step.detail}`">
          <el-tag :type="stepTagType(step.status)" size="small" effect="plain">
            {{ t(`guestAgent.stepStatus.${step.status}`) }}
          </el-tag>
          <span class="step-name">{{ stepLabel(step.name) }}</span>
          <span v-if="step.detail" class="step-detail">{{ step.detail }}</span>
        </li>
      </ul>
    </template>

    <template #footer>
      <el-button :disabled="submitting" @click="visible = false">
        {{ result ? t('common.close') : t('common.cancel') }}
      </el-button>
      <el-button v-if="!result" type="primary" :loading="submitting" @click="submit">
        {{ t('guestAgent.open') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.agent-alert {
  margin-bottom: 12px;
}

.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}

.step-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.step-list li {
  display: flex;
  align-items: baseline;
  gap: 10px;
  padding: 6px 0;
}

.step-name {
  font-weight: 600;
}

.step-detail {
  color: var(--fleet-ink-soft);
  font-size: 12px;
  word-break: break-all;
}
</style>

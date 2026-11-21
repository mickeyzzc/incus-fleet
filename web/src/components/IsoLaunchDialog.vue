<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import { api, type UnattendedAnswerPreview, type UnattendedProfile } from '@/api'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  isoName: string
  isoPool: string
  networks: string[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  launched: [name: string]
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const form = reactive({
  name: '',
  network: '',
  rootDisk: '',
})

const unattended = reactive({
  enabled: false,
  profile: 'debian-preseed' as UnattendedProfile,
  hostname: '',
  username: '',
  password: '',
  timezone: 'UTC',
  custom: false,
  answer: '',
})

const preview = ref<UnattendedAnswerPreview | null>(null)
const previewing = ref(false)

const profiles: { value: UnattendedProfile; labelKey: string }[] = [
  { value: 'debian-preseed', labelKey: 'iso.profileDebian' },
  { value: 'ubuntu-autoinstall', labelKey: 'iso.profileUbuntu' },
  { value: 'kickstart', labelKey: 'iso.profileKickstart' },
  { value: 'nocloud', labelKey: 'iso.profileNocloud' },
]

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

// Guess the installer family from the ISO name so the common case is a
// single click away; the operator can always override.
function guessProfile(iso: string): UnattendedProfile {
  const name = iso.toLowerCase()
  if (name.includes('debian') || name.includes('devuan')) return 'debian-preseed'
  if (name.includes('ubuntu')) return 'ubuntu-autoinstall'
  if (/(rhel|rocky|almalinux|centos|fedora|oracle)/.test(name)) return 'kickstart'
  return 'nocloud'
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      form.name = ''
      form.network = ''
      form.rootDisk = ''
      unattended.enabled = false
      unattended.profile = guessProfile(props.isoName)
      unattended.hostname = ''
      unattended.username = ''
      unattended.password = ''
      unattended.timezone = 'UTC'
      unattended.custom = false
      unattended.answer = ''
      preview.value = null
    }
  },
  { immediate: true },
)

function validateUnattended(): string {
  if (!unattended.custom && !unattended.username.trim()) {
    return t('iso.usernameRequired')
  }
  if (unattended.custom && !unattended.answer.trim()) {
    return t('iso.answerRequired')
  }
  return ''
}

async function loadPreview(): Promise<void> {
  previewing.value = true
  try {
    preview.value = await api.previewUnattendedAnswer(props.cluster, props.isoPool, props.isoName, {
      name: form.name.trim() || 'preview',
      profile: unattended.profile,
      answer_file: unattended.custom ? unattended.answer : undefined,
      spec: unattended.custom
        ? undefined
        : {
            hostname: unattended.hostname || form.name.trim(),
            username: unattended.username,
            password: unattended.password,
            timezone: unattended.timezone,
          },
    })
  } catch (err) {
    ElMessage.error(t('iso.previewFailed', { error: errorMessage(err) }))
  } finally {
    previewing.value = false
  }
}

async function submit(): Promise<void> {
  if (!form.name.trim()) {
    ElMessage.error(t('iso.nameRequired'))
    return
  }

  if (unattended.enabled) {
    const problem = validateUnattended()
    if (problem) {
      ElMessage.error(problem)
      return
    }

    try {
      const launch = await instances.launchUnattended(props.cluster, props.isoPool, props.isoName, {
        name: form.name.trim(),
        profile: unattended.profile,
        answer_file: unattended.custom ? unattended.answer : undefined,
        spec: unattended.custom
          ? undefined
          : {
              hostname: unattended.hostname || form.name.trim(),
              username: unattended.username,
              password: unattended.password,
              timezone: unattended.timezone,
            },
        network: form.network || undefined,
        root_disk: form.rootDisk.trim() || undefined,
      })
      ElMessage.success(t('iso.unattendedLaunched', { name: launch?.name ?? form.name.trim() }))
      emit('launched', form.name.trim())
      visible.value = false
    } catch (err) {
      ElMessage.error(t('iso.launchFailed', { error: errorMessage(err) }))
    }

    return
  }

  try {
    await instances.launchFromISO(props.cluster, props.isoPool, props.isoName, {
      name: form.name.trim(),
      network: form.network || undefined,
      root_disk: form.rootDisk.trim() || undefined,
    })
    ElMessage.success(t('iso.launched', { name: form.name.trim() }))
    emit('launched', form.name.trim())
    visible.value = false
  } catch (err) {
    ElMessage.error(t('iso.launchFailed', { error: errorMessage(err) }))
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`${t('iso.launchTitle')} · ${isoName}`"
    width="560px"
    destroy-on-close
  >
    <p class="launch-hint">{{ t('iso.launchHint') }}</p>

    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('iso.vmName')">
        <el-input v-model="form.name" placeholder="installer-01" />
      </el-form-item>

      <el-form-item :label="t('form.network')">
        <el-select v-model="form.network" clearable style="width: 100%">
          <el-option
            v-for="network in networks"
            :key="network"
            :label="network"
            :value="network"
          />
        </el-select>
        <p class="field-hint">{{ t('iso.networkHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('form.rootDisk')">
        <el-input v-model="form.rootDisk" placeholder="20GiB" />
      </el-form-item>

      <el-form-item>
        <el-switch v-model="unattended.enabled" />
        <span class="mode-label">{{ t('iso.unattended') }}</span>
        <el-tooltip :content="t('iso.unattendedHint')" placement="top">
          <span class="mode-help">?</span>
        </el-tooltip>
      </el-form-item>

      <template v-if="unattended.enabled">
        <el-form-item :label="t('iso.unattendedProfile')">
          <el-select v-model="unattended.profile" style="width: 100%">
            <el-option
              v-for="item in profiles"
              :key="item.value"
              :label="t(item.labelKey)"
              :value="item.value"
            />
          </el-select>
          <p class="field-hint">{{ t('iso.profileHint') }}</p>
        </el-form-item>

        <el-form-item>
          <el-checkbox v-model="unattended.custom">{{ t('iso.customAnswer') }}</el-checkbox>
        </el-form-item>

        <el-form-item v-if="unattended.custom" :label="t('iso.answerFile')">
          <el-input
            v-model="unattended.answer"
            type="textarea"
            :rows="10"
            :placeholder="t('iso.answerPlaceholder')"
          />
        </el-form-item>

        <template v-else>
          <el-form-item :label="t('iso.specUsername')">
            <el-input v-model="unattended.username" placeholder="fleet" />
          </el-form-item>
          <el-form-item :label="t('iso.specPassword')">
            <el-input v-model="unattended.password" type="password" show-password />
          </el-form-item>
          <el-form-item :label="t('iso.specTimezone')">
            <el-input v-model="unattended.timezone" placeholder="UTC" />
          </el-form-item>
        </template>

        <el-form-item>
          <el-button size="small" :loading="previewing" @click="loadPreview">
            {{ t('iso.preview') }}
          </el-button>
        </el-form-item>

        <el-form-item v-if="preview">
          <div class="answer-preview">
            <p class="preview-title">{{ preview.file_name }}</p>
            <pre>{{ preview.content }}</pre>
          </div>
        </el-form-item>
      </template>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="instances.saving" @click="submit">
        {{ unattended.enabled ? t('iso.launchUnattended') : t('iso.launch') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.launch-hint {
  margin-bottom: 12px;
  color: var(--fleet-ink-soft);
  font-size: 13px;
}

.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}

.mode-label {
  margin-left: 8px;
  font-size: 13px;
}

.mode-help {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  margin-left: 6px;
  border: 1px solid var(--fleet-ink-soft);
  border-radius: 50%;
  color: var(--fleet-ink-soft);
  cursor: help;
  font-size: 11px;
}

.answer-preview {
  width: 100%;
}

.preview-title {
  margin: 0 0 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
}

.answer-preview pre {
  max-height: 220px;
  margin: 0;
  padding: 8px;
  overflow: auto;
  border: 1px solid var(--fleet-line);
  border-radius: 4px;
  background: var(--fleet-panel-muted);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>

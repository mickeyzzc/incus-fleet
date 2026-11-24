<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import { api, type UnattendedAnswerPreview, type UnattendedProfile, type UnattendedLaunchRequest } from '@/api'
import { useInstancesStore } from '@/stores/instances'
import { useAnswerProfilesStore } from '@/stores/answerProfiles'
import { errorMessage } from '@/utils/errors'
import { parseSize } from '@/utils/size'

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
const answerProfiles = useAnswerProfilesStore()
const { t } = useI18n()

const TIMEZONES = ['UTC', 'Asia/Shanghai', 'Asia/Tokyo', 'Europe/London', 'Europe/Berlin', 'America/New_York']
const LOCALES = ['en_US', 'en_GB', 'zh_CN', 'zh_TW', 'de_DE', 'fr_FR', 'es_ES', 'ja_JP', 'ko_KR']
const KEYBOARDS = ['us', 'gb', 'cn', 'de', 'fr', 'es', 'jp', 'kr', 'ru']

const form = reactive({
  name: '',
  network: '',
  rootDisk: '',
})

const unattended = reactive({
  enabled: false,
  profile: 'debian-preseed' as UnattendedProfile,
  profileId: '',
  hostname: '',
  username: '',
  password: '',
  timezone: 'UTC',
  disk: '',
  locale: '',
  keyboard: '',
  packages: '',
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

// Saved configurations for the selected installer family; the server also
// resolves per-profile defaults, but showing them here keeps the UI honest.
const availableProfiles = computed(() => answerProfiles.byProfile(unattended.profile))
// Debian preseed and kickstart honour a target disk; Subiquity/NoCloud do not.
const needsDiskField = computed(() => unattended.profile === 'debian-preseed' || unattended.profile === 'kickstart')

function applyProfile(id: string): void {
  unattended.profileId = id
  const selected = availableProfiles.value.find((p) => p.id === id)
  if (!selected) {
    // Empty selection means built-in template; clear back to editable fields.
    unattended.custom = false
    unattended.answer = ''
    return
  }
  if (selected.answer_file) {
    unattended.custom = true
    unattended.answer = selected.answer_file
  } else {
    unattended.custom = false
    unattended.answer = ''
    unattended.hostname = selected.hostname ?? ''
    unattended.username = selected.username ?? ''
    unattended.password = selected.password ?? ''
    unattended.timezone = selected.timezone ?? 'UTC'
    unattended.disk = selected.disk ?? ''
    unattended.locale = selected.locale ?? ''
    unattended.keyboard = selected.keyboard ?? ''
  }
}

function onProfileFamilyChange(): void {
  // Switching installer family invalidates a configuration from another one.
  unattended.profileId = ''
  unattended.custom = false
  unattended.answer = ''
}

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
      unattended.profileId = ''
      unattended.hostname = ''
      unattended.username = ''
      unattended.password = ''
      unattended.timezone = 'UTC'
      unattended.disk = ''
      unattended.locale = ''
      unattended.keyboard = ''
      unattended.packages = ''
      unattended.custom = false
      unattended.answer = ''
      preview.value = null
      void answerProfiles.fetch().then(() => {
        const def = answerProfiles.defaultFor(unattended.profile)
        if (def) {
          applyProfile(def.id)
        }
      })
    }
  },
  { immediate: true },
)

function validateUnattended(): string {
  // A saved profile carries its own answer content; no local fields required.
  if (unansweredProfileSelected()) {
    return ''
  }
  if (!unattended.custom && !unattended.username.trim()) {
    return t('iso.usernameRequired')
  }
  if (unattended.custom && !unattended.answer.trim()) {
    return t('iso.answerRequired')
  }
  return ''
}

function unansweredProfileSelected(): boolean {
  return !unattended.custom && unattended.profileId !== ''
}

function buildUnattendedRequest(name: string): UnattendedLaunchRequest {
  const request: UnattendedLaunchRequest = {
    name,
    profile: unattended.profile,
    ...(unattended.profileId ? { answer_profile_id: unattended.profileId } : {}),
  }
  if (unattended.custom) {
    request.answer_file = unattended.answer
  } else if (!unattended.profileId) {
    request.spec = {
      hostname: unattended.hostname || name,
      username: unattended.username,
      password: unattended.password,
      timezone: unattended.timezone,
      disk: unattended.disk,
      locale: unattended.locale,
      keyboard: unattended.keyboard,
      packages: unattended.packages,
    }
  }
  return request
}

async function loadPreview(): Promise<void> {
  previewing.value = true
  try {
    preview.value = await api.previewUnattendedAnswer(
      props.cluster, props.isoPool, props.isoName,
      buildUnattendedRequest(form.name.trim() || 'preview'),
    )
  } catch (err) {
    ElMessage.error(t('iso.previewFailed', { error: errorMessage(err) }))
  } finally {
    previewing.value = false
  }
}

const rootDiskSize = computed(() => parseSize(form.rootDisk))

async function submit(): Promise<void> {
  if (!form.name.trim()) {
    ElMessage.error(t('iso.nameRequired'))
    return
  }
  if (form.rootDisk.trim() && !rootDiskSize.value.valid) {
    ElMessage.error(t('validation.sizeFormat'))
    return
  }

  if (unattended.enabled) {
    const problem = validateUnattended()
    if (problem) {
      ElMessage.error(problem)
      return
    }

    try {
      const launch = await instances.launchUnattended(
        props.cluster, props.isoPool, props.isoName,
        {
          ...buildUnattendedRequest(form.name.trim()),
          network: form.network || undefined,
          root_disk: rootDiskSize.value.value || undefined,
        },
      )
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
      root_disk: rootDiskSize.value.value || undefined,
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
        <p class="field-hint">{{ t('validation.diskHint') }}</p>
        <p v-if="form.rootDisk.trim() && !rootDiskSize.valid" class="field-error">
          {{ t('validation.sizeFormat') }}
        </p>
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
          <el-select v-model="unattended.profile" style="width: 100%" @change="onProfileFamilyChange">
            <el-option
              v-for="item in profiles"
              :key="item.value"
              :label="t(item.labelKey)"
              :value="item.value"
            />
          </el-select>
          <p class="field-hint">{{ t('iso.profileHint') }}</p>
        </el-form-item>

        <el-form-item :label="t('iso.answerProfile')">
          <el-select
            v-model="unattended.profileId"
            clearable
            style="width: 100%"
            :placeholder="t('iso.noProfile')"
            @change="applyProfile(unattended.profileId)"
          >
            <el-option
              v-for="item in availableProfiles"
              :key="item.id"
              :label="item.name + (item.is_default ? ' (default)' : '')"
              :value="item.id"
            />
          </el-select>
          <p v-if="availableProfiles.length === 0" class="field-hint">{{ t('iso.profileHintNone') }}</p>
        </el-form-item>

        <el-form-item>
          <el-checkbox
            v-model="unattended.custom"
            @change="unattended.custom && (unattended.profileId = '')"
          >{{ t('iso.customAnswer') }}</el-checkbox>
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
          <el-form-item :label="t('iso.specHostname')">
            <el-input v-model="unattended.hostname" :placeholder="t('answerProfiles.hostnamePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('iso.specUsername')">
            <el-input v-model="unattended.username" placeholder="fleet" />
          </el-form-item>
          <el-form-item :label="t('iso.specPassword')">
            <el-input v-model="unattended.password" type="password" show-password />
          </el-form-item>
          <el-form-item :label="t('iso.specTimezone')">
            <el-select
              v-model="unattended.timezone"
              filterable
              allow-create
              default-first-option
              style="width: 100%"
            >
              <el-option v-for="zone in TIMEZONES" :key="zone" :label="zone" :value="zone" />
            </el-select>
          </el-form-item>
          <el-form-item v-if="needsDiskField" :label="t('iso.specDisk')">
            <el-input v-model="unattended.disk" placeholder="/dev/vda" />
          </el-form-item>
          <el-form-item :label="t('iso.specLocale')">
            <el-select v-model="unattended.locale" filterable allow-create default-first-option style="width: 100%">
              <el-option v-for="locale in LOCALES" :key="locale" :label="locale" :value="locale" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('iso.specKeyboard')">
            <el-select v-model="unattended.keyboard" filterable allow-create default-first-option style="width: 100%">
              <el-option v-for="layout in KEYBOARDS" :key="layout" :label="layout" :value="layout" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('answerProfiles.packages')">
            <el-input
              v-model="unattended.packages"
              :placeholder="t('answerProfiles.packagesPlaceholder')"
            />
            <p class="field-hint">{{ t('answerProfiles.packagesHint') }}</p>
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

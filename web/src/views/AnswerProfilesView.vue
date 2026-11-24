<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { AnswerProfile, UnattendedProfile } from '@/api/types'
import { useAnswerProfilesStore } from '@/stores/answerProfiles'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{ embedded?: boolean }>()

const store = useAnswerProfilesStore()
const { t } = useI18n()

// Common installer values; the selects stay searchable and allow custom
// values so uncommon locales/keymaps are still reachable.
const TIMEZONES = [
  'UTC',
  'Asia/Shanghai',
  'Asia/Tokyo',
  'Asia/Singapore',
  'Europe/London',
  'Europe/Berlin',
  'America/New_York',
  'America/Los_Angeles',
]
const LOCALES = [
  'en_US',
  'en_GB',
  'zh_CN',
  'zh_TW',
  'de_DE',
  'fr_FR',
  'es_ES',
  'ja_JP',
  'ko_KR',
]
const KEYBOARDS = ['us', 'gb', 'cn', 'de', 'fr', 'es', 'jp', 'kr', 'ru']

const dialogOpen = ref(false)
const editing = ref<AnswerProfile | null>(null)

const profiles: { value: UnattendedProfile; labelKey: string }[] = [
  { value: 'debian-preseed', labelKey: 'iso.profileDebian' },
  { value: 'ubuntu-autoinstall', labelKey: 'iso.profileUbuntu' },
  { value: 'kickstart', labelKey: 'iso.profileKickstart' },
  { value: 'nocloud', labelKey: 'iso.profileNocloud' },
]

const form = reactive({
  name: '',
  profile: 'debian-preseed' as UnattendedProfile,
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
  is_default: false,
})

const saving = computed(() => store.saving)

function openCreate(): void {
  editing.value = null
  Object.assign(form, {
    name: '', profile: 'debian-preseed', hostname: '', username: '', password: '',
    timezone: 'UTC', disk: '', locale: '', keyboard: '', packages: '', custom: false, answer: '', is_default: false,
  })
  dialogOpen.value = true
}

function openEdit(row: AnswerProfile): void {
  editing.value = row
  Object.assign(form, {
    name: row.name, profile: row.profile, hostname: row.hostname ?? '',
    username: row.username ?? '', password: row.password ?? '', timezone: row.timezone ?? '',
    disk: row.disk ?? '', locale: row.locale ?? '', keyboard: row.keyboard ?? '',
    packages: row.packages ?? '',
    custom: Boolean(row.answer_file), answer: row.answer_file ?? '', is_default: row.is_default,
  })
  dialogOpen.value = true
}

async function submit(): Promise<void> {
  if (!form.name.trim()) {
    ElMessage.error(t('answerProfiles.nameRequired'))
    return
  }
  if (form.custom && !form.answer.trim()) {
    ElMessage.error(t('answerProfiles.answerRequired'))
    return
  }
  const payload = {
    name: form.name.trim(),
    profile: form.profile,
    hostname: form.hostname.trim(),
    username: form.custom ? '' : form.username.trim(),
    password: form.custom ? '' : form.password,
    timezone: form.custom ? '' : form.timezone.trim(),
    disk: form.custom ? '' : form.disk.trim(),
    locale: form.custom ? '' : form.locale.trim(),
    keyboard: form.custom ? '' : form.keyboard.trim(),
    packages: form.custom ? '' : form.packages.trim(),
    answer_file: form.custom ? form.answer : '',
    is_default: form.is_default,
  }
  try {
    if (editing.value) {
      await store.update(editing.value.id, payload)
    } else {
      await store.create(payload)
    }
    ElMessage.success(t('answerProfiles.saved'))
    dialogOpen.value = false
  } catch (err) {
    ElMessage.error(t('answerProfiles.saveFailed', { error: errorMessage(err) }))
  }
}

async function confirmDelete(row: AnswerProfile): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('answerProfiles.deleteConfirm', { name: row.name }),
      t('common.delete'),
      { type: 'warning' },
    )
  } catch {
    return
  }
  try {
    await store.remove(row.id)
    ElMessage.success(t('answerProfiles.deleted'))
  } catch (err) {
    ElMessage.error(t('answerProfiles.deleteFailed', { error: errorMessage(err) }))
  }
}

onMounted(() => { void store.fetch() })
</script>

<template>
  <div class="page">
    <div v-if="!props.embedded" class="page-header">
      <h2>{{ t('answerProfiles.title') }}</h2>
      <p class="subtitle">{{ t('answerProfiles.subtitle') }}</p>
    </div>

    <section class="panel">
      <div class="panel-head">
        <h3>{{ t('answerProfiles.listTitle') }}</h3>
        <div class="panel-actions">
          <el-button size="small" :loading="store.loading" @click="store.fetch(true)">
            <el-icon><Refresh /></el-icon>{{ t('common.refresh') }}
          </el-button>
          <el-button size="small" type="primary" @click="openCreate">
            <el-icon><Plus /></el-icon>{{ t('answerProfiles.add') }}
          </el-button>
        </div>
      </div>
      <div class="panel-body">
        <el-table
          v-loading="store.loading"
          :data="store.cache"
          size="small"
          stripe
          :empty-text="t('answerProfiles.empty')"
        >
          <el-table-column :label="t('answerProfiles.name')" prop="name" min-width="160" />
          <el-table-column :label="t('answerProfiles.profile')" min-width="160">
            <template #default="{ row }">
              {{ t(profiles.find((p) => p.value === (row as AnswerProfile).profile)?.labelKey ?? 'iso.profileNocloud') }}
            </template>
          </el-table-column>
          <el-table-column :label="t('answerProfiles.username')" min-width="120">
            <template #default="{ row }">
              {{ (row as AnswerProfile).answer_file ? t('answerProfiles.custom') : ((row as AnswerProfile).username || '—') }}
            </template>
          </el-table-column>
          <el-table-column :label="t('answerProfiles.isDefault')" width="100" align="center">
            <template #default="{ row }">
              <el-tag v-if="(row as AnswerProfile).is_default" type="success" size="small" effect="light">
                {{ t('common.yes') }}
              </el-tag>
              <span v-else>—</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="160">
            <template #default="{ row }">
              <el-button link type="primary" @click="openEdit(row as AnswerProfile)">{{ t('common.edit') }}</el-button>
              <el-button link type="danger" @click="confirmDelete(row as AnswerProfile)">{{ t('common.delete') }}</el-button>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="t('answerProfiles.empty')">
              <el-button type="primary" @click="openCreate">
                {{ t('answerProfiles.add') }}
              </el-button>
            </el-empty>
          </template>
        </el-table>
      </div>
    </section>

    <el-dialog
      v-model="dialogOpen"
      :title="editing ? t('answerProfiles.edit') : t('answerProfiles.add')"
      width="640px"
      destroy-on-close
    >
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item :label="t('answerProfiles.name')">
          <el-input v-model="form.name" :placeholder="t('answerProfiles.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('answerProfiles.profile')">
          <el-select v-model="form.profile" style="width: 100%">
            <el-option v-for="item in profiles" :key="item.value" :label="t(item.labelKey)" :value="item.value" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-checkbox v-model="form.custom">{{ t('iso.customAnswer') }}</el-checkbox>
        </el-form-item>
        <el-form-item v-if="form.custom" :label="t('iso.answerFile')">
          <el-input v-model="form.answer" type="textarea" :rows="10" spellcheck="false" />
        </el-form-item>
        <template v-else>
          <el-form-item :label="t('iso.specHostname')">
            <el-input v-model="form.hostname" :placeholder="t('answerProfiles.hostnamePlaceholder')" />
            <p class="field-hint">{{ t('answerProfiles.hostnameHint') }}</p>
          </el-form-item>
          <el-form-item :label="t('iso.specUsername')">
            <el-input v-model="form.username" placeholder="fleet" />
          </el-form-item>
          <el-form-item :label="t('iso.specPassword')">
            <el-input v-model="form.password" type="password" show-password />
            <p class="field-hint">{{ t('answerProfiles.passwordHint') }}</p>
          </el-form-item>
          <el-form-item :label="t('iso.specTimezone')">
            <el-select
              v-model="form.timezone"
              filterable
              allow-create
              default-first-option
              style="width: 100%"
            >
              <el-option v-for="zone in TIMEZONES" :key="zone" :label="zone" :value="zone" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('iso.specDisk')">
            <el-input v-model="form.disk" placeholder="/dev/vda" />
            <p class="field-hint">{{ t('answerProfiles.diskHint') }}</p>
          </el-form-item>
          <el-form-item :label="t('iso.specLocale')">
            <el-select
              v-model="form.locale"
              filterable
              allow-create
              default-first-option
              style="width: 100%"
            >
              <el-option v-for="locale in LOCALES" :key="locale" :label="locale" :value="locale" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('iso.specKeyboard')">
            <el-select
              v-model="form.keyboard"
              filterable
              allow-create
              default-first-option
              style="width: 100%"
            >
              <el-option v-for="layout in KEYBOARDS" :key="layout" :label="layout" :value="layout" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('answerProfiles.packages')">
            <el-input v-model="form.packages" :placeholder="t('answerProfiles.packagesPlaceholder')" />
            <p class="field-hint">{{ t('answerProfiles.packagesHint') }}</p>
          </el-form-item>
        </template>
        <el-form-item>
          <el-checkbox v-model="form.is_default">{{ t('answerProfiles.setDefault') }}</el-checkbox>
          <p class="field-hint">{{ t('answerProfiles.setDefaultHint') }}</p>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submit">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.panel-actions {
  display: flex;
  gap: 8px;
}
.field-hint {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>

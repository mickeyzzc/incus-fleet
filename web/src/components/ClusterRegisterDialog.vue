<script setup lang="ts">
import { computed, reactive, ref, toRefs, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import { api } from '@/api'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  registered: []
}>()

const { t } = useI18n()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const form = reactive({
  name: '',
  display_name: '',
  endpoint: '',
  project: 'default',
  tls_cert: '',
  tls_key: '',
  tls_server_cert: '',
})

// Template bindings go through toRefs: vue-tsc 3.3.14 mis-infers template
// unwrapping for many v-model bindings on one reactive object (it emits a
// spurious `.value` access). Refs side-step that codegen path entirely.
const { name, display_name, endpoint, project, tls_cert, tls_key, tls_server_cert } = toRefs(form)

const saving = ref(false)

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      return
    }

    form.name = ''
    form.display_name = ''
    form.endpoint = ''
    form.project = 'default'
    form.tls_cert = ''
    form.tls_key = ''
    form.tls_server_cert = ''
  },
)

function pickFile(target: 'tls_cert' | 'tls_key' | 'tls_server_cert', event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) {
    return
  }

  const reader = new FileReader()
  reader.onload = () => {
    form[target] = String(reader.result ?? '').trim()
  }
  reader.readAsText(file)
}

function validate(): string {
  if (!form.name.trim()) {
    return t('clusterRegister.name') + ' — ' + t('common.name')
  }
  if (!form.endpoint.trim()) {
    return t('clusterRegister.endpoint')
  }
  if (!form.tls_cert.trim() || !form.tls_key.trim()) {
    return t('clusterRegister.cert') + ' / ' + t('clusterRegister.key')
  }

  return ''
}

async function submit(): Promise<void> {
  const missing = validate()
  if (missing) {
    ElMessage.error(missing)
    return
  }

  saving.value = true
  try {
    await api.registerCluster({
      name: form.name.trim(),
      display_name: form.display_name.trim() || undefined,
      endpoint: form.endpoint.trim(),
      project: form.project.trim() || undefined,
      tls_cert: form.tls_cert.trim(),
      tls_key: form.tls_key.trim(),
      tls_server_cert: form.tls_server_cert.trim() || undefined,
    })
    ElMessage.success(t('clusterRegister.registered', { name: form.name.trim() }))
    emit('update:modelValue', false)
    emit('registered')
  } catch (err) {
    ElMessage.error(t('clusterRegister.failed', { error: errorMessage(err) }))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="t('clusterRegister.title')" width="640px" destroy-on-close>
    <p class="field-hint">{{ t('clusterRegister.hint') }}</p>

    <!-- Plain field wrappers: vue-tsc 3.3.14 mis-generates v-model unwrapping
         when many el-form-items with labels bind distinct reactive fields. -->
    <div class="register-form">
      <div class="field">
        <label class="field-label">{{ t('clusterRegister.name') }}<span class="required">*</span></label>
        <el-input v-model="name" placeholder="c64" />
      </div>
      <div class="field">
        <label class="field-label">{{ t('clusterRegister.displayName') }}</label>
        <el-input v-model="display_name" placeholder="Cluster 64" />
      </div>
      <div class="field">
        <label class="field-label">{{ t('clusterRegister.endpoint') }}<span class="required">*</span></label>
        <el-input v-model="endpoint" placeholder="https://192.168.64.21:8443" />
      </div>
      <div class="field">
        <label class="field-label">{{ t('clusterRegister.project') }}</label>
        <el-input v-model="project" placeholder="default" />
      </div>
      <div class="field">
        <label class="field-label">{{ t('clusterRegister.cert') }}<span class="required">*</span></label>
        <input type="file" @change="pickFile('tls_cert', $event)" />
        <span v-if="tls_cert" class="picked">✓</span>
      </div>
      <div class="field">
        <label class="field-label">{{ t('clusterRegister.key') }}<span class="required">*</span></label>
        <input type="file" @change="pickFile('tls_key', $event)" />
        <span v-if="tls_key" class="picked">✓</span>
      </div>
      <div class="field">
        <label class="field-label">{{ t('clusterRegister.serverCert') }}</label>
        <input type="file" @change="pickFile('tls_server_cert', $event)" />
        <span v-if="tls_server_cert" class="picked">✓</span>
      </div>
      <p class="field-hint">{{ t('clusterRegister.certHint') }}</p>
    </div>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="saving" @click="submit">
        {{ t('clusterRegister.add') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.register-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.field-label {
  color: var(--el-text-color-regular);
  font-size: 13px;
  line-height: 1.4;
}

.required {
  margin-left: 3px;
  color: var(--el-color-danger);
}

.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  font-size: 12px;
  line-height: 1.45;
}

.picked {
  margin-left: 8px;
  color: var(--el-color-success);
}
</style>

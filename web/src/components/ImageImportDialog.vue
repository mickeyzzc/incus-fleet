<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { UploadFilled } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  cluster: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  imported: [fingerprint: string | null]
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const file = ref<File | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
const form = reactive({
  type: 'container' as 'container' | 'virtual-machine',
  alias: '',
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      file.value = null
      uploading.value = false
      form.type = 'container'
      form.alias = ''
    }
  },
  { immediate: true },
)

function pickFile(): void {
  fileInput.value?.click()
}

function onFileChange(event: Event): void {
  const input = event.target as HTMLInputElement
  file.value = input.files?.[0] ?? null
}

async function submit(): Promise<void> {
  if (!file.value) {
    ElMessage.error(t('images.fileRequired'))
    return
  }

  uploading.value = true
  try {
    const fingerprint = await instances.importImage(props.cluster, {
      file: file.value,
      type: form.type,
      alias: form.alias.trim(),
    })
    ElMessage.success(t('images.imported'))
    emit('imported', fingerprint)
    visible.value = false
  } catch (err) {
    ElMessage.error(t('images.failed', { error: errorMessage(err) }))
  } finally {
    uploading.value = false
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`${t('images.importTitle')} · ${cluster}`"
    width="520px"
    destroy-on-close
  >
    <p class="import-hint">{{ t('images.importHint') }}</p>

    <div
      class="upload-zone"
      :class="{ 'has-file': file }"
      role="button"
      tabindex="0"
      @click="pickFile"
      @keyup.enter="pickFile"
    >
      <el-icon :size="32" class="upload-icon"><UploadFilled /></el-icon>
      <p v-if="file" class="upload-file">{{ file.name }} · {{ (file.size / 1024 / 1024).toFixed(1) }} MB</p>
      <p v-else class="upload-text">{{ t('images.chooseFile') }}</p>
    </div>
    <input ref="fileInput" type="file" class="hidden-input" @change="onFileChange" />

    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('images.imageType')">
        <el-radio-group v-model="form.type">
          <el-radio-button value="container">{{ t('common.container') }}</el-radio-button>
          <el-radio-button value="virtual-machine">{{ t('common.virtualMachine') }}</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <el-form-item :label="t('images.alias')">
        <el-input v-model="form.alias" :placeholder="t('images.aliasPlaceholder')" clearable />
        <p class="field-hint">{{ t('images.aliasHint') }}</p>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="uploading" @click="submit">
        {{ uploading ? t('images.uploading') : t('images.import') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.import-hint {
  margin-bottom: 12px;
  color: var(--fleet-ink-soft);
  font-size: 13px;
}

.upload-zone {
  border: 1px dashed var(--fleet-line, #dcdfe6);
  border-radius: 8px;
  padding: 22px;
  text-align: center;
  cursor: pointer;
  margin-bottom: 14px;
}

.upload-zone.has-file {
  border-color: var(--el-color-success);
}

.upload-icon {
  color: var(--el-color-primary);
}

.upload-text,
.upload-file {
  margin-top: 8px;
  font-size: 13px;
}

.upload-file {
  font-weight: 600;
}

.hidden-input {
  display: none;
}

.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}
</style>

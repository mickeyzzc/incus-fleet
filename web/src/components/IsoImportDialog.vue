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
  pools: string[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  imported: [name: string]
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const file = ref<File | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
const form = reactive({ pool: 'default' })

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
      form.pool = props.pools[0] ?? 'default'
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
    ElMessage.error(t('iso.fileRequired'))
    return
  }

  uploading.value = true
  try {
    const name = await instances.importISO(props.cluster, {
      file: file.value,
      pool: form.pool,
    })
    ElMessage.success(t('iso.imported'))
    emit('imported', name)
    visible.value = false
  } catch (err) {
    ElMessage.error(t('iso.failed', { error: errorMessage(err) }))
  } finally {
    uploading.value = false
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`${t('iso.importTitle')} · ${cluster}`"
    width="520px"
    destroy-on-close
  >
    <p class="import-hint">{{ t('iso.importHint') }}</p>

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
      <p v-else class="upload-text">{{ t('iso.chooseFile') }}</p>
    </div>
    <input ref="fileInput" type="file" accept=".iso,application/octet-stream" class="hidden-input" @change="onFileChange" />

    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('iso.pool')">
        <el-select v-model="form.pool" style="width: 100%">
          <el-option v-for="pool in pools" :key="pool" :label="pool" :value="pool" />
        </el-select>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="uploading" @click="submit">
        {{ uploading ? t('iso.uploading') : t('iso.import') }}
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
</style>

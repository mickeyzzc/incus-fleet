<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, Upload } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { FileEntry } from '@/api/types'
import { api } from '@/api'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'
import { formatBytes, formatDateTime } from '@/utils/format'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  instance: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const path = ref('/')
const folderName = ref('')
const creatingFolder = ref(false)
const uploadInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const entries = computed(() => instances.files(props.cluster, props.instance, path.value))

const crumbs = computed(() => {
  const parts = path.value.split('/').filter(Boolean)
  const list = [{ name: '/', target: '/' }]
  let current = ''
  for (const part of parts) {
    current += `/${part}`
    list.push({ name: part, target: current })
  }

  return list
})

watch(
  () => [props.modelValue, props.cluster, props.instance] as const,
  ([open, cluster, instance]) => {
    if (!open) {
      return
    }

    path.value = '/'
    void instances.fetchFiles(cluster, instance, '/')
  },
  { immediate: true },
)

async function refresh(): Promise<void> {
  await instances.fetchFiles(props.cluster, props.instance, path.value)
}

async function enter(entry: FileEntry): Promise<void> {
  if (!entry.is_dir) {
    return
  }

  path.value = entry.name.startsWith('/') ? entry.name : joinPath(path.value, entry.name)
  await refresh()
}

async function goto(target: string): Promise<void> {
  path.value = target
  await refresh()
}

function joinPath(dir: string, name: string): string {
  return dir === '/' ? `/${name}` : `${dir}/${name}`
}

function download(entry: FileEntry): void {
  window.open(api.downloadFileURL(props.cluster, props.instance, joinPath(path.value, entry.name)))
}

function pickUpload(): void {
  uploadInput.value?.click()
}

async function onUploadChange(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || uploading.value) {
    return
  }

  uploading.value = true
  try {
    await instances.uploadFile(props.cluster, props.instance, path.value, file)
    ElMessage.success(t('files.uploaded'))
    await refresh()
  } catch (err) {
    ElMessage.error(t('files.failed', { error: errorMessage(err) }))
  } finally {
    uploading.value = false
    input.value = ''
  }
}

function toggleFolderForm(): void {
  creatingFolder.value = !creatingFolder.value
  folderName.value = ''
}

async function submitFolder(): Promise<void> {
  const name = folderName.value.trim()
  if (!name) {
    ElMessage.error(t('files.folderRequired'))
    return
  }

  try {
    await instances.mkdirDir(props.cluster, props.instance, path.value, name)
    ElMessage.success(t('files.created'))
    creatingFolder.value = false
    folderName.value = ''
    await refresh()
  } catch (err) {
    ElMessage.error(t('files.failed', { error: errorMessage(err) }))
  }
}

async function remove(entry: FileEntry): Promise<void> {
  try {
    await ElMessageBox.confirm(t('files.deleteConfirm', { name: entry.name }), t('common.delete'), {
      type: 'warning',
      confirmButtonText: t('common.delete'),
      cancelButtonText: t('common.cancel'),
    })
  } catch {
    return
  }

  try {
    await instances.deleteFile(props.cluster, props.instance, path.value, entry.name)
    ElMessage.success(t('files.deleted', { name: entry.name }))
    await refresh()
  } catch (err) {
    ElMessage.error(t('files.failed', { error: errorMessage(err) }))
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`${t('files.title')} · ${instance}`"
    width="680px"
    destroy-on-close
  >
    <div class="files-toolbar">
      <el-breadcrumb separator="/">
        <el-breadcrumb-item v-for="crumb in crumbs" :key="crumb.target">
          <el-link type="primary" @click="goto(crumb.target)">{{ crumb.name }}</el-link>
        </el-breadcrumb-item>
      </el-breadcrumb>
      <div class="files-actions">
        <el-button :icon="Plus" size="small" @click="toggleFolderForm">
          {{ t('files.mkdir') }}
        </el-button>
        <el-button :icon="Upload" size="small" :loading="uploading" :disabled="uploading" @click="pickUpload">
          {{ t('files.upload') }}
        </el-button>
        <el-button :icon="Refresh" size="small" :loading="instances.filesLoading" @click="refresh">
          {{ t('common.refresh') }}
        </el-button>
        <input
          ref="uploadInput"
          type="file"
          class="hidden-input"
          @change="onUploadChange"
        />
      </div>
    </div>

    <el-form v-if="creatingFolder" inline class="folder-form" @submit.prevent="submitFolder">
      <el-input
        v-model="folderName"
        :placeholder="t('files.folderName')"
        style="width: 240px"
        @keyup.enter="submitFolder"
      />
      <el-button type="primary" :loading="instances.saving" @click="submitFolder">
        {{ t('common.create') }}
      </el-button>
    </el-form>

    <el-table v-loading="instances.filesLoading" :data="entries" size="small" stripe>
      <el-table-column :label="t('common.name')" min-width="180">
        <template #default="{ row }">
          <el-button v-if="row.is_dir" link type="primary" @click="enter(row)">
            {{ row.name }}
          </el-button>
          <strong v-else>{{ row.name }}</strong>
        </template>
      </el-table-column>
      <el-table-column :label="t('common.size')" width="100">
        <template #default="{ row }">
          {{ row.is_dir ? '—' : formatBytes(row.size) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('files.modified')" min-width="150">
        <template #default="{ row }">
          {{ formatDateTime(row.modtime) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('common.actions')" width="140">
        <template #default="{ row }">
          <el-button v-if="!row.is_dir" link type="primary" @click="download(row)">
            {{ t('files.download') }}
          </el-button>
          <el-button v-if="!row.is_dir" link type="danger" @click="remove(row)">
            {{ t('common.delete') }}
          </el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty :description="t('files.empty')" :image-size="60" />
      </template>
    </el-table>
  </el-dialog>
</template>

<style scoped>
.files-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
  gap: 12px;
}

.files-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.hidden-input {
  display: none;
}

.folder-form {
  margin-bottom: 12px;
}
</style>

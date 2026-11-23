<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { ElMessage, ElMessageBox, ElNotification } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import AnswerProfilesView from '@/views/AnswerProfilesView.vue'
import type { ClusterImage, ClusterSummary, ISOSummary, Keystone, StoredScript } from '@/api/types'
import { api } from '@/api'
import { useAssetsStore } from '@/stores/assets'
import { useUploadsStore } from '@/stores/uploads'
import { errorMessage } from '@/utils/errors'
import { formatBytes } from '@/utils/format'

const assets = useAssetsStore()
const uploads = useUploadsStore()
const { task: uploadTask, minimized: uploadMinimized, percent: uploadPercent, etaSeconds: uploadEtaSeconds } = storeToRefs(uploads)
const { t } = useI18n()

const TAB_NAMES = ['images', 'isos', 'keys', 'scripts', 'answers'] as const
type TabName = (typeof TAB_NAMES)[number]

function tabFromQuery(): TabName {
  const requested = new URLSearchParams(window.location.search).get('tab')
  return requested !== null && (TAB_NAMES as readonly string[]).includes(requested)
    ? (requested as TabName)
    : 'images'
}

const activeTab = ref<TabName>(tabFromQuery())

watch(activeTab, (tab) => {
  const url = new URL(window.location.href)
  if (tab === 'images') {
    url.searchParams.delete('tab')
  } else {
    url.searchParams.set('tab', tab)
  }
  window.history.replaceState(null, '', url.toString())
})

const keyDialogOpen = ref(false)
const keyEditing = ref<Keystone | null>(null)
const scriptDialogOpen = ref(false)
const scriptEditing = ref<StoredScript | null>(null)

const keyForm = reactive({ name: '', public_key: '', comment: '' })
const scriptForm = reactive({ name: '', description: '', body: '' })

const saving = computed(() => assets.saving)

// Fleet-wide image and ISO inventory.
const clusterList = ref<ClusterSummary[]>([])
const imagesLoading = ref(false)
const isosLoading = ref(false)
const imagesError = ref('')
const isosError = ref('')
const clustersError = ref('')
const imageRows = ref<ClusterImage[]>([])

const isoRows = ref<ISOSummary[]>([])

// Image and ISO inventories can grow past one screen; paginate client-side.
const imagePage = ref(1)
const imagePageSize = ref(10)
const isoPage = ref(1)
const isoPageSize = ref(10)

const pagedImageRows = computed(() => {
  const start = (imagePage.value - 1) * imagePageSize.value
  return imageRows.value.slice(start, start + imagePageSize.value)
})

const pagedIsoRows = computed(() => {
  const start = (isoPage.value - 1) * isoPageSize.value
  return isoRows.value.slice(start, start + isoPageSize.value)
})

// Deleting the last row of the last page must not strand an empty page.
watch(
  () => [imageRows.value.length, isoRows.value.length],
  () => {
    const lastImage = Math.max(1, Math.ceil(imageRows.value.length / imagePageSize.value))
    if (imagePage.value > lastImage) {
      imagePage.value = lastImage
    }
    const lastIso = Math.max(1, Math.ceil(isoRows.value.length / isoPageSize.value))
    if (isoPage.value > lastIso) {
      isoPage.value = lastIso
    }
  },
)

const copyDialogOpen = ref(false)
const copySource = ref<ClusterImage | null>(null)
const copyTarget = ref('')
const copyBusy = ref(false)

const uploadDialogOpen = ref(false)
const uploadForm = reactive({ cluster: '', pool: 'default' })

onMounted(() => {
  void assets.fetchKeys(true)
  void assets.fetchScripts(true)
  void fetchClusters()
  void fetchImages()
  void fetchISOs()
})

async function fetchClusters(): Promise<void> {
  try {
    clusterList.value = (await api.listClusters()) ?? []
    clustersError.value = ''
  } catch (err) {
    clustersError.value = errorMessage(err)
  }
}

async function fetchImages(): Promise<void> {
  imagesLoading.value = true
  try {
    const result = await api.listAllImagesWithErrors()
    imageRows.value = result.items ?? []
    imagesError.value = result.failedClusters.length
      ? t('assets.partialFailure', { clusters: result.failedClusters.join(', ') })
      : ''
  } catch (err) {
    imageRows.value = []
    imagesError.value = errorMessage(err)
  } finally {
    imagesLoading.value = false
  }
}

async function fetchISOs(): Promise<void> {
  isosLoading.value = true
  try {
    const result = await api.listAllISOsWithErrors()
    isoRows.value = result.items ?? []
    isosError.value = result.failedClusters.length
      ? t('assets.partialFailure', { clusters: result.failedClusters.join(', ') })
      : ''
  } catch (err) {
    isoRows.value = []
    isosError.value = errorMessage(err)
  } finally {
    isosLoading.value = false
  }
}

function openKeyDialog(key: Keystone | null): void {
  keyEditing.value = key
  keyForm.name = key?.name ?? ''
  keyForm.public_key = key?.public_key ?? ''
  keyForm.comment = key?.comment ?? ''
  keyDialogOpen.value = true
}

async function submitKey(): Promise<void> {
  if (!keyForm.name.trim() || !keyForm.public_key.trim()) {
    ElMessage.error(t('assets.fieldsRequired'))
    return
  }

  try {
    const payload = { name: keyForm.name.trim(), public_key: keyForm.public_key.trim(), comment: keyForm.comment.trim() }
    if (keyEditing.value) {
      await assets.updateKey(keyEditing.value.id, payload)
    } else {
      await assets.createKey(payload)
    }
    ElMessage.success(t('assets.keySaved'))
    keyDialogOpen.value = false
  } catch (err) {
    ElMessage.error(t('assets.failed', { error: errorMessage(err) }))
  }
}

async function confirmDeleteKey(key: Keystone): Promise<void> {
  try {
    await ElMessageBox.confirm(t('assets.keyDeleteConfirm', { name: key.name }), t('common.delete'), {
      type: 'warning',
      confirmButtonText: t('common.delete'),
      cancelButtonText: t('common.cancel'),
    })
  } catch {
    return // user dismissed the dialog
  }

  try {
    await assets.deleteKey(key.id)
    ElMessage.success(t('assets.keyDeleted'))
  } catch (err) {
    // A swallowed failure left stale keys on screen with no explanation (#12).
    ElMessage.error(t('assets.deleteFailed', { error: errorMessage(err) }))
  }
}

function openScriptDialog(script: StoredScript | null): void {
  scriptEditing.value = script
  scriptForm.name = script?.name ?? ''
  scriptForm.description = script?.description ?? ''
  scriptForm.body = script?.body ?? ''
  scriptDialogOpen.value = true
}

async function submitScript(): Promise<void> {
  if (!scriptForm.name.trim() || !scriptForm.body.trim()) {
    ElMessage.error(t('assets.fieldsRequired'))
    return
  }

  try {
    const payload = {
      name: scriptForm.name.trim(),
      description: scriptForm.description.trim(),
      body: scriptForm.body,
    }
    if (scriptEditing.value) {
      await assets.updateScript(scriptEditing.value.id, payload)
    } else {
      await assets.createScript(payload)
    }
    ElMessage.success(t('assets.scriptSaved'))
    scriptDialogOpen.value = false
  } catch (err) {
    ElMessage.error(t('assets.failed', { error: errorMessage(err) }))
  }
}

async function confirmDeleteScript(script: StoredScript): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('assets.scriptDeleteConfirm', { name: script.name }),
      t('common.delete'),
      {
        type: 'warning',
        confirmButtonText: t('common.delete'),
        cancelButtonText: t('common.cancel'),
      },
    )
  } catch {
    return // user dismissed the dialog
  }

  try {
    await assets.deleteScript(script.id)
    ElMessage.success(t('assets.scriptDeleted'))
  } catch (err) {
    ElMessage.error(t('assets.deleteFailed', { error: errorMessage(err) }))
  }
}

function shortFingerprint(fingerprint: string): string {
  return fingerprint.length > 16 ? `${fingerprint.slice(0, 12)}…` : fingerprint
}

function openCopyDialog(image: ClusterImage): void {
  copySource.value = image
  copyTarget.value = ''
  copyDialogOpen.value = true
}

const copyTargets = computed(() =>
  clusterList.value.filter((cluster) => cluster.name !== copySource.value?.cluster),
)

async function submitCopy(): Promise<void> {
  const source = copySource.value
  if (!source || !copyTarget.value) {
    ElMessage.warning(t('assets.selectClusterRequired'))
    return
  }

  copyBusy.value = true
  try {
    await api.copyImage(source.cluster, source.fingerprint, {
      target_cluster: copyTarget.value,
      alias: source.alias || undefined,
    })
    ElMessage.success(t('assets.imageCopied', { cluster: copyTarget.value }))
    copyDialogOpen.value = false
  } catch (err) {
    ElMessage.error(t('assets.imageCopyFailed', { error: errorMessage(err) }))
  } finally {
    copyBusy.value = false
  }
}

function confirmDeleteImage(image: ClusterImage): void {
  void ElMessageBox.confirm(t('assets.imageDeleteConfirm'), t('common.delete'), {
    type: 'warning',
    confirmButtonText: t('common.delete'),
    cancelButtonText: t('common.cancel'),
  })
    .then(async () => {
      try {
        await api.deleteImage(image.cluster, image.fingerprint)
        ElMessage.success(t('assets.imageDeleted'))
        await fetchImages()
      } catch (err) {
        ElMessage.error(t('assets.imageDeleteFailed', { error: errorMessage(err) }))
      }
    })
    .catch(() => {})
}

function confirmDeleteISO(iso: ISOSummary): void {
  void ElMessageBox.confirm(t('assets.isoDeleteConfirm'), t('common.delete'), {
    type: 'warning',
    confirmButtonText: t('common.delete'),
    cancelButtonText: t('common.cancel'),
  })
    .then(async () => {
      try {
        await api.deleteISO(iso.cluster ?? '', iso.pool, iso.name)
        ElMessage.success(t('assets.isoDeleted'))
        await fetchISOs()
      } catch (err) {
        ElMessage.error(t('assets.failed', { error: errorMessage(err) }))
      }
    })
    .catch(() => {})
}

function openUploadDialog(): void {
  uploadForm.cluster = clusterList.value[0]?.name ?? ''
  uploadForm.pool = 'default'
  uploadDialogOpen.value = true
}

async function submitUpload(file: File): Promise<void> {
  uploadDialogOpen.value = false
  const cluster = uploadForm.cluster
  const done = await uploads.startISO(cluster, uploadForm.pool, file)
  if (!done) {
    const task = uploadTask.value
    ElNotification.error({
      title: t('assets.uploadFailed', { error: task?.error ?? '' }),
      message: file.name,
      position: 'bottom-right',
      duration: 0,
    })
    return
  }

  ElNotification.success({
    title: t('assets.uploadComplete'),
    message: `${file.name} → ${cluster}`,
    position: 'bottom-right',
    duration: 6000,
  })
  await fetchISOs()
}

function cancelUpload(): void {
  uploads.cancel()
  ElMessage.info(t('assets.uploadCanceled'))
}

function onUploadFile(event: Event): void {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (file) {
    void submitUpload(file)
  }
}

function formattedSpeed(): string {
  const task = uploadTask.value
  if (!task || task.speed <= 0) {
    return '—'
  }

  return `${formatBytes(task.speed)}/s`
}

/** ISO sizes arrive as raw byte strings from the volume config. */
function sizeLabel(size: string): string {
  const parsed = Number(size)
  if (size && Number.isFinite(parsed) && parsed > 0) {
    return formatBytes(parsed)
  }

  return t('common.unknown')
}
</script>

<template>
  <div class="page">
    <el-alert
      v-if="clustersError"
      class="fleet-alert"
      type="warning"
      :closable="false"
      show-icon
      aria-live="polite"
      :title="t('assets.loadFailed', { error: clustersError })"
    />
    <el-tabs v-model="activeTab" class="asset-tabs">
      <el-tab-pane :label="t('assets.tabImages')" name="images">
      <section class="panel">
        <div class="panel-body">
          <div class="section-heading page-header">
            <div>
              <h3 class="section-title">{{ t('assets.imagesTitle') }}</h3>
              <p class="subtitle">{{ t('assets.imagesHint') }}</p>
            </div>
            <div class="page-actions">
              <el-button :icon="Refresh" :loading="imagesLoading" @click="fetchImages">
                {{ t('common.refresh') }}
              </el-button>
            </div>
          </div>

          <el-alert
            v-if="imagesError"
            class="fleet-alert"
            type="error"
            :closable="false"
            show-icon
            aria-live="polite"
            :title="t('assets.loadFailed', { error: imagesError })"
          />

          <el-table v-loading="imagesLoading" :data="pagedImageRows" size="small" stripe>
            <el-table-column :label="t('common.cluster')" width="110">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">{{ row.cluster }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('form.image')" min-width="160">
              <template #default="{ row }">
                <strong>{{ row.alias || row.os || t('common.none') }}</strong>
                <span v-if="row.release" class="mini-label">{{ row.release }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('images.fingerprint')" min-width="130">
              <template #default="{ row }">
                <span class="mono">{{ shortFingerprint(row.fingerprint) }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.size')" width="100">
              <template #default="{ row }">
                {{ formatBytes(row.size) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('images.cached')" width="90">
              <template #default="{ row }">
                <el-tag v-if="row.cached" size="small" type="info" effect="light">
                  {{ t('images.cached') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="160" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openCopyDialog(row)">
                  {{ t('images.distribute') }}
                </el-button>
                <el-button link type="danger" @click="confirmDeleteImage(row)">
                  {{ t('common.delete') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('images.empty')" />
            </template>
          </el-table>
          <el-pagination
            v-model:current-page="imagePage"
            v-model:page-size="imagePageSize"
            class="table-pagination"
            layout="total, sizes, prev, pager, next"
            :total="imageRows.length"
            :page-sizes="[10, 20, 50]"
          />
        </div>
      </section>
      </el-tab-pane>

      <el-tab-pane :label="t('assets.tabISOs')" name="isos">
      <section class="panel">
        <div class="panel-body">
          <div class="section-heading page-header">
            <div>
              <h3 class="section-title">{{ t('assets.isosTitle') }}</h3>
              <p class="subtitle">{{ t('assets.isosHint') }}</p>
            </div>
            <div class="page-actions">
              <el-button :icon="Plus" type="primary" @click="openUploadDialog">
                {{ t('assets.addISO') }}
              </el-button>
            </div>
          </div>

          <el-alert
            v-if="isosError"
            class="fleet-alert"
            type="error"
            :closable="false"
            show-icon
            aria-live="polite"
            :title="t('assets.loadFailed', { error: isosError })"
          />

          <el-table v-loading="isosLoading" :data="pagedIsoRows" size="small" stripe>
            <el-table-column :label="t('common.cluster')" width="110">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">{{ row.cluster }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('iso.name')" min-width="180">
              <template #default="{ row }">
                <strong>{{ row.name }}</strong>
                <span v-if="row.description" class="mini-label">{{ row.description }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="pool" :label="t('cluster.storagePools')" width="130" />
            <el-table-column :label="t('common.size')" width="110">
              <template #default="{ row }">
                {{ sizeLabel(row.size) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="100" fixed="right">
              <template #default="{ row }">
                <el-button link type="danger" @click="confirmDeleteISO(row)">
                  {{ t('common.delete') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('iso.empty')" />
            </template>
          </el-table>
          <el-pagination
            v-model:current-page="isoPage"
            v-model:page-size="isoPageSize"
            class="table-pagination"
            layout="total, sizes, prev, pager, next"
            :total="isoRows.length"
            :page-sizes="[10, 20, 50]"
          />
        </div>
      </section>
      </el-tab-pane>

      <el-tab-pane :label="t('assets.tabKeys')" name="keys">
      <section class="panel">
        <div class="panel-body">
          <div class="section-heading page-header">
            <div>
              <h3 class="section-title">{{ t('assets.keysTitle') }}</h3>
              <p class="subtitle">{{ t('assets.keysHint') }}</p>
            </div>
            <div class="page-actions">
              <el-button :icon="Plus" type="primary" @click="openKeyDialog(null)">
                {{ t('assets.addKey') }}
              </el-button>
            </div>
          </div>

          <el-table v-loading="assets.keysLoading" :data="assets.keyCache" size="small" stripe>
            <el-table-column :label="t('assets.keyName')" min-width="160">
              <template #default="{ row }">
                <strong>{{ row.name }}</strong>
                <span v-if="row.comment" class="mini-label">{{ row.comment }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('assets.keyPublic')" min-width="280">
              <template #default="{ row }">
                <span class="mono key-text">{{ row.public_key }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="150">
              <template #default="{ row }">
                <el-button link type="primary" @click="openKeyDialog(row)">
                  {{ t('common.edit') }}
                </el-button>
                <el-button link type="danger" @click="confirmDeleteKey(row)">
                  {{ t('common.delete') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('assets.keysEmpty')" />
            </template>
          </el-table>
        </div>
      </section>
      </el-tab-pane>

      <el-tab-pane :label="t('assets.tabScripts')" name="scripts">
      <section class="panel">
        <div class="panel-body">
          <div class="section-heading page-header">
            <div>
              <h3 class="section-title">{{ t('assets.scriptsTitle') }}</h3>
              <p class="subtitle">{{ t('assets.scriptsHint') }}</p>
            </div>
            <div class="page-actions">
              <el-button :icon="Plus" type="primary" @click="openScriptDialog(null)">
                {{ t('assets.addScript') }}
              </el-button>
            </div>
          </div>

          <el-table v-loading="assets.scriptsLoading" :data="assets.scriptCache" size="small" stripe>
            <el-table-column :label="t('assets.scriptName')" min-width="180">
              <template #default="{ row }">
                <strong>{{ row.name }}</strong>
                <span v-if="row.description" class="mini-label">{{ row.description }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('assets.scriptBody')" min-width="280">
              <template #default="{ row }">
                <span class="mono key-text">{{ row.body.split('\n')[0] }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="150">
              <template #default="{ row }">
                <el-button link type="primary" @click="openScriptDialog(row)">
                  {{ t('common.edit') }}
                </el-button>
                <el-button link type="danger" @click="confirmDeleteScript(row)">
                  {{ t('common.delete') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('assets.scriptsEmpty')" />
            </template>
          </el-table>
        </div>
      </section>
      </el-tab-pane>

      <el-tab-pane :label="t('assets.tabAnswers')" name="answers">
        <AnswerProfilesView embedded />
      </el-tab-pane>
    </el-tabs>

    <el-dialog
      v-model="keyDialogOpen"
      :title="keyEditing ? t('assets.editKey') : t('assets.addKey')"
      width="560px"
      destroy-on-close
    >
      <el-form label-position="top" @submit.prevent="submitKey">
        <el-form-item :label="t('assets.keyName')">
          <el-input v-model="keyForm.name" placeholder="user@example" />
        </el-form-item>
        <el-form-item :label="t('assets.keyPublic')">
          <el-input
            v-model="keyForm.public_key"
            type="textarea"
            :rows="5"
            placeholder="ssh-ed25519 AAAA… comment"
            spellcheck="false"
          />
          <p class="field-hint">{{ t('assets.keyHint') }}</p>
        </el-form-item>
        <el-form-item :label="t('assets.comment')">
          <el-input v-model="keyForm.comment" :placeholder="t('assets.commentPlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="keyDialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitKey">
          {{ t('common.save') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="scriptDialogOpen"
      :title="scriptEditing ? t('assets.editScript') : t('assets.addScript')"
      width="680px"
      destroy-on-close
    >
      <el-form label-position="top" @submit.prevent="submitScript">
        <el-form-item :label="t('assets.scriptName')">
          <el-input v-model="scriptForm.name" placeholder="init-debian" />
        </el-form-item>
        <el-form-item :label="t('assets.description')">
          <el-input v-model="scriptForm.description" :placeholder="t('assets.descriptionPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('assets.scriptBody')">
          <el-input
            v-model="scriptForm.body"
            type="textarea"
            :rows="12"
            placeholder="#!/bin/sh"
            spellcheck="false"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="scriptDialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitScript">
          {{ t('common.save') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="copyDialogOpen"
      :title="t('images.distributeTitle')"
      width="480px"
      destroy-on-close
    >
      <p class="field-hint">{{ t('assets.imageCopyHint') }}</p>
      <el-form label-position="top" @submit.prevent="submitCopy">
        <el-form-item :label="t('images.distributeTo')">
          <el-select v-model="copyTarget" style="width: 100%">
            <el-option
              v-for="cluster in copyTargets"
              :key="cluster.name"
              :label="cluster.display_name || cluster.name"
              :value="cluster.name"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="copyDialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :disabled="!copyTarget" :loading="copyBusy" @click="submitCopy">
          {{ t('images.distribute') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="uploadDialogOpen"
      :title="t('iso.importTitle')"
      width="480px"
      destroy-on-close
    >
      <p class="field-hint">{{ t('iso.importHint') }}</p>
      <el-form label-position="top">
        <el-form-item :label="t('common.cluster')">
          <el-select v-model="uploadForm.cluster" style="width: 100%">
            <el-option
              v-for="cluster in clusterList"
              :key="cluster.name"
              :label="cluster.display_name || cluster.name"
              :value="cluster.name"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('cluster.storagePools')">
          <el-input v-model="uploadForm.pool" placeholder="default" />
        </el-form-item>
        <el-form-item :label="t('iso.chooseFile')">
          <input
            type="file"
            accept=".iso,application/octet-stream"
            @change="onUploadFile"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="uploadDialogOpen = false">{{ t('common.cancel') }}</el-button>
      </template>
    </el-dialog>

    <transition name="upload-slide">
      <div v-if="uploadTask && !uploadMinimized" class="upload-card">
        <div class="upload-head">
          <strong>{{ uploadTask.name }}</strong>
          <span class="mini-label">→ {{ uploadTask.cluster }}</span>
          <el-button link size="small" @click="uploadMinimized = true">
            {{ t('assets.uploadBackground') }}
          </el-button>
          <el-button link size="small" type="danger" @click="cancelUpload">
            {{ t('assets.uploadCancel') }}
          </el-button>
        </div>
        <el-progress
          :percentage="uploadPercent"
          :stroke-width="8"
          :show-text="false"
        />
        <div class="upload-meta">
          <span>{{ uploadPercent }}% · {{ formattedSpeed() }}</span>
          <span v-if="uploadEtaSeconds !== null">
            {{ t('assets.uploadETA') }} {{ uploadEtaSeconds }}s
          </span>
          <span>{{ formatBytes(uploadTask.loaded) }} / {{ formatBytes(uploadTask.total) }}</span>
        </div>
      </div>
    </transition>

    <transition name="upload-slide">
      <el-button
        v-if="uploadTask && uploadMinimized"
        class="upload-chip"
        size="small"
        type="primary"
        round
        @click="uploadMinimized = false"
      >
        {{ uploadTask.name }} · {{ uploadPercent }}%
      </el-button>
    </transition>
  </div>
</template>

<style scoped>
.table-pagination {
  margin-top: 12px;
  justify-content: flex-end;
}

.key-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: block;
}

.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}

.upload-card {
  position: fixed;
  right: 24px;
  bottom: 24px;
  z-index: 2100;
  width: 340px;
  padding: 14px 16px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  box-shadow: var(--el-box-shadow-light);
}

.upload-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.upload-head strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.upload-meta {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  margin-top: 8px;
  font-size: 12px;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
}

.upload-chip {
  position: fixed;
  right: 24px;
  bottom: 24px;
  z-index: 2100;
}

.upload-slide-enter-active,
.upload-slide-leave-active {
  transition: all 0.25s ease;
}

.upload-slide-enter-from,
.upload-slide-leave-to {
  opacity: 0;
  transform: translateY(12px);
}
</style>

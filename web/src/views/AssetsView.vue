<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { Keystone, StoredScript } from '@/api/types'
import { useAssetsStore } from '@/stores/assets'
import { errorMessage } from '@/utils/errors'

const assets = useAssetsStore()
const { t } = useI18n()

const keyDialogOpen = ref(false)
const keyEditing = ref<Keystone | null>(null)
const scriptDialogOpen = ref(false)
const scriptEditing = ref<StoredScript | null>(null)

const keyForm = reactive({ name: '', public_key: '', comment: '' })
const scriptForm = reactive({ name: '', description: '', body: '' })

const saving = computed(() => assets.saving)

onMounted(() => {
  void assets.fetchKeys(true)
  void assets.fetchScripts(true)
})

function openKeyDialog(key: Keystone | null): void {
  keyEditing.value = key
  keyForm.name = key?.name ?? ''
  keyForm.public_key = key?.public_key ?? ''
  keyForm.comment = key?.comment ?? ''
  keyDialogOpen.value = true
}

async function submitKey(): Promise<void> {
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

function confirmDeleteKey(key: Keystone): void {
  void ElMessageBox.confirm(t('assets.keyDeleteConfirm', { name: key.name }), t('common.delete'), {
    type: 'warning',
    confirmButtonText: t('common.delete'),
    cancelButtonText: t('common.cancel'),
  })
    .then(async () => {
      await assets.deleteKey(key.id)
      ElMessage.success(t('assets.keyDeleted'))
    })
    .catch(() => {})
}

function openScriptDialog(script: StoredScript | null): void {
  scriptEditing.value = script
  scriptForm.name = script?.name ?? ''
  scriptForm.description = script?.description ?? ''
  scriptForm.body = script?.body ?? ''
  scriptDialogOpen.value = true
}

async function submitScript(): Promise<void> {
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

function confirmDeleteScript(script: StoredScript): void {
  void ElMessageBox.confirm(t('assets.scriptDeleteConfirm', { name: script.name }), t('common.delete'), {
    type: 'warning',
    confirmButtonText: t('common.delete'),
    cancelButtonText: t('common.cancel'),
  })
    .then(async () => {
      await assets.deleteScript(script.id)
      ElMessage.success(t('assets.scriptDeleted'))
    })
    .catch(() => {})
}
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>{{ t('assets.title') }}</h2>
        <p class="subtitle">{{ t('assets.hint') }}</p>
      </div>
    </div>

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
          <el-table-column label="Public key" min-width="280">
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
        <el-form-item label="Public key">
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
  </div>
</template>

<style scoped>
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
</style>

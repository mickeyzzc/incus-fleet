<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import type { BatchItem, BatchTarget } from '@/api/types'
import { useAssetsStore } from '@/stores/assets'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  mode: 'keys' | 'script'
  targets: BatchTarget[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  done: []
}>()

const assets = useAssetsStore()
const { t } = useI18n()

const selectedKeys = ref<string[]>([])
const selectedScript = ref('')
const user = ref('root')
const items = ref<BatchItem[]>([])
const running = ref(false)

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const okCount = computed(() => items.value.filter((item) => item.ok).length)
const failCount = computed(() => items.value.filter((item) => !item.ok).length)

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      selectedKeys.value = []
      selectedScript.value = ''
      user.value = 'root'
      items.value = []
      void assets.fetchKeys()
      void assets.fetchScripts()
    }
  },
  { immediate: true },
)

async function submit(): Promise<void> {
  running.value = true
  try {
    if (props.mode === 'keys') {
      if (selectedKeys.value.length === 0) {
        ElMessage.error(t('batch.keysRequired'))
        return
      }
      const targets = props.targets.map((target) => ({ ...target, user: user.value.trim() || 'root' }))
      items.value = await assets.batchInjectKeys(targets, selectedKeys.value)
    } else {
      if (!selectedScript.value) {
        ElMessage.error(t('batch.scriptRequired'))
        return
      }
      items.value = await assets.batchRunScript(props.targets, selectedScript.value)
    }

    if (failCount.value === 0) {
      ElMessage.success(t('batch.allOk', { count: okCount.value }))
    } else {
      ElMessage.warning(t('batch.partial', { ok: okCount.value, failed: failCount.value }))
    }
    emit('done')
  } catch (err) {
    ElMessage.error(t('batch.failed', { error: errorMessage(err) }))
  } finally {
    running.value = false
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="mode === 'keys' ? t('batch.keysTitle', { count: targets.length }) : t('batch.scriptTitle', { count: targets.length })"
    width="680px"
    destroy-on-close
  >
    <p class="batch-hint">
      {{ mode === 'keys' ? t('batch.keysHint') : t('batch.scriptHint') }}
      <span class="mono">{{ targets.map((target) => target.instance).join(', ') }}</span>
    </p>

    <el-form label-position="top" @submit.prevent="submit">
      <template v-if="mode === 'keys'">
        <el-form-item :label="t('batch.selectKeys')">
          <el-select v-model="selectedKeys" multiple style="width: 100%">
            <el-option
              v-for="key in assets.keyCache"
              :key="key.id"
              :label="key.name"
              :value="key.id"
            />
          </el-select>
          <p class="field-hint">{{ t('batch.selectKeysHint') }}</p>
        </el-form-item>
        <el-form-item :label="t('ops.user')">
          <el-input v-model="user" placeholder="root" style="width: 220px" />
        </el-form-item>
      </template>

      <template v-else>
        <el-form-item :label="t('batch.selectScript')">
          <el-select v-model="selectedScript" style="width: 100%">
            <el-option
              v-for="script in assets.scriptCache"
              :key="script.id"
              :label="script.name"
              :value="script.id"
            />
          </el-select>
          <p class="field-hint">{{ t('batch.selectScriptHint') }}</p>
        </el-form-item>
      </template>
    </el-form>

    <el-table v-if="items.length > 0" :data="items" size="small" stripe class="batch-result">
      <el-table-column prop="instance" :label="t('batch.target')" min-width="140" />
      <el-table-column :label="t('batch.result')" min-width="80">
        <template #default="{ row }">
          <el-tag v-if="row.ok" size="small" type="success">{{ t('batch.ok') }}</el-tag>
          <el-tag v-else size="small" type="danger">{{ t('batch.failedTag') }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('ops.result', { code: '' })" min-width="240">
        <template #default="{ row }">
          <span v-if="row.error" class="batch-error">{{ row.error }}</span>
          <span v-else class="mono batch-out">{{ (row.stdout || '').trim().slice(0, 80) }}</span>
        </template>
      </el-table-column>
    </el-table>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.close') }}</el-button>
      <el-button type="primary" :loading="running" @click="submit">
        {{ running ? t('batch.running') : t('batch.run') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.batch-hint {
  margin-bottom: 12px;
  color: var(--fleet-ink-soft);
  font-size: 13px;
}

.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
}

.batch-result {
  margin-top: 8px;
}

.batch-error {
  color: var(--el-color-danger);
  font-size: 12px;
}

.batch-out {
  font-size: 12px;
}
</style>

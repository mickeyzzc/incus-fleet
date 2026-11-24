<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import type { OpsResult } from '@/api/types'
import { useAssetsStore } from '@/stores/assets'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  instance: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  done: []
}>()

const instances = useInstancesStore()
const assets = useAssetsStore()
const { t } = useI18n()

const selectedScript = ref('')
const selectedKey = ref('')

function applyScript(): void {
  const script = assets.scriptCache.find((item) => item.id === selectedScript.value)
  if (script) {
    scriptForm.script = script.body
  }
}

function applyKey(): void {
  const key = assets.keyCache.find((item) => item.id === selectedKey.value)
  if (key) {
    keyForm.key = key.public_key
  }
}

const activeTab = ref<'script' | 'password' | 'key'>('script')

const tabAction = computed(() =>
  activeTab.value === 'script' ? 'run-script' : activeTab.value === 'password' ? 'set-password' : 'inject-key',
)
const running = ref(false)
const result = ref<OpsResult | null>(null)

const scriptForm = reactive({ script: '' })
const passwordForm = reactive({ user: 'root', password: '' })
const keyForm = reactive({ user: 'root', key: '' })

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      activeTab.value = 'script'
      running.value = false
      result.value = null
      scriptForm.script = ''
      passwordForm.user = 'root'
      passwordForm.password = ''
      keyForm.user = 'root'
      keyForm.key = ''
      selectedScript.value = ''
      selectedKey.value = ''
      void assets.fetchScripts()
      void assets.fetchKeys()
    }
  },
  { immediate: true },
)

async function submit(action: 'run-script' | 'set-password' | 'inject-key'): Promise<void> {
  running.value = true
  result.value = null

  const request =
    action === 'run-script'
      ? { action, script: scriptForm.script }
      : action === 'set-password'
        ? { action, user: passwordForm.user.trim(), password: passwordForm.password }
        : { action, user: keyForm.user.trim(), key: keyForm.key.trim() }

  try {
    result.value = await instances.runOps(props.cluster, props.instance, request)
    if (result.value.exit_code === 0) {
      ElMessage.success(t('ops.succeeded'))
      if (action !== 'run-script') {
        emit('done')
        visible.value = false
      }
    } else {
      ElMessage.warning(t('ops.failedExit', { code: result.value.exit_code }))
    }
  } catch (err) {
    ElMessage.error(t('ops.failed', { error: errorMessage(err) }))
  } finally {
    running.value = false
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`${t('ops.title')} · ${instance}`"
    width="640px"
    destroy-on-close
  >
    <el-tabs v-model="activeTab">
      <el-tab-pane :label="t('ops.tabScript')" name="script">
        <el-form label-position="top" @submit.prevent="submit('run-script')">
          <el-form-item :label="t('batch.selectScript')">
            <el-select
              v-model="selectedScript"
              clearable
              style="width: 100%"
              @change="applyScript"
            >
              <el-option
                v-for="item in assets.scriptCache"
                :key="item.id"
                :label="item.name"
                :value="item.id"
              />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('ops.script')">
            <el-input
              v-model="scriptForm.script"
              type="textarea"
              :rows="8"
              :placeholder="t('ops.scriptPlaceholder')"
              spellcheck="false"
            />
            <p class="field-hint">{{ t('ops.scriptHint') }}</p>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane :label="t('ops.tabPassword')" name="password">
        <el-form label-position="top" @submit.prevent="submit('set-password')">
          <el-form-item :label="t('ops.user')">
            <el-input v-model="passwordForm.user" :placeholder="t('ops.userPlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('ops.password')">
            <el-input
              v-model="passwordForm.password"
              type="password"
              show-password
              :placeholder="t('ops.passwordPlaceholder')"
            />
            <p class="field-hint">{{ t('ops.passwordHint') }}</p>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane :label="t('ops.tabKey')" name="key">
        <el-form label-position="top" @submit.prevent="submit('inject-key')">
          <el-form-item :label="t('ops.user')">
            <el-input v-model="keyForm.user" :placeholder="t('ops.userPlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('batch.selectKeys')">
            <el-select v-model="selectedKey" clearable style="width: 100%" @change="applyKey">
              <el-option
                v-for="item in assets.keyCache"
                :key="item.id"
                :label="item.name"
                :value="item.id"
              />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('ops.key')">
            <el-input
              v-model="keyForm.key"
              type="textarea"
              :rows="4"
              :placeholder="t('ops.keyPlaceholder')"
              spellcheck="false"
            />
            <p class="field-hint">{{ t('ops.keyHint') }}</p>
          </el-form-item>
        </el-form>
      </el-tab-pane>
    </el-tabs>

    <template v-if="result">
      <div class="ops-result">
        <p class="result-title">{{ t('ops.result', { code: result.exit_code }) }}</p>
        <pre v-if="result.stdout" class="result-output">{{ result.stdout }}</pre>
        <pre v-if="result.stderr" class="result-output result-error">{{ result.stderr }}</pre>
      </div>
    </template>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.close') }}</el-button>
      <el-button
        type="primary"
        :loading="running"
        @click="submit(tabAction)"
      >
        {{ running ? t('ops.running') : t('ops.run') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}

.ops-result {
  margin-top: 4px;
}

.result-title {
  margin-bottom: 6px;
  font-size: 13px;
  font-weight: 600;
}

.result-output {
  margin: 0 0 8px;
  max-height: 160px;
  overflow: auto;
  padding: 10px;
  border-radius: 6px;
  background: var(--fleet-bg-soft, #f5f7fa);
  color: var(--fleet-ink, #303133);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
}

.result-error {
  color: var(--el-color-danger);
}
</style>

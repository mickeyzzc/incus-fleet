<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import type { ClusterSummary, PluginCatalogEntry, PluginInstallRequest } from '@/api/types'
import { humanText } from '@/plugins/messages'

const props = defineProps<{
  visible: boolean
  catalog: PluginCatalogEntry[]
  selectedId: string
  clusters: ClusterSummary[]
  loading: boolean
}>()

const emit = defineEmits<{
  close: []
  submit: [request: PluginInstallRequest]
}>()

const { t } = useI18n()

const step = ref(0)
const chosenId = ref('')
const accepted = ref(false)
const advancedOpen = ref(false)
const form = reactive({
  cluster: '',
  member: '',
  name: '',
  cpu: '',
  memory: '',
  disk: '',
  network: '',
  filesystem: 'ext4',
  mountPoint: '/data',
  upstream: '',
})

const selected = computed(() => props.catalog.find((entry) => entry.id === chosenId.value))
const deploysInstance = computed(() => Boolean(selected.value?.image))
const members = computed(() => props.clusters.find((item) => item.name === form.cluster)?.members ?? [])
const canAdvance = computed(() => {
  if (step.value !== 0) {
    return true
  }
  if (!selected.value) {
    return false
  }
  return deploysInstance.value ? Boolean(form.cluster) : Boolean(form.upstream.trim())
})

function reset(): void {
  chosenId.value = props.selectedId || props.catalog[0]?.id || ''
  const entry = props.catalog.find((item) => item.id === chosenId.value)
  step.value = 0
  accepted.value = false
  advancedOpen.value = false
  form.cluster = props.clusters.find((item) => item.online)?.name ?? props.clusters[0]?.name ?? ''
  form.member = ''
  form.name = entry ? `fleet-${entry.id}` : ''
  form.cpu = entry?.default_cpu ?? '2'
  form.memory = entry?.default_memory ?? '2GiB'
  form.disk = entry?.default_disk ?? '20GiB'
  form.network = entry?.default_network ?? 'incusbr0'
  form.upstream = ''
}

watch(() => props.visible, (visible) => {
  if (visible) {
    reset()
  }
}, { immediate: true })

function next(): void {
  if (canAdvance.value && step.value < 2) {
    step.value += 1
  }
}

function back(): void {
  if (step.value > 0) {
    step.value -= 1
  }
}

function confirm(): void {
  if (!accepted.value || !selected.value) {
    return
  }
  if (deploysInstance.value) {
    emit('submit', {
      id: selected.value.id,
      cluster: form.cluster,
      member: form.member || undefined,
      name: form.name.trim(),
      cpu: form.cpu,
      memory: form.memory,
      disk: form.disk,
      network: form.network,
      filesystem: form.filesystem.trim() || undefined,
      mount_point: form.mountPoint.trim() || undefined,
    })
    return
  }
  emit('submit', { id: selected.value.id, upstream: form.upstream.trim() })
}
</script>

<template>
  <el-dialog
    :model-value="visible"
    :title="t('plugins.wizard.title', { name: selected?.name ?? '' })"
    width="640px"
    @update:model-value="emit('close')"
  >
    <p class="muted">{{ t('plugins.wizard.hint') }}</p>

    <el-steps :active="step" align-center class="wizard-steps">
      <el-step :title="t('plugins.wizard.stepPlacement')" />
      <el-step :title="t('plugins.wizard.stepResources')" />
      <el-step :title="t('plugins.wizard.stepPermissions')" />
    </el-steps>

    <div v-if="step === 0" class="wizard-panel">
      <el-form label-position="top">
        <el-form-item :label="t('plugins.wizard.choose')">
          <el-select v-model="chosenId" style="width: 100%">
            <el-option
              v-for="entry in catalog"
              :key="entry.id"
              :label="entry.name"
              :value="entry.id"
            />
          </el-select>
        </el-form-item>

        <template v-if="deploysInstance">
          <div class="wizard-grid">
            <el-form-item :label="t('plugins.wizard.cluster')">
              <el-select v-model="form.cluster" style="width: 100%">
                <el-option
                  v-for="cluster in clusters"
                  :key="cluster.name"
                  :label="cluster.display_name || cluster.name"
                  :value="cluster.name"
                  :disabled="!cluster.online"
                />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('plugins.wizard.member')">
              <el-select v-model="form.member" clearable style="width: 100%">
                <el-option :label="t('plugins.wizard.auto')" value="" />
                <el-option
                  v-for="member in members"
                  :key="member.name"
                  :label="member.name"
                  :value="member.name"
                  :disabled="member.status !== 'Online'"
                />
              </el-select>
            </el-form-item>
          </div>
          <el-form-item :label="t('plugins.wizard.name')">
            <el-input v-model="form.name" />
          </el-form-item>
        </template>
        <el-form-item v-else :label="t('plugins.wizard.upstream')">
          <div class="wizard-upstream">
            <el-input v-model="form.upstream" :placeholder="selected?.upstream_hint" />
          </div>
        </el-form-item>
      </el-form>
    </div>

    <div v-else-if="step === 1" class="wizard-panel">
      <div class="wizard-grid">
        <el-form-item :label="t('plugins.wizard.cpu')">
          <el-input v-model="form.cpu" />
        </el-form-item>
        <el-form-item :label="t('plugins.wizard.memory')">
          <el-input v-model="form.memory" />
        </el-form-item>
        <el-form-item :label="t('plugins.wizard.disk')">
          <el-input v-model="form.disk" />
        </el-form-item>
        <el-form-item :label="t('plugins.wizard.network')">
          <el-input v-model="form.network" />
        </el-form-item>
      </div>
      <details class="wizard-advanced">
        <summary @click.prevent="advancedOpen = !advancedOpen">{{ t('plugins.wizard.advanced') }}</summary>
        <template v-if="advancedOpen">
          <div class="wizard-grid">
            <el-form-item :label="t('plugins.wizard.filesystem')">
              <el-input v-model="form.filesystem" />
            </el-form-item>
            <el-form-item :label="t('plugins.wizard.mountPoint')">
              <el-input v-model="form.mountPoint" />
            </el-form-item>
          </div>
        </template>
      </details>
    </div>

    <div v-else class="wizard-panel">
      <h3 class="wizard-effects-title">{{ t('plugins.wizard.permissionsTitle') }}</h3>
      <p class="muted">{{ t('plugins.wizard.permissionsHint') }}</p>
      <ul class="wizard-effects">
        <li v-for="(effect, index) in selected?.effects ?? []" :key="index">
          <span class="effect-label">{{ t('plugins.wizard.effectLabel') }}: {{ humanText(effect.sentence) }}</span>
          <span class="muted">{{ t('plugins.wizard.consequenceLabel') }}: {{ humanText(effect.consequence) }}</span>
        </li>
      </ul>
      <label class="wizard-accept">
        <input v-model="accepted" type="checkbox" />
        <span>{{ t('plugins.wizard.accept') }}</span>
      </label>
    </div>

    <template #footer>
      <el-button text @click="emit('close')">{{ t('common.cancel') }}</el-button>
      <el-button v-if="step > 0" @click="back">{{ t('plugins.wizard.back') }}</el-button>
      <el-button
        v-if="step < 2"
        type="primary"
        :disabled="!canAdvance"
        @click="next"
      >
        {{ t('plugins.wizard.next') }}
      </el-button>
      <el-button
        v-else
        type="primary"
        :loading="loading"
        :disabled="!accepted"
        @click="confirm"
      >
        {{ t('plugins.wizard.install') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.wizard-steps { margin: 16px 0; }
.wizard-panel { min-height: 220px; }
.wizard-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 12px; }
.wizard-upstream { width: 100%; }
.wizard-advanced { border-top: 1px solid var(--el-border-color-lighter); padding-top: 12px; }
.wizard-advanced summary { cursor: pointer; font-size: 13px; color: var(--el-text-color-secondary); }
.wizard-effects-title { margin: 0 0 4px; font-size: 14px; }
.wizard-effects { list-style: none; margin: 12px 0; padding: 0; display: grid; gap: 8px; }
.wizard-effects li { display: grid; gap: 2px; padding: 8px 10px; border: 1px solid var(--el-border-color-light); border-radius: 6px; font-size: 13px; }
.effect-label { color: var(--el-text-color-primary); }
.muted { color: var(--el-text-color-secondary); font-size: 13px; }
.wizard-accept { display: flex; align-items: center; gap: 8px; margin-top: 12px; font-size: 13px; cursor: pointer; }
</style>

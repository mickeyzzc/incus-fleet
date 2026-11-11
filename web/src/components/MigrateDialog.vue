<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Right } from '@element-plus/icons-vue'

import type { ClusterSummary, InstanceSummary, Migration, MigrationMode } from '@/api/types'
import { useMigrationsStore } from '@/stores/migrations'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  instance: InstanceSummary | null
  clusters: ClusterSummary[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  submitted: [migration: Migration]
}>()

const migrations = useMigrationsStore()

const form = reactive({
  target: '',
  mode: 'move' as MigrationMode,
  live: false,
  includeSnapshots: false,
  autoStart: true,
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const targets = computed(() =>
  props.clusters.filter(
    (cluster) => cluster.online && cluster.name !== props.instance?.cluster,
  ),
)

const canLive = computed(
  () =>
    props.instance?.type === 'virtual-machine' && props.instance.migration_stateful === true,
)

const targetDisplay = computed(
  () => targets.value.find((cluster) => cluster.name === form.target)?.display_name ?? '—',
)

watch(
  targets,
  (list) => {
    if (!list.some((cluster) => cluster.name === form.target)) {
      form.target = list[0]?.name ?? ''
    }
  },
  { immediate: true },
)

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      return
    }

    form.target = targets.value[0]?.name ?? ''
    form.mode = 'move'
    form.live = false
    form.includeSnapshots = false
    form.autoStart = true
    migrations.error = ''
  },
  { immediate: true },
)

watch(
  () => form.mode,
  (mode) => {
    if (mode === 'copy') {
      form.live = false
    }
  },
)

watch(canLive, (value) => {
  if (!value) {
    form.live = false
  }
})

async function submit(): Promise<void> {
  if (!props.instance) {
    return
  }
  if (!form.target) {
    ElMessage.error('Select a target cluster.')
    return
  }

  try {
    const migration = await migrations.submit({
      source_cluster: props.instance.cluster,
      target_cluster: form.target,
      instance: props.instance.name,
      mode: form.mode,
      live: form.live,
      include_snapshots: form.includeSnapshots,
      auto_start: form.autoStart,
    })

    ElMessage.success(`Migration queued for ${props.instance.name}.`)
    emit('submitted', migration)
    visible.value = false
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}
</script>

<template>
  <el-dialog v-model="visible" title="Migrate instance" width="580px" destroy-on-close>
    <div v-if="instance" class="instance-summary">
      <div class="summary-name">
        <strong>{{ instance.name }}</strong>
        <span class="muted">{{ instance.type === 'virtual-machine' ? 'Virtual machine' : 'Container' }}</span>
      </div>
      <div class="summary-route">
        <el-tag effect="plain" round>{{ instance.cluster }}</el-tag>
        <el-icon class="arrow"><Right /></el-icon>
        <el-tag effect="plain" round type="primary">{{ targetDisplay }}</el-tag>
      </div>
    </div>

    <el-alert
      v-if="targets.length === 0"
      class="fleet-alert"
      type="warning"
      :closable="false"
      show-icon
      title="No migration target"
      description="At least one other cluster must be online to migrate this instance."
    />

    <el-form v-else label-position="top" class="migrate-form" @submit.prevent="submit">
      <el-form-item label="Target cluster">
        <el-select v-model="form.target" placeholder="Select a cluster" style="width: 100%">
          <el-option
            v-for="cluster in targets"
            :key="cluster.name"
            :label="`${cluster.display_name} · ${cluster.endpoint}`"
            :value="cluster.name"
          />
        </el-select>
      </el-form-item>

      <el-form-item label="Mode">
        <el-radio-group v-model="form.mode">
          <el-radio-button value="move">Move (remove from source)</el-radio-button>
          <el-radio-button value="copy">Copy (keep on source)</el-radio-button>
        </el-radio-group>
        <p class="field-hint">
          {{
            form.mode === 'copy'
              ? 'The source is stopped briefly during transfer and restarted afterwards.'
              : 'The instance is removed from the source after a successful transfer.'
          }}
        </p>
      </el-form-item>

      <div class="switch-row">
        <div>
          <strong>Live migration</strong>
          <p class="field-hint">
            Requires a running virtual machine with <code>migration.stateful=true</code>. Only
            available in move mode.
          </p>
        </div>
        <el-switch v-model="form.live" :disabled="!canLive || form.mode !== 'move'" />
      </div>

      <div class="switch-row">
        <div>
          <strong>Include snapshots</strong>
          <p class="field-hint">Copy instance snapshots along with the instance.</p>
        </div>
        <el-switch v-model="form.includeSnapshots" />
      </div>

      <div class="switch-row">
        <div>
          <strong>Start on target</strong>
          <p class="field-hint">Start the instance on the target if it was running on the source.</p>
        </div>
        <el-switch v-model="form.autoStart" />
      </div>

      <el-alert
        v-if="form.live"
        class="migration-warning"
        type="info"
        :closable="false"
        show-icon
        title="Live move"
        description="The VM keeps running while memory and disk state transfer. The source is stopped after the copy completes and then deleted."
      />

      <el-alert
        v-if="migrations.error"
        class="migration-warning"
        type="error"
        :closable="false"
        show-icon
        :title="migrations.error"
      />
    </el-form>

    <template #footer>
      <el-button @click="visible = false">Cancel</el-button>
      <el-button
        type="primary"
        :loading="migrations.submitting"
        :disabled="targets.length === 0"
        @click="submit"
      >
        Start migration
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.instance-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  padding: 14px 16px;
  margin-bottom: 18px;
  border-radius: 12px;
  background: #f7f8fd;
  border: 1px solid #e6eaf5;
}

.summary-name {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.summary-name strong {
  font-size: 15px;
}

.summary-name span {
  font-size: 12px;
}

.summary-route {
  display: flex;
  align-items: center;
  gap: 4px;
}

.migrate-form {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}

.field-hint code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  background: #eef1f9;
  padding: 1px 5px;
  border-radius: 4px;
}

.switch-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  padding: 12px 0;
  border-top: 1px solid var(--fleet-line);
}

.switch-row strong {
  font-size: 13.5px;
}
</style>

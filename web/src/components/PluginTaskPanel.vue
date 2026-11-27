<script setup lang="ts">
import { computed } from 'vue'
import { CircleCheck, Close, Loading, WarningFilled } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { InstallStepState, PluginInstallTask } from '@/plugins/model'

const props = defineProps<{ task: PluginInstallTask }>()
defineEmits<{ retry: []; cancel: []; dismiss: [] }>()

const { t } = useI18n()

/** The server checkpoint is authoritative; older installs derive it from steps. */
const percent = computed(() => props.task.progress ?? Math.round(
  (props.task.steps.filter((step) => step.state === 'done').length / props.task.steps.length) * 100,
))
const failedStep = computed(() => props.task.steps.find((step) => step.state === 'failed'))
const failedLabel = computed(() => failedStep.value ? t(failedStep.value.labelKey) : '')

function dotClass(state: InstallStepState): string {
  return {
    pending: 'is-muted',
    running: 'is-info',
    done: 'is-success',
    failed: 'is-danger',
  }[state]
}
</script>

<template>
  <section class="plugin-task" :data-state="task.state">
    <header class="task-header">
      <div>
        <h3>{{ t('plugins.task.title') }} · {{ task.pluginName }}</h3>
        <p class="muted">
          {{ task.state === 'running' ? t('plugins.task.running', { percent }) : t(`plugins.task.${task.state}`) }}
        </p>
        <p v-if="task.taskId" class="muted task-meta">
          {{ t('plugins.task.taskId', { id: task.taskId }) }}
          <span v-if="task.attempt && task.attempt > 1"> · {{ t('plugins.task.attempt', { count: task.attempt }) }}</span>
        </p>
      </div>
      <span v-if="task.state === 'succeeded'" class="task-success"><el-icon><CircleCheck /></el-icon></span>
      <span v-else-if="task.state === 'failed'" class="task-failed"><el-icon><WarningFilled /></el-icon></span>
      <span v-else class="task-running"><el-icon><Loading /></el-icon></span>
    </header>

    <ol class="task-steps">
      <li
        v-for="step in task.steps"
        :key="step.key"
        class="task-step"
        :data-step="step.state"
      >
        <span class="dot" :class="dotClass(step.state)" />
        <span>{{ t(step.labelKey) }}</span>
      </li>
    </ol>

    <el-progress :percentage="percent" :status="task.state === 'failed' ? 'exception' : undefined" />

    <p v-if="task.pollError" class="muted task-warning" role="status">
      {{ t('plugins.task.pollFailed', { error: task.pollError }) }}
    </p>

    <div v-if="task.state === 'failed'" class="task-recovery">
      <p>{{ t('plugins.task.failedAt', { step: failedLabel }) }}</p>
      <p class="muted">{{ task.error }} · {{ t('plugins.task.failedHint') }}</p>
      <el-button type="primary" @click="$emit('retry')">{{ t('plugins.task.retry') }}</el-button>
      <el-button text :icon="Close" @click="$emit('dismiss')">{{ t('plugins.task.dismiss') }}</el-button>
    </div>
    <div v-else-if="task.state === 'canceled'" class="task-recovery">
      <p class="muted">{{ t('plugins.task.canceledHint') }}</p>
      <el-button type="primary" @click="$emit('retry')">{{ t('plugins.task.retry') }}</el-button>
      <el-button text :icon="Close" @click="$emit('dismiss')">{{ t('plugins.task.dismiss') }}</el-button>
    </div>
    <div v-else-if="task.state === 'succeeded'" class="task-recovery">
      <el-button :icon="Close" @click="$emit('dismiss')">{{ t('plugins.task.dismiss') }}</el-button>
    </div>
    <div v-else class="task-recovery">
      <el-button text @click="$emit('cancel')">{{ t('plugins.task.cancel') }}</el-button>
    </div>
  </section>
</template>

<style scoped>
.plugin-task { border: 1px solid var(--el-border-color-light); border-radius: 10px; padding: 16px; }
.task-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.task-header h3 { margin: 0 0 4px; font-size: 15px; }
.task-steps { list-style: none; margin: 12px 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 6px 12px; }
.task-step { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--el-text-color-secondary); }
.task-step[data-step='running'] { color: var(--el-text-color-primary); font-weight: 600; }
.task-step[data-step='failed'] { color: var(--el-color-danger); }
.dot { width: 8px; height: 8px; border-radius: 50%; background: var(--el-text-color-placeholder); }
.dot.is-success { background: var(--el-color-success); }
.dot.is-warning { background: var(--el-color-warning); }
.dot.is-danger { background: var(--el-color-danger); }
.dot.is-info { background: var(--el-color-primary); }
.dot.is-muted { background: var(--el-text-color-placeholder); }
.muted { color: var(--el-text-color-secondary); font-size: 13px; }
.task-success { color: var(--el-color-success); }
.task-failed { color: var(--el-color-danger); }
.task-running { color: var(--el-color-primary); }
.task-recovery { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; margin-top: 12px; }
.task-recovery p { margin: 0; font-size: 13px; }
.task-meta { margin: 2px 0 0; font-size: 12px; }
.task-warning { margin: 8px 0 0; color: var(--el-color-warning); }
</style>

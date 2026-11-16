<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  modelValue: boolean
  memberName: string
  currentVersion: string
}>()

const emit = defineEmits<{ (e: 'update:modelValue', value: boolean): void }>()

const { t } = useI18n()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

// Incus upgrades are host OS package operations; the Incus API cannot
// upgrade itself, so fleet guides the operator through the commands.
const steps = computed(() => [
  {
    title: t('node.upgradeStepCheck'),
    command: 'incus --version',
  },
  {
    title: t('node.upgradeStepRepo'),
    command: 'sudo apt-get update && sudo apt-get install -y incus',
  },
  {
    title: t('node.upgradeStepRestart'),
    command: 'sudo systemctl restart incus incus.socket',
  },
  {
    title: t('node.upgradeStepVerify'),
    command: 'incus --version && incus list',
  },
])

async function copyCommand(command: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(command)
  } catch {
    // Clipboard can be unavailable; the operator can select manually.
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="t('node.upgrade')" width="620px" destroy-on-close>
    <div class="upgrade-current">
      <span>{{ t('node.upgradeCurrent') }}</span>
      <el-tag size="small" effect="plain">{{ memberName }}</el-tag>
      <el-tag size="small">{{ currentVersion || t('common.unknown') }}</el-tag>
    </div>

    <el-alert
      class="upgrade-note"
      type="info"
      :closable="false"
      show-icon
      :title="t('node.upgradeNote')"
    />

    <ol class="upgrade-steps">
      <li v-for="(step, index) in steps" :key="index">
        <div class="step-title">{{ step.title }}</div>
        <div class="step-command">
          <code class="mono">{{ step.command }}</code>
          <el-button link type="primary" size="small" @click="copyCommand(step.command)">
            {{ t('node.upgradeCopy') }}
          </el-button>
        </div>
      </li>
    </ol>

    <p class="upgrade-caveat">{{ t('node.upgradeCaveat') }}</p>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.close') }}</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.upgrade-current {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}

.upgrade-note {
  margin-bottom: 12px;
}

.upgrade-steps {
  margin: 0 0 12px;
  padding-left: 20px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.step-title {
  font-size: 13px;
  margin-bottom: 4px;
}

.step-command {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  padding: 6px 10px;
}

.step-command code {
  font-size: 12px;
  word-break: break-all;
}

.upgrade-caveat {
  margin: 0;
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  font-size: 12px;
  line-height: 1.5;
}
</style>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ArrowDown,
  Delete,
  Edit,
  EditPen,
  MoreFilled,
  RefreshRight,
  Right,
  VideoPause,
  VideoPlay,
} from '@element-plus/icons-vue'

import type { InstanceAction, InstanceSummary } from '@/api/types'

const props = withDefaults(
  defineProps<{
    instance: Pick<InstanceSummary, 'name' | 'cluster' | 'status'>
    size?: 'small' | 'default'
    busy?: boolean
    showMigrate?: boolean
    showEdit?: boolean
  }>(),
  {
    size: 'small',
    busy: false,
    showMigrate: true,
    showEdit: true,
  },
)

const emit = defineEmits<{
  action: [action: InstanceAction]
  edit: []
  rename: []
  delete: []
  migrate: []
}>()

const { t } = useI18n()

const running = computed(() => (props.instance.status || '').toLowerCase() === 'running')
const buttonSize = computed(() => props.size)
const menuOpen = ref(false)

function choose(event: 'edit' | 'rename' | 'migrate' | 'delete'): void {
  menuOpen.value = false
  switch (event) {
    case 'edit':
      emit('edit')
      break
    case 'rename':
      emit('rename')
      break
    case 'migrate':
      emit('migrate')
      break
    case 'delete':
      emit('delete')
      break
  }
}
</script>

<template>
  <div class="instance-actions">
    <el-button
      v-if="running"
      :size="buttonSize"
      :icon="VideoPause"
      :disabled="busy"
      @click="emit('action', 'stop')"
    >
      {{ t('common.stop') }}
    </el-button>
    <el-button
      v-else
      :size="buttonSize"
      :icon="VideoPlay"
      :disabled="busy"
      @click="emit('action', 'start')"
    >
      {{ t('common.start') }}
    </el-button>
    <el-button
      v-if="running"
      :size="buttonSize"
      :icon="RefreshRight"
      :disabled="busy"
      @click="emit('action', 'restart')"
    >
      {{ t('common.restart') }}
    </el-button>

    <div class="action-menu-wrap">
      <el-button
        :size="buttonSize"
        :icon="MoreFilled"
        :disabled="busy"
        :aria-expanded="menuOpen"
        @click="menuOpen = !menuOpen"
      >
        {{ t('common.actions') }}
        <el-icon class="menu-caret"><ArrowDown /></el-icon>
      </el-button>
      <div v-show="menuOpen" class="action-menu" role="menu">
        <button
          v-if="showEdit"
          type="button"
          class="action-menu-item"
          :disabled="busy"
          @click="choose('edit')"
        >
          <el-icon><Edit /></el-icon>{{ t('common.edit') }}
        </button>
        <button
          type="button"
          class="action-menu-item"
          :disabled="busy"
          @click="choose('rename')"
        >
          <el-icon><EditPen /></el-icon>{{ t('common.rename') }}
        </button>
        <button
          v-if="showMigrate"
          type="button"
          class="action-menu-item"
          :disabled="busy"
          @click="choose('migrate')"
        >
          <el-icon><Right /></el-icon>{{ t('common.migrate') }}
        </button>
        <button
          type="button"
          class="action-menu-item is-danger"
          :disabled="busy"
          @click="choose('delete')"
        >
          <el-icon><Delete /></el-icon>{{ t('common.delete') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.instance-actions {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.instance-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}

.action-menu-wrap {
  position: relative;
  display: inline-flex;
}

.menu-caret {
  margin-left: 2px;
}

.action-menu {
  position: absolute;
  top: calc(100% + 4px);
  right: 0;
  z-index: 20;
  min-width: 150px;
  padding: 6px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  background: var(--el-bg-color-overlay);
  box-shadow: var(--el-box-shadow-light);
}

.action-menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 10px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--el-text-color-regular);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}

.action-menu-item:hover:not(:disabled) {
  background: var(--el-fill-color-light);
}

.action-menu-item:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.action-menu-item.is-danger {
  color: var(--el-color-danger);
}
</style>

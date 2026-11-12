<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Delete, Edit, RefreshRight, Right, VideoPause, VideoPlay } from '@element-plus/icons-vue'

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
  delete: []
  migrate: []
}>()

const { t } = useI18n()

const running = computed(() => (props.instance.status || '').toLowerCase() === 'running')
const buttonSize = computed(() => props.size)
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
    <el-button
      v-if="showEdit"
      :size="buttonSize"
      :icon="Edit"
      :disabled="busy"
      @click="emit('edit')"
    >
      {{ t('common.edit') }}
    </el-button>
    <el-button
      v-if="showMigrate"
      :size="buttonSize"
      :icon="Right"
      :disabled="busy"
      @click="emit('migrate')"
    >
      {{ t('common.migrate') }}
    </el-button>
    <el-button
      :size="buttonSize"
      :icon="Delete"
      type="danger"
      plain
      :disabled="busy"
      @click="emit('delete')"
    >
      {{ t('common.delete') }}
    </el-button>
  </div>
</template>

<style scoped>
.instance-actions {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 6px;
}

.instance-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}
</style>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { Delete } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(
  defineProps<{
    modelValue: Record<string, string>
    keyPlaceholder?: string
    valuePlaceholder?: string
    addLabel?: string
  }>(),
  {
    keyPlaceholder: '',
    valuePlaceholder: '',
    addLabel: '',
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: Record<string, string>]
}>()

const { t } = useI18n()

// Draft rows keep half-typed entries alive; the parent only sees valid pairs.
const rows = ref<[string, string][]>([])

watch(
  () => props.modelValue,
  (value) => {
    const entries = Object.entries(value)
    const committed = rows.value.filter(([key]) => key.trim())
    if (JSON.stringify(entries) !== JSON.stringify(committed)) {
      rows.value = entries.length > 0 ? entries : []
    }
  },
  { immediate: true, deep: true },
)

function commit(): void {
  const result: Record<string, string> = {}
  for (const [key, value] of rows.value) {
    const name = key.trim()
    if (name) {
      result[name] = value
    }
  }

  emit('update:modelValue', result)
}

function addRow(): void {
  rows.value.push(['', ''])
}

function removeRow(index: number): void {
  rows.value.splice(index, 1)
  commit()
}
</script>

<template>
  <div class="kv-editor">
    <div v-for="([key, value], index) in rows" :key="index" class="kv-row">
      <el-input
        :model-value="key"
        :placeholder="keyPlaceholder || t('admin.keyPlaceholder')"
        class="kv-key"
        @update:model-value="rows[index] = [$event, value]; commit()"
      />
      <el-input
        :model-value="value"
        :placeholder="valuePlaceholder || t('admin.valuePlaceholder')"
        class="kv-value"
        @update:model-value="rows[index] = [key, $event]; commit()"
      />
      <el-button :icon="Delete" circle plain type="danger" @click="removeRow(index)" />
    </div>
    <el-button plain size="small" @click="addRow">
      {{ addLabel || t('admin.addRow') }}
    </el-button>
  </div>
</template>

<style scoped>
.kv-editor {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.kv-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.kv-key {
  flex: 2;
}

.kv-value {
  flex: 3;
}
</style>

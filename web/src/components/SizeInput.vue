<script setup lang="ts">
import { computed, ref, watch } from 'vue'

const props = withDefaults(
  defineProps<{
    modelValue: string
    units?: string[]
    defaultUnit?: string
    min?: number
    max?: number
    disabled?: boolean
    placeholder?: string
  }>(),
  {
    units: () => ['MiB', 'GiB', 'TiB'],
    defaultUnit: 'MiB',
    min: 0,
    max: undefined,
    disabled: false,
    placeholder: '',
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const amount = ref<number | null>(null)
const unit = ref(props.defaultUnit)

function split(value: string): { amount: number | null; unit: string } {
  const match = value.trim().match(/^(\d+(?:\.\d+)?)\s*([A-Za-z%]+)?$/)
  if (!match) {
    return { amount: null, unit: props.defaultUnit }
  }
  const parsedUnit = match[2] ?? props.defaultUnit
  return {
    amount: Number(match[1]),
    unit: props.units.includes(parsedUnit) ? parsedUnit : props.defaultUnit,
  }
}

watch(
  () => props.modelValue,
  (value) => {
    const parsed = split(value)
    amount.value = parsed.amount
    unit.value = parsed.unit
  },
  { immediate: true },
)

const emitValue = computed(() => {
  if (amount.value === null || Number.isNaN(amount.value)) {
    return ''
  }
  return `${amount.value}${unit.value}`
})

function updateAmount(value: number | null | undefined): void {
  amount.value = value ?? null
  emit('update:modelValue', emitValue.value)
}

function updateUnit(value: string): void {
  unit.value = value
  emit('update:modelValue', emitValue.value)
}
</script>

<template>
  <div class="size-input">
    <el-input-number
      :model-value="amount"
      :min="min"
      :max="max"
      :controls="false"
      :disabled="disabled"
      :placeholder="placeholder"
      class="size-amount"
      @update:model-value="updateAmount"
    />
    <select
      :value="unit"
      :disabled="disabled"
      class="size-unit"
      @change="updateUnit(($event.target as HTMLSelectElement).value)"
    >
      <option v-for="item in units" :key="item" :value="item">{{ item }}</option>
    </select>
  </div>
</template>

<style scoped>
.size-input {
  display: flex;
  gap: 8px;
  width: 100%;
}

.size-amount {
  flex: 1;
}

.size-unit {
  width: 96px;
  flex: none;
  height: 32px;
  padding: 0 8px;
  border: 1px solid var(--el-border-color);
  border-radius: var(--el-border-radius-base);
  background: var(--el-fill-color-blank);
  color: var(--el-text-color-regular);
  font-size: 13px;
  cursor: pointer;
}

.size-unit:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}
</style>

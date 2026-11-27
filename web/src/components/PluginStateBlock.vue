<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

type State = 'loading' | 'empty' | 'error' | 'forbidden'

const props = defineProps<{
  state: State
  heading?: string
  message?: string
  actionLabel?: string
}>()

defineEmits<{ action: [] }>()

const { t } = useI18n()

const heading = computed(() => props.heading || t(`plugins.state.${props.state}`))
const detail = computed(() => props.message || (
  props.state === 'loading' ? '' : t(`plugins.state.${props.state}`)
))
</script>

<template>
  <div class="plugin-state" :class="`state-${state}`" :data-state="state">
    <span v-if="state === 'loading'" class="state-spinner" aria-hidden="true" />
    <h3>{{ heading }}</h3>
    <p v-if="detail && detail !== heading">{{ detail }}</p>
    <el-button v-if="actionLabel" text @click="$emit('action')">{{ actionLabel }}</el-button>
  </div>
</template>

<style scoped>
.plugin-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 48px 16px;
  text-align: center;
}
.plugin-state h3 { margin: 0; font-size: 15px; }
.plugin-state p {
  margin: 0 0 4px;
  max-width: 52ch;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.55;
}
.state-error h3,
.state-forbidden h3 { color: var(--el-color-danger); }
.state-spinner {
  width: 24px;
  height: 24px;
  border: 2px solid var(--el-border-color-light);
  border-top-color: var(--el-color-primary);
  border-radius: 50%;
  animation: plugin-spin 0.8s linear infinite;
}
@keyframes plugin-spin { to { transform: rotate(360deg); } }
</style>

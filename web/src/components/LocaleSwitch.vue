<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { SUPPORTED_LOCALES, setLocale, type Locale } from '@/i18n'

const { locale } = useI18n()

const selected = computed({
  get: () => locale.value as Locale,
  set: (value: Locale) => setLocale(value),
})
</script>

<template>
  <el-select
    v-model="selected"
    class="locale-switch"
    size="small"
    :aria-label="$t('app.language')"
    :title="$t('app.language')"
  >
    <el-option
      v-for="item in SUPPORTED_LOCALES"
      :key="item.value"
      :label="item.label"
      :value="item.value"
    />
  </el-select>
</template>

<style scoped>
.locale-switch {
  width: 108px;
}
</style>

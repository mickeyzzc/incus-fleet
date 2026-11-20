<script setup lang="ts">
import { computed, reactive } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Lock, User } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import LocaleSwitch from '@/components/LocaleSwitch.vue'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const { t } = useI18n()

const form = reactive({ username: '', password: '' })

const redirect = computed(() => {
  const value = route.query.redirect
  return typeof value === 'string' && value.startsWith('/') ? value : '/'
})

async function submit(): Promise<void> {
  if (!form.username.trim() || !form.password) {
    return
  }

  const ok = await auth.login(form.username, form.password)
  if (!ok) {
    return
  }

  form.username = ''
  form.password = ''
  await router.push(redirect.value)
}
</script>

<template>
  <main class="login-page">
    <section class="login-card">
      <div class="login-brand">
        <span class="brand-mark">IF</span>
        <div>
          <h1>{{ t('app.brand') }}</h1>
          <p>{{ t('login.tagline') }}</p>
        </div>
        <LocaleSwitch class="login-locale" />
      </div>

      <p class="login-hint">
        {{ t('login.hint') }}
      </p>

      <el-alert
        v-if="auth.error"
        class="fleet-alert"
        type="error"
        :closable="false"
        show-icon
        :title="auth.error"
      />

      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item :label="t('login.username')">
          <el-input
            v-model="form.username"
            size="large"
            :placeholder="t('login.usernamePlaceholder')"
            autocomplete="username"
            :prefix-icon="User"
          />
        </el-form-item>

        <el-form-item :label="t('login.password')">
          <el-input
            v-model="form.password"
            type="password"
            show-password
            size="large"
            :placeholder="t('login.passwordPlaceholder')"
            autocomplete="current-password"
            :prefix-icon="Lock"
            @keyup.enter="submit"
          />
        </el-form-item>

        <el-button
          type="primary"
          size="large"
          native-type="submit"
          :loading="auth.loading"
          :disabled="!form.username.trim() || !form.password"
          style="width: 100%"
        >
          {{ t('login.submit') }}
        </el-button>
      </el-form>

      <p class="login-hint">
        {{ t('login.tip') }}
      </p>
    </section>
  </main>
</template>

<style scoped>
.login-locale {
  margin-left: auto;
}
</style>

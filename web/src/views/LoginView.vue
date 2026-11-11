<script setup lang="ts">
import { computed, reactive } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Key } from '@element-plus/icons-vue'

import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const form = reactive({ token: '' })

const redirect = computed(() => {
  const value = route.query.redirect
  return typeof value === 'string' && value.startsWith('/') ? value : '/'
})

async function submit(): Promise<void> {
  if (!form.token.trim()) {
    return
  }

  const ok = await auth.login(form.token)
  if (!ok) {
    return
  }

  form.token = ''
  await router.push(redirect.value)
}
</script>

<template>
  <main class="login-page">
    <section class="login-card">
      <div class="login-brand">
        <span class="brand-mark">IF</span>
        <div>
          <h1>Incus Fleet</h1>
          <p>Multi-cluster control plane</p>
        </div>
      </div>

      <p class="login-hint">
        Sign in with the shared admin token configured on this server. The token is exchanged for
        an HttpOnly session cookie.
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
        <el-form-item label="Admin token">
          <el-input
            v-model="form.token"
            type="password"
            show-password
            size="large"
            placeholder="Enter the admin token"
            autocomplete="current-password"
            :prefix-icon="Key"
            @keyup.enter="submit"
          />
        </el-form-item>

        <el-button
          type="primary"
          size="large"
          native-type="submit"
          :loading="auth.loading"
          :disabled="!form.token.trim()"
          style="width: 100%"
        >
          Sign in
        </el-button>
      </el-form>

      <p class="login-hint">
        Tip: automation clients may send
        <code class="mono">Authorization: Bearer &lt;token&gt;</code> directly to the API.
      </p>
    </section>
  </main>
</template>

import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api, ApiError } from '@/api'
import { errorMessage } from '@/utils/errors'

/** Authentication state backed by the server-side session cookie. */
export const useAuthStore = defineStore('auth', () => {
  const authenticated = ref(false)
  const initialized = ref(false)
  const loading = ref(false)
  const error = ref('')
  const expiresAt = ref<string | null>(null)

  api.onUnauthorized = () => {
    authenticated.value = false
    expiresAt.value = null
  }

  const isAuthenticated = computed(() => authenticated.value)

  /** Loads the current session exactly once. */
  async function initialize(): Promise<void> {
    if (initialized.value) {
      return
    }

    loading.value = true
    error.value = ''

    try {
      const session = await api.session()
      authenticated.value = session.authenticated
      expiresAt.value = session.expires_at ?? null
    } catch (err) {
      authenticated.value = false
      if (!(err instanceof ApiError) || err.status !== 401) {
        error.value = errorMessage(err)
      }
    } finally {
      initialized.value = true
      loading.value = false
    }
  }

  /** Exchanges an admin token for a session cookie. */
  async function login(token: string): Promise<boolean> {
    loading.value = true
    error.value = ''

    try {
      const session = await api.login(token.trim())
      authenticated.value = session.authenticated
      expiresAt.value = session.expires_at ?? null
      initialized.value = true
      return true
    } catch (err) {
      authenticated.value = false
      error.value = errorMessage(err)
      return false
    } finally {
      loading.value = false
    }
  }

  /** Clears the server session and local state. */
  async function logout(): Promise<void> {
    try {
      await api.logout()
    } catch {
      // A stale or unreachable session must not block local sign-out.
    } finally {
      authenticated.value = false
      expiresAt.value = null
      initialized.value = true
      error.value = ''
    }
  }

  return {
    authenticated,
    initialized,
    loading,
    error,
    expiresAt,
    isAuthenticated,
    initialize,
    login,
    logout,
  }
})

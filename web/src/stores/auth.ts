import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api, ApiError } from '@/api'
import type { UserRole } from '@/api/types'
import { errorMessage } from '@/utils/errors'

/** Collapses duplicate sign-in requests (keyboard + form submit). */
let loginInFlight: Promise<boolean> | null = null

/** Authentication state backed by the server-side session cookie. */
export const useAuthStore = defineStore('auth', () => {
  const authenticated = ref(false)
  const initialized = ref(false)
  const loading = ref(false)
  const error = ref('')
  const expiresAt = ref<string | null>(null)
  const username = ref('')
  const role = ref<UserRole | ''>('')

  api.onUnauthorized = () => {
    authenticated.value = false
    expiresAt.value = null
    username.value = ''
    role.value = ''
  }

  const isAuthenticated = computed(() => authenticated.value)
  /** Whether the current account may mutate fleet state. */
  const canWrite = computed(() => role.value === 'admin' || role.value === 'operator')
  /** Whether the current account may manage users and fleet settings. */
  const canAdmin = computed(() => role.value === 'admin')

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
      username.value = session.username ?? ''
      role.value = session.role ?? ''
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
  async function login(user: string, password: string): Promise<boolean> {
    if (loginInFlight) {
      return loginInFlight
    }

    loading.value = true
    error.value = ''

    loginInFlight = (async () => {
      try {
        const session = await api.login(user.trim(), password)
        authenticated.value = session.authenticated
        expiresAt.value = session.expires_at ?? null
        username.value = session.username ?? user.trim()
        role.value = session.role ?? ''
        initialized.value = true
        return true
      } catch (err) {
        authenticated.value = false
        error.value = errorMessage(err)
        return false
      }
    })()

    try {
      return await loginInFlight
    } finally {
      loginInFlight = null
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
      username.value = ''
      role.value = ''
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
    username,
    role,
    isAuthenticated,
    canWrite,
    canAdmin,
    initialize,
    login,
    logout,
  }
})

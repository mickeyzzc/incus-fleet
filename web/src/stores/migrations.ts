import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api } from '@/api'
import type { Migration, MigrationRequest, MigrationStatus } from '@/api/types'
import { errorMessage } from '@/utils/errors'

const ACTIVE_STATUSES: readonly MigrationStatus[] = [
  'pending',
  'preflight',
  'stopping',
  'transferring',
  'starting',
  'verifying',
]

/** Migration history, submission, and live polling. */
export const useMigrationsStore = defineStore('migrations', () => {
  const items = ref<Migration[]>([])
  const loading = ref(false)
  const submitting = ref(false)
  const error = ref('')
  const lastUpdated = ref<Date | null>(null)

  let pollTimer: ReturnType<typeof setInterval> | null = null
  let pollIntervalMs = 2000

  const active = computed(() =>
    items.value.filter((migration) => ACTIVE_STATUSES.includes(migration.status)),
  )
  const hasActive = computed(() => active.value.length > 0)

  /** Fetches recent migrations. Polling stays silent after the first load. */
  async function fetchAll(limit = 50, silent = items.value.length > 0): Promise<void> {
    if (!silent) {
      loading.value = true
    }
    error.value = ''

    try {
      items.value = await api.listMigrations(limit)
      lastUpdated.value = new Date()
    } catch (err) {
      error.value = errorMessage(err)
    } finally {
      if (!silent) {
        loading.value = false
      }
    }
  }

  /** Submits a new migration and prepends it to the list. */
  async function submit(request: MigrationRequest): Promise<Migration> {
    submitting.value = true
    error.value = ''

    try {
      const migration = await api.submitMigration(request)
      items.value = [migration, ...items.value.filter((item) => item.id !== migration.id)]
      return migration
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      submitting.value = false
    }
  }

  /** Refreshes one migration in place. */
  async function refresh(id: string): Promise<Migration | null> {
    try {
      const migration = await api.getMigration(id)
      const index = items.value.findIndex((item) => item.id === id)
      if (index === -1) {
        items.value.unshift(migration)
      } else {
        items.value[index] = migration
      }

      return migration
    } catch (err) {
      error.value = errorMessage(err)
      return null
    }
  }

  /** Cancels a pending or in-flight migration. */
  async function cancel(id: string): Promise<Migration | null> {
    error.value = ''
    try {
      const migration = await api.cancelMigration(id)
      const index = items.value.findIndex((item) => item.id === id)
      if (index !== -1) {
        items.value[index] = migration
      }
      return migration
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    }
  }

  /** Resubmits a terminal migration with the same parameters. */
  async function retry(id: string): Promise<Migration> {
    error.value = ''
    try {
      const migration = await api.retryMigration(id)
      items.value = [migration, ...items.value]
      return migration
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    }
  }

  /** Starts periodic polling while migrations are active. */
  function startPolling(intervalMs = 2000): void {
    pollIntervalMs = intervalMs
    stopPolling()

    pollTimer = setInterval(() => {
      if (document.visibilityState === 'visible') {
        void poll()
      }
    }, pollIntervalMs)
  }

  async function poll(): Promise<void> {
    if (!hasActive.value) {
      stopPolling()
      return
    }

    await fetchAll()
  }

  /** Stops periodic polling. */
  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  return {
    items,
    loading,
    submitting,
    error,
    lastUpdated,
    active,
    hasActive,
    fetchAll,
    submit,
    refresh,
    cancel,
    retry,
    startPolling,
    stopPolling,
  }
})

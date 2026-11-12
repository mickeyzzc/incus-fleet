import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api } from '@/api'
import type {
  BatchItem,
  BatchTarget,
  Keystone,
  StoredScript,
} from '@/api/types'
import { errorMessage } from '@/utils/errors'

/** Assets store keeps shared keys and scripts and runs batch operations. */
export const useAssetsStore = defineStore('assets', () => {
  const keyCache = ref<Keystone[]>([])
  const scriptCache = ref<StoredScript[]>([])
  const keysLoading = ref(false)
  const scriptsLoading = ref(false)
  const saving = ref(false)
  const error = ref('')
  const lastBatch = ref<BatchItem[]>([])
  const batchRunning = ref(false)

  const hasKeys = computed(() => keyCache.value.length > 0)

  /** Loads every stored key. */
  async function fetchKeys(force = false): Promise<void> {
    if (!force && keyCache.value.length > 0) {
      return
    }
    keysLoading.value = true
    try {
      keyCache.value = await api.listKeys()
    } catch (err) {
      error.value = errorMessage(err)
    } finally {
      keysLoading.value = false
    }
  }

  /** Creates one key and refreshes the list. */
  async function createKey(request: { name: string; public_key: string; comment?: string }): Promise<void> {
    saving.value = true
    try {
      await api.createKey(request)
      await fetchKeys(true)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Updates one key. */
  async function updateKey(id: string, request: { name: string; public_key: string; comment?: string }): Promise<void> {
    saving.value = true
    try {
      await api.updateKey(id, request)
      await fetchKeys(true)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Deletes one key. */
  async function deleteKey(id: string): Promise<void> {
    saving.value = true
    try {
      await api.deleteKey(id)
      keyCache.value = keyCache.value.filter((key) => key.id !== id)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Loads every stored script. */
  async function fetchScripts(force = false): Promise<void> {
    if (!force && scriptCache.value.length > 0) {
      return
    }
    scriptsLoading.value = true
    try {
      scriptCache.value = await api.listScripts()
    } catch (err) {
      error.value = errorMessage(err)
    } finally {
      scriptsLoading.value = false
    }
  }

  /** Creates one script and refreshes the list. */
  async function createScript(request: { name: string; description?: string; body: string }): Promise<void> {
    saving.value = true
    try {
      await api.createScript(request)
      await fetchScripts(true)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Updates one script. */
  async function updateScript(id: string, request: { name: string; description?: string; body: string }): Promise<void> {
    saving.value = true
    try {
      await api.updateScript(id, request)
      await fetchScripts(true)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Deletes one script. */
  async function deleteScript(id: string): Promise<void> {
    saving.value = true
    try {
      await api.deleteScript(id)
      scriptCache.value = scriptCache.value.filter((script) => script.id !== id)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  /** Injects stored keys into every target instance. */
  async function batchInjectKeys(targets: BatchTarget[], keyIds: string[]): Promise<BatchItem[]> {
    batchRunning.value = true
    lastBatch.value = []
    try {
      lastBatch.value = await api.batchKeys({ targets, key_ids: keyIds })
      return lastBatch.value
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      batchRunning.value = false
    }
  }

  /** Runs one stored script on every target instance. */
  async function batchRunScript(targets: BatchTarget[], scriptId: string): Promise<BatchItem[]> {
    batchRunning.value = true
    lastBatch.value = []
    try {
      lastBatch.value = await api.batchScripts({ targets, script_id: scriptId })
      return lastBatch.value
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      batchRunning.value = false
    }
  }

  /** Starts, stops, or restarts every target instance. */
  async function batchPower(targets: BatchTarget[], action: 'start' | 'stop' | 'restart', force = false): Promise<BatchItem[]> {
    batchRunning.value = true
    lastBatch.value = []
    try {
      lastBatch.value = await api.batchPower({ targets, action, force })
      return lastBatch.value
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      batchRunning.value = false
    }
  }

  return {
    keyCache,
    scriptCache,
    keysLoading,
    scriptsLoading,
    saving,
    error,
    lastBatch,
    batchRunning,
    hasKeys,
    fetchKeys,
    createKey,
    updateKey,
    deleteKey,
    fetchScripts,
    createScript,
    updateScript,
    deleteScript,
    batchInjectKeys,
    batchRunScript,
    batchPower,
  }
})

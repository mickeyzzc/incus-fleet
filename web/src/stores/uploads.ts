import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api } from '@/api'
import { errorMessage } from '@/utils/errors'

export interface UploadTask {
  name: string
  cluster: string
  loaded: number
  total: number
  speed: number
  error: string
}

/** Tracks the active ISO upload so its progress survives page navigation. */
export const useUploadsStore = defineStore('uploads', () => {
  const task = ref<UploadTask | null>(null)
  const minimized = ref(false)
  let controller: AbortController | null = null
  let lastSample: { loaded: number; at: number } | null = null

  const percent = computed(() => {
    const current = task.value
    if (!current || current.total <= 0) {
      return 0
    }
    return Math.min(100, Math.round((current.loaded / current.total) * 100))
  })

  const etaSeconds = computed(() => {
    const current = task.value
    if (!current || current.speed <= 0) {
      return null
    }
    return Math.max(0, Math.ceil((current.total - current.loaded) / current.speed))
  })

  const active = computed(() => task.value !== null)

  /** Starts an ISO upload. Resolves when the transfer finishes or is canceled. */
  async function startISO(cluster: string, pool: string, file: File): Promise<boolean> {
    cancel()
    controller = new AbortController()
    lastSample = { loaded: 0, at: performance.now() }
    minimized.value = false
    task.value = { name: file.name, cluster, loaded: 0, total: file.size, speed: 0, error: '' }

    try {
      await api.importISOWithProgress(
        cluster,
        { file, pool: pool || undefined },
        (loaded, total) => {
          const current = task.value
          if (!current) {
            return
          }
          const now = performance.now()
          if (lastSample) {
            const seconds = (now - lastSample.at) / 1000
            if (seconds >= 0.25 && loaded > lastSample.loaded) {
              current.speed = (loaded - lastSample.loaded) / seconds
              lastSample = { loaded, at: now }
            }
          }
          current.loaded = loaded
          current.total = total
        },
        controller.signal,
      )
      task.value = null
      minimized.value = false
      controller = null
      return true
    } catch (err) {
      if (task.value && !controller?.signal.aborted) {
        task.value.error = errorMessage(err)
      } else if (controller?.signal.aborted) {
        task.value = null
      }
      controller = null
      minimized.value = false
      return false
    }
  }

  function cancel(): void {
    if (controller) {
      controller.abort()
      controller = null
    }
    task.value = null
    minimized.value = false
  }

  function dismiss(): void {
    task.value = null
    minimized.value = false
  }

  return { task, minimized, percent, etaSeconds, active, startISO, cancel, dismiss }
})

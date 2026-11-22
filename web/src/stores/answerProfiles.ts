import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { api } from '@/api'
import type { AnswerProfile, UnattendedProfile } from '@/api/types'
import { errorMessage } from '@/utils/errors'

export type AnswerProfileInput = Omit<AnswerProfile, 'id' | 'created_at' | 'updated_at'>

/** Answer profiles store keeps reusable unattended-install configurations. */
export const useAnswerProfilesStore = defineStore('answerProfiles', () => {
  const cache = ref<AnswerProfile[]>([])
  const loading = ref(false)
  const saving = ref(false)
  const error = ref('')

  const hasProfiles = computed(() => cache.value.length > 0)

  function byProfile(profile: UnattendedProfile): AnswerProfile[] {
    return cache.value.filter((item) => item.profile === profile)
  }

  function defaultFor(profile: UnattendedProfile): AnswerProfile | undefined {
    return cache.value.find((item) => item.profile === profile && item.is_default)
  }

  async function fetch(force = false): Promise<void> {
    if (!force && cache.value.length > 0) {
      return
    }
    loading.value = true
    try {
      cache.value = (await api.listAnswerProfiles()) ?? []
    } catch (err) {
      error.value = errorMessage(err)
    } finally {
      loading.value = false
    }
  }

  async function create(request: AnswerProfileInput): Promise<void> {
    saving.value = true
    try {
      await api.createAnswerProfile(request)
      await fetch(true)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  async function update(id: string, request: AnswerProfileInput): Promise<void> {
    saving.value = true
    try {
      await api.updateAnswerProfile(id, request)
      await fetch(true)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  async function remove(id: string): Promise<void> {
    saving.value = true
    try {
      await api.deleteAnswerProfile(id)
      cache.value = cache.value.filter((item) => item.id !== id)
    } catch (err) {
      error.value = errorMessage(err)
      throw err
    } finally {
      saving.value = false
    }
  }

  return {
    cache,
    loading,
    saving,
    error,
    hasProfiles,
    byProfile,
    defaultFor,
    fetch,
    create,
    update,
    remove,
  }
})

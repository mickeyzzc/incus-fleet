import { beforeEach, describe, expect, it, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

import { api } from '@/api'
import type { AnswerProfile } from '@/api/types'
import { useAnswerProfilesStore } from '@/stores/answerProfiles'

const sample: AnswerProfile = {
  id: 'ap1',
  name: 'lab',
  profile: 'debian-preseed',
  username: 'ops',
  is_default: true,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

describe('answer profiles store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listAnswerProfiles).mockReset()
    vi.mocked(api.createAnswerProfile).mockReset()
    vi.mocked(api.updateAnswerProfile).mockReset()
    vi.mocked(api.deleteAnswerProfile).mockReset()
  })

  it('loads profiles once and can force refresh', async () => {
    vi.mocked(api.listAnswerProfiles).mockResolvedValue([sample])
    const store = useAnswerProfilesStore()
    await store.fetch()
    await store.fetch()
    expect(api.listAnswerProfiles).toHaveBeenCalledTimes(1)
    expect(store.hasProfiles).toBe(true)
    expect(store.defaultFor('debian-preseed')?.id).toBe('ap1')
    expect(store.byProfile('nocloud')).toHaveLength(0)
    await store.fetch(true)
    expect(api.listAnswerProfiles).toHaveBeenCalledTimes(2)
  })

  it('creates, updates and deletes with list refresh', async () => {
    vi.mocked(api.listAnswerProfiles).mockResolvedValue([sample])
    vi.mocked(api.createAnswerProfile).mockResolvedValue(sample)
    vi.mocked(api.updateAnswerProfile).mockResolvedValue(sample)
    vi.mocked(api.deleteAnswerProfile).mockResolvedValue(undefined)
    const store = useAnswerProfilesStore()
    const payload = {
      name: 'lab', profile: 'debian-preseed' as const, username: 'ops',
      is_default: true,
    }
    await store.create(payload)
    expect(api.createAnswerProfile).toHaveBeenCalledWith(payload)
    await store.update('ap1', payload)
    expect(api.updateAnswerProfile).toHaveBeenCalledWith('ap1', payload)
    await store.remove('ap1')
    expect(api.deleteAnswerProfile).toHaveBeenCalledWith('ap1')
    // After delete the local cache drops the removed row.
    expect(store.cache.some((p) => p.id === 'ap1')).toBe(false)
  })

  it('captures errors', async () => {
    vi.mocked(api.listAnswerProfiles).mockRejectedValue(new Error('boom'))
    const store = useAnswerProfilesStore()
    await store.fetch()
    expect(store.error).toContain('boom')
  })
})

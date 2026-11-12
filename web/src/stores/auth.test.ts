import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '@/api'
import { setLocale } from '@/i18n'
import { useAuthStore } from './auth'

describe('auth store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('initializes from an authenticated session exactly once', async () => {
    vi.mocked(api.session).mockResolvedValue({
      authenticated: true,
      expires_at: '2026-11-12T00:00:00Z',
    })
    const store = useAuthStore()

    await store.initialize()
    await store.initialize()

    expect(api.session).toHaveBeenCalledTimes(1)
    expect(store.authenticated).toBe(true)
    expect(store.initialized).toBe(true)
    expect(store.loading).toBe(false)
    expect(store.expiresAt).toBe('2026-11-12T00:00:00Z')
    expect(store.isAuthenticated).toBe(true)
  })

  it('treats a 401 during initialize as unauthenticated without an error', async () => {
    vi.mocked(api.session).mockRejectedValue(new ApiError(401, 'unauthorized', 'no session'))
    const store = useAuthStore()

    await store.initialize()

    expect(store.authenticated).toBe(false)
    expect(store.error).toBe('')
    expect(store.initialized).toBe(true)
  })

  it('records non-401 initialize failures', async () => {
    vi.mocked(api.session).mockRejectedValue(new Error('network down'))
    const store = useAuthStore()

    await store.initialize()

    expect(store.authenticated).toBe(false)
    expect(store.error).toBe('network down')
  })

  it('logs in with a trimmed token', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true })
    const store = useAuthStore()

    await expect(store.login('  secret  ')).resolves.toBe(true)

    expect(api.login).toHaveBeenCalledWith('secret')
    expect(store.authenticated).toBe(true)
    expect(store.initialized).toBe(true)
    expect(store.error).toBe('')
  })

  it('reports invalid tokens in the active language', async () => {
    vi.mocked(api.login).mockRejectedValue(new ApiError(401, 'invalid_token', 'invalid admin token'))
    const store = useAuthStore()

    await expect(store.login('wrong')).resolves.toBe(false)

    expect(store.authenticated).toBe(false)
    expect(store.error).toBe('Invalid admin token.')

    setLocale('zh-CN')
    await expect(store.login('wrong')).resolves.toBe(false)
    expect(store.error).toBe('管理员 Token 无效。')
  })

  it('reports generic login failures', async () => {
    vi.mocked(api.login).mockRejectedValue(new Error('connection refused'))
    const store = useAuthStore()

    await expect(store.login('secret')).resolves.toBe(false)

    expect(store.error).toBe('connection refused')
  })

  it('clears local state on logout even when the request fails', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true })
    vi.mocked(api.logout).mockRejectedValue(new Error('already gone'))
    const store = useAuthStore()
    await store.login('secret')

    await store.logout()

    expect(store.authenticated).toBe(false)
    expect(store.expiresAt).toBeNull()
    expect(store.error).toBe('')
    expect(store.initialized).toBe(true)
  })

  it('clears authentication when the API reports an unauthorized response', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true })
    const store = useAuthStore()
    await store.login('secret')

    api.onUnauthorized?.()

    expect(store.authenticated).toBe(false)
    expect(store.expiresAt).toBeNull()
  })
})

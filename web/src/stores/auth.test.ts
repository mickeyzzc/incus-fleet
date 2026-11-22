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

  it('logs in with trimmed credentials', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true })
    const store = useAuthStore()

    await expect(store.login('  admin  ', 'secret')).resolves.toBe(true)

    expect(api.login).toHaveBeenCalledWith('admin', 'secret')
    expect(store.authenticated).toBe(true)
    expect(store.initialized).toBe(true)
    expect(store.error).toBe('')
  })

  it('reports invalid credentials in the active language', async () => {
    vi.mocked(api.login).mockRejectedValue(new ApiError(401, 'invalid_credentials', 'invalid credentials'))
    const store = useAuthStore()

    await expect(store.login('admin', 'wrong')).resolves.toBe(false)

    expect(store.authenticated).toBe(false)
    expect(store.error).toBe('Invalid username or password.')

    setLocale('zh-CN')
    await expect(store.login('admin', 'wrong')).resolves.toBe(false)
    expect(store.error).toBe('用户名或密码错误。')
  })

  it('reports generic login failures', async () => {
    vi.mocked(api.login).mockRejectedValue(new Error('connection refused'))
    const store = useAuthStore()

    await expect(store.login('admin', 'secret')).resolves.toBe(false)

    expect(store.error).toBe('connection refused')
  })

  it('clears local state on logout even when the request fails', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true })
    vi.mocked(api.logout).mockRejectedValue(new Error('already gone'))
    const store = useAuthStore()
    await store.login('admin', 'secret')

    await store.logout()

    expect(store.authenticated).toBe(false)
    expect(store.expiresAt).toBeNull()
    expect(store.error).toBe('')
    expect(store.initialized).toBe(true)
  })

  it('clears authentication when the API reports an unauthorized response', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true })
    const store = useAuthStore()
    await store.login('admin', 'secret')

    api.onUnauthorized?.()

    expect(store.authenticated).toBe(false)
    expect(store.expiresAt).toBeNull()
  })
})

describe('role awareness', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('derives canWrite and canAdmin from the session role', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true, role: 'operator', username: 'op' })
    const store = useAuthStore()
    await store.login('op', 'secret')

    expect(store.role).toBe('operator')
    expect(store.username).toBe('op')
    expect(store.canWrite).toBe(true)
    expect(store.canAdmin).toBe(false)
  })

  it('grants admin capabilities to administrators', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true, role: 'admin', username: 'root' })
    const store = useAuthStore()
    await store.login('root', 'secret')

    expect(store.canAdmin).toBe(true)
    expect(store.canWrite).toBe(true)
  })

  it('denies writing for viewers', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true, role: 'viewer', username: 'look' })
    const store = useAuthStore()
    await store.login('look', 'secret')

    expect(store.canWrite).toBe(false)
    expect(store.canAdmin).toBe(false)
  })

  it('clears identity when the API reports unauthorized', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true, role: 'admin', username: 'root' })
    const store = useAuthStore()
    await store.login('root', 'secret')

    api.onUnauthorized?.()

    expect(store.username).toBe('')
    expect(store.role).toBe('')
    expect(store.canAdmin).toBe(false)
  })

  it('restores identity from an existing session', async () => {
    vi.mocked(api.session).mockResolvedValue({
      authenticated: true,
      username: 'restored',
      role: 'operator',
    })
    const store = useAuthStore()
    await store.initialize()

    expect(store.username).toBe('restored')
    expect(store.role).toBe('operator')
  })
})

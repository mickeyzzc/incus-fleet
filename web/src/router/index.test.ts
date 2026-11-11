import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { createFleetRouter } from './index'

describe('fleet router', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    document.title = ''
  })

  it('sends unauthenticated visitors to login without a redirect for the dashboard', async () => {
    vi.mocked(api.session).mockResolvedValue({ authenticated: false })
    const router = createFleetRouter(createMemoryHistory())

    await router.push('/')
    await router.isReady()

    expect(api.session).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.name).toBe('login')
    expect(router.currentRoute.value.query).toEqual({})
  })

  it('preserves the requested path for protected pages', async () => {
    vi.mocked(api.session).mockResolvedValue({ authenticated: false })
    const router = createFleetRouter(createMemoryHistory())

    await router.push('/instances')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('login')
    expect(router.currentRoute.value.query).toEqual({ redirect: '/instances' })
  })

  it('allows the login page when unauthenticated and updates the title', async () => {
    vi.mocked(api.session).mockResolvedValue({ authenticated: false })
    const router = createFleetRouter(createMemoryHistory())

    await router.push('/login')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('login')
    expect(document.title).toBe('Sign in · Incus Fleet')
  })

  it('redirects authenticated users away from the login page', async () => {
    vi.mocked(api.session).mockResolvedValue({ authenticated: true })
    const router = createFleetRouter(createMemoryHistory())

    await router.push('/login')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('dashboard')
    expect(document.title).toBe('Dashboard · Incus Fleet')
  })

  it('allows authenticated navigation and loads the session only once', async () => {
    vi.mocked(api.session).mockResolvedValue({ authenticated: true })
    const router = createFleetRouter(createMemoryHistory())

    await router.push('/instances')
    await router.isReady()
    expect(router.currentRoute.value.name).toBe('instances')
    expect(document.title).toBe('Instances · Incus Fleet')

    await router.push('/migrations')
    await router.isReady()
    expect(document.title).toBe('Migrations · Incus Fleet')
    expect(api.session).toHaveBeenCalledTimes(1)
  })

  it('redirects unknown paths to the dashboard', async () => {
    vi.mocked(api.session).mockResolvedValue({ authenticated: true })
    const router = createFleetRouter(createMemoryHistory())

    await router.push('/does-not-exist')
    await router.isReady()

    expect(router.currentRoute.value.path).toBe('/')
    expect(document.title).toBe('Dashboard · Incus Fleet')
  })
})

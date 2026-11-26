import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { setLocale } from '@/i18n'
import { createFleetRouter, handleRouteError } from './index'

describe('fleet router', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    document.title = ''
    sessionStorage.removeItem('incus-fleet.chunk-reload')
  })

  it('reloads once on a stale chunk load failure', () => {
    const reload = vi.fn()
    vi.stubGlobal('location', { reload, href: 'http://localhost/' })

    handleRouteError(new Error('Failed to fetch dynamically imported module: old.js'))
    handleRouteError(new Error('Failed to fetch dynamically imported module: old.js'))

    expect(reload).toHaveBeenCalledTimes(1)
    expect(sessionStorage.getItem('incus-fleet.chunk-reload')).toBeTruthy()

    handleRouteError(new Error('unrelated failure'))
    expect(reload).toHaveBeenCalledTimes(1)
    vi.unstubAllGlobals()
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

    // Migration history now lives inside the instances workspace.
    await router.push('/migrations')
    await router.isReady()
    expect(router.currentRoute.value.path).toBe('/instances')
    expect(router.currentRoute.value.query).toEqual({ view: 'migrations' })
    expect(document.title).toBe('Instances · Incus Fleet')
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

  it('uses translated titles after switching language', async () => {
    vi.mocked(api.session).mockResolvedValue({ authenticated: true })
    setLocale('zh-CN')
    const router = createFleetRouter(createMemoryHistory())

    await router.push('/instances')
    await router.isReady()

    expect(document.title).toBe('实例 · Incus Fleet')
  })
})

describe('fleet router role guard', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    document.title = ''
  })

  it('blocks non-admins from the users page', async () => {
    const { useAuthStore } = await import('@/stores/auth')
    const auth = useAuthStore()
    auth.authenticated = true
    auth.initialized = true
    auth.role = 'viewer'

    const router = createFleetRouter(createMemoryHistory())
    await router.push('/users')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('dashboard')
  })

  it('allows admins into the users page', async () => {
    const { useAuthStore } = await import('@/stores/auth')
    const auth = useAuthStore()
    auth.authenticated = true
    auth.initialized = true
    auth.role = 'admin'

    const router = createFleetRouter(createMemoryHistory())
    await router.push('/users')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('users')
  })

  it('exposes the plugin manager route for authenticated accounts', async () => {
    const { useAuthStore } = await import('@/stores/auth')
    const auth = useAuthStore()
    auth.authenticated = true
    auth.initialized = true
    auth.role = 'admin'

    const router = createFleetRouter(createMemoryHistory())
    await router.push('/plugins')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('plugins')
    expect(router.currentRoute.value.meta.titleKey).toBe('titles.plugins')
  })

  it('resolves the plugin route component', async () => {
    const route = createFleetRouter(createMemoryHistory()).getRoutes().find((r) => r.name === 'plugins')
    expect(route).toBeDefined()
    expect(typeof route!.components?.default).toBe('function')
  })
})

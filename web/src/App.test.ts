import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { clusterSummary } from '@/test/fixtures'
import { useAuthStore } from '@/stores/auth'
import { useClustersStore } from '@/stores/clusters'
import App from './App.vue'

const LoginStub = { template: '<div class="login-stub">Login page</div>' }
const DashboardStub = { template: '<div class="dashboard-stub">Dashboard page</div>' }
const Blank = { template: '<div />' }

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/login',
        name: 'login',
        component: LoginStub,
        meta: { public: true, title: 'Sign in' },
      },
      { path: '/', name: 'dashboard', component: DashboardStub, meta: { title: 'Dashboard' } },
      { path: '/instances', name: 'instances', component: Blank, meta: { title: 'Instances' } },
    ],
  })
}

async function mountApp(path = '/', authenticated = true) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.authenticated = authenticated
  auth.initialized = true

  const router = createTestRouter()
  await router.push(path)
  await router.isReady()

  const wrapper = mount(App, {
    global: { plugins: [pinia, router, ElementPlus] },
  })
  await flushPromises()

  return { wrapper, router }
}

function buttonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`${text} button not found`)
  }

  return button
}

describe('App shell', () => {
  beforeEach(() => {
    vi.mocked(api.listClusters).mockResolvedValue([
      clusterSummary(),
      clusterSummary({ name: 'c2', display_name: 'Cluster 63', online: false }),
    ])
  })

  it('renders only the router view on public pages', async () => {
    const { wrapper } = await mountApp('/login', false)

    expect(wrapper.text()).toContain('Login page')
    expect(wrapper.text()).not.toContain('Sign out')
    expect(api.listClusters).not.toHaveBeenCalled()
  })

  it('renders the authenticated shell with cluster navigation', async () => {
    const { wrapper } = await mountApp()

    expect(wrapper.text()).toContain('Incus Fleet')
    expect(wrapper.text()).toContain('Dashboard page')
    expect(wrapper.text()).toContain('Cluster 62')
    expect(wrapper.text()).toContain('Cluster 63')
    expect(wrapper.text()).toContain('1/2 clusters online')
    expect(wrapper.find('.topbar h1').text()).toBe('Dashboard')
    expect(api.listClusters).toHaveBeenCalledTimes(1)
  })

  it('keeps a deep link while the initial session is unresolved', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.authenticated = false
    auth.initialized = false

    const router = createTestRouter()
    await router.push('/instances')
    await router.isReady()

    mount(App, { global: { plugins: [pinia, router, ElementPlus] } })
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('instances')

    // Session resolution completes: the URL must not be replaced by the
    // dashboard or the login page.
    auth.initialized = true
    auth.authenticated = true
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('instances')
  })

  it('uses cached clusters instead of refetching them', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.authenticated = true
    auth.initialized = true
    const clusters = useClustersStore()
    clusters.items = [clusterSummary()]

    const router = createTestRouter()
    await router.push('/')
    await router.isReady()

    const wrapper = mount(App, {
      global: { plugins: [pinia, router, ElementPlus] },
    })
    await flushPromises()

    expect(api.listClusters).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Cluster 62')
  })

  it('shows an empty cluster hint when nothing is available', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([])

    const { wrapper } = await mountApp()

    expect(wrapper.text()).toContain('No clusters loaded')
    expect(wrapper.text()).toContain('0/0 clusters online')
  })

  it('signs out and returns to the login page', async () => {
    vi.mocked(api.logout).mockResolvedValue(undefined)

    const { wrapper, router } = await mountApp()
    await buttonByText(wrapper, 'Sign out').trigger('click')
    await flushPromises()

    expect(api.logout).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.name).toBe('login')
    expect(wrapper.text()).toContain('Login page')
  })

  it('reflects the active route title and nested cluster path', async () => {
    const { wrapper, router } = await mountApp()

    await router.push('/instances')
    await flushPromises()

    expect(wrapper.find('.topbar h1').text()).toBe('Instances')
    expect(wrapper.text()).toContain('Manage independent Incus clusters from one place.')
  })

  it('routes to the login view with a redirect when the session expires mid-page', async () => {
    const { router } = await mountApp('/instances')

    // A background 401 flips the auth store; the shell must leave the broken
    // page instead of letting every request fail in place (#12).
    const auth = useAuthStore()
    auth.authenticated = false
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('login')
    expect(router.currentRoute.value.query.redirect).toBe('/instances')
  })
})

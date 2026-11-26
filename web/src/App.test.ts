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
      { path: '/clusters/:cluster', name: 'cluster', component: Blank, meta: { title: 'Cluster' } },
      {
        path: '/clusters/:cluster/members/:member',
        name: 'node-detail',
        component: Blank,
        meta: { title: 'Node' },
      },
    ],
  })
}

async function mountApp(
  path = '/',
  authenticated = true,
  role: 'admin' | 'operator' | 'viewer' = 'admin',
) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.authenticated = authenticated
  auth.initialized = true
  auth.role = role

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
    expect(wrapper.find('.breadcrumbs').text()).toContain('Dashboard')
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

  it('renders the fleet → cluster → node breadcrumb on node pages', async () => {
    const { wrapper, router } = await mountApp('/clusters/c1/members/node-1')

    expect(router.currentRoute.value.name).toBe('node-detail')
    expect(wrapper.find('.breadcrumbs').text()).toContain('Clusters')
    expect(wrapper.find('.breadcrumbs').text()).toContain('Cluster 62')
    expect(wrapper.find('.breadcrumbs').text()).toContain('node-1')
  })

  it('toggles the mobile navigation', async () => {
    const { wrapper } = await mountApp()

    expect(wrapper.find('.app-shell').classes()).not.toContain('sidebar-collapsed')
    await wrapper.find('.mobile-menu-toggle').trigger('click')
    expect(wrapper.find('.app-shell').classes()).toContain('sidebar-collapsed')
    await wrapper.find('.mobile-menu-toggle').trigger('click')
    expect(wrapper.find('.app-shell').classes()).not.toContain('sidebar-collapsed')
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

    expect(wrapper.find('.breadcrumbs').text()).toContain('Instances')
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

  it('loads the plugin list once for administrators', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: false })

    const { wrapper } = await mountApp('/', true, 'admin')
    await flushPromises()

    expect(api.listPlugins).toHaveBeenCalled()
    expect(wrapper.text()).toContain('Plugins')
  })

  it('hides the plugin manager entry from non-administrators', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })

    const { wrapper } = await mountApp('/', true, 'viewer')
    await flushPromises()

    expect(wrapper.text()).not.toContain('Plugins')
  })

  it('opens an installed plugin with a full document navigation', async () => {
    vi.mocked(api.listPlugins).mockResolvedValue({
      plugins: [{
        id: 'vm', name: 'VictoriaMetrics', version: '', enabled: true,
        upstream: 'http://127.0.0.1:8428', role: 'viewer', kind: 'external',
        ui: { path: '/plugin-ui/vm/', title_key: 'VictoriaMetrics', requires_role: 'viewer' },
      }],
      enabled: true,
    })
    const assign = vi.fn()
    vi.stubGlobal('location', { assign, href: 'http://localhost/' })

    const { wrapper } = await mountApp('/', true, 'admin')
    await flushPromises()

    const item = wrapper.findAll('.el-menu-item').find((el) => el.text().includes('VictoriaMetrics'))
    expect(item).toBeDefined()
    await item!.trigger('click')
    expect(assign).toHaveBeenCalledWith('/plugin-ui/vm/')
    vi.unstubAllGlobals()
  })
})

  it('keeps the active menu highlight for a trailing-slash deep link', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([])
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })

    const { wrapper, router } = await mountApp('/users', true, 'admin')
    await router.push('/users/')
    await flushPromises()

    const active = wrapper.find('.el-menu-item.is-active')
    expect(active.exists()).toBe(true)
    expect(active.text()).toContain('Users')
  })

it('opens the sidebar as an off-canvas drawer on narrow viewports', async () => {
  const original = window.matchMedia
  let viewportListener: ((event: MediaQueryListEvent) => void) | null = null
  const mocked = vi.fn().mockImplementation((query: string) => ({
    matches: query.includes('960px'),
    media: query,
    onchange: null,
    addEventListener: vi.fn((_event: string, listener: (event: MediaQueryListEvent) => void) => {
      viewportListener = listener
    }),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: mocked as unknown as typeof window.matchMedia,
  })

  try {
    const { wrapper } = await mountApp()
    expect(wrapper.find('.app-shell').classes()).toContain('sidebar-collapsed')

    await wrapper.find('.mobile-menu-toggle').trigger('click')
    expect(wrapper.find('.sidebar-backdrop').exists()).toBe(true)

    wrapper.findComponent({ name: 'ElMenu' }).vm.$emit('select')
    await flushPromises()
    expect(wrapper.find('.sidebar-backdrop').exists()).toBe(false)

    viewportListener!({ matches: false } as MediaQueryListEvent)
    await flushPromises()
    expect(wrapper.find('.app-shell').classes()).not.toContain('sidebar-collapsed')
    wrapper.unmount()
  } finally {
    if (original) {
      Object.defineProperty(window, 'matchMedia', {
        configurable: true,
        writable: true,
        value: original,
      })
    } else {
      delete (window as { matchMedia?: typeof window.matchMedia }).matchMedia
    }
  }
})

it('labels table body cells from their headers for narrow-screen cards', async () => {
  const table = document.createElement('div')
  table.className = 'el-table'
  table.innerHTML = `
    <div class="el-table__header-wrapper"><table><thead><tr><th>Name</th><th>Status</th></tr></thead></table></div>
    <div class="el-table__body-wrapper"><table><tbody><tr><td>web</td><td>Running</td></tr></tbody></table></div>`
  document.body.appendChild(table)

  try {
    const { wrapper } = await mountApp()
    await new Promise((resolve) => setTimeout(resolve, 30))

    const cells = table.querySelectorAll('.el-table__body-wrapper td')
    expect(cells[0]?.getAttribute('data-label')).toBe('Name')
    expect(cells[1]?.getAttribute('data-label')).toBe('Status')
    wrapper.unmount()
  } finally {
    table.remove()
  }
})

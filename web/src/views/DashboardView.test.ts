import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { clusterSummary, instanceSummary, migration } from '@/test/fixtures'
import DashboardView from './DashboardView.vue'

function mountDashboard() {
  return mount(DashboardView, {
    global: {
      plugins: [ElementPlus],
      stubs: { RouterLink: RouterLinkStub },
    },
  })
}

function buttonByText(wrapper: ReturnType<typeof mountDashboard>, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`${text} button not found`)
  }

  return button
}

describe('DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('loads fleet statistics and renders every cluster', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([
      clusterSummary(),
      clusterSummary({
        name: 'c2',
        display_name: 'Cluster 63',
        online: false,
        error: 'connection refused',
      }),
    ])
    vi.mocked(api.listInstances).mockResolvedValue([
      instanceSummary(),
      instanceSummary({ name: 'vm1', type: 'virtual-machine' }),
    ])
    vi.mocked(api.listMigrations).mockResolvedValue([migration({ status: 'transferring' })])

    const wrapper = mountDashboard()
    await flushPromises()

    expect(api.listClusters).toHaveBeenCalledTimes(1)
    expect(api.listInstances).toHaveBeenCalledWith('any')
    expect(wrapper.text()).toContain('Cluster 62')
    expect(wrapper.text()).toContain('Cluster 63')
    expect(wrapper.text()).toContain('connection refused')
    expect(wrapper.text()).toContain('1 / 2')
    expect(wrapper.text()).toContain('4 running')
    expect(wrapper.text()).toContain('Active migrations')
    expect(wrapper.findAllComponents(RouterLinkStub).length).toBe(2)
  })

  it('refreshes statistics on demand', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([clusterSummary()])
    vi.mocked(api.listInstances).mockResolvedValue([])
    vi.mocked(api.listMigrations).mockResolvedValue([])

    const wrapper = mountDashboard()
    await flushPromises()

    await buttonByText(wrapper, 'Refresh').trigger('click')
    await flushPromises()

    expect(api.listClusters).toHaveBeenCalledTimes(2)
    expect(api.listInstances).toHaveBeenCalledTimes(2)
    expect(api.listMigrations).toHaveBeenCalledTimes(2)
  })

  it('reconnects a cluster and reports success or failure', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([clusterSummary()])
    vi.mocked(api.listInstances).mockResolvedValue([])
    vi.mocked(api.listMigrations).mockResolvedValue([])
    vi.mocked(api.reconnectCluster).mockResolvedValue(clusterSummary({ online: true }))

    const wrapper = mountDashboard()
    await flushPromises()

    await buttonByText(wrapper, 'Reconnect').trigger('click')
    await flushPromises()
    expect(api.reconnectCluster).toHaveBeenCalledWith('c1')
    expect(ElMessage.success).toHaveBeenCalledWith('Reconnected c1.')

    vi.mocked(api.reconnectCluster).mockRejectedValue(new Error('still unreachable'))
    await buttonByText(wrapper, 'Reconnect').trigger('click')
    await flushPromises()
    expect(ElMessage.error).toHaveBeenCalledWith('still unreachable')
  })

  it('shows the empty state when no clusters are configured', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([])
    vi.mocked(api.listInstances).mockResolvedValue([])
    vi.mocked(api.listMigrations).mockResolvedValue([])

    const wrapper = mountDashboard()
    await flushPromises()

    expect(wrapper.text()).toContain('No clusters configured')
  })

  it('shows list errors', async () => {
    vi.mocked(api.listClusters).mockRejectedValue(new Error('control plane down'))
    vi.mocked(api.listInstances).mockResolvedValue([])
    vi.mocked(api.listMigrations).mockResolvedValue([])

    const wrapper = mountDashboard()
    await flushPromises()

    expect(wrapper.text()).toContain('control plane down')
  })
})

describe('DashboardView fleet resource summary', () => {
  it('renders fleet-wide CPU, memory, disk, and instance totals', async () => {
    setActivePinia(createPinia())
    vi.mocked(api.listClusters).mockResolvedValue([
      clusterSummary({
        cpu: { total: 4, architecture: 'x86_64' },
        memory: { used: 2_000_000_000, total: 16_000_000_000 },
      }),
      clusterSummary({
        name: 'c2',
        display_name: 'Cluster 63',
        cpu: { total: 8, architecture: 'x86_64' },
        memory: { used: 1_200_000_000, total: 16_000_000_000 },
      }),
    ])

    const wrapper = mountDashboard()
    await flushPromises()

    expect(wrapper.text()).toContain('12 vCPU')
    expect(wrapper.text()).toContain('3.0 GiB')
    expect(wrapper.text()).toContain('29.8 GiB')

    wrapper.unmount()
  })
})

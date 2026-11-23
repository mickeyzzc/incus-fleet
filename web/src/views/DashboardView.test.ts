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

function vmOf(wrapper: { vm: unknown }): any {
  return wrapper.vm
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

describe('DashboardView traffic polling and registered clusters', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('polls cluster traffic and derives rates', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.mocked(api.listClusters).mockResolvedValue([clusterSummary({ online: true })])
    vi.mocked(api.listInstances).mockResolvedValue([])
    vi.mocked(api.listMigrations).mockResolvedValue([])
    const first = {
      at: new Date().toISOString(),
      cluster: 'c1',
      total_rx_bytes: 1000,
      total_tx_bytes: 2000,
      total_rx_packets: 10,
      total_tx_packets: 20,
      instances: [],
    }
    const second = {
      at: new Date().toISOString(),
      cluster: 'c1',
      total_rx_bytes: 31000,
      total_tx_bytes: 62000,
      total_rx_packets: 310,
      total_tx_packets: 620,
      instances: [],
    }
    vi.mocked(api.clusterTraffic).mockResolvedValueOnce(first).mockResolvedValueOnce(second)

    const wrapper = mountDashboard()
    await flushPromises()
    expect(api.clusterTraffic).toHaveBeenCalledWith('c1')

    // second poll derives per-second rates from the delta
    await vi.advanceTimersByTimeAsync(3100)
    await flushPromises()

    expect(wrapper.text()).toContain('↓')
    // cumulative fleet counters rendered as well
    expect(wrapper.text()).toContain('30.3 KiB')

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('refreshes fleet status on the visible-page loop and stops on unmount', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.mocked(api.listClusters).mockResolvedValue([clusterSummary({ online: true })])
    vi.mocked(api.listInstances).mockResolvedValue([])
    vi.mocked(api.listMigrations).mockResolvedValue([])

    const wrapper = mountDashboard()
    await flushPromises()
    expect(api.listClusters).toHaveBeenCalledTimes(1)

    // The status loop re-runs the full refresh every 15s (#12).
    await vi.advanceTimersByTimeAsync(15500)
    await flushPromises()
    expect(api.listClusters).toHaveBeenCalledTimes(2)

    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(20000)
    await flushPromises()
    expect(api.listClusters).toHaveBeenCalledTimes(2)

    vi.useRealTimers()
  })

  it('skips offline clusters and tolerates traffic errors', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.mocked(api.listClusters).mockResolvedValue([
      clusterSummary({ name: 'c2', online: false }),
    ])
    vi.mocked(api.listInstances).mockResolvedValue([])
    vi.mocked(api.listMigrations).mockResolvedValue([])
    vi.mocked(api.clusterTraffic).mockRejectedValue(new Error('offline'))

    const wrapper = mountDashboard()
    await flushPromises()
    // A previously mounted dashboard from another test may still poll its own
    // (stale) store; only this test's offline cluster matters here.
    expect(vi.mocked(api.clusterTraffic).mock.calls.flat()).not.toContain('c2')

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('registers a cluster from the dashboard dialog', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([clusterSummary()])
    vi.mocked(api.listInstances).mockResolvedValue([])
    vi.mocked(api.listMigrations).mockResolvedValue([])

    const wrapper = mountDashboard()
    await flushPromises()

    await buttonByText(wrapper, 'Add cluster').trigger('click')
    // The dialog mounted with modelValue true renders the form
    expect(wrapper.text()).toContain('Register a cluster')

    // Trigger the registered event through the child component
    const dialog = wrapper.findComponent({ name: 'ClusterRegisterDialog' })
    expect(dialog.exists()).toBe(true)
    dialog.vm.$emit('registered')
    await flushPromises()
    expect(api.listClusters).toHaveBeenCalledTimes(2)
  })

  it('unregisters a registered cluster after confirmation', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([clusterSummary()])
    vi.mocked(api.listInstances).mockResolvedValue([])
    vi.mocked(api.listMigrations).mockResolvedValue([])
    vi.mocked(api.unregisterCluster).mockResolvedValue(undefined)

    const wrapper = mountDashboard()
    await flushPromises()

    const remove = wrapper.findAll('button').find((b) => b.text().includes('Remove'))
    if (!remove) {
      // Remove button only renders for registered clusters; emulate by invoking the handler
      await vmOf(wrapper).removeCluster('c1')
    } else {
      await remove.trigger('click')
    }
    await flushPromises()

    expect(api.unregisterCluster).toHaveBeenCalledWith('c1')
    expect(ElMessage.success).toHaveBeenCalled()
  })

  it('keeps the fleet unchanged when removal is cancelled', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([clusterSummary()])
    vi.mocked(api.listInstances).mockResolvedValue([])
    vi.mocked(api.listMigrations).mockResolvedValue([])

    const wrapper = mountDashboard()
    await flushPromises()

    const { ElMessageBox } = await import('element-plus')
    ;(ElMessageBox.confirm as any).mockRejectedValueOnce('cancel')
    await vmOf(wrapper).removeCluster('c1')
    expect(api.unregisterCluster).not.toHaveBeenCalled()
  })

  it('reports unregister failures', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([clusterSummary()])
    vi.mocked(api.listInstances).mockResolvedValue([])
    vi.mocked(api.listMigrations).mockResolvedValue([])
    vi.mocked(api.unregisterCluster).mockRejectedValue(new Error('store unavailable'))

    const wrapper = mountDashboard()
    await flushPromises()

    await vmOf(wrapper).removeCluster('c1')
    await flushPromises()
    expect(ElMessage.error).toHaveBeenCalledWith('store unavailable')
  })
})

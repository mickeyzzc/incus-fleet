import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import InstanceDetailDrawer from '@/components/InstanceDetailDrawer.vue'
import MigrateDialog from '@/components/MigrateDialog.vue'
import { clusterSummary, instanceDetail, instanceSummary } from '@/test/fixtures'
import ClusterView from './ClusterView.vue'

const Blank = { template: '<div />' }

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'dashboard', component: Blank },
      { path: '/clusters/:cluster', name: 'cluster', component: Blank },
    ],
  })
}

function mockFleet() {
  vi.mocked(api.listClusters).mockResolvedValue([
    clusterSummary(),
    clusterSummary({ name: 'c2', display_name: 'Cluster 63' }),
  ])
  vi.mocked(api.listClusterInstances).mockImplementation(async (cluster) =>
    cluster === 'c1'
      ? [instanceSummary(), instanceSummary({ name: 'vm1', type: 'virtual-machine' })]
      : [instanceSummary({ name: 'db', cluster: 'c2' })],
  )
  vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
  vi.mocked(api.reconnectCluster).mockResolvedValue(clusterSummary({ online: true }))
}

async function mountCluster(cluster = 'c1') {
  const router = createTestRouter()
  await router.push(`/clusters/${cluster}`)
  await router.isReady()

  const wrapper = mount(ClusterView, {
    props: { cluster },
    global: { plugins: [ElementPlus, router] },
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

describe('ClusterView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockFleet()
  })

  it('renders cluster details and instances', async () => {
    const { wrapper } = await mountCluster()

    expect(wrapper.text()).toContain('Cluster 62')
    expect(wrapper.text()).toContain('https://192.0.2.10:8443')
    expect(wrapper.text()).toContain('node-a')
    expect(wrapper.text()).toContain('web')
    expect(wrapper.text()).toContain('vm1')
    expect(wrapper.text()).toContain('2 shown')
  })

  it('filters by instance type', async () => {
    const { wrapper } = await mountCluster()

    const select = wrapper.findComponent({ name: 'ElSelect' })
    select.vm.$emit('update:modelValue', 'virtual-machine')
    await flushPromises()

    expect(api.listClusterInstances).toHaveBeenLastCalledWith('c1', 'virtual-machine')
  })

  it('opens the detail drawer and migration dialog', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Details').trigger('click')
    await flushPromises()

    expect(api.getInstance).toHaveBeenCalledWith('c1', 'web')
    expect(wrapper.findComponent(InstanceDetailDrawer).props('modelValue')).toBe(true)

    await buttonByText(wrapper, 'Migrate').trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(MigrateDialog)
    expect(dialog.props('modelValue')).toBe(true)
    expect(dialog.props('instance')).toMatchObject({ name: 'web', cluster: 'c1' })
  })

  it('refreshes, reconnects, and reports reconnect errors', async () => {
    const { wrapper } = await mountCluster()
    expect(api.listClusters).toHaveBeenCalledTimes(1)

    await buttonByText(wrapper, 'Refresh').trigger('click')
    await flushPromises()
    expect(api.listClusters).toHaveBeenCalledTimes(2)

    await buttonByText(wrapper, 'Reconnect').trigger('click')
    await flushPromises()
    expect(api.reconnectCluster).toHaveBeenCalledWith('c1')
    expect(ElMessage.success).toHaveBeenCalledWith('Reconnected c1.')

    vi.mocked(api.reconnectCluster).mockRejectedValue(new Error('still down'))
    await buttonByText(wrapper, 'Reconnect').trigger('click')
    await flushPromises()
    expect(ElMessage.error).toHaveBeenCalledWith('still down')
  })

  it('reloads when the route cluster changes', async () => {
    const { wrapper } = await mountCluster()

    await wrapper.setProps({ cluster: 'c2' })
    await flushPromises()

    expect(api.listClusterInstances).toHaveBeenCalledWith('c2', 'any')
    expect(wrapper.text()).toContain('db')
  })

  it('surfaces offline cluster and instance loading errors', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([
      clusterSummary({ online: false, error: 'connection refused' }),
    ])
    vi.mocked(api.listClusterInstances).mockRejectedValue(new Error('instances unavailable'))

    const { wrapper } = await mountCluster()

    expect(wrapper.text()).toContain('connection refused')
    expect(wrapper.text()).toContain('instances unavailable')
  })

  it('navigates back to the dashboard', async () => {
    const { wrapper, router } = await mountCluster()

    const back = wrapper.find('.title-with-back button')
    await back.trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('dashboard')
  })
})

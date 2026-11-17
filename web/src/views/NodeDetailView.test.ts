import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { defineComponent } from 'vue'

import NodeDetailView from './NodeDetailView.vue'
import { api } from '@/api'
import { setLocale } from '@/i18n'
import { clusterSummary } from '@/test/fixtures'
import { useClustersStore } from '@/stores/clusters'

const Blank = { template: '<div />' }

const NodePanelStub = defineComponent({
  name: 'NodeNetworkPanel',
  props: ['cluster', 'member'],
  template: '<div class="node-panel" />',
})

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'dashboard', component: Blank },
      { path: '/clusters/:cluster', name: 'cluster', component: Blank },
      { path: '/clusters/:cluster/members/:member', name: 'node-detail', component: Blank },
    ],
  })
}

async function mountDetail(cluster = 'c1', member = 'node-1') {
  const router = createTestRouter()
  await router.push(`/clusters/${cluster}/members/${member}`)
  await router.isReady()

  const wrapper = mount(NodeDetailView, {
    props: { cluster, member },
    global: {
      plugins: [ElementPlus, router],
      stubs: { NodeNetworkPanel: NodePanelStub },
    },
  })
  await flushPromises()

  return { wrapper, router }
}

describe('NodeDetailView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listClusters).mockResolvedValue([clusterSummary()])
  })

  it('renders member metadata and the network panel', async () => {
    const clusters = useClustersStore()
    await clusters.fetchAll()

    const { wrapper } = await mountDetail()

    expect(wrapper.text()).toContain('node-1')
    expect(wrapper.text()).toContain('database-leader')
    expect(wrapper.text()).toContain('x86_64')
    expect(wrapper.text()).toContain('https://192.0.2.10:8443')
    expect(wrapper.text()).toContain('Online')

    const panel = wrapper.findComponent(NodePanelStub)
    expect(panel.props()).toMatchObject({ cluster: 'c1', member: 'node-1' })
  })

  it('fetches the cluster inventory when opened directly', async () => {
    const { wrapper } = await mountDetail()
    await flushPromises()

    expect(api.listClusters).toHaveBeenCalled()
    expect(wrapper.findComponent(NodePanelStub).exists()).toBe(true)
  })

  it('translates the member status into the active language', async () => {
    setLocale('zh-CN')
    const clusters = useClustersStore()
    await clusters.fetchAll()

    const { wrapper } = await mountDetail()

    expect(wrapper.text()).toContain('在线')
  })

  it('reports a missing member without mounting the panel', async () => {
    const clusters = useClustersStore()
    await clusters.fetchAll()

    const { wrapper } = await mountDetail('c1', 'ghost')

    expect(wrapper.text()).toContain('Member ghost is not reported by the cluster.')
    expect(wrapper.findComponent(NodePanelStub).exists()).toBe(false)
  })

  it('navigates back to the cluster page', async () => {
    const clusters = useClustersStore()
    await clusters.fetchAll()

    const { wrapper, router } = await mountDetail()

    const back = wrapper.findAll('button').find((button) => button.text().includes('Back to cluster'))
    await back?.trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('cluster')
    expect(router.currentRoute.value.params.cluster).toBe('c1')
  })

  it('refreshes the cluster inventory from the header', async () => {
    const clusters = useClustersStore()
    await clusters.fetchAll()
    const { wrapper } = await mountDetail()

    vi.mocked(api.listClusters).mockClear()
    const refresh = wrapper.findAll('button').find((button) => button.text().includes('Refresh'))
    await refresh?.trigger('click')
    await flushPromises()

    expect(api.listClusters).toHaveBeenCalled()
  })
})

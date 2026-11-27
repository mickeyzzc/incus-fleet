import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { defineComponent, h } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import PluginsView from './PluginsView.vue'

const DialogStub = defineComponent({
  name: 'ElDialog',
  props: { modelValue: { type: Boolean, default: false } },
  setup(props, { slots }) {
    return () =>
      props.modelValue ? h('div', { class: 'dialog-stub' }, [slots.default?.(), slots.footer?.()]) : null
  },
})

function mountView() {
  return mount(PluginsView, {
    global: { plugins: [ElementPlus], stubs: { ElDialog: DialogStub } },
  })
}

async function openCatalog(wrapper: ReturnType<typeof mountView>) {
  const tab = wrapper.findAll('.el-tabs__item').find((item) => item.text().includes('Catalog'))
  await tab!.trigger('click')
  await flushPromises()
}

describe('PluginsView install flow', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.mocked(api.listClusters).mockResolvedValue([])
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.listPluginCatalog).mockResolvedValue({
      catalog: [
        {
          id: 'vm',
          name: 'VictoriaMetrics',
          description: 'metrics',
          upstream_hint: 'http://vm.internal:8428',
          ui_path: '/plugin-ui/vm/',
          wiring: 'metrics.query_url',
        },
      ],
    })
    vi.mocked(api.installPlugin).mockResolvedValue({
      token: 'plugin-token',
      plugin: {
        id: 'vm',
        name: 'VictoriaMetrics',
        version: '1.0.0',
        enabled: true,
        role: 'viewer',
        kind: 'external',
        ui: { path: '/plugin-ui/vm/', title_key: 'plugins.victoriaMetrics' },
      },
    })
  })

  it('connects an upstream-only plugin from the catalog', async () => {
    const wrapper = mountView()
    await flushPromises()
    await openCatalog(wrapper)

    await wrapper.find('.catalog-card .el-button--primary').trigger('click')
    await flushPromises()

    const dialog = wrapper.find('.dialog-stub')
    expect(dialog.exists()).toBe(true)
    const input = dialog.find('input')
    await input.setValue('http://vm.example:8428')

    const confirm = dialog.findAll('button').find((button) => button.text().includes('One-click install'))
    await confirm!.trigger('click')
    await flushPromises()

    expect(api.installPlugin).toHaveBeenCalledWith({ id: 'vm', upstream: 'http://vm.example:8428' })
  })

  it('deploys an instance-backed plugin with the selected cluster', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([
      {
        name: 'c1',
        display_name: 'c1',
        online: true,
        members: [{ name: 'node-a', status: 'Online' }],
      } as never,
    ])
    vi.mocked(api.listPluginCatalog).mockResolvedValue({
      catalog: [
        {
          id: 'vmlogs',
          name: 'VictoriaLogs',
          description: 'logs',
          image: 'vmlogs-oci',
          ui_path: '/plugin-ui/vmlogs/',
          wiring: 'logs.query_url',
        },
      ],
    })

    const wrapper = mountView()
    await flushPromises()
    await openCatalog(wrapper)

    await wrapper.find('.catalog-card .el-button--primary').trigger('click')
    await flushPromises()

    const dialog = wrapper.find('.dialog-stub')
    expect(dialog.exists()).toBe(true)
    const confirm = dialog.findAll('button').find((button) => button.text().includes('One-click install'))
    await confirm!.trigger('click')
    await flushPromises()

    expect(api.installPlugin).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 'vmlogs',
        cluster: 'c1',
        name: 'fleet-vmlogs',
      }),
    )
  })
})

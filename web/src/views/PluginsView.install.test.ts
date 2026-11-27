import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { PluginInstallTaskStatus } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import PluginsView from './PluginsView.vue'

function taskStatus(overrides: Partial<PluginInstallTaskStatus> = {}): PluginInstallTaskStatus {
  return {
    id: 'task-1',
    plugin_id: 'vm',
    state: 'running',
    attempt: 1,
    created_at: '2026-11-27T10:00:00Z',
    updated_at: '2026-11-27T10:00:00Z',
    ...overrides,
  }
}

const DialogStub = defineComponent({
  name: 'ElDialog',
  props: { modelValue: { type: Boolean, default: false } },
  setup(props, { slots }) {
    return () => props.modelValue
      ? h('div', { class: 'wizard' }, [slots.default?.(), slots.footer?.()])
      : null
  },
})

const Blank = { template: '<div />' }

async function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.authenticated = true
  auth.initialized = true
  auth.role = 'admin'

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/plugins', name: 'plugins', component: PluginsView },
      { path: '/plugins/:id', name: 'plugin-detail', component: Blank },
    ],
  })
  await router.push('/plugins')
  await router.isReady()

  const wrapper = mount(PluginsView, {
    global: { plugins: [pinia, router, ElementPlus], stubs: { ElDialog: DialogStub } },
  })
  await flushPromises()
  return wrapper
}

function button(wrapper: any, text: string) {
  const found = wrapper.findAll('button').find((item: any) => item.text().includes(text))
  if (!found) throw new Error(`button "${text}" not found`)
  return found
}

async function startWizard(wrapper: ReturnType<typeof mount>) {
  await button(wrapper, 'Install plugin').trigger('click')
  await flushPromises()
  expect(wrapper.find('.wizard').exists()).toBe(true)
}

async function finishWizard(wrapper: ReturnType<typeof mount>) {
  const wizard = wrapper.find('.wizard')
  await button(wizard, 'Next').trigger('click')
  await button(wizard, 'Next').trigger('click')
  await wizard.find('.wizard-accept input').setValue(true)
  await button(wizard, 'Install').trigger('click')
  await flushPromises()
  await flushPromises()
}

describe('PluginsView install flow', () => {
  beforeEach(() => {
    vi.mocked(api.listClusters).mockResolvedValue([
      { name: 'c1', display_name: 'c1', online: true } as never,
    ])
    vi.mocked(api.listPlugins).mockResolvedValue({ plugins: [], enabled: true })
    vi.mocked(api.listPluginCatalog).mockResolvedValue({
      catalog: [{
        id: 'vm', name: 'VictoriaMetrics', description: 'metrics',
        value: 'metrics store', publisher: 'fleet-official', tier: 'official', permissions: 2,
        ui_path: '/plugin-ui/vm/', wiring: 'metrics.query_url', image: 'vm-oci',
      }],
    })
    vi.mocked(api.submitPluginInstallTask).mockResolvedValue({ task: taskStatus({ state: 'queued' }) })
    vi.mocked(api.getPluginInstallTask).mockResolvedValue({ task: taskStatus({ state: 'succeeded' }) })
    vi.mocked(api.cancelPluginInstallTask).mockResolvedValue(undefined)
    vi.mocked(api.resumePluginInstallTask).mockResolvedValue({ status: 'queued' })
  })

  it('starts from the single header action, not from a catalog card', async () => {
    const wrapper = await mountView()
    const catalog = wrapper.findAll('.el-tabs__item').find((item: any) => item.text().includes('Catalog'))
    await catalog!.trigger('click')
    await flushPromises()

    expect(wrapper.findAll('.catalog-card .el-button--primary')).toHaveLength(0)
    await startWizard(wrapper)
    expect(wrapper.find('.wizard').exists()).toBe(true)
  })

  it('connects an upstream-only plugin and closes the wizard', async () => {
    vi.mocked(api.listPluginCatalog).mockResolvedValue({
      catalog: [{
        id: 'vm', name: 'VictoriaMetrics', description: 'metrics',
        ui_path: '/plugin-ui/vm/', wiring: 'metrics.query_url',
        upstream_hint: 'http://vm.example:8428',
      }],
    })
    const wrapper = await mountView()
    await startWizard(wrapper)

    await wrapper.find('.wizard-upstream input').setValue('http://vm.example:8428')
    await finishWizard(wrapper)

    expect(api.submitPluginInstallTask).toHaveBeenCalledWith({ id: 'vm', upstream: 'http://vm.example:8428' })
    expect(api.installPlugin).not.toHaveBeenCalled()
    expect(wrapper.find('.wizard').exists()).toBe(false)
    expect(wrapper.find('.plugin-task').exists()).toBe(true)
  })

  it('deploys an instance-backed plugin with the selected cluster', async () => {
    const wrapper = await mountView()
    await startWizard(wrapper)
    await finishWizard(wrapper)

    expect(api.submitPluginInstallTask).toHaveBeenCalledWith(expect.objectContaining({
      id: 'vm',
      cluster: 'c1',
      name: 'fleet-vm',
    }))
    expect(wrapper.text()).toContain('Install finished')
  })

  it('reports a failed install at its step and resumes on demand', async () => {
    vi.mocked(api.getPluginInstallTask)
      .mockResolvedValueOnce({ task: taskStatus({ state: 'failed', error: 'image import failed' }) })
      .mockResolvedValueOnce({ task: taskStatus({ state: 'succeeded' }) })

    const wrapper = await mountView()
    await startWizard(wrapper)
    await finishWizard(wrapper)

    expect(wrapper.text()).toContain('image import failed')
    expect(wrapper.text()).toContain('Failed at')

    await button(wrapper, 'Resume install').trigger('click')
    await flushPromises()
    await flushPromises()

    expect(api.resumePluginInstallTask).toHaveBeenCalledWith('task-1')
    expect(wrapper.text()).toContain('Install finished')
  })

  it('cancels a running task from the panel', async () => {
    vi.mocked(api.getPluginInstallTask).mockResolvedValue({ task: taskStatus() })

    const wrapper = await mountView()
    await startWizard(wrapper)
    await finishWizard(wrapper)

    await button(wrapper, 'Cancel install').trigger('click')
    await flushPromises()

    expect(api.cancelPluginInstallTask).toHaveBeenCalledWith('task-1')
    expect(wrapper.text()).toContain('Install canceled')
  })

  it('keeps the task panel free of nested dialogs', async () => {
    const wrapper = await mountView()
    await startWizard(wrapper)
    await finishWizard(wrapper)

    expect(wrapper.findAll('.wizard')).toHaveLength(0)
    expect(wrapper.findAll('.el-dialog')).toHaveLength(0)
  })
})

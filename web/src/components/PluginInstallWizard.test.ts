import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { defineComponent, h } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { PluginCatalogEntry } from '@/api/types'
import PluginInstallWizard from './PluginInstallWizard.vue'

const DialogStub = defineComponent({
  name: 'ElDialog',
  props: { modelValue: { type: Boolean, default: false } },
  setup(props, { slots }) {
    return () => props.modelValue
      ? h('div', { class: 'wizard' }, [slots.default?.(), slots.footer?.()])
      : null
  },
})

function catalog(overrides: Partial<PluginCatalogEntry> = {}): PluginCatalogEntry[] {
  return [{
    id: 'vm',
    name: 'VictoriaMetrics',
    description: 'metrics store',
    ui_path: '/plugin-ui/vm/',
    wiring: 'metrics.query_url',
    image: 'vm-oci',
    default_cpu: '2',
    default_memory: '4GiB',
    default_disk: '20GiB',
    default_network: 'incusbr0',
    effects: [
      { sentence: 'metrics', consequence: 'consequenceMetrics' },
      { sentence: 'Fleet will show a new menu entry', consequence: 'Nothing is removed' },
    ],
    ...overrides,
  }]
}

async function render(entries = catalog(), selectedId = 'vm') {
  const wrapper = mount(PluginInstallWizard, {
    props: {
      visible: true,
      catalog: entries,
      selectedId,
      clusters: [{ name: 'c1', display_name: 'c1', online: true } as never],
      loading: false,
    },
    global: { plugins: [ElementPlus], stubs: { ElDialog: DialogStub } },
  })
  await wrapper.vm.$nextTick()
  return wrapper
}

function button(wrapper: ReturnType<typeof mount>, text: string) {
  const found = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!found) throw new Error(`button "${text}" not found`)
  return found
}

describe('PluginInstallWizard', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows three steps with one decision each', async () => {
    const wrapper = await render()
    const steps = wrapper.findAll('.el-step__title').map((item) => item.text())
    expect(steps).toHaveLength(3)
    expect(steps.join(' ')).toContain('Placement')
    expect(steps.join(' ')).toContain('Resources')
    expect(steps.join(' ')).toContain('Permissions')
  })

  it('keeps advanced resource fields collapsed until requested', async () => {
    const wrapper = await render()
    await button(wrapper, 'Next').trigger('click')

    expect(wrapper.text()).not.toContain('Filesystem')
    const advanced = wrapper.find('.wizard-advanced summary')
    expect(advanced.exists()).toBe(true)
    await advanced.trigger('click')
    expect(wrapper.text()).toContain('Filesystem')
  })

  it('renders effects as human sentences with their consequences', async () => {
    const wrapper = await render()
    await button(wrapper, 'Next').trigger('click')
    await button(wrapper, 'Next').trigger('click')

    const text = wrapper.text()
    expect(text).toContain('Fleet will send its metrics to this plugin')
    expect(text).toContain('Metric ingestion changes immediately')
    expect(text).toContain('Fleet will show a new menu entry')
  })

  it('gates install on the single acceptance checkbox', async () => {
    const wrapper = await render()
    await button(wrapper, 'Next').trigger('click')
    await button(wrapper, 'Next').trigger('click')

    let install = button(wrapper, 'Install')
    expect(install.attributes('disabled')).toBeDefined()

    await wrapper.find('.wizard-accept input').setValue(true)
    install = button(wrapper, 'Install')
    expect(install.attributes('disabled')).toBeUndefined()
  })

  it('emits an instance-backed install request after approval', async () => {
    const wrapper = await render()
    await button(wrapper, 'Next').trigger('click')
    await button(wrapper, 'Next').trigger('click')
    await wrapper.find('.wizard-accept input').setValue(true)
    await button(wrapper, 'Install').trigger('click')

    expect(wrapper.emitted('submit')?.[0]).toEqual([{
      id: 'vm',
      cluster: 'c1',
      member: undefined,
      name: 'fleet-vm',
      cpu: '2',
      memory: '4GiB',
      disk: '20GiB',
      network: 'incusbr0',
      filesystem: 'ext4',
      mount_point: '/data',
    }])
  })

  it('emits an upstream-only request without instance fields', async () => {
    const entries = catalog({ image: undefined, default_cpu: undefined })
    const wrapper = await render(entries, 'vm')
    const upstream = wrapper.find('.wizard-upstream input')
    expect(upstream.exists()).toBe(true)
    await upstream.setValue('http://vm.example:8428')

    await button(wrapper, 'Next').trigger('click')
    await button(wrapper, 'Next').trigger('click')
    await wrapper.find('.wizard-accept input').setValue(true)
    await button(wrapper, 'Install').trigger('click')

    const request = wrapper.emitted('submit')?.[0]?.[0] as Record<string, unknown>
    expect(request).toEqual({ id: 'vm', upstream: 'http://vm.example:8428' })
  })

  it('emits close from the cancel action', async () => {
    const wrapper = await render()
    await button(wrapper, 'Cancel').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})

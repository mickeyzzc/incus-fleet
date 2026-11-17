import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { clusterSummary, instanceSummary, migration } from '@/test/fixtures'
import MigrateDialog from './MigrateDialog.vue'

const clusters = [
  clusterSummary(),
  clusterSummary({ name: 'c2', display_name: 'Cluster 63', endpoint: 'https://192.0.2.11:8443' }),
]

function mountDialog(instance = instanceSummary({ type: 'virtual-machine', migration_stateful: true })) {
  return mount(MigrateDialog, {
    props: { modelValue: true, instance, clusters },
    global: { plugins: [ElementPlus] },
  })
}

function submitButton(wrapper: ReturnType<typeof mountDialog>) {
  const button = wrapper.findAll('button').find((item) => item.text().includes('Start migration'))
  if (!button) {
    throw new Error('submit button not found')
  }

  return button
}

function vmOf(wrapper: { vm: unknown }): any {
  return wrapper.vm
}

describe('MigrateDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('defaults to the first online target and submits a move', async () => {
    vi.mocked(api.submitMigration).mockResolvedValue(migration({ status: 'pending' }))
    const wrapper = mountDialog()
    await flushPromises()

    expect(wrapper.text()).toContain('Cluster 63')
    const select = wrapper.findComponent({ name: 'ElSelect' })
    expect(select.props('modelValue')).toBe('c2')

    await submitButton(wrapper).trigger('click')
    await flushPromises()

    expect(api.submitMigration).toHaveBeenCalledWith({
      source_cluster: 'c1',
      target_cluster: 'c2',
      instance: 'web',
      mode: 'move',
      live: false,
      include_snapshots: false,
      auto_start: true,
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Migration queued for web.')
    expect(wrapper.emitted('submitted')?.[0]?.[0]).toMatchObject({ status: 'pending' })
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([false])
  })

  it('requires a target selection', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const select = wrapper.findComponent({ name: 'ElSelect' })
    select.vm.$emit('update:modelValue', '')
    await flushPromises()

    await submitButton(wrapper).trigger('click')
    await flushPromises()

    expect(api.submitMigration).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Select a target cluster.')
  })

  it('selects a target when cluster data arrives after opening', async () => {
    const wrapper = mount(MigrateDialog, {
      props: { modelValue: true, instance: instanceSummary(), clusters: [] },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    expect(submitButton(wrapper).attributes('disabled')).toBeDefined()

    await wrapper.setProps({ clusters })
    await flushPromises()

    expect(wrapper.findComponent({ name: 'ElSelect' }).props('modelValue')).toBe('c2')
    expect(wrapper.text()).toContain('Cluster 63')
    expect(submitButton(wrapper).attributes('disabled')).toBeUndefined()
  })

  it('shows a warning when no other cluster is online', async () => {
    const wrapper = mount(MigrateDialog, {
      props: {
        modelValue: true,
        instance: instanceSummary(),
        clusters: [clusterSummary()],
      },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('No migration target')
    expect(submitButton(wrapper).attributes('disabled')).toBeDefined()
  })

  it('switches to copy mode, toggles options, and submits the selected flags', async () => {
    vi.mocked(api.submitMigration).mockResolvedValue(migration({ mode: 'copy' }))
    const wrapper = mountDialog()
    await flushPromises()

    const radio = wrapper.findComponent({ name: 'ElRadioGroup' })
    radio.vm.$emit('update:modelValue', 'copy')
    await flushPromises()

    const switches = wrapper.findAllComponents({ name: 'ElSwitch' })
    expect(switches).toHaveLength(3)
    expect(switches[0]?.props('modelValue')).toBe(false)

    switches[1]?.vm.$emit('update:modelValue', true)
    switches[2]?.vm.$emit('update:modelValue', false)
    await flushPromises()

    await submitButton(wrapper).trigger('click')
    await flushPromises()

    expect(api.submitMigration).toHaveBeenCalledWith(
      expect.objectContaining({
        mode: 'copy',
        live: false,
        include_snapshots: true,
        auto_start: false,
      }),
    )
    expect(wrapper.text()).toContain('source is stopped briefly')
  })

  it('enables live mode only for stateful VMs and clears it for copy mode', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const switches = wrapper.findAllComponents({ name: 'ElSwitch' })
    expect(switches[0]?.props('disabled')).toBe(false)

    switches[0]?.vm.$emit('update:modelValue', true)
    await flushPromises()
    expect(wrapper.text()).toContain('The VM keeps running')

    const radio = wrapper.findComponent({ name: 'ElRadioGroup' })
    radio.vm.$emit('update:modelValue', 'copy')
    await flushPromises()

    expect(switches[0]?.props('modelValue')).toBe(false)
  })

  it('disables live mode for containers', async () => {
    const wrapper = mountDialog(instanceSummary())
    await flushPromises()

    const switches = wrapper.findAllComponents({ name: 'ElSwitch' })
    expect(switches[0]?.props('disabled')).toBe(true)
  })

  it('reports submission failures', async () => {
    vi.mocked(api.submitMigration).mockRejectedValue(new Error('network down'))
    const wrapper = mountDialog()
    await flushPromises()

    await submitButton(wrapper).trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('network down')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('resets the form when reopened', async () => {
    const wrapper = mount(MigrateDialog, {
      props: { modelValue: false, instance: instanceSummary(), clusters },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    // Closed dialogs keep no selected target.
    await wrapper.setProps({ modelValue: true })
    await flushPromises()

    expect(wrapper.findComponent({ name: 'ElSelect' }).props('modelValue')).toBe('c2')
    vmOf(wrapper).$emit('update:modelValue', false)
    await wrapper.setProps({ modelValue: false })
    await wrapper.setProps({ modelValue: true })
    await flushPromises()

    expect(wrapper.findComponent({ name: 'ElRadioGroup' }).props('modelValue')).toBe('move')
  })
})

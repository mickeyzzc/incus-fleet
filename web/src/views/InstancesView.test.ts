import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import CloneInstanceDialog from '@/components/CloneInstanceDialog.vue'
import InstanceDetailDrawer from '@/components/InstanceDetailDrawer.vue'
import InstanceFormDialog from '@/components/InstanceFormDialog.vue'
import MigrateDialog from '@/components/MigrateDialog.vue'
import {
  clusterResources,
  clusterSummary,
  instanceDetail,
  instanceImage,
  instanceSummary,
} from '@/test/fixtures'
import InstancesView from './InstancesView.vue'

function mockFleet() {
  vi.mocked(api.listClusters).mockResolvedValue([
    clusterSummary(),
    clusterSummary({ name: 'c2', display_name: 'Cluster 63' }),
  ])
  vi.mocked(api.listInstances).mockResolvedValue([
    instanceSummary(),
    instanceSummary({ name: 'vm1', type: 'virtual-machine', live_migratable: true }),
    instanceSummary({ name: 'db', cluster: 'c2' }),
  ])
  vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
  vi.mocked(api.getResources).mockResolvedValue(clusterResources())
  vi.mocked(api.listImages).mockResolvedValue([instanceImage()])
  vi.mocked(api.instanceAction).mockResolvedValue(undefined)
  vi.mocked(api.deleteInstance).mockResolvedValue(undefined)
}

function mountInstances() {
  return mount(InstancesView, {
    global: { plugins: [ElementPlus] },
  })
}

function buttonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`${text} button not found`)
  }

  return button
}

describe('InstancesView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockFleet()
  })

  it('renders all instances across clusters', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    expect(api.listClusters).toHaveBeenCalledTimes(1)
    expect(api.listInstances).toHaveBeenCalledWith('any')
    expect(wrapper.text()).toContain('web')
    expect(wrapper.text()).toContain('vm1')
    expect(wrapper.text()).toContain('db')
    expect(wrapper.text()).toContain('Showing 3')
  })

  it('filters by search, cluster, and type', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    await wrapper.find('input').setValue('vm1')
    expect(wrapper.text()).toContain('Showing 1')

    await wrapper.find('input').setValue('')
    const selects = wrapper.findAllComponents({ name: 'ElSelect' })
    // Type filter, cluster filter, plus the pagination page-size select.
    expect(selects).toHaveLength(3)
    selects[1]?.vm.$emit('update:modelValue', 'c1')
    await flushPromises()
    expect(wrapper.text()).toContain('Showing 2')

    selects[0]?.vm.$emit('update:modelValue', 'virtual-machine')
    await flushPromises()
    expect(api.listInstances).toHaveBeenLastCalledWith('virtual-machine')
  })

  it('opens details and the migrate dialog', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    await wrapper.find('.name-link').trigger('click')
    await flushPromises()

    expect(api.getInstance).toHaveBeenCalledWith('c1', 'web')
    expect(wrapper.findComponent(InstanceDetailDrawer).props('modelValue')).toBe(true)

    await buttonByText(wrapper, 'Migrate').trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(MigrateDialog)
    expect(dialog.props('modelValue')).toBe(true)
    expect(dialog.props('instance')).toMatchObject({ name: 'web', cluster: 'c1' })
  })

  it('opens the create dialog and lifecycle actions', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    await buttonByText(wrapper, 'Create instance').trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(InstanceFormDialog)
    expect(dialog.props('modelValue')).toBe(true)
    expect(dialog.props('mode')).toBe('create')
    expect(dialog.props('cluster')).toBe('')
    expect(api.listImages).toHaveBeenCalledWith('c1')

    await buttonByText(wrapper, 'Stop').trigger('click')
    await flushPromises()
    expect(api.instanceAction).toHaveBeenCalledWith('c1', 'web', {
      action: 'stop',
      force: false,
    })

    await buttonByText(wrapper, 'Edit').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(InstanceFormDialog).props('mode')).toBe('edit')
  })

  it('opens the clone dialog from the row action', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    const cloneButton = wrapper.findAll('button').find((b) => b.text().includes('Clone'))
    await cloneButton?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(CloneInstanceDialog)
    expect(dialog.exists()).toBe(true)
    expect(dialog.props('cluster')).toBe('c1')
    expect(dialog.props('instance')).toBe('web')
    dialog.vm.$emit('cloned', 'web-clone')
    await flushPromises()
    expect(api.listInstances).toHaveBeenCalled()
  })

  it('paginates and re-fetches on size change', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    const pagination = wrapper.findComponent({ name: 'ElPagination' })
    expect(pagination.exists()).toBe(true)
    pagination.vm.$emit('update:current-page', 1)
    pagination.vm.$emit('update:page-size', 50)
    await flushPromises()

    expect(wrapper.text()).toContain('web')
  })

  it('deletes an instance after confirmation', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    await buttonByText(wrapper, 'Delete').trigger('click')
    await flushPromises()

    expect(api.deleteInstance).toHaveBeenCalledWith('c1', 'web', true)
    expect(ElMessage.success).toHaveBeenCalledWith('Deleted web.')
  })

  it('refreshes and surfaces errors', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    await buttonByText(wrapper, 'Refresh').trigger('click')
    await flushPromises()
    expect(api.listClusters).toHaveBeenCalledTimes(2)
    expect(api.listInstances).toHaveBeenCalledTimes(2)

    vi.mocked(api.listInstances).mockRejectedValue(new Error('cluster unreachable'))
    await buttonByText(wrapper, 'Refresh').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('cluster unreachable')
  })

  it('shows an empty state when filters match nothing', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    await wrapper.find('input').setValue('nothing-here')

    expect(wrapper.text()).toContain('No instances match the filters')
  })
})

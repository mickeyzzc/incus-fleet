import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
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

    // The delete dialog carries the volumes option (#13 F5); its footer
    // button performs the deletion.
    const dialog = wrapper.findComponent({ name: 'InstanceDeleteDialog' })
    expect(dialog.exists()).toBe(true)
    await dialog.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.deleteInstance).toHaveBeenCalledWith('c1', 'web', true, false)
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

  it('configures the table to keep selections across polling refreshes', () => {
    const wrapper = mountInstances()

    // row-key + reserve-selection survive the 15s refresh instead of
    // clearing the user's batch picks (#12).
    const table = wrapper.findComponent({ name: 'ElTable' })
    expect(typeof table.props('rowKey')).toBe('function')
    const selectionColumn = wrapper
      .findAllComponents({ name: 'ElTableColumn' })
      .find((column) => column.props('type') === 'selection')
    expect(selectionColumn?.props('reserveSelection')).toBe(true)

    wrapper.unmount()
  })

  it('runs batch power on selected instances after confirmation', async () => {
    vi.mocked(api.batchPower).mockResolvedValue([
      { cluster: 'c1', instance: 'web', ok: true },
      { cluster: 'c2', instance: 'db', ok: true },
    ])
    vi.mocked(ElMessageBox.confirm).mockResolvedValue('confirm' as never)
    const wrapper = mountInstances()
    await flushPromises()

    // Select two instances through the table's selection-change event.
    const table = wrapper.findComponent({ name: 'ElTable' })
    table.vm.$emit('selection-change', [
      { cluster: 'c1', name: 'web' },
      { cluster: 'c2', name: 'db' },
    ])
    await flushPromises()

    await buttonByText(wrapper, 'Start').trigger('click')
    await flushPromises()

    expect(api.batchPower).toHaveBeenCalledWith({
      targets: [
        { cluster: 'c1', instance: 'web' },
        { cluster: 'c2', instance: 'db' },
      ],
      action: 'start',
      force: false,
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Batch finished: 2 succeeded.')
  })

  it('opens the batch keys dialog for the current selection', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    const table = wrapper.findComponent({ name: 'ElTable' })
    table.vm.$emit('selection-change', [{ cluster: 'c1', name: 'web' }])
    await flushPromises()

    await buttonByText(wrapper, 'Inject keys').trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'BatchOpsDialog' })
    expect(dialog.exists()).toBe(true)
    expect(dialog.props('mode')).toBe('keys')
    expect(dialog.props('targets')).toEqual([{ cluster: 'c1', instance: 'web' }])
  })

  it('does not open batch tools without a selection', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    // The toolbar is hidden while nothing is selected.
    expect(wrapper.find('.batch-bar').exists()).toBe(false)
  })
})

describe('InstancesView dialogs', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockFleet()
    vi.mocked(api.listKeys).mockResolvedValue([])
    vi.mocked(api.listScripts).mockResolvedValue([])
  })

  it('opens the create dialog', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    await buttonByText(wrapper, 'Create instance').trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-dialog').exists()).toBe(true)
  })

  it('opens the migrate dialog from a row', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    const row = wrapper.findAll('.el-table__row')[0]!
    const migrate = row.findAll('button').find((b) => b.text().includes('Migrate'))
    await migrate!.trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-dialog').exists()).toBe(true)
  })

  it('opens the clone dialog from a row', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    const row = wrapper.findAll('.el-table__row')[0]!
    const clone = row.findAll('button').find((b) => b.text().includes('Clone'))
    await clone!.trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-dialog').exists()).toBe(true)
  })
})

describe('InstancesView helper actions', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockFleet()
    vi.mocked(api.listKeys).mockResolvedValue([])
    vi.mocked(api.listScripts).mockResolvedValue([])
  })

  it('opens the batch dialog only when rows are selected', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    const vm = wrapper.vm as unknown as {
      openBatch: (mode: 'keys' | 'script') => void
      batchOpen: boolean
    }

    // No selection: the dialog must stay closed.
    vm.openBatch('keys')
    await flushPromises()
    expect(vm.batchOpen).toBe(false)
  })

  it('opens the batch dialog for selected rows', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    const vm = wrapper.vm as unknown as {
      handleSelectionChange: (rows: Array<{ cluster: string; name: string }>) => void
      openBatch: (mode: 'keys' | 'script') => void
      batchOpen: boolean
    }

    vm.handleSelectionChange([{ cluster: 'c1', name: 'web' }])
    vm.openBatch('script')
    await flushPromises()

    expect(vm.batchOpen).toBe(true)
  })

  it('resets to the first page when filters change', async () => {
    const wrapper = mountInstances()
    await flushPromises()

    const vm = wrapper.vm as unknown as { page: number; search: string }
    vm.page = 3
    vm.search = 'web'
    await flushPromises()

    expect(vm.page).toBe(1)
  })
})

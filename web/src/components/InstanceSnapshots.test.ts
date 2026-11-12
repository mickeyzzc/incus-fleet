import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { instanceSnapshot, instanceSummary } from '@/test/fixtures'
import InstanceSnapshots from './InstanceSnapshots.vue'

function mountPanel() {
  return mount(InstanceSnapshots, {
    props: {
      cluster: 'c1',
      instance: 'web',
      summary: instanceSummary(),
    },
    global: { plugins: [ElementPlus] },
  })
}

describe('InstanceSnapshots', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listSnapshots).mockResolvedValue([instanceSnapshot()])
    vi.mocked(api.getInstance).mockResolvedValue(
      instanceSummary({ type: 'virtual-machine', migration_stateful: true }) as never,
    )
    vi.mocked(api.createSnapshot).mockResolvedValue(undefined)
    vi.mocked(api.restoreSnapshot).mockResolvedValue(undefined)
    vi.mocked(api.deleteSnapshot).mockResolvedValue(undefined)
    vi.mocked(ElMessageBox.confirm).mockResolvedValue('confirm' as never)
  })

  it('loads and renders snapshots', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(api.listSnapshots).toHaveBeenCalledWith('c1', 'web')
    expect(wrapper.text()).toContain('snap0')
  })

  it('creates a snapshot', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('New snapshot'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    await dialog.findAll('input')[0]?.setValue('backup')
    await dialog.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.createSnapshot).toHaveBeenCalledWith('c1', 'web', {
      name: 'backup',
      stateful: false,
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Snapshot backup created.')
  })

  it('rejects an empty snapshot name', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('New snapshot'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    await dialog.findAll('button').at(-1)?.trigger('click')

    expect(api.createSnapshot).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter a name.')
  })

  it('restores and deletes snapshots after confirmation', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const row = wrapper.find('.el-table__row')
    await row?.findAll('button').find((b) => b.text().includes('Restore'))?.trigger('click')
    await flushPromises()

    expect(api.restoreSnapshot).toHaveBeenCalledWith('c1', 'web', 'snap0')
    expect(ElMessage.success).toHaveBeenCalledWith('web restored to snap0.')

    await row?.findAll('button').find((b) => b.text().includes('Delete'))?.trigger('click')
    await flushPromises()

    expect(api.deleteSnapshot).toHaveBeenCalledWith('c1', 'web', 'snap0')
    expect(ElMessage.success).toHaveBeenCalledWith('Snapshot snap0 deleted.')
  })

  it('cancels deletion without calling the API', async () => {
    const { ElMessageBox } = await import('element-plus')
    vi.mocked(ElMessageBox.confirm).mockRejectedValueOnce('cancel')
    const wrapper = mountPanel()
    await flushPromises()

    const row = wrapper.findAll('.el-table__row').at(0)
    await row?.findAll('button').find((b) => b.text().includes('Delete'))?.trigger('click')
    await flushPromises()

    expect(api.deleteSnapshot).not.toHaveBeenCalled()
  })

  it('refreshes the snapshot list from the toolbar', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('Refresh'))?.trigger('click')
    await flushPromises()

    expect(api.listSnapshots).toHaveBeenCalledTimes(2)
  })

  it('reports failures', async () => {
    vi.mocked(api.restoreSnapshot).mockRejectedValue(new Error('restore failed'))
    const wrapper = mountPanel()
    await flushPromises()

    const row = wrapper.find('.el-table__row')
    await row?.findAll('button').find((b) => b.text().includes('Restore'))?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('Snapshot action failed: restore failed')
  })
})

import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { storageVolume } from '@/test/fixtures'
import StorageVolumesDialog from './StorageVolumesDialog.vue'

import type { StoragePoolResource } from '@/api/types'

const pool: StoragePoolResource = {
  name: 'default',
  driver: 'dir',
  used_by: [],
  locations: [],
  total: 1,
  used: 0,
}

function mountDialog() {
  return mount(StorageVolumesDialog, {
    props: { modelValue: true, cluster: 'c1', pool },
    global: { plugins: [ElementPlus] },
  })
}

describe('StorageVolumesDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listVolumes).mockResolvedValue([storageVolume()])
    vi.mocked(api.createVolume).mockResolvedValue(undefined)
    vi.mocked(api.deleteVolume).mockResolvedValue(undefined)
    vi.mocked(ElMessageBox.confirm).mockResolvedValue('confirm' as never)
  })

  it('loads and lists pool volumes', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    expect(api.listVolumes).toHaveBeenCalledWith('c1', 'default')
    expect(wrapper.text()).toContain('data')
  })

  it('creates a volume', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('New volume'))?.trigger('click')
    await flushPromises()

    const inputs = wrapper.findAll('.volume-form input')
    await inputs[0]?.setValue('assets')
    await inputs[1]?.setValue('5GiB')
    await inputs[2]?.setValue('shared')

    await wrapper.findAll('.volume-form button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.createVolume).toHaveBeenCalledWith('c1', 'default', {
      name: 'assets',
      description: 'shared',
      size: '5GiB',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Volume assets created.')
  })

  it('rejects an empty volume name', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('New volume'))?.trigger('click')
    await flushPromises()

    await wrapper.findAll('.volume-form button').at(-1)?.trigger('click')

    expect(api.createVolume).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter a name.')
  })

  it('deletes a volume after confirmation', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const row = wrapper.find('.el-table__row')
    await row?.findAll('button').find((b) => b.text().includes('Delete'))?.trigger('click')
    await flushPromises()

    expect(api.deleteVolume).toHaveBeenCalledWith('c1', 'default', 'data')
    expect(ElMessage.success).toHaveBeenCalledWith('Volume data deleted.')
  })

  it('reports failures', async () => {
    vi.mocked(api.deleteVolume).mockRejectedValue(new Error('volume in use'))
    const wrapper = mountDialog()
    await flushPromises()

    const row = wrapper.find('.el-table__row')
    await row?.findAll('button').find((b) => b.text().includes('Delete'))?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('Volume action failed: volume in use')
  })
})

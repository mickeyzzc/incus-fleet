import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { clusterSummary } from '@/test/fixtures'
import RenameClusterDialog from './RenameClusterDialog.vue'

function mountDialog(currentName = 'Cluster 62') {
  return mount(RenameClusterDialog, {
    props: { modelValue: true, cluster: 'c62', currentName },
    global: { plugins: [ElementPlus] },
  })
}

describe('RenameClusterDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('renames the display name and emits the new label', async () => {
    vi.mocked(api.renameCluster).mockResolvedValue(clusterSummary({ display_name: 'Production edge' }))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('input').setValue('Production edge')
    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.renameCluster).toHaveBeenCalledWith('c62', 'Production edge')
    expect(ElMessage.success).toHaveBeenCalledWith('Cluster renamed to Production edge.')
    expect(wrapper.emitted('renamed')).toEqual([['Production edge']])
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
  })

  it('keeps the identifier visible and blocks empty names', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    expect(wrapper.text()).toContain('c62')
    expect(wrapper.findAll('button').at(-1)?.attributes('disabled')).toBeDefined()

    await wrapper.find('input').setValue('   ')
    await flushPromises()
    expect(wrapper.text()).toContain('Enter a display name between 1 and 64 characters.')
    expect(wrapper.findAll('button').at(-1)?.attributes('disabled')).toBeDefined()
  })

  it('reports rename failures', async () => {
    vi.mocked(api.renameCluster).mockRejectedValue(new Error('rename refused'))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('input').setValue('New name')
    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('rename refused')
    expect(wrapper.emitted('renamed')).toBeUndefined()
  })
})

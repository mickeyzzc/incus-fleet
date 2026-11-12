import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { clusterSummary } from '@/test/fixtures'
import ImageDistributeDialog from './ImageDistributeDialog.vue'

const clusters = [
  clusterSummary(),
  clusterSummary({ name: 'c2', display_name: 'Cluster 63' }),
]

function mountDialog() {
  return mount(ImageDistributeDialog, {
    attachTo: document.body,
    props: { modelValue: true, cluster: 'c1', fingerprint: 'abc123', clusters },
    global: { plugins: [ElementPlus] },
  })
}

describe('ImageDistributeDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.copyImage).mockResolvedValue(undefined)
  })

  it('defaults to the first online target and distributes', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    await new Promise((resolve) => setTimeout(resolve, 400))
    await flushPromises()

    const select = wrapper.findComponent({ name: 'ElSelect' })
    expect(select.props('modelValue')).toBe('c2')

    const inputs = wrapper.findAll('input[type="text"]')
    await inputs[0]?.setValue('debian/13')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.copyImage).toHaveBeenCalledWith('c1', 'abc123', {
      target_cluster: 'c2',
      alias: 'debian/13',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Image distributed to c2.')
    expect(wrapper.emitted('distributed')).toHaveLength(1)
  })

  it('reports distribution failures', async () => {
    vi.mocked(api.copyImage).mockRejectedValue(new Error('transfer refused'))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('Image action failed: transfer refused')
    expect(wrapper.emitted('distributed')).toBeUndefined()
  })
  it('closes via the visible setter', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    wrapper.findComponent({ name: 'ElDialog' }).vm.$emit('update:modelValue', false)
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
  })
})

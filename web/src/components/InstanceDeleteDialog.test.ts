import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { describe, expect, it } from 'vitest'

import InstanceDeleteDialog from './InstanceDeleteDialog.vue'

function mountDialog(target: { cluster: string; name: string } | null, busy = false) {
  return mount(InstanceDeleteDialog, {
    props: { target, busy },
    global: { plugins: [ElementPlus] },
  })
}

describe('InstanceDeleteDialog', () => {
  it('confirms deletion without volumes by default', async () => {
    const wrapper = mountDialog({ cluster: 'c1', name: 'web' })
    await flushPromises()

    expect(wrapper.text()).toContain('Delete instance web?')
    expect(wrapper.text()).toContain('Also delete attached storage volumes')

    const checkbox = wrapper.find('input[type="checkbox"]')
    expect((checkbox.element as HTMLInputElement).checked).toBe(false)

    await wrapper.findAll('button').at(-1)?.trigger('click')
    expect(wrapper.emitted('confirm')).toEqual([[false]])
  })

  it('passes the volumes option through when checked', async () => {
    const wrapper = mountDialog({ cluster: 'c1', name: 'web' })
    await flushPromises()

    await wrapper.find('input[type="checkbox"]').setValue(true)
    await wrapper.findAll('button').at(-1)?.trigger('click')

    expect(wrapper.emitted('confirm')).toEqual([[true]])
  })

  it('cancels from the footer and the dialog close', async () => {
    const wrapper = mountDialog({ cluster: 'c1', name: 'web' })
    await flushPromises()

    const cancel = wrapper.findAll('button').find((b) => b.text().includes('Cancel'))
    await cancel!.trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    dialog.vm.$emit('update:modelValue', false)
    await flushPromises()
    expect(wrapper.emitted('cancel')).toHaveLength(2)
  })

  it('shows the busy state and resets the option for the next target', async () => {
    const wrapper = mountDialog({ cluster: 'c1', name: 'web' }, true)
    await flushPromises()

    expect(wrapper.find('.el-button--danger').classes()).toContain('is-loading')

    await wrapper.setProps({ target: null })
    await flushPromises()
    await wrapper.setProps({ target: { cluster: 'c1', name: 'db' } })
    await flushPromises()

    // The checkbox resets on every new target so a prior choice never leaks.
    const checkbox = wrapper.find('input[type="checkbox"]')
    expect((checkbox.element as HTMLInputElement).checked).toBe(false)
  })
})

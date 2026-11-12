import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { describe, expect, it } from 'vitest'

import KeyValueEditor from './KeyValueEditor.vue'

function typeInput(wrapper: ReturnType<typeof mount>, index: number, value: string): void {
  const inputs = wrapper.findAll('input')
  inputs[index]?.setValue(value)
}

describe('KeyValueEditor', () => {
  it('renders existing entries as editable rows', () => {
    const wrapper = mount(KeyValueEditor, {
      props: { modelValue: { size: '10GiB', driver: 'zfs' } },
      global: { plugins: [ElementPlus] },
    })

    const inputs = wrapper.findAll('input')
    expect(inputs).toHaveLength(4)
    expect(inputs[0]?.element).toBeTruthy()
    expect((inputs[0]?.element as HTMLInputElement).value).toBe('size')
    expect((inputs[1]?.element as HTMLInputElement).value).toBe('10GiB')
    expect((inputs[2]?.element as HTMLInputElement).value).toBe('driver')
    expect((inputs[3]?.element as HTMLInputElement).value).toBe('zfs')
  })

  it('emits updated pairs when values change', async () => {
    const wrapper = mount(KeyValueEditor, {
      props: { modelValue: { size: '10GiB' } },
      global: { plugins: [ElementPlus] },
    })

    typeInput(wrapper, 1, '20GiB')

    expect(wrapper.emitted('update:modelValue')).toEqual([[{ size: '20GiB' }]])
  })

  it('drops rows whose key is cleared and adds new rows on demand', async () => {
    const wrapper = mount(KeyValueEditor, {
      props: { modelValue: {} },
      global: { plugins: [ElementPlus] },
    })

    await wrapper.find('button').trigger('click')
    expect(wrapper.findAll('input')).toHaveLength(2)

    typeInput(wrapper, 0, 'zfs.pool_name')

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ 'zfs.pool_name': '' }])

    const remove = wrapper.find('.kv-row button')
    await remove.trigger('click')

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{}])
  })

  it('emits an empty map for an empty editor', async () => {
    const wrapper = mount(KeyValueEditor, {
      props: { modelValue: { only: 'entry' } },
      global: { plugins: [ElementPlus] },
    })

    await wrapper.find('.kv-row button').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[{}]])
  })
})

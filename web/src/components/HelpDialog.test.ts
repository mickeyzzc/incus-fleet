import { mount } from '@vue/test-utils'
import { flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { describe, expect, it } from 'vitest'

import { setLocale } from '@/i18n'
import HelpDialog from './HelpDialog.vue'

async function mountDialog(open = true) {
  const wrapper = mount(HelpDialog, {
    props: { modelValue: open },
    global: { plugins: [ElementPlus] },
    attachTo: document.body,
  })
  await flushPromises()

  return wrapper
}

describe('HelpDialog', () => {
  it('renders the overview document for the active locale', async () => {
    setLocale('en')
    const wrapper = await mountDialog()

    const menu = wrapper.find('.help-menu')
    expect(menu.exists()).toBe(true)
    expect(wrapper.text()).toContain('Incus Fleet is a single control plane')

    wrapper.unmount()
  })

  it('switches documents from the side menu', async () => {
    const wrapper = await mountDialog()

    await wrapper.find('.help-menu').findAll('li').find((li) => li.text() === 'Deployment')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('deploy/config.example.yaml')

    wrapper.unmount()
  })

  it('exposes a language switcher bound to the app locale', async () => {
    setLocale('en')
    const wrapper = await mountDialog()

    // The footer select is bound to the shared app locale.
    const select = wrapper.find('.help-footer')
    expect(select.exists()).toBe(true)
    expect(select.html()).toContain('el-select')

    wrapper.unmount()
  })

  it('starts on the overview page every time it opens', async () => {
    const wrapper = await mountDialog(false)
    expect(wrapper.find('.help-menu').exists()).toBe(false)

    await wrapper.setProps({ modelValue: true })
    await flushPromises()
    expect(wrapper.find('.help-menu').exists()).toBe(true)

    wrapper.unmount()
  })
})

describe('HelpDialog language binding', () => {
  it('re-renders the document when the shared locale changes', async () => {
    setLocale('en')
    const wrapper = await mountDialog()

    const select = wrapper.findComponent({ name: 'ElSelect' })
    await select.vm.$emit('update:modelValue', 'zh-CN')
    await flushPromises()

    expect(wrapper.text()).toContain('统一控制平面')

    wrapper.unmount()
  })
})

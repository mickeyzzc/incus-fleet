import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import NodeUpgradeDialog from './NodeUpgradeDialog.vue'

async function mountDialog(open = true) {
  const wrapper = mount(NodeUpgradeDialog, {
    props: {
      modelValue: open,
      memberName: 'node-a',
      currentVersion: '7.0.2',
    },
    global: { plugins: [ElementPlus] },
    attachTo: document.body,
  })

  return wrapper
}

describe('NodeUpgradeDialog', () => {
  it('shows the member, the current Incus version, and the guided steps', async () => {
    const wrapper = await mountDialog()

    expect(wrapper.text()).toContain('node-a')
    expect(wrapper.text()).toContain('7.0.2')
    expect(wrapper.text()).toContain('apt-get install -y incus')
    expect(wrapper.text()).toContain('systemctl restart incus incus.socket')

    wrapper.unmount()
  })

  it('copies a step command to the clipboard', async () => {
    const writeText = vi.fn<(text: string) => Promise<void>>(async () => {})
    Object.assign(navigator, { clipboard: { writeText } })

    const wrapper = await mountDialog()
    const copyButtons = wrapper.findAll('button').filter((b) => b.text() === 'Copy')
    expect(copyButtons.length).toBeGreaterThan(0)
    await copyButtons[0]!.trigger('click')
    await flushPromises()

    expect(writeText).toHaveBeenCalledWith('incus --version')

    wrapper.unmount()
  })

  it('emits update:modelValue when the dialog closes', async () => {
    const wrapper = await mountDialog()

    await wrapper.findComponent({ name: 'ElDialog' }).vm.$emit('update:modelValue', false)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])

    wrapper.unmount()
  })
})

describe('NodeUpgradeDialog degraded clipboard', () => {
  it('still closes cleanly when the clipboard is unavailable', async () => {
    Object.assign(navigator, { clipboard: { writeText: () => Promise.reject(new Error('denied')) } })

    const wrapper = await mountDialog()
    const copyButtons = wrapper.findAll('button').filter((b) => b.text() === 'Copy')
    await copyButtons[0]!.trigger('click')
    await flushPromises()

    // No unhandled rejection: the copy helper swallows clipboard failures.
    expect(wrapper.text()).toContain('incus --version')

    wrapper.unmount()
  })
})

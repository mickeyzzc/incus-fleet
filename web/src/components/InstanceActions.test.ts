import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { describe, expect, it } from 'vitest'

import { instanceSummary } from '@/test/fixtures'
import InstanceActions from './InstanceActions.vue'

function mountActions(status = 'Running', props: Record<string, unknown> = {}) {
  return mount(InstanceActions, {
    props: { instance: instanceSummary({ status }), ...props },
    global: { plugins: [ElementPlus] },
  })
}

function buttonByText(wrapper: ReturnType<typeof mountActions>, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`${text} button not found`)
  }

  return button
}

describe('InstanceActions', () => {
  it('shows stop and restart for running instances', () => {
    const wrapper = mountActions('Running')

    expect(wrapper.text()).toContain('Stop')
    expect(wrapper.text()).toContain('Restart')
    expect(wrapper.text()).not.toContain('Start')
  })

  it('shows start for stopped instances', () => {
    const wrapper = mountActions('Stopped')

    expect(wrapper.text()).toContain('Start')
    expect(wrapper.text()).not.toContain('Stop')
    expect(wrapper.text()).not.toContain('Restart')
  })

  it('emits lifecycle and management events', async () => {
    const wrapper = mountActions('Running')

    await buttonByText(wrapper, 'Stop').trigger('click')
    await buttonByText(wrapper, 'Restart').trigger('click')
    await buttonByText(wrapper, 'Edit').trigger('click')
    await buttonByText(wrapper, 'Migrate').trigger('click')
    await buttonByText(wrapper, 'Delete').trigger('click')

    expect(wrapper.emitted('action')).toEqual([['stop'], ['restart']])
    expect(wrapper.emitted('edit')).toHaveLength(1)
    expect(wrapper.emitted('migrate')).toHaveLength(1)
    expect(wrapper.emitted('delete')).toHaveLength(1)
  })

  it('hides optional actions and disables the rest when busy', async () => {
    const wrapper = mountActions('Running', {
      busy: true,
      showEdit: false,
      showMigrate: false,
    })

    expect(wrapper.text()).not.toContain('Edit')
    expect(wrapper.text()).not.toContain('Migrate')
    for (const button of wrapper.findAll('button')) {
      expect(button.attributes('disabled')).toBeDefined()
    }
  })
})

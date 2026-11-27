import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { describe, expect, it } from 'vitest'

import PluginStateBlock from './PluginStateBlock.vue'

function render(state: 'loading' | 'empty' | 'error' | 'forbidden', message = '') {
  return mount(PluginStateBlock, {
    props: { state, message, actionLabel: 'Try again' },
    global: { plugins: [ElementPlus] },
  })
}

describe('PluginStateBlock', () => {
  it('renders all four product states with an i18n heading', () => {
    const headings: Record<string, string> = {
      loading: 'Loading plugin data',
      empty: 'Nothing to show here yet',
      error: 'Could not load plugin data',
      forbidden: 'You do not have permission',
    }

    for (const [state, heading] of Object.entries(headings)) {
      const wrapper = render(state as 'loading', 'extra detail')
      expect(wrapper.attributes('data-state')).toBe(state)
      expect(wrapper.text()).toContain(heading)
      expect(wrapper.text()).toContain('extra detail')
    }
  })

  it('emits the single next action', async () => {
    const wrapper = render('error')
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('action')).toHaveLength(1)
  })
})

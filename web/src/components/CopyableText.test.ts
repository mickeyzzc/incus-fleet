import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import CopyableText from './CopyableText.vue'

describe('CopyableText', () => {
  const writeText = vi.fn().mockResolvedValue(undefined)

  beforeEach(() => {
    writeText.mockClear()
    vi.stubGlobal('navigator', { clipboard: { writeText } })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders the display label and copies the underlying value', async () => {
    const wrapper = mount(CopyableText, {
      props: { value: 'secret-value', display: 'Shown value' },
      global: { stubs: { ElButton: true } },
    })

    expect(wrapper.text()).toContain('Shown value')
    await wrapper.find('.copyable-button').trigger('click')
    await Promise.resolve()
    expect(writeText).toHaveBeenCalledWith('secret-value')
  })

  it('reports a failed clipboard write without throwing', async () => {
    writeText.mockRejectedValueOnce(new Error('denied'))
    const wrapper = mount(CopyableText, {
      props: { value: 'value' },
      global: { stubs: { ElButton: true } },
    })

    await wrapper.find('.copyable-button').trigger('click')
    await Promise.resolve()
    expect(writeText).toHaveBeenCalledWith('value')
  })
})

import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'

import { useVisiblePolling } from './useInstanceActions'

describe('useVisiblePolling', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  function mountPolling(loader: () => Promise<void>) {
    const Host = defineComponent({
      setup() {
        useVisiblePolling(loader)
        return () => h('div')
      },
    })

    return mount(Host, { global: { plugins: [ElementPlus] } })
  }

  it('polls on the interval while the page is visible', async () => {
    const loader = vi.fn().mockResolvedValue(undefined)
    mountPolling(loader)

    expect(loader).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(15000)
    expect(loader).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(15000)
    expect(loader).toHaveBeenCalledTimes(2)
  })

  it('skips polls while the page is hidden and stops on unmount', async () => {
    const loader = vi.fn().mockResolvedValue(undefined)
    const wrapper = mountPolling(loader)

    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    await vi.advanceTimersByTimeAsync(45000)
    expect(loader).not.toHaveBeenCalled()

    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(45000)
    expect(loader).not.toHaveBeenCalled()
  })
})

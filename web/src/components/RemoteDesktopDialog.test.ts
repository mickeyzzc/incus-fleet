import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import RemoteDesktopDialog from './RemoteDesktopDialog.vue'

// The dialog arms the console, then polls screenshots on a timer; fake timers
// keep the loop deterministic.
function mountDialog(open = true) {
  return mount(RemoteDesktopDialog, {
    props: { modelValue: open, cluster: 'c1', instance: 'vm-01' },
    global: { plugins: [ElementPlus] },
  })
}

describe('RemoteDesktopDialog', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(api.prepareDesktop).mockResolvedValue({ ready: true, restarted: false })
    vi.mocked(api.desktopScreenshot).mockResolvedValue(new Blob(['png']))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('arms the console and starts polling when opened', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()

    expect(api.prepareDesktop).toHaveBeenCalledWith('c1', 'vm-01')

    // The first frame follows the host-element wait; then the loop repeats on
    // the poll interval.
    await vi.advanceTimersByTimeAsync(4200)
    await flushPromises()
    expect(vi.mocked(api.desktopScreenshot).mock.calls.length).toBeGreaterThanOrEqual(3)
    expect(wrapper.text()).toContain('Live')
  })

  it('does nothing when initially closed', async () => {
    mountDialog(false)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)

    expect(api.prepareDesktop).not.toHaveBeenCalled()
    expect(api.desktopScreenshot).not.toHaveBeenCalled()
  })

  it('reports prepare failures and keeps polling stopped', async () => {
    vi.mocked(api.prepareDesktop).mockRejectedValue(new Error('member offline'))
    const wrapper = mountDialog(true)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)

    expect(wrapper.text()).toContain('member offline')
    expect(api.desktopScreenshot).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalled()
  })

  it('keeps polling through transient screenshot errors', async () => {
    vi.mocked(api.desktopScreenshot)
      .mockRejectedValueOnce(new Error('guest is restarting'))
      .mockResolvedValue(new Blob(['png']))
    const wrapper = mountDialog(true)
    await flushPromises()

    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()

    expect(vi.mocked(api.desktopScreenshot).mock.calls.length).toBeGreaterThanOrEqual(2)
    expect(wrapper.text()).toContain('Live')
  })

  it('sends key events with modifiers and swallows bare modifiers', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()

    const canvas = wrapper.find('canvas')
    await canvas.trigger('keydown', { key: 'Enter', ctrlKey: false })
    expect(api.sendDesktopInput).toHaveBeenCalledWith('c1', 'vm-01', {
      type: 'key',
      key: 'Enter',
      modifiers: [],
    })

    vi.mocked(api.sendDesktopInput).mockClear()
    await canvas.trigger('keydown', { key: 'F1', ctrlKey: true, altKey: true })
    expect(api.sendDesktopInput).toHaveBeenCalledWith('c1', 'vm-01', {
      type: 'key',
      key: 'F1',
      modifiers: ['Control', 'Alt'],
    })

    // A bare modifier press stays in the browser.
    vi.mocked(api.sendDesktopInput).mockClear()
    await canvas.trigger('keydown', { key: 'Control', ctrlKey: true })
    expect(api.sendDesktopInput).not.toHaveBeenCalled()
  })

  it('sends pointer down and up on click with normalised coordinates', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()

    // jsdom reports zero-size rects, so coordinates clamp to 0 — the important
    // assertion is the down/up pair with the matching button.
    await wrapper.find('canvas').trigger('mousedown', { button: 0, clientX: 10, clientY: 10 })
    await wrapper.find('canvas').trigger('mouseup', { button: 0, clientX: 10, clientY: 10 })

    expect(api.sendDesktopInput).toHaveBeenNthCalledWith(1, 'c1', 'vm-01', {
      type: 'pointer',
      x: expect.any(Number),
      y: expect.any(Number),
      button: 'left',
      down: true,
    })
    expect(api.sendDesktopInput).toHaveBeenNthCalledWith(2, 'c1', 'vm-01', {
      type: 'pointer',
      x: expect.any(Number),
      y: expect.any(Number),
      button: 'left',
      down: false,
    })
  })

  it('maps the wheel onto wheel button transitions', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()

    await wrapper.find('canvas').trigger('wheel', { deltaY: 120 })
    expect(api.sendDesktopInput).toHaveBeenCalledWith('c1', 'vm-01', {
      type: 'pointer',
      x: expect.any(Number),
      y: expect.any(Number),
      button: 'wheel-down',
      down: true,
    })
  })

  it('sends Ctrl+Alt+Del from the toolbar button', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()

    const buttons = wrapper.findAll('button').map((b) => b.text())
    expect(buttons.some((text) => text.includes('Ctrl+Alt+Del'))).toBe(true)

    await wrapper.findAll('button').find((b) => b.text().includes('Ctrl+Alt+Del'))!.trigger('click')
    expect(api.sendDesktopInput).toHaveBeenCalledWith('c1', 'vm-01', {
      type: 'key',
      key: 'Delete',
      modifiers: ['Control', 'Alt'],
    })
  })

  it('stops polling when closed', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()
    const callsAfterOpen = vi.mocked(api.desktopScreenshot).mock.calls.length

    await wrapper.setProps({ modelValue: false })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)

    expect(vi.mocked(api.desktopScreenshot).mock.calls.length).toBe(callsAfterOpen)
  })

  it('stops polling when the dialog itself emits close', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()
    const callsAfterOpen = vi.mocked(api.desktopScreenshot).mock.calls.length

    // The v-model setter runs on the dialog's own update event; the parent then
    // rebinds the prop, which stops the loop through the watch.
    wrapper.findComponent({ name: 'ElDialog' }).vm.$emit('update:modelValue', false)
    await flushPromises()
    await wrapper.setProps({ modelValue: false })
    await vi.advanceTimersByTimeAsync(3000)

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
    expect(vi.mocked(api.desktopScreenshot).mock.calls.length).toBe(callsAfterOpen)
  })

  it('stops polling on unmount', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()
    const callsAfterOpen = vi.mocked(api.desktopScreenshot).mock.calls.length

    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(3000)

    expect(vi.mocked(api.desktopScreenshot).mock.calls.length).toBe(callsAfterOpen)
  })

  it('refreshes a frame from the toolbar button', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()
    const callsBefore = vi.mocked(api.desktopScreenshot).mock.calls.length

    const refresh = wrapper.findAll('button').find((b) => b.text().includes('Refresh'))
    expect(refresh).toBeDefined()
    await refresh!.trigger('click')
    await flushPromises()

    expect(vi.mocked(api.desktopScreenshot).mock.calls.length).toBe(callsBefore + 1)
  })

  it('sends throttled drag moves while a button is held', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()
    vi.mocked(api.sendDesktopInput).mockClear()

    await wrapper.find('canvas').trigger('mousemove', { buttons: 1, clientX: 20, clientY: 30 })
    expect(api.sendDesktopInput).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(160)
    await flushPromises()
    expect(api.sendDesktopInput).toHaveBeenCalledTimes(1)
    expect(api.sendDesktopInput).toHaveBeenCalledWith('c1', 'vm-01', {
      type: 'pointer',
      x: expect.any(Number),
      y: expect.any(Number),
      move: true,
    })
  })

  it('does not send moves for plain hovering', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()

    await wrapper.find('canvas').trigger('mousemove', { buttons: 0, clientX: 20, clientY: 30 })
    await vi.advanceTimersByTimeAsync(300)

    expect(api.sendDesktopInput).not.toHaveBeenCalled()
  })

  it('keeps the browser context menu off the console canvas', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()

    const canvas = wrapper.find('canvas')
    await canvas.trigger('contextmenu')
    expect(api.sendDesktopInput).not.toHaveBeenCalled()
  })
})

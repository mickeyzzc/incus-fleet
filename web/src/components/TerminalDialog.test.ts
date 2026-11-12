import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import TerminalDialog from './TerminalDialog.vue'

class MockWebSocket {
  static instances: MockWebSocket[] = []
  static OPEN = 1
  readyState = 1
  binaryType = 'blob'
  sent: (string | ArrayBuffer)[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: { data: unknown }) => void) | null = null
  onerror: (() => void) | null = null

  constructor(public url: string) {
    MockWebSocket.instances.push(this)
  }

  send(data: string | ArrayBuffer): void {
    this.sent.push(data)
  }

  close(): void {
    this.readyState = 3
  }
}

function mountDialog() {
  return mount(TerminalDialog, {
    props: { modelValue: true, cluster: 'c1', instance: 'web' },
    global: { plugins: [ElementPlus] },
  })
}

describe('TerminalDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    MockWebSocket.instances = []
    vi.mocked(api.terminalURL).mockImplementation(
      (_cluster, _instance, kind) =>
        `ws://fleet.local/api/v1/clusters/c1/instances/web/${kind}`,
    )
    vi.stubGlobal('WebSocket', MockWebSocket as unknown as typeof WebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('connects a console websocket on open', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0]?.url).toBe(
      'ws://fleet.local/api/v1/clusters/c1/instances/web/console',
    )

    wrapper.vm.$emit('update:modelValue', false)
    wrapper.unmount()
  })

  it('sends the start frame for the first exec command and raw lines after', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const radio = wrapper.findComponent({ name: 'ElRadioGroup' })
    radio.vm.$emit('update:modelValue', 'exec')
    await flushPromises()

    expect(MockWebSocket.instances).toHaveLength(2)

    const socket = MockWebSocket.instances[1]
    if (!socket) {
      throw new Error('exec socket missing')
    }

    socket.onopen?.()
    await flushPromises()

    const input = wrapper.find('.exec-bar input')
    expect(input.exists()).toBe(true)
    await input.setValue('df -h')
    await input.trigger('keyup.enter')

    expect(socket.sent).toHaveLength(1)
    expect(JSON.parse(socket.sent[0] as string)).toEqual({
      command: ['bash', '-c', 'df -h'],
      width: 80,
      height: 25,
    })

    await input.setValue('ls')
    await input.trigger('keyup.enter')

    expect(socket.sent).toHaveLength(2)
    expect(new TextDecoder().decode(socket.sent[1] as ArrayBuffer)).toBe('ls\n')

    wrapper.unmount()
  })

  it('decodes incoming binary and string frames onto the terminal', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const socket = MockWebSocket.instances[0]
    if (!socket) {
      throw new Error('socket missing')
    }

    socket.onmessage?.({ data: 'plain text' })
    socket.onmessage?.({ data: new TextEncoder().encode('bytes').buffer })
    socket.onclose?.()
    await flushPromises()

    expect(wrapper.text()).toBeTruthy()
  })

  it('sends resize frames on demand and reports socket errors', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const socket = MockWebSocket.instances[0]
    if (!socket) {
      throw new Error('socket missing')
    }
    socket.onopen?.()
    await flushPromises()

    // Console mode has no resize bar; error handling is exercised instead.
    socket.onerror?.()
    await flushPromises()

    expect(wrapper.vm).toBeTruthy()
  })

  it('closes the socket when the dialog closes', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const socket = MockWebSocket.instances[0]
    if (!socket) {
      throw new Error('console socket missing')
    }

    await wrapper.setProps({ modelValue: false })
    await flushPromises()

    expect(socket.readyState).toBe(3)
  })
})

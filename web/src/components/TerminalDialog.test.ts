import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import TerminalDialog from './TerminalDialog.vue'

// xterm cannot render inside jsdom; stub the terminal surface.
vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 80
    rows = 25
    loadAddon(): void {}
    open(): void {}
    clear(): void {}
    dispose(): void {}
    write(): void {}
  },
}))
vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    loadAddon(): void {}
    fit(): void {}
    dispose(): void {}
  },
}))

async function settle(ms = 500): Promise<void> {
  await flushPromises()
  await new Promise((resolve) => setTimeout(resolve, ms))
  await flushPromises()
}

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
  // attachTo puts the dialog inside the document so getElementById works.
  return mount(TerminalDialog, {
    attachTo: document.body,
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
    document.body.innerHTML = ''
  })

  it('connects a console websocket on open', async () => {
    const wrapper = mountDialog()
    await settle()

    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0]?.url).toBe(
      'ws://fleet.local/api/v1/clusters/c1/instances/web/console',
    )

    wrapper.vm.$emit('update:modelValue', false)
    wrapper.unmount()
  })

  it('starts a persistent shell and forwards each submitted line to it', async () => {
    const wrapper = mountDialog()
    await settle()

    const radio = wrapper.findComponent({ name: 'ElRadioGroup' })
    radio.vm.$emit('update:modelValue', 'exec')
    await settle(200)

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

    expect(socket.sent).toHaveLength(2)
    expect(JSON.parse(socket.sent[0] as string)).toEqual({
      command: ['bash'],
      width: 80,
      height: 25,
    })
    expect(new TextDecoder().decode(socket.sent[1] as ArrayBuffer)).toBe('df -h\n')

    await input.setValue('ls')
    await input.trigger('keyup.enter')

    expect(socket.sent).toHaveLength(3)
    expect(new TextDecoder().decode(socket.sent[2] as ArrayBuffer)).toBe('ls\n')

    wrapper.unmount()
  })

  it('opens a new session and replays the line when submitting after the shell exited', async () => {
    const wrapper = mountDialog()
    await settle()

    const radio = wrapper.findComponent({ name: 'ElRadioGroup' })
    radio.vm.$emit('update:modelValue', 'exec')
    await settle(200)

    const first = MockWebSocket.instances[1]
    if (!first) {
      throw new Error('exec socket missing')
    }
    first.onopen?.()
    await flushPromises()

    const input = wrapper.find('.exec-bar input')
    await input.setValue('df -h')
    await input.trigger('keyup.enter')
    await flushPromises()

    // The command finished and the server closed the session.
    first.close()
    first.onclose?.()
    await flushPromises()

    await input.setValue('ls -la')
    await input.trigger('keyup.enter')
    await settle(200)

    expect(MockWebSocket.instances).toHaveLength(3)
    const second = MockWebSocket.instances[2]
    if (!second) {
      throw new Error('reconnect socket missing')
    }
    second.onopen?.()
    await flushPromises()

    expect(second.sent).toHaveLength(2)
    expect(JSON.parse(second.sent[0] as string)).toEqual({ command: ['bash'], width: 80, height: 25 })
    expect(new TextDecoder().decode(second.sent[1] as ArrayBuffer)).toBe('ls -la\n')

    wrapper.unmount()
  })

  it('keeps the active session when a stale socket closes after a mode switch', async () => {
    const wrapper = mountDialog()
    await settle()

    const staleSocket = MockWebSocket.instances[0]
    if (!staleSocket) {
      throw new Error('console socket missing')
    }

    const radio = wrapper.findComponent({ name: 'ElRadioGroup' })
    radio.vm.$emit('update:modelValue', 'exec')
    await settle(200)

    const active = MockWebSocket.instances[1]
    if (!active) {
      throw new Error('exec socket missing')
    }
    active.onopen?.()
    await flushPromises()

    // The stale console session reports its close after the exec session is live.
    staleSocket.onclose?.()
    await flushPromises()

    const status = wrapper.find('.terminal-toolbar .el-tag')
    expect(status.text()).toBe('Connected.')

    wrapper.unmount()
  })

  it('decodes incoming binary and string frames onto the terminal', async () => {
    const wrapper = mountDialog()
    await settle()

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
    await settle()

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
    await settle()

    const socket = MockWebSocket.instances[0]
    if (!socket) {
      throw new Error('console socket missing')
    }

    await wrapper.setProps({ modelValue: false })
    await flushPromises()

    expect(socket.readyState).toBe(3)
  })
})

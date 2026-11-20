import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import RemoteDesktopDialog from './RemoteDesktopDialog.vue'

const { SpiceMainConn, spiceStop } = vi.hoisted(() => {
  const stop = vi.fn()
  // The component instantiates with `new`, so the mock must be constructible;
  // arrow functions cannot be invoked with `new`.
  const conn = vi.fn(function SpiceMainConnMock(this: unknown) {
    return { stop }
  })
  return { SpiceMainConn: conn, spiceStop: stop }
})

vi.mock('spice-client', () => ({ SpiceMainConn }))

const waitReal = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

function mountDialog(open = true) {
  return mount(RemoteDesktopDialog, {
    props: { modelValue: open, cluster: 'c1', instance: 'vm-01' },
    global: { plugins: [ElementPlus] },
    attachTo: document.body,
  })
}

describe('RemoteDesktopDialog', () => {
  beforeEach(() => {
    SpiceMainConn.mockClear()
    spiceStop.mockClear()
    vi.mocked(api.vgaURL).mockReturnValue('ws://fleet.local/api/v1/clusters/c1/instances/vm-01/vga')
    vi.mocked(api.prepareSPICE).mockResolvedValue({
      port: 5931,
      applied: true,
      restart_required: false,
    })
    vi.mocked(api.vgaURL).mockReturnValue(
      'ws://fleet.local/api/v1/clusters/c1/instances/vm-01/vga',
    )
  })

  it('starts a SPICE session when opened', async () => {
    mountDialog(true)
    await flushPromises()
    await waitReal(250)
    await flushPromises()

    // The desktop uses Incus's own VGA channel: bridging a raw TCP port only
    // works when the daemon shares the member, and the listener is loopback-only.
    expect(api.vgaURL).toHaveBeenCalledWith('c1', 'vm-01')
    expect(SpiceMainConn).toHaveBeenCalledWith(
      expect.objectContaining({
        uri: 'ws://fleet.local/api/v1/clusters/c1/instances/vm-01/vga',
        screen_id: 'remote-desktop-host',
      }),
    )
  })

  it('stops the session when closed', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()
    await waitReal(250)
    await flushPromises()
    expect(SpiceMainConn).toHaveBeenCalled()

    await wrapper.setProps({ modelValue: false })
    await flushPromises()

    expect(spiceStop).toHaveBeenCalled()
  })

  it('does nothing when initially closed', async () => {
    mountDialog(false)
    await flushPromises()

    expect(SpiceMainConn).not.toHaveBeenCalled()
  })

  it('marks the session as connected on success', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()

    await waitReal(250)
    await flushPromises()
    const options = ((SpiceMainConn as unknown as { mock: { calls: unknown[][] } }).mock.calls[0]?.[0] ?? {}) as unknown as { onsuccess: () => void }
    options.onsuccess()
    await flushPromises()

    expect(wrapper.text()).toContain('Connected. The SPICE session is live.')
  })

  it('shows and reports connection errors', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()
    await waitReal(250)
    await flushPromises()

    const options = ((SpiceMainConn as unknown as { mock: { calls: unknown[][] } }).mock.calls[0]?.[0] ?? {}) as unknown as { onerror: (err: unknown) => void }
    options.onerror(new Error('tunnel down'))
    await flushPromises()

    expect(wrapper.text()).toContain('tunnel down')

    await wrapper.find('.desktop-error').trigger('click')
    expect(ElMessage.error).toHaveBeenCalledWith(
      'Remote desktop failed: tunnel down',
    )
  })
})

describe('RemoteDesktopDialog protocol errors', () => {
  it('explains the platform limitation instead of echoing the client error', async () => {
    // Simulate the client reporting the protocol mismatch it actually produces.
    // The mock is constructible, so replace its implementation with one that
    // immediately reports the error the real client produces.
    vi.mocked(SpiceMainConn).mockImplementation(function (this: unknown, options: { onerror?: (e: unknown) => void }) {
      options.onerror?.(new Error('Unexpected protocol mismatch.'))
      return { stop: vi.fn() }
    } as never)

    const wrapper = mountDialog(true)
    await flushPromises()
    await waitReal(250)
    await flushPromises()

    expect(wrapper.text()).toContain('QEMU build cannot stream')
    expect(wrapper.text()).not.toContain('protocol mismatch')

    wrapper.unmount()
  })
})

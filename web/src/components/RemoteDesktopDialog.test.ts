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

function mountDialog(open = true) {
  return mount(RemoteDesktopDialog, {
    props: { modelValue: open, cluster: 'c1', instance: 'vm-01' },
    global: { plugins: [ElementPlus] },
  })
}

describe('RemoteDesktopDialog', () => {
  beforeEach(() => {
    SpiceMainConn.mockClear()
    spiceStop.mockClear()
    vi.mocked(api.vgaURL).mockReturnValue('ws://fleet.local/api/v1/clusters/c1/instances/vm-01/vga')
  })

  it('starts a SPICE session when opened', async () => {
    mountDialog(true)
    await flushPromises()

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

    const options = (SpiceMainConn.mock?.calls[0]?.[0] ?? {}) as unknown as { onsuccess: () => void }
    options.onsuccess()
    await flushPromises()

    expect(wrapper.text()).toContain('Connected. The SPICE session is live.')
  })

  it('shows and reports connection errors', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()

    const options = (SpiceMainConn.mock?.calls[0]?.[0] ?? {}) as unknown as { onerror: (err: unknown) => void }
    options.onerror(new Error('tunnel down'))
    await flushPromises()

    expect(wrapper.text()).toContain('tunnel down')

    await wrapper.find('.desktop-error').trigger('click')
    expect(ElMessage.error).toHaveBeenCalledWith(
      'Remote desktop failed: tunnel down',
    )
  })
})

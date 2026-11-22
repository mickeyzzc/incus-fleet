import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import OverlayDialog from './OverlayDialog.vue'

const members = [
  {
    name: 'node-a',
    status: 'Online',
    database: true,
    roles: [],
    groups: [],
    architecture: 'x86_64',
    url: 'https://192.0.2.11:8443',
  },
  {
    name: 'node-b',
    status: 'Online',
    database: false,
    roles: [],
    groups: [],
    architecture: 'x86_64',
    url: 'https://192.0.2.12:8443',
  },
]

function mountDialog(open = true) {
  return mount(OverlayDialog, {
    props: {
      modelValue: open,
      cluster: 'c62',
      members: members as never,
    },
    global: { plugins: [ElementPlus, createPinia()] },
  })
}

describe('OverlayDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.enableOverlay).mockResolvedValue({
      cluster: 'c62',
      name: 'c62',
      bridge: 'fleetbr0',
      vni: 6200,
      subnet: '198.51.100.0/24',
      gateway_member: 'node-a',
      mtu: 1450,
      enabled: true,
      members: [],
      tunnels: [],
    })
  })

  it('prefills the name and first member as gateway', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const inputs = wrapper.findAll('input')
    expect((inputs[0]?.element as HTMLInputElement).value).toBe('c62')
  })

  it('requires a subnet before contacting the backend', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('Enable'))!.trigger('click')

    expect(api.enableOverlay).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter a subnet in CIDR form.')
  })

  it('submits the overlay definition', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const subnet = wrapper.findAll('input')[1]
    await subnet?.setValue('198.51.100.0/24')
    await wrapper.findAll('button').find((b) => b.text().includes('Enable'))!.trigger('click')
    await flushPromises()

    expect(api.enableOverlay).toHaveBeenCalledWith(
      'c62',
      expect.objectContaining({
        name: 'c62',
        subnet: '198.51.100.0/24',
        gateway_member: 'node-a',
        mtu: 1450,
        vni: 1,
      }),
    )
    expect(ElMessage.success).toHaveBeenCalled()
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })

  it('reports backend failures', async () => {
    vi.mocked(api.enableOverlay).mockRejectedValue(new Error('members unavailable'))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('input')[1]?.setValue('198.51.100.0/24')
    await wrapper.findAll('button').find((b) => b.text().includes('Enable'))!.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('members unavailable')
    expect(wrapper.emitted('saved')).toBeUndefined()
  })

  it('omits empty optional fields', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('input')[1]?.setValue('198.51.100.0/24')
    await wrapper.findAll('button').find((b) => b.text().includes('Enable'))!.trigger('click')
    await flushPromises()

    const payload = vi.mocked(api.enableOverlay).mock.calls[0]?.[1]
    expect(payload?.dhcp_start).toBeUndefined()
    expect(payload?.dhcp_end).toBeUndefined()
    expect(payload?.underlay_nic).toBeUndefined()
  })

  it('prefills an existing overlay and disables the name field', async () => {
    const wrapper = mount(OverlayDialog, {
      props: {
        modelValue: true,
        cluster: 'c62',
        members: members as never,
        overlay: {
          cluster: 'c62',
          name: 'c62',
          bridge: 'fleetbr0',
          vni: 6200,
          subnet: '198.51.100.0/24',
          gateway_member: 'node-b',
          mtu: 1400,
          dhcp_start: '198.51.100.20',
          dhcp_end: '198.51.100.100',
          enabled: true,
          members: [],
          tunnels: [],
        },
      },
      global: { plugins: [ElementPlus, createPinia()] },
    })
    await flushPromises()

    const nameInput = wrapper.findAll('input')[0]
    expect((nameInput?.element as HTMLInputElement).value).toBe('c62')
    expect((nameInput?.element as HTMLInputElement).disabled).toBe(true)
    const subnet = wrapper.findAll('input')[1]
    expect((subnet?.element as HTMLInputElement).value).toBe('198.51.100.0/24')
  })

  it('sends the optional dhcp range and underlay nic when provided', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const inputs = wrapper.findAll('input')
    await inputs[1]?.setValue('198.51.100.0/24')
    // DHCP start/end and underlay nic are the trailing text inputs.
    const text = inputs.filter((item) => item.attributes('role') === undefined)
    await text[text.length - 3]?.setValue('198.51.100.20')
    await text[text.length - 2]?.setValue('198.51.100.100')
    await text[text.length - 1]?.setValue('enp1s0')

    await wrapper.findAll('button').find((b) => b.text().includes('Enable'))!.trigger('click')
    await flushPromises()

    expect(api.enableOverlay).toHaveBeenCalledWith(
      'c62',
      expect.objectContaining({
        dhcp_start: '198.51.100.20',
        dhcp_end: '198.51.100.100',
        underlay_nic: 'enp1s0',
      }),
    )
  })

  it('closes without submitting when cancelled', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('Cancel'))!.trigger('click')

    expect(api.enableOverlay).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:modelValue')).toBeTruthy()
    expect(wrapper.emitted('update:modelValue')![0]).toEqual([false])
  })
})

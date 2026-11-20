import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import InstallAgentDialog from './InstallAgentDialog.vue'

function mountDialog(open = true) {
  return mount(InstallAgentDialog, {
    props: { modelValue: open, cluster: 'c1', instance: 'vm1' },
    global: { plugins: [ElementPlus, createPinia()] },
  })
}

describe('InstallAgentDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.installGuestAgent).mockResolvedValue({
      steps: [
        { name: 'validate', status: 'ok' },
        { name: 'resolve-address', status: 'ok', detail: 'lease 10.0.0.99' },
        { name: 'connect', status: 'ok', detail: 'via member jump' },
        { name: 'install', status: 'ok' },
        { name: 'verify', status: 'warn', detail: 'reboot the guest' },
      ],
    })
  })

  it('submits the credentials and renders the step list', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const inputs = wrapper.findAll('input')
    await inputs[1]?.setValue('root')
    await inputs[2]?.setValue('guest-secret')
    await inputs[3]?.setValue('sudo-secret')

    await wrapper.findAll('button').find((b) => b.text().includes('Install agent'))!.trigger('click')
    await flushPromises()

    expect(api.installGuestAgent).toHaveBeenCalledWith('c1', 'vm1', {
      ssh_user: 'root',
      ssh_password: 'guest-secret',
      sudo_password: 'sudo-secret',
    })

    const text = wrapper.text()
    expect(text).toContain('OK')
    expect(text).toContain('lease 10.0.0.99')
    expect(text).toContain('Run install.sh')
    expect(text).toContain('Reboot may be needed')
    expect(wrapper.emitted('done')).toHaveLength(1)
  })

  it('omits empty optional fields', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('input')[2]?.setValue('guest-secret')
    await wrapper.findAll('button').find((b) => b.text().includes('Install agent'))!.trigger('click')
    await flushPromises()

    expect(api.installGuestAgent).toHaveBeenCalledWith('c1', 'vm1', {
      ssh_user: 'root',
      ssh_password: 'guest-secret',
    })
  })

  it('requires credentials before contacting the backend', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('Install agent'))!.trigger('click')

    expect(api.installGuestAgent).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter the SSH password or a sudo password.')
  })

  it('reports failures', async () => {
    vi.mocked(api.installGuestAgent).mockRejectedValue(new Error('guest refused'))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('input')[2]?.setValue('guest-secret')
    await wrapper.findAll('button').find((b) => b.text().includes('Install agent'))!.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('Agent install failed: guest refused')
    expect(wrapper.emitted('done')).toBeUndefined()
  })

  it('resets the form and result when reopened', async () => {
    const wrapper = mountDialog(true)
    await flushPromises()

    await wrapper.findAll('input')[2]?.setValue('guest-secret')
    await wrapper.findAll('button').find((b) => b.text().includes('Install agent'))!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('OK')

    await wrapper.setProps({ modelValue: false })
    await flushPromises()
    await wrapper.setProps({ modelValue: true })
    await flushPromises()

    expect(wrapper.text()).not.toContain('OK')
    const inputs = wrapper.findAll('input')
    expect((inputs[2]?.element as HTMLInputElement).value).toBe('')
  })
})

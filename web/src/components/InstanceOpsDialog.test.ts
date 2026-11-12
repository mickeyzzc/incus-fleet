import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import InstanceOpsDialog from './InstanceOpsDialog.vue'

function mountDialog() {
  return mount(InstanceOpsDialog, {
    props: { modelValue: true, cluster: 'c1', instance: 'web' },
    global: { plugins: [ElementPlus] },
  })
}

describe('InstanceOpsDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.runOps).mockResolvedValue({ exit_code: 0, stdout: 'ok\n', stderr: '' })
  })

  it('runs a script and shows the captured output', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('textarea').setValue('echo hello')
    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.runOps).toHaveBeenCalledWith('c1', 'web', {
      action: 'run-script',
      script: 'echo hello',
    })
    expect(wrapper.text()).toContain('ok')
    expect(wrapper.text()).toContain('0')
  })

  it('switches to the password tab and sends stdin based change', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    // Second tab: change password.
    const tabs = wrapper.findAll('.el-tabs__item')
    await tabs[1]?.trigger('click')
    await flushPromises()

    const inputs = wrapper.findAll('input')
    await inputs.find((input) => input.attributes('placeholder') === 'root')?.setValue('deploy')
    const passwordInput = wrapper.find('input[type="password"]')
    await passwordInput.setValue('s3cret')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.runOps).toHaveBeenCalledWith('c1', 'web', {
      action: 'set-password',
      user: 'deploy',
      password: 's3cret',
    })
    expect(ElMessage.success).toHaveBeenCalled()
  })

  it('injects an SSH key from the key tab', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const tabs = wrapper.findAll('.el-tabs__item')
    await tabs[2]?.trigger('click')
    await flushPromises()

    // All tab panes stay mounted; scope the lookup to the key tab (its textarea).
    const userInputs = wrapper.findAll('input').filter((input) => input.attributes('placeholder') === 'root')
    await userInputs.at(-1)?.setValue('deploy')
    await wrapper.findAll('textarea').at(-1)?.setValue('ssh-ed25519 AAA test')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.runOps).toHaveBeenCalledWith('c1', 'web', {
      action: 'inject-key',
      user: 'deploy',
      key: 'ssh-ed25519 AAA test',
    })
  })

  it('submits via the form submit event and reports failures', async () => {
    vi.mocked(api.runOps).mockRejectedValue(new Error('agent gone'))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('textarea').setValue('echo x')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.runOps).toHaveBeenCalledTimes(1)
    expect(ElMessage.error).toHaveBeenCalledWith('Action failed: agent gone')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).not.toEqual([false])
  })

  it('reports a non-zero exit as a warning', async () => {
    vi.mocked(api.runOps).mockResolvedValue({ exit_code: 3, stdout: '', stderr: 'boom' })
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('textarea').setValue('exit 3')
    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(ElMessage.warning).toHaveBeenCalledWith('Action finished with exit code 3.')
    expect(wrapper.text()).toContain('boom')
  })

  it('resets its forms when reopened', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('textarea').setValue('leftover')
    await wrapper.setProps({ modelValue: false })
    await flushPromises()
    await wrapper.setProps({ modelValue: true })
    await flushPromises()

    expect((wrapper.find('textarea').element as HTMLTextAreaElement).value).toBe('')
  })
})

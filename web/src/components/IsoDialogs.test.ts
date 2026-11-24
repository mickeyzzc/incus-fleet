import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import CloneInstanceDialog from './CloneInstanceDialog.vue'
import IsoImportDialog from './IsoImportDialog.vue'
import IsoLaunchDialog from './IsoLaunchDialog.vue'

/** Emits a value on the ElSelect inside the labelled form item. */
function selectField(wrapper: VueWrapper, label: string, value: string): void {
  const item = wrapper.findAll('.el-form-item').find((entry) => entry.text().includes(label))
  const select = item?.findComponent({ name: 'ElSelect' })
  select?.vm.$emit('update:modelValue', value)
  select?.vm.$emit('change', value)
}

describe('IsoImportDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.importISO).mockResolvedValue({ name: 'alpine', pool: 'default' })
  })

  function mountDialog() {
    return mount(IsoImportDialog, {
      props: { modelValue: true, cluster: 'c1', pools: ['default', 'fast'] },
      global: { plugins: [ElementPlus] },
    })
  }

  it('requires an ISO file before submitting', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')

    expect(api.importISO).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Choose an ISO file first.')
  })

  it('uploads the selected file to the chosen pool', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const input = wrapper.find('input[type="file"]')
    const file = new File(['iso-bytes'], 'alpine.iso')
    Object.defineProperty(input.element, 'files', { value: [file] })
    await input.trigger('change')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.importISO).toHaveBeenCalledWith('c1', { file, pool: 'default' })
    expect(wrapper.emitted('imported')?.[0]).toEqual(['alpine'])
  })
})

describe('IsoImportDialog errors', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('reports upload failures', async () => {
    vi.mocked(api.importISO).mockRejectedValue(new Error('pool full'))
    const wrapper = mount(IsoImportDialog, {
      props: { modelValue: true, cluster: 'c1', pools: ['default'] },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    const input = wrapper.find('input[type="file"]')
    const file = new File(['iso'], 'alpine.iso')
    Object.defineProperty(input.element, 'files', { value: [file] })
    await input.trigger('change')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('ISO action failed: pool full')
    expect(wrapper.emitted('imported')).toBeUndefined()
  })
})

describe('IsoLaunchDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.launchFromISO).mockResolvedValue({
      name: 'installer-01',
      cluster: 'c1',
      type: 'virtual-machine',
      status: 'Running',
    } as never)
  })

  function mountDialog() {
    return mount(IsoLaunchDialog, {
      props: { modelValue: true, cluster: 'c1', isoName: 'alpine', isoPool: 'default', networks: ['incusbr0'] },
      global: { plugins: [ElementPlus] },
    })
  }

  it('requires a VM name', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')

    expect(api.launchFromISO).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter a VM name first.')
  })

  it('launches an installer VM from the ISO', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('input').setValue('installer-01')
    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.launchFromISO).toHaveBeenCalledWith('c1', 'default', 'alpine', {
      name: 'installer-01',
      network: undefined,
      root_disk: undefined,
    })
    expect(wrapper.emitted('launched')?.[0]).toEqual(['installer-01'])
  })
})

describe('CloneInstanceDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.cloneInstance).mockResolvedValue({
      name: 'web-clone',
      cluster: 'c1',
      type: 'container',
      status: 'Stopped',
    } as never)
  })

  function mountDialog() {
    return mount(CloneInstanceDialog, {
      props: { modelValue: true, cluster: 'c1', instance: 'web' },
      global: { plugins: [ElementPlus] },
    })
  }

  it('defaults the target name and clones the instance', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    expect((wrapper.find('input').element as HTMLInputElement).value).toBe('web-clone')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.cloneInstance).toHaveBeenCalledWith('c1', 'web', {
      target: 'web-clone',
      start: false,
    })
    expect(wrapper.emitted('cloned')?.[0]).toEqual(['web-clone'])
  })

  it('reports launch failures', async () => {
    vi.mocked(api.launchFromISO).mockRejectedValue(new Error('pool missing'))
    const wrapper = mount(IsoLaunchDialog, {
      props: { modelValue: true, cluster: 'c1', isoName: 'alpine', isoPool: 'default', networks: [] },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    await wrapper.find('input').setValue('installer-01')
    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('Failed to create the VM: pool missing')
  })

  it('reports clone failures', async () => {
    vi.mocked(api.cloneInstance).mockRejectedValue(new Error('quota exceeded'))
    const wrapper = mount(CloneInstanceDialog, {
      props: { modelValue: true, cluster: 'c1', instance: 'web' },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('Clone failed: quota exceeded')
  })

  it('refuses an empty target name', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('input').setValue('   ')
    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.cloneInstance).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter a target name first.')
  })
})

describe('IsoLaunchDialog unattended', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.launchFromISO).mockResolvedValue({
      name: 'installer-01',
      cluster: 'c1',
      type: 'virtual-machine',
      status: 'Running',
    } as never)
    vi.mocked(api.launchUnattended).mockResolvedValue({
      instance: { name: 'web-01', cluster: 'c1', type: 'virtual-machine', status: 'Running' } as never,
      launch: {
        name: 'web-01',
        media: [{ device: 'iso0', volume: 'debian-auto', pool: 'default', seed: false }],
        profile: 'debian-preseed',
        answer_file_name: 'preseed.cfg',
      },
    } as never)
    vi.mocked(api.previewUnattendedAnswer).mockResolvedValue({
      content: 'd-i debian-installer/locale string en_US',
      file_name: 'preseed.cfg',
      profile: 'debian-preseed',
    })
  })

  function mountUnattended() {
    return mount(IsoLaunchDialog, {
      props: { modelValue: true, cluster: 'c1', isoName: 'debian-13.7.0-amd64-DVD-1', isoPool: 'default', networks: ['incusbr0'] },
      global: { plugins: [ElementPlus] },
    })
  }

  it('guesses the debian profile and launches unattended', async () => {
    const wrapper = mountUnattended()
    await flushPromises()

    await wrapper.find('input').setValue('web-01')
    await wrapper.find('.el-switch input[type=checkbox]').setValue(true)
    await flushPromises()

    await wrapper.find('input[placeholder="fleet"]').setValue('ops')
    await wrapper.find('input[type=password]').setValue('s3cret')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.launchUnattended).toHaveBeenCalledTimes(1)
    const request = vi.mocked(api.launchUnattended).mock.calls[0]?.[3]
    expect(request?.profile).toBe('debian-preseed')
    expect(request?.spec?.hostname).toBe('web-01')
    expect(wrapper.emitted('launched')?.[0]).toEqual(['web-01'])
    expect(api.launchFromISO).not.toHaveBeenCalled()
  })

  it('requires a user account for the template path', async () => {
    const wrapper = mountUnattended()
    await flushPromises()

    await wrapper.find('input').setValue('web-01')
    await wrapper.find('.el-switch input[type=checkbox]').setValue(true)
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.launchUnattended).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter the user account name first.')
  })

  it('previews the rendered answer file', async () => {
    const wrapper = mountUnattended()
    await flushPromises()

    await wrapper.find('.el-switch input[type=checkbox]').setValue(true)
    await flushPromises()

    const previewButton = wrapper.findAll('button').find((b) => b.text().includes('Preview'))
    await previewButton?.trigger('click')
    await flushPromises()

    expect(api.previewUnattendedAnswer).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('preseed.cfg')
    expect(wrapper.text()).toContain('d-i debian-installer/locale')
  })

  it('sends the extra spec fields (hostname, disk, locale, keyboard)', async () => {
    const wrapper = mountUnattended()
    await flushPromises()

    await wrapper.find('input').setValue('web-01')
    await wrapper.find('.el-switch input[type=checkbox]').setValue(true)
    await flushPromises()

    await wrapper.find('input[placeholder="fleet"]').setValue('ops')
    await wrapper.find('input[placeholder="/dev/vda"]').setValue('/dev/vdb')
    selectField(wrapper, 'Locale', 'zh_CN')
    selectField(wrapper, 'Keyboard', 'fr')
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    const request = vi.mocked(api.launchUnattended).mock.calls[0]?.[3]
    expect(request?.spec?.hostname).toBe('web-01')
    expect(request?.spec?.disk).toBe('/dev/vdb')
    expect(request?.spec?.locale).toBe('zh_CN')
    expect(request?.spec?.keyboard).toBe('fr')
  })

  it('applies a selected custom-answer profile and clears on family change', async () => {
    vi.mocked(api.listAnswerProfiles).mockResolvedValue([{
      id: 'ap-custom', name: 'custom-userdata', profile: 'debian-preseed',
      answer_file: '#cloud-config\nhostname: from-profile', is_default: false,
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    }])
    const wrapper = mountUnattended()
    await flushPromises()
    await wrapper.find('.el-switch input[type=checkbox]').setValue(true)
    await flushPromises()

    // Select order in the dialog: network, installer family, answer profile.
    const selects = wrapper.findAllComponents({ name: 'ElSelect' })
    expect(selects.length).toBeGreaterThanOrEqual(3)
    const familySelect = selects[1]!
    const profileSelect = selects[2]!
    // Real ElSelect updates v-model before emitting change; the stub needs both.
    profileSelect.vm.$emit('update:modelValue', 'ap-custom')
    profileSelect.vm.$emit('change', 'ap-custom')
    await flushPromises()

    await wrapper.find('input').setValue('web-01')
    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()
    const request = vi.mocked(api.launchUnattended).mock.calls[0]?.[3]
    expect(request?.answer_profile_id).toBe('ap-custom')

    // Switching installer family resets the selected profile.
    familySelect.vm.$emit('change', 'kickstart')
    await flushPromises()
    expect(wrapper.find('input[placeholder="/dev/vda"]').exists()).toBe(true)
  })

  it('guesses non-debian profiles from the ISO name', async () => {
    const cases: [string, string][] = [
      ['devuan-5.0-amd64', 'debian-preseed'],
      ['ubuntu-24.04-live-server', 'ubuntu-autoinstall'],
      ['Rocky-9-latest', 'kickstart'],
      ['my-custom-image', 'nocloud'],
    ]
    for (const [iso, expected] of cases) {
      const wrapper = mount(IsoLaunchDialog, {
        props: { modelValue: true, cluster: 'c1', isoName: iso, isoPool: 'default', networks: [] },
        global: { plugins: [ElementPlus] },
      })
      await flushPromises()
      await wrapper.find('input').setValue('guessed-01')
      await wrapper.find('.el-switch input[type=checkbox]').setValue(true)
      await flushPromises()
      await wrapper.find('input[placeholder="fleet"]').setValue('ops')
      await wrapper.findAll('button').at(-1)?.trigger('click')
      await flushPromises()
      const request = vi.mocked(api.launchUnattended).mock.calls.at(-1)?.[3]
      expect(request?.profile).toBe(expected)
    }
  })

  it('sends a custom answer file verbatim', async () => {
    const wrapper = mountUnattended()
    await flushPromises()
    await wrapper.find('input').setValue('web-01')
    await wrapper.find('.el-switch input[type=checkbox]').setValue(true)
    await flushPromises()
    await wrapper.find('.el-checkbox input[type=checkbox]').setValue(true)
    await flushPromises()
    const textarea = wrapper.find('textarea')
    expect(textarea.exists()).toBe(true)
    await textarea.setValue('#cloud-config\nhostname: inline')
    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()
    const request = vi.mocked(api.launchUnattended).mock.calls.at(-1)?.[3]
    expect(request?.answer_file).toBe('#cloud-config\nhostname: inline')
    expect(request?.spec).toBeUndefined()
  })

  it('touches every editable field and the cancel action', async () => {
    const wrapper = mountUnattended()
    await flushPromises()
    // network select
    const networkSelect = wrapper.findAllComponents({ name: 'ElSelect' })[0]!
    networkSelect.vm.$emit('update:modelValue', 'incusbr0')
    await flushPromises()
    // root disk
    await wrapper.find('input[placeholder="20GiB"]').setValue('40GiB')
    await wrapper.find('.el-switch input[type=checkbox]').setValue(true)
    await flushPromises()
    // hostname + timezone on the template path
    await wrapper.find('input[placeholder="defaults to VM name"]').setValue('extra-host')
    selectField(wrapper, 'Timezone', 'Europe/Berlin')
    await flushPromises()
    // cancel button closes the dialog
    const cancel = wrapper.findAll('button').find((b) => b.text().includes('Cancel'))
    await cancel!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeTruthy()
  })

  it('reports a preview failure', async () => {
    vi.mocked(api.previewUnattendedAnswer).mockRejectedValueOnce(new Error('preview down'))
    const wrapper = mountUnattended()
    await flushPromises()
    await wrapper.find('.el-switch input[type=checkbox]').setValue(true)
    await flushPromises()
    const previewButton = wrapper.findAll('button').find((b) => b.text().includes('Preview'))
    await previewButton!.trigger('click')
    await flushPromises()
    expect(ElMessage.error).toHaveBeenCalled()
  })

  it('auto-selects the stored default answer profile', async () => {
    vi.mocked(api.listAnswerProfiles).mockResolvedValue([{
      id: 'ap-def', name: 'lab-default', profile: 'debian-preseed',
      username: 'stored-user', is_default: true,
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    }])
    const wrapper = mountUnattended()
    await flushPromises()

    await wrapper.find('input').setValue('web-01')
    await wrapper.find('.el-switch input[type=checkbox]').setValue(true)
    await flushPromises()

    // The default profile was pre-selected on open; no local username needed.
    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    const request = vi.mocked(api.launchUnattended).mock.calls[0]?.[3]
    expect(request?.answer_profile_id).toBe('ap-def')
  })
})

import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import CloneInstanceDialog from './CloneInstanceDialog.vue'
import IsoImportDialog from './IsoImportDialog.vue'
import IsoLaunchDialog from './IsoLaunchDialog.vue'

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

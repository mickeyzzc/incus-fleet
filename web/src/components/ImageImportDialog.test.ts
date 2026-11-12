import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import ImageImportDialog from './ImageImportDialog.vue'

function mountDialog() {
  return mount(ImageImportDialog, {
    props: { modelValue: true, cluster: 'c1' },
    global: { plugins: [ElementPlus] },
  })
}

function pickFile(wrapper: ReturnType<typeof mount>, name = 'debian13.tar', size = 1024): void {
  const input = wrapper.find('input[type="file"]')
  const file = new File(['tarball'], name, { type: 'application/octet-stream' })
  Object.defineProperty(file, 'size', { value: size })
  Object.defineProperty(input.element, 'files', { value: [file] })
  input.trigger('change')
}

describe('ImageImportDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.importImage).mockResolvedValue('abc123def456')
  })

  it('requires a file before submitting', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')

    expect(api.importImage).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Choose an image file first.')
  })

  it('uploads the selected file with type and alias', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    pickFile(wrapper)
    await flushPromises()

    const radios = wrapper.findComponent({ name: 'ElRadioGroup' })
    radios.vm.$emit('update:modelValue', 'virtual-machine')
    await flushPromises()

    const inputs = wrapper.findAll('input[type="text"]')
    await inputs[0]?.setValue('debian/13')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.importImage).toHaveBeenCalledWith('c1', {
      file: expect.any(File),
      type: 'virtual-machine',
      alias: 'debian/13',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Image imported.')
    expect(wrapper.emitted('imported')?.[0]?.[0]).toBe('abc123def456')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
  })

  it('passes an optional rootfs file for split images', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    pickFile(wrapper)
    await flushPromises()

    const inputs = wrapper.findAll('input[type="file"]')
    const rootfs = new File(['rootfs'], 'debian13.root', { type: 'application/octet-stream' })
    Object.defineProperty(inputs[1]?.element, 'files', { value: [rootfs] })
    await inputs[1]?.trigger('change')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.importImage).toHaveBeenCalledWith('c1', {
      file: expect.any(File),
      rootfs,
      type: 'container',
      alias: '',
    })
  })

  it('ignores empty file selections', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const input = wrapper.find('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [] })
    await input.trigger('change')
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.importImage).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Choose an image file first.')
  })

  it('opens the file picker from the upload zone', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const zone = wrapper.find('.upload-zone')
    await zone.trigger('click')
    await zone.trigger('keyup.enter')

    const input = wrapper.find('input[type="file"]')
    const file = new File(['tarball'], 'debian13.tar')
    Object.defineProperty(input.element, 'files', { value: [file] })
    await input.trigger('change')
    await flushPromises()

    expect(wrapper.text()).toContain('debian13.tar')
    expect(wrapper.text()).toContain('MB')
  })

  it('reports upload failures', async () => {
    vi.mocked(api.importImage).mockRejectedValue(new Error('upload failed'))
    const wrapper = mountDialog()
    await flushPromises()

    pickFile(wrapper, 'broken.tar')
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('Image action failed: upload failed')
    expect(wrapper.emitted('imported')).toBeUndefined()
  })
})

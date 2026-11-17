import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import ClusterRegisterDialog from './ClusterRegisterDialog.vue'
import { api } from '@/api'
import { ElMessage } from 'element-plus'

// Stub Element Plus components that don't render well in jsdom
const ElInputStub = defineComponent({
  name: 'ElInput',
  props: ['modelValue', 'placeholder', 'type'],
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () => h('input', {
      value: props.modelValue,
      placeholder: props.placeholder,
      type: props.type || 'text',
      onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value),
    })
  },
})

const ElFormStub = defineComponent({
  name: 'ElForm',
  props: ['model', 'labelPosition'],
  setup(_, { slots }) {
    return () => h('form', {}, slots.default?.())
  },
})

const ElFormItemStub = defineComponent({
  name: 'ElFormItem',
  props: ['label', 'required'],
  setup(_, { slots }) {
    return () => h('div', {}, slots.default?.())
  },
})

const ElButtonStub = defineComponent({
  name: 'ElButton',
  props: ['type', 'loading', 'icon'],
  emits: ['click'],
  setup(props, { emit, slots }) {
    return () => h('button', {
      class: props.type ? `el-button--${props.type}` : '',
      onClick: () => emit('click'),
      disabled: props.loading,
    }, slots.default?.())
  },
})

const ElDialogStub = defineComponent({
  name: 'ElDialog',
  props: ['modelValue', 'title', 'width', 'destroyOnClose'],
  emits: ['update:modelValue'],
  setup(props, { slots }) {
    return () => h('div', {
      style: { display: props.modelValue ? 'block' : 'none' },
    }, [
      h('div', { class: 'el-dialog__header' }, props.title),
      h('div', { class: 'el-dialog__body' }, slots.default?.()),
      h('div', { class: 'el-dialog__footer' }, slots.footer?.()),
    ])
  },
})

// Wait for FileReader to finish loading a picked file.
async function flushFileReaders(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 20))
}

function pickFile(input: HTMLInputElement, file: File): Promise<void> {
  Object.defineProperty(input, 'files', { value: [file], configurable: true })
  input.dispatchEvent(new Event('change'))
  return flushFileReaders()
}

function vmOf(wrapper: { vm: unknown }): any {
  return wrapper.vm
}

describe('ClusterRegisterDialog', () => {
  const mountDialog = (props = {}) => {
    return mount(ClusterRegisterDialog, {
      props: { modelValue: true, ...props },
      global: {
        stubs: {
          ElDialog: ElDialogStub,
          ElForm: ElFormStub,
          ElFormItem: ElFormItemStub,
          ElInput: ElInputStub,
          ElButton: ElButtonStub,
        },
      },
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders form fields when opened', () => {
    const wrapper = mountDialog()
    expect(wrapper.find('input[placeholder="c64"]').exists()).toBe(true)
    expect(wrapper.find('input[placeholder="Cluster 64"]').exists()).toBe(true)
    expect(wrapper.find('input[placeholder="https://192.168.64.21:8443"]').exists()).toBe(true)
    expect(wrapper.findAll('input[type="file"]').length).toBe(3)
  })

  it('shows validation errors for missing required fields', async () => {
    const wrapper = mountDialog()
    const addButton = wrapper.findAll('button').filter((b) => b.text().includes('Add'))[0]!
    await addButton.trigger('click')
    expect(ElMessage.error).toHaveBeenCalled()
    expect(api.registerCluster).not.toHaveBeenCalled()
  })

  it('registers a cluster with cert and key files', async () => {
    const wrapper = mountDialog()

    const inputs = wrapper.findAll('input[type="file"]')
    await pickFile(inputs[0]!.element as HTMLInputElement, new File(['cert-content'], 'cert.pem'))
    await pickFile(inputs[1]!.element as HTMLInputElement, new File(['key-content'], 'key.pem'))

    await wrapper.find('input[placeholder="c64"]').setValue('test-cluster')
    await wrapper.find('input[placeholder="https://192.168.64.21:8443"]').setValue('https://10.0.0.1:8443')

    const addButton = wrapper.findAll('button').filter((b) => b.text().includes('Add'))[0]!
    await addButton.trigger('click')

    expect(api.registerCluster).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'test-cluster',
        endpoint: 'https://10.0.0.1:8443',
        tls_cert: 'cert-content',
        tls_key: 'key-content',
      }),
    )
    expect(ElMessage.success).toHaveBeenCalled()
    expect(wrapper.emitted('registered')).toBeTruthy()
  })

  it('includes optional fields when provided', async () => {
    const wrapper = mountDialog()

    const inputs = wrapper.findAll('input[type="file"]')
    await pickFile(inputs[0]!.element as HTMLInputElement, new File(['cert'], 'cert.pem'))
    await pickFile(inputs[1]!.element as HTMLInputElement, new File(['key'], 'key.pem'))
    await pickFile(inputs[2]!.element as HTMLInputElement, new File(['server-cert'], 'server.pem'))

    await wrapper.find('input[placeholder="c64"]').setValue('c1')
    await wrapper.find('input[placeholder="Cluster 64"]').setValue('Cluster One')
    await wrapper.find('input[placeholder="https://192.168.64.21:8443"]').setValue('https://10.0.0.1:8443')
    await wrapper.find('input[placeholder="default"]').setValue('custom-project')

    const addButton = wrapper.findAll('button').filter((b) => b.text().includes('Add'))[0]!
    await addButton.trigger('click')

    expect(api.registerCluster).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'c1',
        display_name: 'Cluster One',
        endpoint: 'https://10.0.0.1:8443',
        project: 'custom-project',
        tls_cert: 'cert',
        tls_key: 'key',
        tls_server_cert: 'server-cert',
      }),
    )
  })

  it('shows error when registration fails', async () => {
    const wrapper = mountDialog()
    ;(api.registerCluster as any).mockRejectedValueOnce(new Error('connection refused'))

    const inputs = wrapper.findAll('input[type="file"]')
    await pickFile(inputs[0]!.element as HTMLInputElement, new File(['cert'], 'cert.pem'))
    await pickFile(inputs[1]!.element as HTMLInputElement, new File(['key'], 'key.pem'))

    await wrapper.find('input[placeholder="c64"]').setValue('fail')
    await wrapper.find('input[placeholder="https://192.168.64.21:8443"]').setValue('https://x:8443')

    const addButton = wrapper.findAll('button').filter((b) => b.text().includes('Add'))[0]!
    await addButton.trigger('click')

    expect(ElMessage.error).toHaveBeenCalled()
  })

  it('form retains values on close, clears on next open', async () => {
    const wrapper = mountDialog()
    await wrapper.find('input[placeholder="c64"]').setValue('test')

    // Close then re-open so the watch on modelValue fires.
    await wrapper.setProps({ modelValue: false })
    await wrapper.setProps({ modelValue: true })

    expect(vmOf(wrapper).form.name).toBe('')
    expect(vmOf(wrapper).form.endpoint).toBe('')
  })
})

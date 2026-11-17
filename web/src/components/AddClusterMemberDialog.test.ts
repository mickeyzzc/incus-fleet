import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { ElMessage } from 'element-plus'

import AddClusterMemberDialog from './AddClusterMemberDialog.vue'
import { api } from '@/api'

const ElInputStub = defineComponent({
  name: 'ElInput',
  props: ['modelValue', 'placeholder', 'type', 'showPassword', 'autocomplete'],
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () =>
      h('input', {
        value: props.modelValue,
        placeholder: props.placeholder,
        type: props.type || 'text',
        onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value),
      })
  },
})

const ElInputNumberStub = defineComponent({
  name: 'ElInputNumber',
  props: ['modelValue', 'min', 'max'],
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () =>
      h('input', {
        type: 'number',
        value: props.modelValue,
        onInput: (event: Event) => emit('update:modelValue', Number((event.target as HTMLInputElement).value)),
      })
  },
})

const ElSwitchStub = defineComponent({
  name: 'ElSwitch',
  props: ['modelValue'],
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () =>
      h('input', {
        type: 'checkbox',
        checked: props.modelValue,
        onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).checked),
      })
  },
})

const ElButtonStub = defineComponent({
  name: 'ElButton',
  props: ['type', 'loading', 'icon', 'disabled'],
  emits: ['click'],
  setup(props, { emit, slots }) {
    return () =>
      h(
        'button',
        {
          class: props.type ? `el-button--${props.type}` : '',
          disabled: props.loading || props.disabled,
          onClick: () => emit('click'),
        },
        slots.default?.(),
      )
  },
})

const ElDialogStub = defineComponent({
  name: 'ElDialog',
  props: ['modelValue', 'title', 'width', 'destroyOnClose'],
  setup(props, { slots }) {
    return () =>
      h('div', { class: 'el-dialog', style: { display: props.modelValue ? 'block' : 'none' } }, [
        h('div', { class: 'el-dialog__header' }, props.title),
        h('div', { class: 'el-dialog__body' }, [slots.default?.()]),
        h('div', { class: 'el-dialog__footer' }, slots.footer?.()),
      ])
  },
})

const ElAlertStub = defineComponent({
  name: 'ElAlert',
  props: ['title', 'type', 'closable', 'showIcon'],
  setup(props) {
    return () => h('div', { class: 'el-alert' }, props.title)
  },
})

const ElTagStub = defineComponent({
  name: 'ElTag',
  props: ['type', 'size', 'effect'],
  setup(_, { slots }) {
    return () => h('span', { class: 'el-tag' }, slots.default?.())
  },
})

const ElCollapseStub = defineComponent({
  name: 'ElCollapse',
  setup(_, { slots }) {
    return () => h('div', { class: 'el-collapse' }, slots.default?.())
  },
})

const ElCollapseItemStub = defineComponent({
  name: 'ElCollapseItem',
  props: ['title', 'name'],
  setup(props, { slots }) {
    return () => h('div', { class: 'el-collapse-item' }, [h('div', props.title), slots.default?.()])
  },
})

function mountDialog(modelValue = true) {
  return mount(AddClusterMemberDialog, {
    props: { modelValue, cluster: 'c2' },
    global: {
      stubs: {
        ElInput: ElInputStub,
        ElInputNumber: ElInputNumberStub,
        ElSwitch: ElSwitchStub,
        ElButton: ElButtonStub,
        ElDialog: ElDialogStub,
        ElAlert: ElAlertStub,
        ElTag: ElTagStub,
        ElCollapse: ElCollapseStub,
        ElCollapseItem: ElCollapseItemStub,
      },
    },
  })
}

function inputByPlaceholder(wrapper: ReturnType<typeof mountDialog>, placeholder: string) {
  const input = wrapper.find(`input[placeholder="${placeholder}"]`)
  if (input.exists()) {
    return input
  }

  throw new Error(`input ${placeholder} not found`)
}

function buttonByText(wrapper: ReturnType<typeof mountDialog>, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`${text} button not found`)
  }

  return button
}

describe('AddClusterMemberDialog', () => {
  beforeEach(() => {
    vi.mocked(api.joinClusterMember).mockReset()
    vi.mocked(api.listAgents).mockReset()
    vi.mocked(api.listAgents).mockResolvedValue([])
    vi.mocked(api.createAgentEnrollmentToken).mockReset()
    vi.mocked(api.bootstrapAgent).mockReset()
  })

  async function switchToSSH(wrapper: ReturnType<typeof mountDialog>) {
    await buttonByText(wrapper, 'SSH').trigger('click')
  }

  it('requires an address before submitting', async () => {
    const wrapper = mountDialog()
    await switchToSSH(wrapper)

    await buttonByText(wrapper, 'Add node').trigger('click')

    expect(api.joinClusterMember).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Address is required.')
  })

  it('sends the full provisioning request', async () => {
    vi.mocked(api.joinClusterMember).mockResolvedValue({
      member: {
        name: 'node-c',
        status: 'Online',
        database: false,
        roles: [],
      },
      steps: [],
    })

    const wrapper = mountDialog()
    await switchToSSH(wrapper)
    await inputByPlaceholder(wrapper, '198.51.100.11').setValue('198.51.100.11')
    await inputByPlaceholder(wrapper, 'Detect from the host hostname').setValue('node-c')
    await inputByPlaceholder(wrapper, 'Defaults to node_access.ssh_user from the server config.').setValue('node-admin')
    await inputByPlaceholder(wrapper, '/srv/incus').setValue('/srv/incus')

    await buttonByText(wrapper, 'Add node').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(api.joinClusterMember).toHaveBeenCalledWith('c2', {
      address: '198.51.100.11',
      name: 'node-c',
      ssh_user: 'node-admin',
      ssh_port: 22,
      install_incus: true,
      install_vms: true,
      ignore_host_key: true,
      storage_source: '/srv/incus',
    })
    expect(wrapper.emitted('added')).toHaveLength(1)
    expect(wrapper.text()).toContain('node-c is online')
  })

  it('includes passwords and advanced repository overrides when provided', async () => {
    vi.mocked(api.joinClusterMember).mockResolvedValue({
      member: { name: 'node', status: 'Online', database: false, roles: [] },
      steps: [],
    })

    const wrapper = mountDialog()
    await switchToSSH(wrapper)
    await inputByPlaceholder(wrapper, '198.51.100.11').setValue('198.51.100.11')

    const passwordInputs = wrapper.findAll('input[type="password"]')
    await passwordInputs[0]?.setValue('ssh-secret')
    await passwordInputs[1]?.setValue('sudo-secret')
    await inputByPlaceholder(wrapper, 'Default: Zabbly Incus 7.0').setValue('https://example.com/incus')
    await inputByPlaceholder(wrapper, 'Default: pkgs.zabbly.com/key.asc').setValue('https://example.com/key.asc')

    await buttonByText(wrapper, 'Add node').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(api.joinClusterMember).toHaveBeenCalledWith(
      'c2',
      expect.objectContaining({
        ssh_password: 'ssh-secret',
        sudo_password: 'sudo-secret',
        incus_repo: 'https://example.com/incus',
        incus_repo_key: 'https://example.com/key.asc',
      }),
    )
  })

  it('shows provisioning steps after a successful join', async () => {
    vi.mocked(api.joinClusterMember).mockResolvedValue({
      member: { name: 'node', status: 'Online', database: false, roles: [] },
      steps: [
        { name: 'connect', status: 'ok', detail: 'SSH connection established' },
        { name: 'install', status: 'skipped' },
        { name: 'sudoers', status: 'warning', detail: 'no sudo' },
      ],
    })

    const wrapper = mountDialog()
    await switchToSSH(wrapper)
    await inputByPlaceholder(wrapper, '198.51.100.11').setValue('198.51.100.11')
    await buttonByText(wrapper, 'Add node').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(wrapper.text()).toContain('SSH connection')
    expect(wrapper.text()).toContain('Skipped')
    expect(wrapper.text()).toContain('Warning')
    expect(wrapper.text()).toContain('no sudo')
    expect(wrapper.text()).toContain('OK')
  })

  it('reports join failures and keeps the form', async () => {
    vi.mocked(api.joinClusterMember).mockRejectedValue(new Error('ssh refused'))
    const wrapper = mountDialog()
    await switchToSSH(wrapper)
    await inputByPlaceholder(wrapper, '198.51.100.11').setValue('198.51.100.11')

    await buttonByText(wrapper, 'Add node').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(ElMessage.error).toHaveBeenCalledWith('ssh refused')
    expect(wrapper.emitted('added')).toBeUndefined()
    expect(wrapper.text()).toContain('Target host')
  })

  it('bootstraps through an existing online agent', async () => {
    vi.mocked(api.listAgents).mockResolvedValue([
      {
        id: 'agent-1',
        cluster: 'c2',
        member: 'node-c',
        hostname: 'node-c',
        status: 'online',
        version: '0.1.0',
      },
    ])
    vi.mocked(api.bootstrapAgent).mockResolvedValue({
      ok: true,
      task_id: 'task-1',
      mode: 'bootstrap',
      message: 'Incus bootstrap complete',
      steps: ['install', 'service', 'preseed', 'ready'],
    })

    const wrapper = mountDialog()
    await new Promise((resolve) => setTimeout(resolve, 0))
    await inputByPlaceholder(wrapper, 'Member name (must match agent --member)').setValue('node-c')
    await inputByPlaceholder(wrapper, '198.51.100.11').setValue('198.51.100.11')
    await new Promise((resolve) => setTimeout(resolve, 0))

    await buttonByText(wrapper, 'Bootstrap via agent').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(api.bootstrapAgent).toHaveBeenCalledWith('agent-1', {
      mode: 'join',
      address: '198.51.100.11',
      storage_source: '/srv/incus',
      install_mode: 'existing',
    })
    expect(wrapper.emitted('added')).toHaveLength(1)
    expect(wrapper.text()).toContain('node-c is online')
    expect(wrapper.text()).toContain('Install Incus')
    expect(wrapper.text()).toContain('Preseed')
  })

  it('creates an enrollment token and can fall back to SSH', async () => {
    vi.mocked(api.createAgentEnrollmentToken).mockResolvedValue({
      token: 'one-time-token',
      cluster: 'c2',
      member: 'node-c',
      expires_at: '2026-11-18T00:15:00Z',
    })

    const wrapper = mountDialog()
    await inputByPlaceholder(wrapper, 'Member name (must match agent --member)').setValue('node-c')
    await buttonByText(wrapper, 'Create enrollment token').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(api.createAgentEnrollmentToken).toHaveBeenCalledWith('c2', 'node-c')
    expect(wrapper.text()).toContain('one-time-token')
    expect(wrapper.text()).toContain('--cluster c2 --member node-c')

    await buttonByText(wrapper, 'Use SSH instead').trigger('click')
    expect(wrapper.text()).toContain('SSH access')
    wrapper.unmount()
  })

  it('resets the form when reopened and closes from the footer', async () => {
    const wrapper = mountDialog()
    await inputByPlaceholder(wrapper, '198.51.100.11').setValue('198.51.100.11')

    await wrapper.setProps({ modelValue: false })
    await wrapper.setProps({ modelValue: true })

    expect((inputByPlaceholder(wrapper, '198.51.100.11').element as HTMLInputElement).value).toBe('')

    await buttonByText(wrapper, 'Cancel').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
  })
})

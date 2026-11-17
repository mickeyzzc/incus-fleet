import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { defineComponent, h } from 'vue'
import NodeView from './NodeView.vue'
import { api } from '@/api'
import { ElMessage, ElMessageBox } from 'element-plus'

// Stub Element Plus components
const ElButtonStub = defineComponent({
  name: 'ElButton',
  props: ['type', 'loading', 'icon', 'link'],
  emits: ['click'],
  setup(props, { emit, slots }) {
    return () => h('button', {
      class: ['el-button', props.type ? `el-button--${props.type}` : '', props.link ? 'el-button--link' : ''].filter(Boolean).join(' '),
      onClick: () => emit('click'),
      disabled: props.loading,
    }, slots.default?.())
  },
})

const ElInputStub = defineComponent({
  name: 'ElInput',
  props: ['modelValue', 'placeholder', 'type', 'showPassword'],
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () => h('input', {
      value: props.modelValue,
      placeholder: props.placeholder,
      type: props.type || 'text',
      onInput: (e) => emit('update:modelValue', (e.target as HTMLInputElement).value),
    })
  },
})

const ElInputNumberStub = defineComponent({
  name: 'ElInputNumber',
  props: ['modelValue', 'min', 'max'],
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () => h('input', {
      type: 'number',
      value: props.modelValue,
      min: props.min,
      max: props.max,
      onInput: (e) => emit('update:modelValue', Number((e.target as HTMLInputElement).value)),
    })
  },
})

const radioGroupKey = Symbol('radio-group')

const ElRadioGroupStub = defineComponent({
  name: 'ElRadioGroup',
  props: ['modelValue'],
  emits: ['update:modelValue'],
  setup(_props, { emit, slots }) {
    provide(radioGroupKey, (value: unknown) => emit('update:modelValue', value))
    return () => h('div', {}, slots.default?.())
  },
})

const ElRadioButtonStub = defineComponent({
  name: 'ElRadioButton',
  props: ['value', 'label'],
  setup(props, { slots }) {
    const choose = inject<(value: unknown) => void>(radioGroupKey)
    return () => h('button', { onClick: () => choose?.(props.value) }, slots.default?.() ?? props.label)
  },
})

const ElSwitchStub = defineComponent({
  name: 'ElSwitch',
  props: ['modelValue'],
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () => h('input', {
      type: 'checkbox',
      checked: props.modelValue,
      onChange: (e) => emit('update:modelValue', (e.target as HTMLInputElement).checked),
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

const ElTagStub = defineComponent({
  name: 'ElTag',
  props: ['type', 'size'],
  setup(props, { slots }) {
    return () => h('span', { class: `el-tag el-tag--${props.type} el-tag--${props.size}` }, slots.default?.())
  },
})

const ElAlertStub = defineComponent({
  name: 'ElAlert',
  props: ['type', 'closable', 'showIcon', 'title'],
  setup(props, { slots }) {
    return () => h('div', { class: `el-alert el-alert--${props.type}` }, slots.default?.() ?? props.title)
  },
})

const ElDialogStub = defineComponent({
  name: 'ElDialog',
  props: ['modelValue', 'title', 'width', 'destroyOnClose'],
  emits: ['update:modelValue'],
  setup(props, { slots }) {
    return () => h('div', {
      class: 'el-dialog',
      style: { display: props.modelValue ? 'block' : 'none' },
    }, [
      h('div', { class: 'el-dialog__header' }, props.title),
      h('div', { class: 'el-dialog__body' }, slots.default?.()),
      h('div', { class: 'el-dialog__footer' }, slots.footer?.()),
    ])
  },
})

// Stub tables - render column slots with row data via provide/inject
import { provide, inject } from 'vue'

const rowKey = Symbol('el-table-row')

const ElTableStub = defineComponent({
  name: 'ElTable',
  props: ['data', 'loading', 'size', 'stripe'],
  setup(props, { slots }) {
    return () => {
      const data = props.data || []
      return h('div', { class: 'el-table-stub' }, [
        h('div', { class: 'el-table__header' }),
        h('div', { class: 'el-table__body' }, data.map((row: any, index: number) =>
          h('div', { class: 'el-table__row', key: index },
            // Provide the row so ElTableColumn stubs can inject it
            (() => {
              const rowProvider = defineComponent({
                setup(_, { slots: itemSlots }) {
                  provide(rowKey, row)
                  return () => itemSlots.default?.()
                },
              })
              return h(rowProvider, {}, slots)
            })(),
          ),
        )),
      ])
    }
  },
})

const ElTableColumnStub = defineComponent({
  name: 'ElTableColumn',
  props: ['label', 'prop', 'minWidth', 'width', 'fixed'],
  setup(_, { slots }) {
    return () => {
      const row = inject(rowKey)
      return slots.default?.({ row })
    }
  },
})

function vmOf(wrapper: { vm: unknown }): any {
  return wrapper.vm
}

describe('NodeView', () => {
  const mockSnapshot = {
    host: 'test-node',
    nics: [
      {
        name: 'eth0',
        state: 'up',
        kind: 'ethernet',
        mac: 'aa:bb:cc:dd:ee:ff',
        mtu: 1500,
        addresses: [
          { family: 'inet', address: '10.62.0.50', prefixlen: 24 },
          { family: 'inet6', address: 'fe80::1', prefixlen: 64 },
        ],
        wifi: null,
      },
      {
        name: 'wlan0',
        state: 'down',
        kind: 'wifi',
        mac: '11:22:33:44:55:66',
        mtu: 1500,
        addresses: [],
        wifi: { connected: false, ssid: '', signal: 0, frequency: 0 },
      },
    ],
    routes: [
      { destination: '10.62.0.0/24', gateway: '', device: 'eth0', metric: '100' },
      { destination: 'default', gateway: '10.62.0.1', device: 'eth0', metric: '100' },
    ],
    firewall: [
      { handle: '42', rule: 'tcp dport 22 accept comment "fleet_rule"' },
      { handle: '43', rule: 'tcp dport 80 accept comment "fleet_rule"' },
    ],
    wifi_scan: [
      { ssid: 'OfficeWiFi', signal: -45, frequency: 2437, secure: true },
      { ssid: 'GuestWiFi', signal: -60, frequency: 2462, secure: false },
    ],
    wifi_device: 'wlan0',
    capabilities: ['nics', 'routes', 'firewall'],
  }

  const mountView = (props = {}) => {
    return mount(NodeView, {
      props,
      global: {
        stubs: {
          ElButton: ElButtonStub,
          ElInput: ElInputStub,
          ElInputNumber: ElInputNumberStub,
          ElRadioGroup: ElRadioGroupStub,
          ElRadioButton: ElRadioButtonStub,
          ElSwitch: ElSwitchStub,
          ElForm: ElFormStub,
          ElFormItem: ElFormItemStub,
          ElTag: ElTagStub,
          ElAlert: ElAlertStub,
          ElDialog: ElDialogStub,
          ElTable: ElTableStub,
          ElTableColumn: ElTableColumnStub,
          ElMessage: true,
          ElMessageBox: true,
          Plus: { template: '<span />' },
          Refresh: { template: '<span />' },
        },
      },
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
    ;(api.nodeSnapshot as any).mockResolvedValue(mockSnapshot)
    ;(ElMessageBox.confirm as any).mockResolvedValue('confirm')
  })

  it('fetches snapshot on mount', async () => {
    mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()
    expect(api.nodeSnapshot).toHaveBeenCalledWith(undefined)
  })

  it('stateTag returns correct tag types', async () => {
    const wrapper = mountView()
    await nextTick()

    expect(vmOf(wrapper).stateTag('up')).toBe('success')
    expect(vmOf(wrapper).stateTag('down')).toBe('info')
    expect(vmOf(wrapper).stateTag('unknown')).toBe('warning')
  })

  it('toggleNIC calls api.nodeNICState', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    const nic = { name: 'eth0', state: 'up', kind: 'ethernet', addresses: [] }
    await vmOf(wrapper).toggleNIC(nic)
    expect(api.nodeNICState).toHaveBeenCalledWith('eth0', false)
    expect(ElMessage.success).toHaveBeenCalled()
  })

  it('toggleNIC handles down to up', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    const nic = { name: 'wlan0', state: 'down', kind: 'wifi', addresses: [] }
    await vmOf(wrapper).toggleNIC(nic)
    expect(api.nodeNICState).toHaveBeenCalledWith('wlan0', true)
    expect(ElMessage.success).toHaveBeenCalled()
  })

  it('openAddressDialog sets form correctly for DHCP', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    const nic = {
      name: 'eth0',
      state: 'up',
      kind: 'ethernet',
      addresses: [{ family: 'inet6', address: 'fe80::1', prefixlen: 64 }],
    }
    vmOf(wrapper).openAddressDialog(nic)
    expect(vmOf(wrapper).addressTarget).toEqual(nic)
    expect(vmOf(wrapper).addressForm.dhcp).toBe(true)
    expect(vmOf(wrapper).addressDialogOpen).toBe(true)
  })

  it('openAddressDialog sets form correctly for static IP', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    const nic = {
      name: 'eth0',
      state: 'up',
      kind: 'ethernet',
      addresses: [{ family: 'inet', address: '10.62.0.50', prefixlen: 24 }],
    }
    vmOf(wrapper).openAddressDialog(nic)
    expect(vmOf(wrapper).addressTarget).toEqual(nic)
    expect(vmOf(wrapper).addressForm.dhcp).toBe(false)
    expect(vmOf(wrapper).addressForm.address).toBe('10.62.0.50')
    expect(vmOf(wrapper).addressDialogOpen).toBe(true)
  })

  it('submitAddress calls api.nodeApplyAddress for DHCP', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    vmOf(wrapper).addressTarget = { name: 'eth0', state: 'up', kind: 'ethernet', addresses: [] }
    vmOf(wrapper).addressForm.dhcp = true
    await vmOf(wrapper).submitAddress()

    expect(api.nodeApplyAddress).toHaveBeenCalledWith('eth0', {
      dhcp: true,
      address: undefined,
      gateway: undefined,
      persist: true,
    })
    expect(ElMessage.success).toHaveBeenCalled()
    expect(vmOf(wrapper).addressDialogOpen).toBe(false)
  })

  it('submitAddress calls api.nodeApplyAddress for static IP', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    vmOf(wrapper).addressTarget = { name: 'eth0', state: 'up', kind: 'ethernet', addresses: [] }
    vmOf(wrapper).addressForm.dhcp = false
    vmOf(wrapper).addressForm.address = '10.63.0.50'
    vmOf(wrapper).addressForm.gateway = '10.63.0.1'
    await vmOf(wrapper).submitAddress()

    expect(api.nodeApplyAddress).toHaveBeenCalledWith('eth0', {
      dhcp: false,
      address: '10.63.0.50',
      gateway: '10.63.0.1',
      persist: true,
    })
  })

  it('submitRoute calls api.nodeAddRoute', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    vmOf(wrapper).routeForm.destination = '10.64.0.0/24'
    vmOf(wrapper).routeForm.gateway = '10.62.0.1'
    vmOf(wrapper).routeForm.device = 'eth0'
    await vmOf(wrapper).submitRoute()

    expect(api.nodeAddRoute).toHaveBeenCalledWith('10.64.0.0/24', '10.62.0.1', 'eth0')
    expect(ElMessage.success).toHaveBeenCalled()
    expect(vmOf(wrapper).routeDialogOpen).toBe(false)
  })

  it('confirmDeleteRoute calls api.nodeDeleteRoute', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    await vmOf(wrapper).confirmDeleteRoute('10.62.0.0/24')
    expect(api.nodeDeleteRoute).toHaveBeenCalledWith('10.62.0.0/24')
    expect(ElMessage.success).toHaveBeenCalled()
  })

  it('submitFirewall calls api.nodeAddFirewallRule', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    vmOf(wrapper).firewallForm.port = 8080
    vmOf(wrapper).firewallForm.proto = 'tcp'
    vmOf(wrapper).firewallForm.action = 'accept'
    vmOf(wrapper).firewallForm.comment = 'test rule'
    await vmOf(wrapper).submitFirewall()

    expect(api.nodeAddFirewallRule).toHaveBeenCalledWith({
      port: 8080,
      proto: 'tcp',
      action: 'accept',
      comment: 'test rule',
    })
    expect(ElMessage.success).toHaveBeenCalled()
    expect(vmOf(wrapper).firewallDialogOpen).toBe(false)
  })

  it('confirmDeleteFirewall calls api.nodeDeleteFirewallRule', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    await vmOf(wrapper).confirmDeleteFirewall('42')
    expect(api.nodeDeleteFirewallRule).toHaveBeenCalledWith('42')
    expect(ElMessage.success).toHaveBeenCalled()
  })

  it('connectWiFi calls api.nodeWifiConnect', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    vmOf(wrapper).wifiForm.ssid = 'TestWiFi'
    vmOf(wrapper).wifiForm.password = 'password123'
    await vmOf(wrapper).connectWiFi('TestWiFi')

    expect(api.nodeWifiConnect).toHaveBeenCalledWith('wlan0', 'TestWiFi', 'password123')
    expect(ElMessage.success).toHaveBeenCalled()
    expect(vmOf(wrapper).wifiDialogOpen).toBe(false)
    expect(vmOf(wrapper).wifiForm.password).toBe('')
  })

  it('connectSavedNetwork confirms then connects', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    ;(ElMessageBox.confirm as any).mockResolvedValueOnce('confirm')
    ;(api.nodeWifiConnect as any).mockResolvedValueOnce(undefined)

    await vmOf(wrapper).connectSavedNetwork('SavedWiFi')
    expect(api.nodeWifiConnect).toHaveBeenCalledWith('wlan0', 'SavedWiFi', '')
  })

  it('refresh calls api.nodeSnapshot', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    ;(api.nodeSnapshot as any).mockClear()
    await vmOf(wrapper).refresh()
    expect(api.nodeSnapshot).toHaveBeenCalledWith('wlan0')
  })

  it('shows error when snapshot fetch fails', async () => {
    ;(api.nodeSnapshot as any).mockRejectedValueOnce(new Error('network error'))
    mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    expect(ElMessage.error).toHaveBeenCalled()
  })

  it('shows error when toggleNIC fails', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    ;(api.nodeNICState as any).mockRejectedValueOnce(new Error('permission denied'))
    const nic = { name: 'eth0', state: 'up', kind: 'ethernet', addresses: [] }
    await vmOf(wrapper).toggleNIC(nic)
    expect(ElMessage.error).toHaveBeenCalled()
  })

  it('wifiDevice computed returns wifi_device from snapshot', async () => {
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    expect(vmOf(wrapper).wifiDevice).toBe('wlan0')
  })

  it('wifiDevice computed returns empty string when no snapshot', async () => {
    ;(api.nodeSnapshot as any).mockResolvedValueOnce(null)
    const wrapper = mountView()
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()

    expect(vmOf(wrapper).wifiDevice).toBe('')
  })
})
describe('NodeView template interactions', () => {
  const mockSnapshot = {
    host: 'test-node',
    nics: [
      {
        name: 'eth0',
        state: 'up',
        kind: 'ethernet',
        addresses: [{ family: 'inet', address: '10.62.0.50', prefixlen: 24 }],
      },
      { name: 'wlan0', state: 'down', kind: 'wifi', addresses: [] },
    ],
    routes: [
      { destination: '10.62.0.0/24', gateway: '', device: 'eth0', metric: '100' },
      { destination: 'default', gateway: '10.62.0.1', device: 'eth0', metric: '100' },
    ],
    firewall: [{ handle: '42', rule: 'tcp dport 22 accept comment "fleet_rule"' }],
    wifi_scan: [
      { ssid: 'OfficeWiFi', signal: -45, frequency: 2437, secure: true },
      { ssid: 'GuestWiFi', signal: -60, frequency: 2462, secure: false },
    ],
    wifi_device: 'wlan0',
  }

  function build() {
    return mount(NodeView, {
      global: {
        stubs: {
          ElButton: ElButtonStub,
          ElInput: ElInputStub,
          ElInputNumber: ElInputNumberStub,
          ElRadioGroup: ElRadioGroupStub,
          ElRadioButton: ElRadioButtonStub,
          ElSwitch: ElSwitchStub,
          ElForm: ElFormStub,
          ElFormItem: ElFormItemStub,
          ElTag: ElTagStub,
          ElAlert: ElAlertStub,
          ElDialog: ElDialogStub,
          ElTable: ElTableStub,
          ElTableColumn: ElTableColumnStub,
          ElMessage: true,
          ElMessageBox: true,
          Plus: { template: '<span />' },
          Refresh: { template: '<span />' },
        },
      },
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
    ;(api.nodeSnapshot as any).mockResolvedValue(mockSnapshot)
    ;(ElMessageBox.confirm as any).mockResolvedValue('confirm')
  })

  async function flush() {
    await nextTick()
    await vi.dynamicImportSettled?.()
    await nextTick()
  }

  it('clicks per-row NIC actions', async () => {
    const wrapper = build()
    await flush()

    const rows = wrapper.findAll('.el-table__row')
    const firstRow = rows[0]!

    // toggle first (fresh DOM), then open the address dialog
    await firstRow.findAll('button')[1]!.trigger('click')
    await new Promise((r) => setTimeout(r, 10))
    expect(api.nodeNICState).toHaveBeenCalledWith('eth0', false)

    // re-query after the refresh re-rendered the table
    const refreshedRow = wrapper.findAll('.el-table__row')[0]!
    await refreshedRow.findAll('button')[0]!.trigger('click')
    expect(vmOf(wrapper).addressDialogOpen).toBe(true)
  })

  it('clicks route add, per-row delete and dialog actions', async () => {
    const wrapper = build()
    await flush()

    const addRoute = wrapper.findAll('button').find((b) => b.text().includes('Add route'))!
    await addRoute!.trigger('click')
    expect(vmOf(wrapper).routeDialogOpen).toBe(true)

    const routeDialog = wrapper.findAll('.el-dialog').find((d) => d.text().includes('Add route'))
    await routeDialog!.find('input[placeholder="10.63.0.0/24"]').setValue('10.64.0.0/24')
    await routeDialog!.find('input[placeholder="10.62.0.1"]').setValue('10.62.0.1')
    await routeDialog!.find('input[placeholder="enp1s0"]').setValue('eth0')

    const inputs = wrapper.findAll('input')
    const create = routeDialog!.findAll('button').filter((b) => b.text().includes('Create'))
    await create[0]!.trigger('click')
    await new Promise((r) => setTimeout(r, 10))
    expect(api.nodeAddRoute).toHaveBeenCalledWith('10.64.0.0/24', '10.62.0.1', 'eth0')

    // dialog Cancel closes the route dialog
    const routeFooter = wrapper.findAll('.el-dialog__footer').find((f) => f.text().includes('Create'))
    const cancel = routeFooter!.findAll('button').filter((b) => b.text().includes('Cancel'))
    await cancel[0]!.trigger('click')
    expect(vmOf(wrapper).routeDialogOpen).toBe(false)
    expect(inputs.length).toBeGreaterThan(0)
  })

  it('clicks per-row route delete', async () => {
    const wrapper = build()
    await flush()

    // The routes table is the second rendered table; find the delete button inside a row
    const rows = wrapper.findAll('.el-table__row')
    const deleteButton = rows
      .filter((r) => r.text().includes('10.62.0.0/24'))
      .map((r) => r.findAll('button'))
      .flat()
      .find((b) => b.text().includes('Delete'))!
    await deleteButton!.trigger('click')
    expect(api.nodeDeleteRoute).toHaveBeenCalledWith('10.62.0.0/24')
  })

  it('clicks firewall add and per-row delete', async () => {
    const wrapper = build()
    await flush()

    const addRule = wrapper.findAll('button').find((b) => b.text().includes('Add rule'))!
    await addRule!.trigger('click')
    expect(vmOf(wrapper).firewallDialogOpen).toBe(true)

    // change port via input-number stub
    const port = wrapper.find('input[type="number"]')
    await port.setValue('8080')

    const firewallDialog = wrapper.findAll('.el-dialog').find((d) => d.text().includes('Add rule'))
    const create = firewallDialog!.findAll('button').filter((b) => b.text().includes('Create'))
    await create[0]!.trigger('click')
    await new Promise((r) => setTimeout(r, 10))
    expect(api.nodeAddFirewallRule).toHaveBeenCalledWith(
      expect.objectContaining({ port: 8080 }),
    )

    // per-row delete of the firewall rule
    const rows = wrapper.findAll('.el-table__row')
    const deleteButton = rows
      .filter((r) => r.text().includes('tcp dport 22'))
      .map((r) => r.findAll('button'))
      .flat()
      .find((b) => b.text().includes('Delete'))!
    await deleteButton!.trigger('click')
    expect(api.nodeDeleteFirewallRule).toHaveBeenCalledWith('42')
  })

  it('clicks wifi connect for secure and open networks', async () => {
    const wrapper = build()
    await flush()

    const rows = wrapper.findAll('.el-table__row')
    const secureRow = rows.find((r) => r.text().includes('OfficeWiFi'))!
    const connect = secureRow!.findAll('button').find((b) => b.text().includes('Connect'))!
    await connect!.trigger('click')
    expect(vmOf(wrapper).wifiDialogOpen).toBe(true)
    expect(vmOf(wrapper).wifiForm.ssid).toBe('OfficeWiFi')

    // cancel wifi dialog
    const cancel = wrapper.findAll('button').filter((b) => b.text().includes('Cancel'))
    await cancel[cancel.length - 1]!.trigger('click')
    expect(vmOf(wrapper).wifiDialogOpen).toBe(false)

    // open network goes through confirmation
    const openRow = rows.find((r) => r.text().includes('GuestWiFi'))!
    const openConnect = openRow!.findAll('button').find((b) => b.text().includes('Connect'))!
    await openConnect!.trigger('click')
    expect(ElMessageBox.confirm).toHaveBeenCalled()
    expect(api.nodeWifiConnect).toHaveBeenCalledWith('wlan0', 'GuestWiFi', '')
  })

  it('toggles address radio and persist switch', async () => {
    const wrapper = build()
    await flush()

    await wrapper.findAll('button').find((b) => b.text() === 'DHCP')!.trigger('click')
    expect(vmOf(wrapper).addressForm.dhcp).toBe(true)
    await wrapper.findAll('button').find((b) => b.text() === 'Static')!.trigger('click')
    expect(vmOf(wrapper).addressForm.dhcp).toBe(false)

    const checkbox = wrapper.find('input[type="checkbox"]')
    await checkbox.setValue(false)
    expect(vmOf(wrapper).addressForm.persist).toBe(false)
  })

  it('clicks NIC dialog save and cancel', async () => {
    const wrapper = build()
    await flush()

    const row = wrapper.findAll('.el-table__row')[0]!
    await row.findAll('button')[0]!.trigger('click')

    await wrapper.find('input[placeholder="10.62.0.50"]').setValue('10.63.0.77')
    await wrapper.find('input[placeholder="10.62.0.1"]').setValue('10.63.0.1')

    const save = wrapper.findAll('button').find((b) => b.text().includes('Save'))!
    await save.trigger('click')
    expect(api.nodeApplyAddress).toHaveBeenCalledWith('eth0', expect.objectContaining({ address: '10.63.0.77' }))

    // re-open and cancel
    await row.findAll('button')[0]!.trigger('click')
    const cancel = wrapper.findAll('button').filter((b) => b.text().includes('Cancel'))
    await cancel[0]!.trigger('click')
    expect(vmOf(wrapper).addressDialogOpen).toBe(false)
  })

  it('handles errors from route and firewall submissions', async () => {
    const wrapper = build()
    await flush()

    ;(api.nodeAddRoute as any).mockRejectedValueOnce(new Error('bad route'))
    vmOf(wrapper).routeForm.destination = 'x'
    await vmOf(wrapper).submitRoute()
    expect(ElMessage.error).toHaveBeenCalled()

    ;(api.nodeAddFirewallRule as any).mockRejectedValueOnce(new Error('bad rule'))
    await vmOf(wrapper).submitFirewall()
    expect(ElMessage.error).toHaveBeenCalled()

    ;(api.nodeApplyAddress as any).mockRejectedValueOnce(new Error('bad address'))
    vmOf(wrapper).addressTarget = { name: 'eth0', state: 'up', kind: 'ethernet', addresses: [] }
    await vmOf(wrapper).submitAddress()
    expect(ElMessage.error).toHaveBeenCalled()

    ;(api.nodeWifiConnect as any).mockRejectedValueOnce(new Error('bad wifi'))
    await vmOf(wrapper).connectWiFi('Net')
    expect(ElMessage.error).toHaveBeenCalled()
  })

  it('handles cancelled confirmations gracefully', async () => {
    const wrapper = build()
    await flush()

    ;(ElMessageBox.confirm as any).mockRejectedValueOnce('cancel')
    await vmOf(wrapper).toggleNIC({ name: 'eth0', state: 'up', kind: 'ethernet', addresses: [] })
    expect(api.nodeNICState).not.toHaveBeenCalled()

    ;(ElMessageBox.confirm as any).mockRejectedValueOnce('cancel')
    await vmOf(wrapper).confirmDeleteRoute('10.62.0.0/24')
    expect(api.nodeDeleteRoute).not.toHaveBeenCalled()

    ;(ElMessageBox.confirm as any).mockRejectedValueOnce('cancel')
    await vmOf(wrapper).confirmDeleteFirewall('42')
    expect(api.nodeDeleteFirewallRule).not.toHaveBeenCalled()

    ;(ElMessageBox.confirm as any).mockRejectedValueOnce('cancel')
    await vmOf(wrapper).connectSavedNetwork('OpenNet')
    expect(api.nodeWifiConnect).not.toHaveBeenCalled()
  })

  it('submitAddress returns early without target', async () => {
    const wrapper = build()
    await flush()

    vmOf(wrapper).addressTarget = null
    await vmOf(wrapper).submitAddress()
    expect(api.nodeApplyAddress).not.toHaveBeenCalled()
  })
})

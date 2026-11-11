/* eslint-disable vue/one-component-per-file -- test-local Element Plus stubs */
import { afterEach, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { config } from '@vue/test-utils'

// Element Plus components rely on ResizeObserver, which jsdom does not provide.
class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

globalThis.ResizeObserver = globalThis.ResizeObserver ?? ResizeObserverStub

// ElSelect schedules popper sizing from DOM measurements that are always zero
// in jsdom, which can loop. A light stub keeps selection props observable.
const ElSelectStub = defineComponent({
  name: 'ElSelect',
  props: {
    modelValue: { type: [String, Number, Array, Boolean], default: undefined },
    placeholder: { type: String, default: '' },
  },
  emits: ['update:modelValue'],
  setup(_props, { slots }) {
    return () => h('div', { class: 'el-select-stub' }, slots.default?.())
  },
})

const ElOptionStub = defineComponent({
  name: 'ElOption',
  props: {
    label: { type: String, default: '' },
    value: { type: [String, Number], default: '' },
  },
  setup(props) {
    return () => h('div', { class: 'el-option-stub', 'data-value': props.value }, props.label)
  },
})

config.global.stubs = {
  transition: false,
  'transition-group': false,
  teleport: true,
  ElSelect: ElSelectStub,
  ElOption: ElOptionStub,
}

// All stores and components share the `api` singleton from `@/api`. Tests
// replace it with spies while keeping the real ApiError class.
vi.mock('@/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api')>()

  return {
    ...actual,
    api: {
      onUnauthorized: undefined,
      setBearerToken: vi.fn(),
      login: vi.fn(),
      logout: vi.fn(),
      session: vi.fn(),
      health: vi.fn(),
      version: vi.fn(),
      listClusters: vi.fn(),
      getCluster: vi.fn(),
      reconnectCluster: vi.fn(),
      listClusterInstances: vi.fn(),
      listInstances: vi.fn(),
      getInstance: vi.fn(),
      listMigrations: vi.fn(),
      getMigration: vi.fn(),
      submitMigration: vi.fn(),
    },
  }
})

// Keep notifications observable without rendering message popups.
vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal<typeof import('element-plus')>()

  return {
    ...actual,
    ElMessage: {
      success: vi.fn(),
      error: vi.fn(),
      info: vi.fn(),
      warning: vi.fn(),
    },
  }
})

afterEach(() => {
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

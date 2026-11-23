/* eslint-disable vue/one-component-per-file -- test-local Element Plus stubs */
import { afterEach, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { config } from '@vue/test-utils'

import i18n, { setLocale } from '@/i18n'

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
  emits: ['update:modelValue', 'change'],
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

config.global.plugins = [i18n]

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
      changePassword: vi.fn(),
      listUsers: vi.fn(),
      listSessions: vi.fn().mockResolvedValue([]),
      revokeUserSessions: vi.fn().mockResolvedValue(0),
      createUser: vi.fn(),
      updateUser: vi.fn(),
      deleteUser: vi.fn(),
      health: vi.fn(),
      version: vi.fn(),
      listClusters: vi.fn(),
      getCluster: vi.fn(),
      reconnectCluster: vi.fn(),
      listClusterInstances: vi.fn(),
      listInstances: vi.fn(),
      listInstancesWithErrors: vi.fn().mockResolvedValue({ items: [], failedClusters: [] }),
      lastInventoryFailures: [] as string[],
      getInstance: vi.fn(),
      listAllImages: vi.fn(),
      listAllISOs: vi.fn(),
      listAllImagesWithErrors: vi.fn().mockResolvedValue({ items: [], failedClusters: [] }),
      listAllISOsWithErrors: vi.fn().mockResolvedValue({ items: [], failedClusters: [] }),
      importISOWithProgress: vi.fn(),
      listImages: vi.fn(),
      deleteImage: vi.fn(),
      listSnapshots: vi.fn(),
      createSnapshot: vi.fn(),
      restoreSnapshot: vi.fn(),
      deleteSnapshot: vi.fn(),
      listVolumes: vi.fn(),
      listAllVolumes: vi.fn(),
      listAllVolumesWithErrors: vi.fn().mockResolvedValue({ items: [], failedClusters: [] }),
      createVolume: vi.fn(),
      deleteVolume: vi.fn(),
      attachDevice: vi.fn(),
      detachDevice: vi.fn(),
      updateDevice: vi.fn(),
      installGuestAgent: vi.fn(),
      listInstanceFiles: vi.fn(),
      importImage: vi.fn(),
      copyImage: vi.fn(),
      refreshImage: vi.fn(),
      listISOs: vi.fn(),
      importISO: vi.fn(),
      deleteISO: vi.fn(),
      launchFromISO: vi.fn(),
      previewUnattendedAnswer: vi.fn(),
      launchUnattended: vi.fn(),
      runOps: vi.fn(),
      rebuildInstance: vi.fn(),
      cloneInstance: vi.fn(),
      vgaURL: vi.fn(() => 'ws://fleet.local/api/v1/clusters/c1/instances/web/vga'),
      prepareSPICE: vi.fn(),
      prepareDesktop: vi.fn(),
      desktopScreenshot: vi.fn(),
      sendDesktopInput: vi.fn(),
      instanceMetrics: vi.fn(),
      listKeys: vi.fn(),
      createKey: vi.fn(),
      updateKey: vi.fn(),
      deleteKey: vi.fn(),
      listScripts: vi.fn(),
      createScript: vi.fn(),
      updateScript: vi.fn(),
      deleteScript: vi.fn(),
      listAnswerProfiles: vi.fn(),
      createAnswerProfile: vi.fn(),
      updateAnswerProfile: vi.fn(),
      deleteAnswerProfile: vi.fn(),
      batchKeys: vi.fn(),
      batchScripts: vi.fn(),
      batchPower: vi.fn(),
      spiceURL: vi.fn(() => 'ws://fleet.local/api/v1/clusters/c1/instances/web/spice?port=5931'),
      uploadInstanceFile: vi.fn(),
      mkdirInstanceDir: vi.fn(),
      deleteInstanceFile: vi.fn(),
      downloadFileURL: vi.fn(),
      terminalURL: vi.fn(),
      getResources: vi.fn(),
      createInstance: vi.fn(),
      updateInstance: vi.fn(),
      instanceAction: vi.fn(),
      deleteInstance: vi.fn(),
      createStoragePool: vi.fn(),
      updateStoragePool: vi.fn(),
      deleteStoragePool: vi.fn(),
      createNetwork: vi.fn(),
      updateNetwork: vi.fn(),
      deleteNetwork: vi.fn(),
      listOverlays: vi.fn().mockResolvedValue([]),
      getOverlay: vi.fn(),
      enableOverlay: vi.fn(),
      reconcileOverlay: vi.fn(),
      disableOverlay: vi.fn(),
      listProfiles: vi.fn(),
      createProfile: vi.fn(),
      updateProfile: vi.fn(),
      deleteProfile: vi.fn(),
      updateMember: vi.fn(),
      listMigrations: vi.fn(),
      getMigration: vi.fn(),
      submitMigration: vi.fn(),
      cancelMigration: vi.fn(),
      retryMigration: vi.fn(),
      nodeSnapshot: vi.fn(),
      nodeTasks: vi.fn(),
      nodeTask: vi.fn(),
      nodeAudit: vi.fn(),
      nodeNICState: vi.fn(),
      nodeApplyAddress: vi.fn(),
      nodeWifiConnect: vi.fn(),
      nodeAddRoute: vi.fn(),
      nodeDeleteRoute: vi.fn(),
      nodeAddFirewallRule: vi.fn(),
      nodeDeleteFirewallRule: vi.fn(),
      hostMetrics: vi.fn(),
      metricQueryRange: vi.fn(),
      clusterTraffic: vi.fn(),
      registerCluster: vi.fn(),
      listAgents: vi.fn(),
      createAgentEnrollmentToken: vi.fn(),
      bootstrapAgent: vi.fn(),
      joinClusterMember: vi.fn(),
      unregisterCluster: vi.fn(),
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
    ElMessageBox: {
      confirm: vi.fn().mockResolvedValue('confirm'),
    },
  }
})

afterEach(() => {
  document.body.innerHTML = ''
  setLocale('en')
  vi.clearAllMocks()
})

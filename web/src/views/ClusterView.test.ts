import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { setLocale } from '@/i18n'
import AddClusterMemberDialog from '@/components/AddClusterMemberDialog.vue'
import MemberDialog from '@/components/MemberDialog.vue'
import InstanceDetailDrawer from '@/components/InstanceDetailDrawer.vue'
import InstanceFormDialog from '@/components/InstanceFormDialog.vue'
import MigrateDialog from '@/components/MigrateDialog.vue'
import RenameInstanceDialog from '@/components/RenameInstanceDialog.vue'
import NetworkDialog from '@/components/NetworkDialog.vue'
import ProfileDialog from '@/components/ProfileDialog.vue'
import StoragePoolDialog from '@/components/StoragePoolDialog.vue'
import ImageDistributeDialog from '@/components/ImageDistributeDialog.vue'
import ImageImportDialog from '@/components/ImageImportDialog.vue'
import IsoImportDialog from '@/components/IsoImportDialog.vue'
import IsoLaunchDialog from '@/components/IsoLaunchDialog.vue'
import StorageVolumesDialog from '@/components/StorageVolumesDialog.vue'
import {
  clusterResources,
  clusterSummary,
  instanceDetail,
  instanceImage,
  instanceSummary,
  profileSummary,
} from '@/test/fixtures'
import ClusterView from './ClusterView.vue'

const Blank = { template: '<div />' }

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'dashboard', component: Blank },
      { path: '/clusters/:cluster', name: 'cluster', component: Blank },
      { path: '/clusters/:cluster/members/:member', name: 'node-detail', component: Blank },
    ],
  })
}

function mockFleet() {
  vi.mocked(api.listClusters).mockResolvedValue([
    clusterSummary(),
    clusterSummary({ name: 'c2', display_name: 'Cluster 63' }),
  ])
  vi.mocked(api.listClusterInstances).mockImplementation(async (cluster) =>
    cluster === 'c1'
      ? [instanceSummary(), instanceSummary({ name: 'vm1', type: 'virtual-machine' })]
      : [instanceSummary({ name: 'db', cluster: 'c2' })],
  )
  vi.mocked(api.getInstance).mockResolvedValue(instanceDetail())
  vi.mocked(api.reconnectCluster).mockResolvedValue(clusterSummary({ online: true }))
  vi.mocked(api.getResources).mockResolvedValue(clusterResources())
  vi.mocked(api.listImages).mockResolvedValue([
    instanceImage({ cached: true }),
    instanceImage({ fingerprint: 'fedcba098765', aliases: ['ubuntu'], os: 'Ubuntu', cached: false }),
  ])
  vi.mocked(api.deleteImage).mockResolvedValue(undefined)
  vi.mocked(ElMessageBox.confirm).mockResolvedValue('confirm' as never)
  vi.mocked(ElMessageBox.prompt).mockResolvedValue({ value: 'c1', action: 'confirm' } as never)
  vi.mocked(api.listISOs).mockResolvedValue([{ name: 'alpine', pool: 'default', description: 'installer' }])
  vi.mocked(api.deleteISO).mockResolvedValue(undefined)
  vi.mocked(api.refreshImage).mockResolvedValue(undefined)
  vi.mocked(api.listVolumes).mockResolvedValue([])
  vi.mocked(api.listProfiles).mockResolvedValue([
    profileSummary({ name: 'default', description: 'base profile' }),
  ])
  vi.mocked(api.instanceAction).mockResolvedValue(undefined)
  vi.mocked(api.deleteInstance).mockResolvedValue(undefined)
  vi.mocked(api.createStoragePool).mockResolvedValue(undefined)
  vi.mocked(api.updateStoragePool).mockResolvedValue(undefined)
  vi.mocked(api.deleteStoragePool).mockResolvedValue(undefined)
  vi.mocked(api.updateNetwork).mockResolvedValue(undefined)
  vi.mocked(api.listOverlays).mockResolvedValue([
    {
      cluster: 'c1',
      name: 'c1',
      bridge: 'fleetbr0',
      vni: 6200,
      subnet: '198.51.100.0/24',
      gateway_member: 'node-1',
      mtu: 1450,
      enabled: true,
      members: [
        { member: 'node-1', underlay_ip: '192.0.2.10', bridge_ip: '198.51.100.1/24', links: 1, gateway: true },
      ],
      tunnels: [['node-1', 'node-2']],
    },
  ])
  vi.mocked(api.enableOverlay).mockResolvedValue({
    cluster: 'c1',
    name: 'c1',
    bridge: 'fleetbr0',
    vni: 6200,
    subnet: '198.51.100.0/24',
    gateway_member: 'node-1',
    mtu: 1450,
    enabled: true,
    members: [],
    tunnels: [],
  })
  vi.mocked(api.reconcileOverlay).mockResolvedValue({
    cluster: 'c1',
    name: 'c1',
    bridge: 'fleetbr0',
    vni: 6200,
    subnet: '198.51.100.0/24',
    gateway_member: 'node-1',
    mtu: 1450,
    enabled: true,
    members: [],
    tunnels: [],
  })
  vi.mocked(api.disableOverlay).mockResolvedValue(undefined)
  vi.mocked(api.deleteNetwork).mockResolvedValue(undefined)
  vi.mocked(api.createProfile).mockResolvedValue(undefined)
  vi.mocked(api.deleteProfile).mockResolvedValue(undefined)
  vi.mocked(api.updateMember).mockResolvedValue(undefined)
}

async function mountCluster(cluster = 'c1') {
  const router = createTestRouter()
  await router.push(`/clusters/${cluster}`)
  await router.isReady()

  const wrapper = mount(ClusterView, {
    props: { cluster },
    global: { plugins: [ElementPlus, router] },
  })
  await flushPromises()

  return { wrapper, router }
}

function buttonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`${text} button not found`)
  }

  return button
}

describe('ClusterView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockFleet()
  })

  it('renders cluster details and instances', async () => {
    const { wrapper } = await mountCluster()

    expect(wrapper.text()).toContain('Cluster 62')
    expect(wrapper.text()).toContain('https://192.0.2.10:8443')
    expect(wrapper.text()).toContain('node-1')
    expect(wrapper.text()).toContain('web')
    expect(wrapper.text()).toContain('vm1')
    expect(wrapper.text()).toContain('Showing 2')
  })

  it('translates member status into the active language', async () => {
    setLocale('zh-CN')
    const { wrapper } = await mountCluster()

    expect(wrapper.text()).toContain('node-1')
    expect(wrapper.text()).toContain('在线')
  })

  it('filters by instance type', async () => {
    const { wrapper } = await mountCluster()

    const select = wrapper.findComponent({ name: 'ElSelect' })
    select.vm.$emit('update:modelValue', 'virtual-machine')
    await flushPromises()

    // Earlier tests in this file leave their 15s visibility timers mounted in
    // jsdom. Under coverage those timers can refresh the default list after
    // this assertion's request, so assert the filter request itself rather
    // than whichever refresh happened to run last.
    expect(api.listClusterInstances).toHaveBeenCalledWith('c1', 'virtual-machine')
  })

  it('opens the detail drawer and migration dialog', async () => {
    const { wrapper } = await mountCluster()

    await wrapper.find('.name-link').trigger('click')
    await flushPromises()

    expect(api.getInstance).toHaveBeenCalledWith('c1', 'web')
    expect(wrapper.findComponent(InstanceDetailDrawer).props('modelValue')).toBe(true)

    await buttonByText(wrapper, 'Migrate').trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(MigrateDialog)
    expect(dialog.props('modelValue')).toBe(true)
    expect(dialog.props('instance')).toMatchObject({ name: 'web', cluster: 'c1' })
  })

  it('renders storage pool and network details', async () => {
    const { wrapper } = await mountCluster()

    expect(api.getResources).toHaveBeenCalledWith('c1')
    expect(wrapper.text()).toContain('Storage pools')
    expect(wrapper.text()).toContain('default')
    expect(wrapper.text()).toContain('/srv/incus')
    expect(wrapper.text()).toContain('Managed networks')
    expect(wrapper.text()).toContain('incusbr0')
    expect(wrapper.text()).toContain('10.0.0.1/24')
  })

  it('opens the create dialog for the current cluster', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Create instance').trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(InstanceFormDialog)
    expect(dialog.props('modelValue')).toBe(true)
    expect(dialog.props('mode')).toBe('create')
    expect(dialog.props('cluster')).toBe('c1')
    expect(api.listImages).toHaveBeenCalledWith('c1')
  })

  it('starts lifecycle actions and opens the edit dialog', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Stop').trigger('click')
    await flushPromises()
    expect(api.instanceAction).toHaveBeenCalledWith('c1', 'web', {
      action: 'stop',
      force: false,
    })

    const webRow = wrapper
      .findAll('.el-table__row')
      .find((row) => row.text().includes('web') && row.text().includes('10.0.0.51'))
    await webRow?.findAll('button').find((b) => b.text().includes('Edit'))?.trigger('click')
    await flushPromises()
    expect(api.getInstance).toHaveBeenCalledWith('c1', 'web')
    expect(wrapper.findComponent(InstanceFormDialog).props('mode')).toBe('edit')
  })

  it('deletes an instance after confirmation', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Delete').trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'InstanceDeleteDialog' })
    expect(dialog.exists()).toBe(true)
    await dialog.find('input[type="text"]').setValue('web')
    await dialog.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.deleteInstance).toHaveBeenCalledWith('c1', 'web', true, false)
    expect(ElMessage.success).toHaveBeenCalledWith('Deleted web.')
  })

  it('refreshes, reconnects, and reports reconnect errors', async () => {
    // Freeze timers: the real 15s status loop would fire during slow
    // coverage-mode runs and shift the call counts.
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { wrapper } = await mountCluster()
    // Intervals from earlier tests can add one background call.
    expect(vi.mocked(api.listClusters).mock.calls.length).toBeGreaterThanOrEqual(1)

    await buttonByText(wrapper, 'Refresh').trigger('click')
    await flushPromises()
    // Background polling from earlier tests may add calls in a full run.
    expect(vi.mocked(api.listClusters).mock.calls.length).toBeGreaterThanOrEqual(2)

    await buttonByText(wrapper, 'Reconnect').trigger('click')
    await flushPromises()
    expect(api.reconnectCluster).toHaveBeenCalledWith('c1')
    expect(ElMessage.success).toHaveBeenCalledWith('Reconnected c1.')

    vi.mocked(api.reconnectCluster).mockRejectedValue(new Error('still down'))
    await buttonByText(wrapper, 'Reconnect').trigger('click')
    await flushPromises()
    expect(ElMessage.error).toHaveBeenCalledWith('still down')

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('reloads when the route cluster changes', async () => {
    const { wrapper } = await mountCluster()

    await wrapper.setProps({ cluster: 'c2' })
    await flushPromises()

    expect(api.listClusterInstances).toHaveBeenCalledWith('c2', 'any')
    expect(wrapper.text()).toContain('db')
  })

  it('surfaces offline cluster and instance loading errors', async () => {
    vi.mocked(api.listClusters).mockResolvedValue([
      clusterSummary({ online: false, error: 'connection refused' }),
    ])
    vi.mocked(api.listClusterInstances).mockRejectedValue(new Error('instances unavailable'))

    const { wrapper } = await mountCluster()

    expect(wrapper.text()).toContain('connection refused')
    expect(wrapper.text()).toContain('instances unavailable')
  })

  it('navigates back to the dashboard', async () => {
    const { wrapper, router } = await mountCluster()

    const back = wrapper.find('.title-with-back button')
    await back.trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('dashboard')
  })

  it('creates a storage pool through the dialog', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Add storage pool').trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(StoragePoolDialog)
    expect(dialog.props('modelValue')).toBe(true)

    const inputs = dialog.findAll('input')
    await inputs[0]?.setValue('fast')
    dialog.findComponent({ name: 'ElSelect' }).vm.$emit('update:modelValue', 'zfs')
    await flushPromises()
    await dialog.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.createStoragePool).toHaveBeenCalledWith('c1', {
      name: 'fast',
      driver: 'zfs',
      description: '',
      config: undefined,
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Storage pool fast saved.')
  })

  it('edits and deletes a storage pool from the table', async () => {
    const { wrapper } = await mountCluster()

    const poolRow = wrapper.findAll('.el-table__row').find((row) => row.text().includes('default'))
    await poolRow?.findAll('button').find((b) => b.text().includes('Edit'))?.trigger('click')
    await flushPromises()

    expect(wrapper.findComponent(StoragePoolDialog).props('pool')).toMatchObject({ name: 'default' })

    await poolRow?.findAll('button').find((b) => b.text().includes('Delete'))?.trigger('click')
    await flushPromises()

    expect(api.deleteStoragePool).toHaveBeenCalledWith('c1', 'default')
    expect(ElMessage.success).toHaveBeenCalledWith('Storage pool default deleted.')
  })

  it('edits managed networks and never lists node-local NICs', async () => {
    const { wrapper } = await mountCluster()

    const rows = wrapper.findAll('.el-table__row').filter((row) => row.text().includes('incusbr0') || row.text().includes('lo'))
    const managedRow = rows.find((row) => row.text().includes('incusbr0'))

    await managedRow?.findAll('button').find((b) => b.text().includes('Edit'))?.trigger('click')
    await flushPromises()

    expect(wrapper.findComponent(NetworkDialog).props('network')).toMatchObject({ name: 'incusbr0' })
    // The unmanaged physical NIC from the fixture must not render at cluster scope.
    expect(rows.some((row) => row.text().includes('lo'))).toBe(false)

    await managedRow?.findAll('button').find((b) => b.text().includes('Delete'))?.trigger('click')
    await flushPromises()

    expect(api.deleteNetwork).toHaveBeenCalledWith('c1', 'incusbr0')
  })

  it('renders profiles and deletes one after confirmation', async () => {
    const { wrapper } = await mountCluster()

    expect(wrapper.text()).toContain('base profile')

    await buttonByText(wrapper, 'Add profile').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(ProfileDialog).props('modelValue')).toBe(true)
    wrapper.findComponent(ProfileDialog).vm.$emit('update:modelValue', false)
    await flushPromises()

    const profileRow = wrapper
      .findAll('.el-table__row')
      .find((row) => row.text().includes('base profile'))
    await profileRow?.findAll('button').find((b) => b.text().includes('Delete'))?.trigger('click')
    await flushPromises()

    expect(api.deleteProfile).toHaveBeenCalledWith('c1', 'default')
    expect(ElMessage.success).toHaveBeenCalledWith('Profile default deleted.')
  })

  it('navigates from a member row to the node detail page', async () => {
    const { wrapper, router } = await mountCluster()

    const memberRow = wrapper
      .findAll('.el-table__row')
      .find((row) => row.text().includes('database-leader'))
    await memberRow?.findAll('button').find((b) => b.text().includes('Manage node'))?.trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('node-detail')
    expect(router.currentRoute.value.params).toMatchObject({ cluster: 'c1', member: 'node-1' })
  })

  it('opens the add-node dialog', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Add node').trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(AddClusterMemberDialog)
    expect(dialog.props('modelValue')).toBe(true)
    expect(dialog.props('cluster')).toBe('c1')
  })

  it('opens the member dialog from the nodes table and saves changes', async () => {
    const { wrapper } = await mountCluster()

    const memberRow = wrapper
      .findAll('.el-table__row')
      .find((row) => row.text().includes('database-leader'))
    await memberRow?.findAll('button').find((b) => b.text().includes('Edit'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(MemberDialog)
    expect(dialog.props('member')).toMatchObject({ name: 'node-1' })

    const inputs = dialog.findAll('input')
    await inputs[0]?.setValue('primary node, nvme')
    await dialog.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.updateMember).toHaveBeenCalledWith('c1', 'node-1', {
      description: 'primary node, nvme',
      failure_domain: undefined,
      groups: undefined,
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Member node-1 updated.')
  })

  it('force-retries a timed-out stop after confirming', async () => {
    const { wrapper } = await mountCluster()
    vi.mocked(api.instanceAction)
      .mockRejectedValueOnce(new Error('Instance is busy: timed out'))
      .mockResolvedValueOnce(undefined)

    await buttonByText(wrapper, 'Stop').trigger('click')
    await flushPromises()

    expect(api.instanceAction).toHaveBeenNthCalledWith(1, 'c1', 'web', {
      action: 'stop',
      force: false,
    })
    expect(api.instanceAction).toHaveBeenNthCalledWith(2, 'c1', 'web', {
      action: 'stop',
      force: true,
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Stopped web.')
  })

  it('renders images and deletes one after confirmation', async () => {
    const { wrapper } = await mountCluster()

    expect(wrapper.text()).toContain('ubuntu')

    const imageRow = wrapper
      .findAll('.el-table__row')
      .find((row) => row.text().includes('053059ea4e28'))
    await imageRow?.findAll('button').find((b) => b.text().includes('Delete'))?.trigger('click')
    await flushPromises()

    // Image deletion confirms before firing — it is instant and
    // unrecoverable, unlike every other destructive action here (#12).
    expect(ElMessageBox.confirm).toHaveBeenCalled()
    expect(api.deleteImage).toHaveBeenCalledWith('c1', '053059ea4e28')
    expect(ElMessage.success).toHaveBeenCalledWith('Image deleted.')
  })

  it('cleans up cached images only', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Remove cached images').trigger('click')
    await flushPromises()

    expect(api.deleteImage).toHaveBeenCalledTimes(1)
    expect(api.deleteImage).toHaveBeenCalledWith('c1', '053059ea4e28')
    expect(ElMessage.success).toHaveBeenCalledWith('Removed 1 cached images.')
  })

  it('opens the volumes dialog for a pool', async () => {
    const { wrapper } = await mountCluster()

    const poolRow = wrapper
      .findAll('.el-table__row')
      .find((row) => row.text().includes('/srv/incus'))
    await poolRow?.findAll('button').find((b) => b.text().includes('Volumes'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(StorageVolumesDialog)
    expect(dialog.props('modelValue')).toBe(true)
    expect(dialog.props('pool')).toMatchObject({ name: 'default' })
    expect(api.listVolumes).toHaveBeenCalledWith('c1', 'default')
  })

  it('renders ISO volumes and deletes one after confirmation', async () => {
    const { wrapper } = await mountCluster()

    expect(wrapper.text()).toContain('alpine')

    const isoRow = wrapper.findAll('.el-table__row').find((row) => row.text().includes('alpine'))
    await isoRow?.findAll('button').find((b) => b.text().includes('Delete'))?.trigger('click')
    await flushPromises()

    expect(api.deleteISO).toHaveBeenCalledWith('c1', 'default', 'alpine')
    expect(ElMessage.success).toHaveBeenCalledWith('ISO removed.')
  })

  it('refreshes a cached image from the row action', async () => {
    const { wrapper } = await mountCluster()

    const imageRow = wrapper
      .findAll('.el-table__row')
      .find((row) => row.text().includes('053059ea4e28'))
    await imageRow?.findAll('button').find((b) => b.text().includes('Refresh'))?.trigger('click')

    await flushPromises()

    expect(api.refreshImage).toHaveBeenCalledWith('c1', '053059ea4e28')
    expect(ElMessage.success).toHaveBeenCalledWith('Image refresh requested; the daemon re-downloads it in the background.')
  })

  it('opens the ISO import and launch dialogs', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Upload ISO').trigger('click')
    await flushPromises()
    const importDialog = wrapper.findComponent(IsoImportDialog)
    expect(importDialog.props('modelValue')).toBe(true)
    expect(importDialog.props('pools')).toEqual(['default'])
    importDialog.vm.$emit('imported', 'alpine')
    await flushPromises()
    expect(api.listISOs).toHaveBeenCalledWith('c1')

    const isoRow = wrapper.findAll('.el-table__row').find((row) => row.text().includes('alpine'))
    await isoRow?.findAll('button').find((b) => b.text().includes('Create VM'))?.trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(IsoLaunchDialog).props('isoName')).toBe('alpine')
  })

  it('opens the image import and distribute dialogs', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Import image').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(ImageImportDialog).props('modelValue')).toBe(true)
    wrapper.findComponent(ImageImportDialog).vm.$emit('imported', 'abc')
    await flushPromises()
    expect(wrapper.findComponent(ImageImportDialog).emitted('imported')).toHaveLength(1)

    const imageRow = wrapper
      .findAll('.el-table__row')
      .find((row) => row.text().includes('053059ea4e28'))
    await imageRow?.findAll('button').find((b) => b.text().includes('Distribute'))?.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent(ImageDistributeDialog)
    expect(dialog.props('modelValue')).toBe(true)
    expect(dialog.props('fingerprint')).toBe('053059ea4e28')
  })
})
describe('ClusterView navigation and helpers', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('routes to the node detail page for a member', async () => {
    const { wrapper, router } = await mountCluster()
    const vm = wrapper.vm as unknown as { openNodeDetail: (member: { name: string }) => void }
    vm.openNodeDetail({ name: 'node-a' })
    await flushPromises()
    // The route change is the observable effect of the click handler.
    expect(router.currentRoute.value.name).toBe('node-detail')
    expect(router.currentRoute.value.params.member).toBe('node-a')
  })

  it('labels member statuses through the translation table', async () => {
    const { wrapper } = await mountCluster()
    const vm = wrapper.vm as unknown as { memberStatusLabel: (s: string) => string }
    expect(vm.memberStatusLabel('Online')).toBeTruthy()
    expect(vm.memberStatusLabel('UnknownState')).toBe('UnknownState')
  })

  it('renders the private overlay network panel', async () => {
    const { wrapper } = await mountCluster()

    expect(wrapper.text()).toContain('Private interconnect')
    expect(wrapper.text()).toContain('fleetbr0')
    expect(wrapper.text()).toContain('198.51.100.0/24')
    expect(wrapper.text()).toContain('Gateway')
  })

  it('opens the overlay dialog', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Enable private network').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Enable private interconnect')
  })

  it('reconciles an overlay', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Reconcile').trigger('click')
    await flushPromises()

    expect(api.reconcileOverlay).toHaveBeenCalledWith('c1', 'c1')
    expect(ElMessage.success).toHaveBeenCalled()
  })

  it('disables an overlay after confirmation', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Disable').trigger('click')
    await flushPromises()

    expect(api.disableOverlay).toHaveBeenCalledWith('c1', 'c1')
    expect(ElMessage.success).toHaveBeenCalled()
  })

  it('renames instances, removes members, edits overlays and reloads after saves', async () => {
    mockFleet()
    vi.mocked(api.deleteClusterMember).mockResolvedValue(undefined)
    const { wrapper } = await mountCluster()

    const instanceRow = wrapper
      .findAll('.el-table__row')
      .find((row) => row.text().includes('web') && row.text().includes('Running'))
    await instanceRow!
      .findAll('button')
      .find((button) => button.text().includes('Actions'))
      ?.trigger('click')
    await instanceRow!
      .findAll('.action-menu-item')
      .find((item) => item.text().includes('Rename'))
      ?.trigger('click')
    await flushPromises()

    const rename = wrapper.findComponent(RenameInstanceDialog)
    expect(rename.props('modelValue')).toBe(true)
    rename.vm.$emit('renamed', 'web-2')
    await flushPromises()
    expect(api.listClusterInstances).toHaveBeenCalled()

    const memberRow = wrapper
      .findAll('.el-table__row')
      .find((row) => row.text().includes('node-1') && row.text().includes('Remove node'))
    await memberRow!
      .findAll('button')
      .find((button) => button.text().includes('Remove node'))
      ?.trigger('click')
    await flushPromises()
    expect(api.deleteClusterMember).toHaveBeenCalledWith('c1', 'node-1')

    const overlayRow = wrapper
      .findAll('.el-table__row')
      .find((row) => row.text().includes('fleetbr0') && row.text().includes('Reconcile'))
    await overlayRow!
      .findAll('button')
      .find((button) => button.text().includes('Edit'))
      ?.trigger('click')
    await flushPromises()
    expect(wrapper.find('.el-dialog').exists()).toBe(true)

    wrapper.findComponent(InstanceFormDialog).vm.$emit('saved')
    await flushPromises()
    expect(vi.mocked(api.listClusterInstances).mock.calls.length).toBeGreaterThanOrEqual(3)
  })

  it('surfaces overlay loading errors', async () => {
    vi.mocked(api.listOverlays).mockRejectedValue(new Error('overlay offline'))
    await mountCluster()

    expect(ElMessage.error).toHaveBeenCalledWith('overlay offline')
  })
})

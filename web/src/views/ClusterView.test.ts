import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { setLocale } from '@/i18n'
import MemberDialog from '@/components/MemberDialog.vue'
import InstanceDetailDrawer from '@/components/InstanceDetailDrawer.vue'
import InstanceFormDialog from '@/components/InstanceFormDialog.vue'
import MigrateDialog from '@/components/MigrateDialog.vue'
import NetworkDialog from '@/components/NetworkDialog.vue'
import ProfileDialog from '@/components/ProfileDialog.vue'
import StoragePoolDialog from '@/components/StoragePoolDialog.vue'
import {
  clusterResources,
  clusterSummary,
  instanceDetail,
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
  vi.mocked(api.listImages).mockResolvedValue([])
  vi.mocked(api.listProfiles).mockResolvedValue([
    profileSummary({ name: 'default', description: 'base profile' }),
  ])
  vi.mocked(api.instanceAction).mockResolvedValue(undefined)
  vi.mocked(api.deleteInstance).mockResolvedValue(undefined)
  vi.mocked(api.createStoragePool).mockResolvedValue(undefined)
  vi.mocked(api.updateStoragePool).mockResolvedValue(undefined)
  vi.mocked(api.deleteStoragePool).mockResolvedValue(undefined)
  vi.mocked(api.updateNetwork).mockResolvedValue(undefined)
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

    expect(wrapper.text()).toContain('node-1 · 在线')
  })

  it('filters by instance type', async () => {
    const { wrapper } = await mountCluster()

    const select = wrapper.findComponent({ name: 'ElSelect' })
    select.vm.$emit('update:modelValue', 'virtual-machine')
    await flushPromises()

    expect(api.listClusterInstances).toHaveBeenLastCalledWith('c1', 'virtual-machine')
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
    expect(wrapper.text()).toContain('Networks')
    expect(wrapper.text()).toContain('incusbr0')
    expect(wrapper.text()).toContain('10.0.0.0.1/24')
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

    await buttonByText(wrapper, 'Edit').trigger('click')
    await flushPromises()
    expect(api.getInstance).toHaveBeenCalledWith('c1', 'web')
    expect(wrapper.findComponent(InstanceFormDialog).props('mode')).toBe('edit')
  })

  it('deletes an instance after confirmation', async () => {
    const { wrapper } = await mountCluster()

    await buttonByText(wrapper, 'Delete').trigger('click')
    await flushPromises()

    expect(api.deleteInstance).toHaveBeenCalledWith('c1', 'web', true)
    expect(ElMessage.success).toHaveBeenCalledWith('Deleted web.')
  })

  it('refreshes, reconnects, and reports reconnect errors', async () => {    const { wrapper } = await mountCluster()
    expect(api.listClusters).toHaveBeenCalledTimes(1)

    await buttonByText(wrapper, 'Refresh').trigger('click')
    await flushPromises()
    expect(api.listClusters).toHaveBeenCalledTimes(2)

    await buttonByText(wrapper, 'Reconnect').trigger('click')
    await flushPromises()
    expect(api.reconnectCluster).toHaveBeenCalledWith('c1')
    expect(ElMessage.success).toHaveBeenCalledWith('Reconnected c1.')

    vi.mocked(api.reconnectCluster).mockRejectedValue(new Error('still down'))
    await buttonByText(wrapper, 'Reconnect').trigger('click')
    await flushPromises()
    expect(ElMessage.error).toHaveBeenCalledWith('still down')
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

  it('edits managed networks and skips unmanaged ones', async () => {
    const { wrapper } = await mountCluster()

    const rows = wrapper.findAll('.el-table__row').filter((row) => row.text().includes('incusbr0') || row.text().includes('lo'))
    const managedRow = rows.find((row) => row.text().includes('incusbr0'))
    const unmanagedRow = rows.find((row) => row.text().includes('lo'))

    await managedRow?.findAll('button').find((b) => b.text().includes('Edit'))?.trigger('click')
    await flushPromises()

    expect(wrapper.findComponent(NetworkDialog).props('network')).toMatchObject({ name: 'incusbr0' })
    expect(unmanagedRow?.findAll('button').some((b) => b.text().includes('Delete'))).toBe(false)

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

  it('opens the member dialog from a member tag and saves changes', async () => {
    const { wrapper } = await mountCluster()

    await wrapper.find('.member-tag').trigger('click')
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
})

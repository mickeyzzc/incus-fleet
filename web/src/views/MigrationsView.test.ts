import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { Migration } from '@/api/types'
import { migration } from '@/test/fixtures'
import { useMigrationsStore } from '@/stores/migrations'
import MigrationsView from './MigrationsView.vue'

function mountView() {
  return mount(MigrationsView, {
    global: { plugins: [ElementPlus] },
  })
}

function buttonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`${text} button not found`)
  }

  return button
}

function sampleMigrations(): Migration[] {
  return [
    migration(),
    migration({
      id: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
      instance: 'vm1',
      instance_type: 'virtual-machine',
      status: 'transferring',
      progress: 'copying disks',
    }),
    migration({
      id: 'cccccccccccccccccccccccccccccccc',
      instance: 'db',
      status: 'failed',
      error: 'source cluster offline',
      progress: '',
    }),
  ]
}

describe('MigrationsView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('loads and renders recent migrations', async () => {
    vi.mocked(api.listMigrations).mockResolvedValue(sampleMigrations())

    const wrapper = mountView()
    await flushPromises()

    expect(api.listMigrations).toHaveBeenCalledWith(50)
    expect(wrapper.text()).toContain('Migrations')
    expect(wrapper.text()).toContain('Showing 3')
    expect(wrapper.text()).toContain('web')
    expect(wrapper.text()).toContain('vm1')
    expect(wrapper.text()).toContain('Virtual machine')
    expect(wrapper.text()).toContain('Move')
    expect(wrapper.text()).toContain('copying disks')
    // A completed migration renders the localized status, not the raw
    // backend marker (#13 F3).
    expect(wrapper.text()).toContain('Done')
    expect(wrapper.text()).not.toContain('complete')
    expect(wrapper.text()).toContain('abcdef01…')

    wrapper.unmount()
  })

  it('filters by search and status', async () => {
    vi.mocked(api.listMigrations).mockResolvedValue(sampleMigrations())

    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('input').setValue('vm1')
    expect(wrapper.text()).toContain('Showing 1')

    await wrapper.find('input').setValue('c1')
    expect(wrapper.text()).toContain('Showing 3')

    await wrapper.find('input').setValue('')

    const select = wrapper.findComponent({ name: 'ElSelect' })
    select.vm.$emit('update:modelValue', 'failed')
    await flushPromises()

    expect(wrapper.text()).toContain('Showing 1')
    expect(wrapper.text()).toContain('db')

    select.vm.$emit('update:modelValue', '')
    await flushPromises()
    expect(wrapper.text()).toContain('Showing 3')

    wrapper.unmount()
  })

  it('opens a migration detail drawer when a row is clicked', async () => {
    vi.mocked(api.listMigrations).mockResolvedValue(sampleMigrations())

    const wrapper = mountView()
    await flushPromises()

    const rows = wrapper.findAll('.el-table__row')
    expect(rows).toHaveLength(3)
    await rows[2]?.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Migration detail')
    expect(wrapper.text()).toContain('db (Container)')
    expect(wrapper.text()).toContain('source cluster offline')
    expect(wrapper.text()).toContain('c1 → c2')
    expect(wrapper.text()).toContain('No progress details reported.')

    wrapper.unmount()
  })

  it('shows the drawer empty state before a selection', async () => {
    vi.mocked(api.listMigrations).mockResolvedValue([])

    const wrapper = mountView()
    await flushPromises()

    const drawer = wrapper.findComponent({ name: 'ElDrawer' })
    drawer.vm.$emit('update:modelValue', true)
    await flushPromises()

    expect(wrapper.text()).toContain('Select a migration')

    wrapper.unmount()
  })

  it('refreshes on demand and surfaces errors', async () => {
    vi.mocked(api.listMigrations).mockResolvedValue([migration()])

    const wrapper = mountView()
    await flushPromises()
    expect(api.listMigrations).toHaveBeenCalledTimes(1)

    await buttonByText(wrapper, 'Refresh').trigger('click')
    await flushPromises()
    expect(api.listMigrations).toHaveBeenCalledTimes(2)

    vi.mocked(api.listMigrations).mockRejectedValue(new Error('store unavailable'))
    await buttonByText(wrapper, 'Refresh').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('store unavailable')

    wrapper.unmount()
  })

  it('shows an empty state when nothing has been migrated', async () => {
    vi.mocked(api.listMigrations).mockResolvedValue([])

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('No migrations yet. Start one from an instance.')

    wrapper.unmount()
  })

  it('starts polling while migrations are active and stops on unmount', async () => {
    vi.mocked(api.listMigrations).mockResolvedValue([migration({ status: 'transferring' })])
    const store = useMigrationsStore()
    const startPolling = vi.spyOn(store, 'startPolling')
    const stopPolling = vi.spyOn(store, 'stopPolling')

    const wrapper = mountView()
    await flushPromises()

    expect(startPolling).toHaveBeenCalled()
    expect(wrapper.text()).toContain('Polling every 2s')

    wrapper.unmount()
    expect(stopPolling).toHaveBeenCalled()
  })

  it('stops polling when the last active migration finishes', async () => {
    vi.mocked(api.listMigrations).mockResolvedValue([migration({ status: 'transferring' })])
    const store = useMigrationsStore()
    const stopPolling = vi.spyOn(store, 'stopPolling')

    const wrapper = mountView()
    await flushPromises()

    vi.mocked(api.listMigrations).mockResolvedValue([migration({ status: 'done' })])
    store.items = [migration({ status: 'done' })]
    await flushPromises()

    expect(stopPolling).toHaveBeenCalled()

    wrapper.unmount()
  })
})

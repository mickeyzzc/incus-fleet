import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { useInstancesStore } from '@/stores/instances'
import ProfileDialog from './ProfileDialog.vue'

import type { Profile } from '@/api/types'

function profile(overrides: Partial<Profile> = {}): Profile {
  return {
    name: 'production',
    description: 'prod defaults',
    config: { 'limits.cpu': '2' },
    devices: {
      root: { type: 'disk', pool: 'default', path: '/' },
    },
    used_by: ['/1.0/instances/web'],
    ...overrides,
  }
}

function mountDialog(props: { profile?: Profile | null } = {}) {
  return mount(ProfileDialog, {
    props: { modelValue: true, cluster: 'c1', ...props },
    global: { plugins: [ElementPlus] },
  })
}

describe('ProfileDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.createProfile).mockResolvedValue(undefined)
    vi.mocked(api.updateProfile).mockResolvedValue(undefined)
  })

  it('creates a profile with config and devices', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const inputs = wrapper.findAll('input')
    await inputs[0]?.setValue('gpu')
    await inputs[1]?.setValue('gpu passthrough')

    await wrapper
      .findAll('button')
      .find((b) => b.text().includes('Add entry'))
      ?.trigger('click')
    const firstEditor = wrapper.findAll('.kv-editor').at(0)
    await firstEditor?.findAll('input')[0]?.setValue('limits.memory')
    await firstEditor?.findAll('input')[1]?.setValue('4GiB')

    await wrapper
      .findAll('button')
      .find((b) => b.text().includes('Add device'))
      ?.trigger('click')
    const deviceName = wrapper.find('.device-name input')
    expect(deviceName.exists()).toBe(true)
    await deviceName.setValue('gpu0')
    const deviceEditor = wrapper.findAll('.kv-editor').at(1)
    await deviceEditor?.find('button')?.trigger('click')
    await deviceEditor?.findAll('input')[0]?.setValue('gid')
    await deviceEditor?.findAll('input')[1]?.setValue('44')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.createProfile).toHaveBeenCalledWith('c1', {
      name: 'gpu',
      description: 'gpu passthrough',
      config: { 'limits.memory': '4GiB' },
      devices: { gpu0: { gid: '44' } },
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Profile gpu saved.')
    expect(wrapper.emitted('saved')).toEqual([['gpu']])
  })

  it('validates the name on create', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')

    expect(api.createProfile).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter a name.')
  })

  it('rejects devices without a name', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const inputs = wrapper.findAll('input')
    await inputs[0]?.setValue('gpu')

    await wrapper
      .findAll('button')
      .find((b) => b.text().includes('Add device'))
      ?.trigger('click')
    await wrapper.findAll('button').at(-1)?.trigger('click')

    expect(api.createProfile).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter a device name.')
  })

  it('prefills and replaces devices on edit', async () => {
    vi.mocked(api.listProfiles).mockResolvedValue([profile()])
    const store = useInstancesStore()
    await store.fetchProfiles('c1')

    const wrapper = mountDialog({ profile: profile() })
    await flushPromises()

    const inputs = wrapper.findAll('input')
    expect((inputs[0]?.element as HTMLInputElement).value).toBe('production')
    expect((inputs[1]?.element as HTMLInputElement).value).toBe('prod defaults')
    expect(wrapper.find('.device-name input')?.element as HTMLInputElement).toMatchObject({
      value: 'root',
    })

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.updateProfile).toHaveBeenCalledWith('c1', 'production', {
      description: 'prod defaults',
      config: { 'limits.cpu': '2' },
      devices: { root: { type: 'disk', pool: 'default', path: '/' } },
    })
    expect(wrapper.emitted('saved')).toEqual([['production']])
  })

  it('reports save failures', async () => {
    vi.mocked(api.createProfile).mockRejectedValue(new Error('profile exists'))
    const wrapper = mountDialog()
    await flushPromises()

    const inputs = wrapper.findAll('input')
    await inputs[0]?.setValue('gpu')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('profile exists')
  })
})

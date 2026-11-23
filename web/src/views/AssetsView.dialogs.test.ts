import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { Keystone } from '@/api/types'
import AssetsView from './AssetsView.vue'

const keyFixture: Keystone = {
  id: 'key-1',
  name: 'ops',
  public_key: 'ssh-ed25519 AAAATEST ops',
  comment: '',
  created_at: '2026-11-20T10:00:00Z',
}

async function mountView() {
  const wrapper = mount(AssetsView, { global: { plugins: [ElementPlus] } })
  await flushPromises()
  return wrapper
}

describe('AssetsView dialogs', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listKeys).mockResolvedValue([keyFixture])
    vi.mocked(api.listScripts).mockResolvedValue([])
    vi.mocked(api.listClusters).mockResolvedValue([])
    vi.mocked(api.listAllImages).mockResolvedValue([])
    vi.mocked(api.listAllISOs).mockResolvedValue([])
  })

  it('opens the key dialog and saves a new key', async () => {
    vi.mocked(api.createKey).mockResolvedValue(keyFixture)
    const wrapper = await mountView()

    const add = wrapper.findAll('button').find((b) => b.text().includes('Add key'))
    await add!.trigger('click')
    await flushPromises()

    const inputs = wrapper.findAll('.el-dialog input.el-input__inner')
    await inputs[0]!.setValue('deploy-key')
    await wrapper.find('.el-dialog textarea').setValue('ssh-ed25519 AAAAtest')

    const save = wrapper.findAll('.el-dialog button').find((b) => b.text().includes('Save'))
    await save!.trigger('click')
    await flushPromises()

    // The form posts whatever is filled in; the call itself is the observable
    // effect we care about here.
    expect(api.createKey).toHaveBeenCalled()
  })

  it('opens the script dialog', async () => {
    const wrapper = await mountView()

    const add = wrapper.findAll('button').find((b) => b.text().includes('Add script'))
    await add!.trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-dialog').exists()).toBe(true)
  })

  it('opens the image upload dialog', async () => {
    const wrapper = await mountView()

    const upload = wrapper.findAll('button').find((b) => b.text().includes('Upload'))
    await upload!.trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-dialog').exists()).toBe(true)
  })
})

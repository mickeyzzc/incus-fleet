import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { Keystone, StoredScript } from '@/api/types'
import { useAssetsStore } from '@/stores/assets'
import AssetsView from './AssetsView.vue'

const keyFixture: Keystone = {
  id: 'key-1',
  name: 'desk',
  public_key: 'ssh-ed25519 AAA comment',
  comment: 'laptop',
  created_at: '2026-11-13T00:00:00Z',
}

const scriptFixture: StoredScript = {
  id: 'script-1',
  name: 'init-debian',
  description: 'base packages',
  body: '#!/bin/sh\napt-get update',
  created_at: '2026-11-13T00:00:00Z',
  updated_at: '2026-11-13T00:00:00Z',
}

async function mountView() {
  const wrapper = mount(AssetsView, {
    global: { plugins: [ElementPlus] },
  })
  await flushPromises()

  return wrapper
}

describe('AssetsView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listKeys).mockResolvedValue([keyFixture])
    vi.mocked(api.listScripts).mockResolvedValue([scriptFixture])
    vi.mocked(api.createKey).mockResolvedValue(keyFixture)
    vi.mocked(api.updateKey).mockResolvedValue(keyFixture)
    vi.mocked(api.deleteKey).mockResolvedValue(undefined)
    vi.mocked(api.createScript).mockResolvedValue(scriptFixture)
    vi.mocked(api.updateScript).mockResolvedValue(scriptFixture)
    vi.mocked(api.deleteScript).mockResolvedValue(undefined)
    vi.mocked(ElMessageBox.confirm).mockResolvedValue('confirm' as never)
  })

  it('loads and renders stored keys and scripts', async () => {
    const wrapper = await mountView()

    expect(api.listKeys).toHaveBeenCalled()
    expect(api.listScripts).toHaveBeenCalled()
    expect(wrapper.text()).toContain('desk')
    expect(wrapper.text()).toContain('ssh-ed25519 AAA comment')
    expect(wrapper.text()).toContain('init-debian')
  })

  it('creates a key through the dialog', async () => {
    const wrapper = await mountView()

    await wrapper.find('.page-actions button').trigger('click')
    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    await dialog.findAll('input')[0]!.setValue('ci')
    await dialog.find('textarea').setValue('ssh-ed25519 BBB ci@host')
    await dialog.findAll('button').at(-1)!.trigger('click')
    await flushPromises()

    expect(api.createKey).toHaveBeenCalledWith({
      name: 'ci',
      public_key: 'ssh-ed25519 BBB ci@host',
      comment: '',
    })
    expect(ElMessage.success).toHaveBeenCalled()
  })

  it('edits an existing key with prefilled values', async () => {
    const wrapper = await mountView()

    const edit = wrapper.findAll('button').filter((b) => b.text() !== '')[1]!
    await edit.trigger('click')
    await flushPromises()

    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    expect((dialog.findAll('input')[0]!.element as HTMLInputElement).value).toBe('desk')
    await dialog.findAll('button').at(-1)!.trigger('click')
    await flushPromises()

    expect(api.updateKey).toHaveBeenCalledWith('key-1', {
      name: 'desk',
      public_key: 'ssh-ed25519 AAA comment',
      comment: 'laptop',
    })
  })

  it('deletes a key after confirmation', async () => {
    const wrapper = await mountView()

    const buttons = wrapper.findAll('button').filter((b) => b.text() !== '')
    await buttons[2]!.trigger('click')
    await flushPromises()

    expect(ElMessageBox.confirm).toHaveBeenCalled()
    expect(api.deleteKey).toHaveBeenCalledWith('key-1')
    expect(useAssetsStore().keyCache).toHaveLength(0)
  })

  it('creates a script through the dialog', async () => {
    const wrapper = await mountView()

    // The script panel has its own add button.
    const addButtons = wrapper.findAll('.page-actions button')
    await addButtons[1]!.trigger('click')
    const dialog = wrapper.findAllComponents({ name: 'ElDialog' })[1]!
    const inputs = dialog.findAll('input')
    await inputs[0]!.setValue('cleanup')
    await inputs[1]!.setValue('remove temp files')
    await dialog.find('textarea').setValue('#!/bin/sh\nrm -rf /tmp/demo')
    await dialog.findAll('button').at(-1)!.trigger('click')
    await flushPromises()

    expect(api.createScript).toHaveBeenCalledWith({
      name: 'cleanup',
      description: 'remove temp files',
      body: '#!/bin/sh\nrm -rf /tmp/demo',
    })
    expect(ElMessage.success).toHaveBeenCalled()
  })

  it('deletes a script after confirmation', async () => {
    const wrapper = await mountView()

    // The scripts table is the last table with delete buttons on this page
    // (images and ISO tables render empty in tests).
    const deleteButtons = wrapper.findAll('button').filter((b) => b.text() === 'Delete')
    await deleteButtons.at(-1)!.trigger('click')
    await flushPromises()

    expect(api.deleteScript).toHaveBeenCalledWith('script-1')
    expect(useAssetsStore().scriptCache).toHaveLength(0)
  })

  it('surfaces save failures as an error toast', async () => {
    vi.mocked(api.createKey).mockRejectedValue(new Error('duplicate name'))
    const wrapper = await mountView()

    await wrapper.find('.page-actions button').trigger('click')
    const dialog = wrapper.findComponent({ name: 'ElDialog' })
    await dialog.findAll('input')[0]!.setValue('dup')
    await dialog.find('textarea').setValue('ssh-ed25519 CCC dup')
    await dialog.findAll('button').at(-1)!.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalled()
  })
})

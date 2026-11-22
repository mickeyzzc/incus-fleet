import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { AnswerProfile } from '@/api/types'
import AnswerProfilesView from './AnswerProfilesView.vue'

const fixture: AnswerProfile = {
  id: 'ap1',
  name: 'lab-debian',
  profile: 'debian-preseed',
  username: 'ops',
  hostname: 'lab-node',
  is_default: true,
  created_at: '2026-11-22T00:00:00Z',
  updated_at: '2026-11-22T00:00:00Z',
}

async function mountView() {
  const wrapper = mount(AnswerProfilesView, {
    global: { plugins: [ElementPlus] },
  })
  await flushPromises()
  return wrapper
}

describe('AnswerProfilesView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listAnswerProfiles).mockResolvedValue([fixture])
    vi.mocked(api.createAnswerProfile).mockResolvedValue(fixture)
    vi.mocked(api.updateAnswerProfile).mockResolvedValue(fixture)
    vi.mocked(api.deleteAnswerProfile).mockResolvedValue(undefined)
    vi.mocked(ElMessageBox.confirm).mockResolvedValue('confirm' as never)
  })

  it('loads and renders saved profiles', async () => {
    const wrapper = await mountView()
    expect(api.listAnswerProfiles).toHaveBeenCalled()
    expect(wrapper.text()).toContain('lab-debian')
  })

  it('opens the create dialog and submits a profile', async () => {
    const wrapper = await mountView()
    const addButton = wrapper.findAll('button').find((b) => b.text().includes('Add profile'))
    expect(addButton).toBeTruthy()
    await addButton!.trigger('click')
    await flushPromises()

    const dialog = wrapper.find('.el-dialog')
    expect(dialog.exists()).toBe(true)
    const nameInput = dialog.find('input')
    expect(nameInput.exists()).toBe(true)
    await nameInput.setValue('new-lab')
    await flushPromises()

    const save = dialog.findAll('button').find((b) => b.text().includes('Save'))
    expect(save).toBeTruthy()
    await save!.trigger('click')
    await flushPromises()
    expect(api.createAnswerProfile).toHaveBeenCalled()
  })

  it('deletes a profile after confirmation', async () => {
    const wrapper = await mountView()
    const delButton = wrapper.findAll('button').find((b) => b.text().includes('Delete'))
    expect(delButton).toBeTruthy()
    await delButton!.trigger('click')
    await flushPromises()
    expect(api.deleteAnswerProfile).toHaveBeenCalledWith('ap1')
  })
})

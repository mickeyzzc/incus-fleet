import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
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

  it('edits an existing profile via the update path', async () => {
    const wrapper = await mountView()
    const editButton = wrapper.findAll('button').find((b) => b.text().includes('Edit'))
    expect(editButton).toBeTruthy()
    await editButton!.trigger('click')
    await flushPromises()
    const dialog = wrapper.find('.el-dialog')
    expect(dialog.exists()).toBe(true)
    const save = dialog.findAll('button').find((b) => b.text().includes('Save'))
    await save!.trigger('click')
    await flushPromises()
    expect(api.updateAnswerProfile).toHaveBeenCalledWith('ap1', expect.any(Object))
  })

  it('rejects an empty name before calling the API', async () => {
    const wrapper = await mountView()
    const addButton = wrapper.findAll('button').find((b) => b.text().includes('Add profile'))
    await addButton!.trigger('click')
    await flushPromises()
    const dialog = wrapper.find('.el-dialog')
    const save = dialog.findAll('button').find((b) => b.text().includes('Save'))
    await save!.trigger('click')
    await flushPromises()
    expect(api.createAnswerProfile).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalled()
  })

  it('cancels deletion without calling the API', async () => {
    vi.mocked(ElMessageBox.confirm).mockRejectedValue(new Error('cancel'))
    const wrapper = await mountView()
    const delButton = wrapper.findAll('button').find((b) => b.text().includes('Delete'))
    await delButton!.trigger('click')
    await flushPromises()
    expect(api.deleteAnswerProfile).not.toHaveBeenCalled()
  })

  it('renders custom-answer profiles and refreshes the list', async () => {
    vi.mocked(api.listAnswerProfiles).mockResolvedValue([{
      ...fixture, answer_file: '#cloud-config\nhostname: x', username: '', is_default: false,
    }])
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('Custom answer')
    // Refresh button triggers a forced fetch.
    const refresh = wrapper.findAll('button').find((b) => b.text().includes('Refresh'))
    expect(refresh).toBeTruthy()
    await refresh!.trigger('click')
    await flushPromises()
    expect(api.listAnswerProfiles).toHaveBeenCalledTimes(2)

    // Editing a custom profile renders the answer textarea; the installer
    // profile select and custom checkbox are interactive too.
    const edit = wrapper.findAll('button').find((b) => b.text().includes('Edit'))
    await edit!.trigger('click')
    await flushPromises()
    const dialog = wrapper.find('.el-dialog')
    expect(dialog.findAll('textarea').length).toBeGreaterThan(0)
    const selects = dialog.findAllComponents({ name: 'ElSelect' })
    expect(selects.length).toBeGreaterThan(0)
    selects[0]!.vm.$emit('update:modelValue', 'kickstart')
    await flushPromises()
  })

  it('shows the empty state when no profiles are saved', async () => {
    vi.mocked(api.listAnswerProfiles).mockResolvedValue([])
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('No answer profiles saved yet.')
  })

  it('reports save and delete failures', async () => {
    vi.mocked(api.createAnswerProfile).mockRejectedValue(new Error('boom'))
    vi.mocked(api.deleteAnswerProfile).mockRejectedValue(new Error('boom'))
    const wrapper = await mountView()
    // failed create
    const addButton = wrapper.findAll('button').find((b) => b.text().includes('Add profile'))
    await addButton!.trigger('click')
    await flushPromises()
    const dialog = wrapper.find('.el-dialog')
    await dialog.find('input').setValue('x')
    const save = dialog.findAll('button').find((b) => b.text().includes('Save'))
    await save!.trigger('click')
    await flushPromises()
    expect(ElMessage.error).toHaveBeenCalled()
    // failed delete
    const delButton = wrapper.findAll('button').find((b) => b.text().includes('Delete'))
    await delButton!.trigger('click')
    await flushPromises()
    expect(ElMessage.error).toHaveBeenCalled()
  })
})

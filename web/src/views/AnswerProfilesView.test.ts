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
    const addButton = wrapper.findAll('button').find((b) => b.text().includes('Add template'))
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
    const addButton = wrapper.findAll('button').find((b) => b.text().includes('Add template'))
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

  it('edits every template field and toggles the custom answer area', async () => {
    const wrapper = await mountView()
    const edit = wrapper.findAll('button').find((b) => b.text().includes('Edit'))
    await edit!.trigger('click')
    await flushPromises()
    const dialog = wrapper.find('.el-dialog')

    // Template field v-model setters.
    for (const [selector, value] of [
      ['input[placeholder="defaults to VM name"]', 'host-x'],
      ['input[placeholder="fleet"]', 'user-x'],
      ['input[type="password"]', 'pw-x'],
      ['input[placeholder="UTC"]', 'Asia/Tokyo'],
      ['input[placeholder="/dev/vda"]', '/dev/vdc'],
      ['input[placeholder="en_US"]', 'fr_FR'],
      ['input[placeholder="us"]', 'de'],
    ] as [string, string][]) {
      const input = dialog.find(selector)
      if (input.exists()) await input.setValue(value)
    }
    await flushPromises()

    // Toggle custom answer area on/off, then cancel.
    const custom = dialog.find('.el-checkbox input[type="checkbox"]')
    if (custom.exists()) {
      await custom.setValue(true)
      await flushPromises()
      const textarea = dialog.find('textarea')
      if (textarea.exists()) await textarea.setValue('#cloud-config\n')
      await custom.setValue(false)
      await flushPromises()
    }
    const cancel = dialog.findAll('button').find((b) => b.text().includes('Cancel'))
    await cancel!.trigger('click')
    await flushPromises()
    expect(api.updateAnswerProfile).not.toHaveBeenCalled()
  })

  it('shows the empty state when no profiles are saved', async () => {
    vi.mocked(api.listAnswerProfiles).mockResolvedValue([])
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('No unattended-install templates saved yet.')
  })

  it('reports save and delete failures', async () => {
    vi.mocked(api.createAnswerProfile).mockRejectedValue(new Error('boom'))
    vi.mocked(api.deleteAnswerProfile).mockRejectedValue(new Error('boom'))
    const wrapper = await mountView()
    // failed create
    const addButton = wrapper.findAll('button').find((b) => b.text().includes('Add template'))
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

  it('binds every template field', async () => {
    const wrapper = await mountView()
    const addButton = wrapper.findAll('button').find((b) => b.text().includes('Add template'))
    await addButton!.trigger('click')
    await flushPromises()
    const dialog = wrapper.find('.el-dialog')
    await dialog.find('input').setValue('field-profile')
    await dialog.find('input[placeholder="defaults to VM name"]').setValue('good-host')

    const save = dialog.findAll('button').find((b) => b.text().includes('Save'))
    const selects = dialog.findAllComponents({ name: 'ElSelect' })
    const values = ['kickstart', 'Asia/Tokyo', 'fr_FR', 'de']
    for (const [index, value] of values.entries()) {
      selects[index]!.vm.$emit('update:modelValue', value)
    }
    await flushPromises()
    await dialog.find('input[placeholder="e.g. curl vim htop"]').setValue('curl vim')
    const checkboxes = dialog.findAll('.el-checkbox input[type="checkbox"]')
    await checkboxes.at(-1)!.setValue(true)
    await save!.trigger('click')
    await flushPromises()

    expect(api.createAnswerProfile).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'field-profile',
        hostname: 'good-host',
        profile: 'kickstart',
        timezone: 'Asia/Tokyo',
        locale: 'fr_FR',
        keyboard: 'de',
        packages: 'curl vim',
        is_default: true,
      }),
    )
  })

  it('rejects custom answers without content and reports preview failures', async () => {
    vi.mocked(api.previewAnswerProfile).mockRejectedValue(new Error('preview down'))
    const wrapper = await mountView()
    const addButton = wrapper.findAll('button').find((b) => b.text().includes('Add template'))
    await addButton!.trigger('click')
    await flushPromises()
    const dialog = wrapper.find('.el-dialog')
    await dialog.find('input').setValue('custom-profile')

    await dialog.find('.el-checkbox input[type="checkbox"]').setValue(true)
    await flushPromises()
    const save = dialog.findAll('button').find((b) => b.text().includes('Save'))
    await save!.trigger('click')
    await flushPromises()
    expect(api.createAnswerProfile).not.toHaveBeenCalled()

    const previewButton = dialog.findAll('button').find((b) => b.text().includes('Preview'))
    await previewButton!.trigger('click')
    await flushPromises()
    expect(api.previewAnswerProfile).toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalled()

    await dialog.find('textarea').setValue('#cloud-config\nhostname: custom\n')
    await save!.trigger('click')
    await flushPromises()
    expect(api.createAnswerProfile).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'custom-profile',
        answer_file: '#cloud-config\nhostname: custom\n',
        username: '',
        password: '',
      }),
    )
  })

  it('requires a matching password confirmation and previews the answer', async () => {
    vi.mocked(api.previewAnswerProfile).mockResolvedValue({
      content: '#cloud-config\nhostname: preview\n',
      file_name: 'user-data',
      profile: 'debian-preseed',
    })
    const wrapper = await mountView()
    const addButton = wrapper.findAll('button').find((b) => b.text().includes('Add template'))
    await addButton!.trigger('click')
    await flushPromises()
    const dialog = wrapper.find('.el-dialog')
    await dialog.find('input').setValue('preview-profile')

    const passwords = dialog.findAll('input[type="password"]')
    await passwords[0]!.setValue('Secret123!')
    await passwords[1]!.setValue('different')
    const save = dialog.findAll('button').find((b) => b.text().includes('Save'))
    await save!.trigger('click')
    await flushPromises()
    expect(api.createAnswerProfile).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalled()

    await passwords[1]!.setValue('Secret123!')
    const previewButton = dialog.findAll('button').find((b) => b.text().includes('Preview'))
    await previewButton!.trigger('click')
    await flushPromises()
    expect(api.previewAnswerProfile).toHaveBeenCalled()
    expect(dialog.text()).toContain('user-data')
  })
})

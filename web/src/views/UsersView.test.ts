import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { User } from '@/api/types'
import UsersView from './UsersView.vue'

const admin: User = {
  id: 'u1',
  username: 'admin',
  role: 'admin',
  disabled: false,
  must_change_password: false,
  created_at: '2026-11-20T10:00:00Z',
  updated_at: '2026-11-20T10:00:00Z',
}

const viewer: User = {
  id: 'u2',
  username: 'reader',
  role: 'viewer',
  disabled: false,
  must_change_password: false,
  created_at: '2026-11-20T10:00:00Z',
  updated_at: '2026-11-20T10:00:00Z',
}

async function mountView() {
  const wrapper = mount(UsersView, { global: { plugins: [ElementPlus] } })
  await flushPromises()
  return wrapper
}

describe('UsersView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listUsers).mockResolvedValue([admin, viewer])
  })

  it('loads and renders accounts', async () => {
    const wrapper = await mountView()

    expect(api.listUsers).toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin')
    expect(wrapper.text()).toContain('reader')
  })

  it('labels each role', async () => {
    const wrapper = await mountView()

    expect(wrapper.text()).toContain('Administrator')
    expect(wrapper.text()).toContain('Viewer')
  })

  it('reports a load failure', async () => {
    const error = vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)
    vi.mocked(api.listUsers).mockRejectedValueOnce(new Error('denied'))

    await mountView()

    expect(error).toHaveBeenCalled()
    error.mockRestore()
  })

  it('creates an account and reloads', async () => {
    const success = vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as never)
    vi.mocked(api.createUser).mockResolvedValue(viewer)

    const wrapper = await mountView()
    await wrapper.findAll('button').find((b) => b.text().includes('Add user'))!.trigger('click')
    await flushPromises()

    const usernameInput = wrapper.find('.el-dialog input')
    await usernameInput.setValue('newbie')
    const passwordInput = wrapper.findAll('.el-dialog input')[1]!
    await passwordInput.setValue('correct-horse-9')

    // The create dialog submits with "Create", not "Save changes" (#13 F2).
    const save = wrapper.findAll('.el-dialog button').find((b) => b.text().includes('Create'))
    expect(save?.text()).toBe('Create')
    await save!.trigger('click')
    await flushPromises()

    expect(api.createUser).toHaveBeenCalledWith(
      expect.objectContaining({ username: 'newbie', password: 'correct-horse-9', role: 'viewer' }),
    )
    expect(success).toHaveBeenCalled()
    success.mockRestore()
  })

  it('requires a password when creating', async () => {
    const error = vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)

    const wrapper = await mountView()
    await wrapper.findAll('button').find((b) => b.text().includes('Add user'))!.trigger('click')
    await flushPromises()
    await wrapper.find('.el-dialog input').setValue('onlyname')

    const save = wrapper.findAll('.el-dialog button').find((b) => b.text().includes('Create'))
    await save!.trigger('click')
    await flushPromises()

    expect(api.createUser).not.toHaveBeenCalled()
    expect(error).toHaveBeenCalled()
    error.mockRestore()
  })

  it('requires a username', async () => {
    const error = vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)

    const wrapper = await mountView()
    await wrapper.findAll('button').find((b) => b.text().includes('Add user'))!.trigger('click')
    await flushPromises()

    const save = wrapper.findAll('.el-dialog button').find((b) => b.text().includes('Create'))
    await save!.trigger('click')
    await flushPromises()

    expect(api.createUser).not.toHaveBeenCalled()
    expect(error).toHaveBeenCalled()
    error.mockRestore()
  })

  it('reports create failures', async () => {
    const error = vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)
    vi.mocked(api.createUser).mockRejectedValueOnce(new Error('conflict'))

    const wrapper = await mountView()
    await wrapper.findAll('button').find((b) => b.text().includes('Add user'))!.trigger('click')
    await flushPromises()
    await wrapper.find('.el-dialog input').setValue('dup')
    await wrapper.findAll('.el-dialog input')[1]!.setValue('correct-horse-9')

    const save = wrapper.findAll('.el-dialog button').find((b) => b.text().includes('Create'))
    await save!.trigger('click')
    await flushPromises()

    expect(error).toHaveBeenCalled()
    error.mockRestore()
  })

  it('edits an account without changing its password', async () => {
    const success = vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as never)
    vi.mocked(api.updateUser).mockResolvedValue({ ...viewer, role: 'operator' })

    const wrapper = await mountView()
    const edit = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await edit!.trigger('click')
    await flushPromises()

    const save = wrapper.findAll('.el-dialog button').find((b) => b.text().includes('Save'))
    await save!.trigger('click')
    await flushPromises()

    expect(api.updateUser).toHaveBeenCalledWith(
      'u1',
      expect.objectContaining({ password: undefined }),
    )
    expect(success).toHaveBeenCalled()
    success.mockRestore()
  })

  it('deletes an account after confirmation', async () => {
    const success = vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as never)
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm' as never)
    vi.mocked(api.deleteUser).mockResolvedValue(undefined)

    const wrapper = await mountView()
    const remove = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await remove!.trigger('click')
    await flushPromises()

    expect(api.deleteUser).toHaveBeenCalledWith('u1')
    expect(success).toHaveBeenCalled()
    success.mockRestore()
  })

  it('does nothing when deletion is cancelled', async () => {
    vi.spyOn(ElMessageBox, 'confirm').mockRejectedValue('cancel' as never)

    const wrapper = await mountView()
    const remove = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await remove!.trigger('click')
    await flushPromises()

    expect(api.deleteUser).not.toHaveBeenCalled()
  })

  it('reports deletion failures', async () => {
    const error = vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)
    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm' as never)
    vi.mocked(api.deleteUser).mockRejectedValueOnce(new Error('last admin'))

    const wrapper = await mountView()
    const remove = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    await remove!.trigger('click')
    await flushPromises()

    expect(error).toHaveBeenCalled()
    error.mockRestore()
  })
})

describe('UsersView additional paths', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listUsers).mockResolvedValue([admin, viewer])
  })

  it('marks the signed-in account', async () => {
    const { useAuthStore } = await import('@/stores/auth')
    const auth = useAuthStore()
    auth.username = 'admin'

    const wrapper = await mountView()
    expect(wrapper.text()).toContain('You')
  })

  it('toggles the disabled switch when editing', async () => {
    const success = vi.spyOn(ElMessage, 'success').mockImplementation(() => undefined as never)
    vi.mocked(api.updateUser).mockResolvedValue({ ...admin, disabled: true })

    const wrapper = await mountView()
    const edit = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await edit!.trigger('click')
    await flushPromises()

    const save = wrapper.findAll('.el-dialog button').find((b) => b.text().includes('Save'))
    await save!.trigger('click')
    await flushPromises()

    expect(api.updateUser).toHaveBeenCalled()
    expect(success).toHaveBeenCalled()
    success.mockRestore()
  })

  it('closes the dialog from the footer', async () => {
    const wrapper = await mountView()
    await wrapper.findAll('button').find((b) => b.text().includes('Add user'))!.trigger('click')
    await flushPromises()

    const cancel = wrapper.findAll('.el-dialog button').find((b) => b.text().includes('Cancel'))
    await cancel!.trigger('click')
    await flushPromises()

    expect(api.createUser).not.toHaveBeenCalled()
  })

  it('shows an empty state without accounts', async () => {
    vi.mocked(api.listUsers).mockResolvedValueOnce([])
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('No accounts yet')
  })
})

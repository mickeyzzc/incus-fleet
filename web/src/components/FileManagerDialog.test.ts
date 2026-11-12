import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import FileManagerDialog from './FileManagerDialog.vue'

import type { FileEntry } from '@/api/types'

function entry(overrides: Partial<FileEntry> = {}): FileEntry {
  return {
    name: 'app.log',
    size: 1024,
    mode: 0o644,
    is_dir: false,
    modtime: '2026-11-11T10:00:00Z',
    ...overrides,
  }
}

function mountDialog() {
  return mount(FileManagerDialog, {
    props: { modelValue: true, cluster: 'c1', instance: 'web' },
    global: { plugins: [ElementPlus] },
  })
}

describe('FileManagerDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listInstanceFiles).mockResolvedValue([
      entry({ name: 'logs', is_dir: true, size: 4096 }),
      entry(),
    ])
    vi.mocked(api.mkdirInstanceDir).mockResolvedValue(undefined)
    vi.mocked(api.deleteInstanceFile).mockResolvedValue(undefined)
    vi.mocked(api.downloadFileURL).mockImplementation(
      (cluster, instance, path) =>
        `/api/v1/clusters/${cluster}/instances/${instance}/files?path=${encodeURIComponent(path)}`,
    )
    vi.mocked(api.uploadInstanceFile).mockResolvedValue(undefined)
    vi.mocked(ElMessageBox.confirm).mockResolvedValue('confirm' as never)
    vi.stubGlobal('open', vi.fn())
  })

  it('lists the root directory on open', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    expect(api.listInstanceFiles).toHaveBeenCalledWith('c1', 'web', '/')
    expect(wrapper.text()).toContain('logs')
    expect(wrapper.text()).toContain('app.log')
  })

  it('navigates into directories', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const row = wrapper.findAll('.el-table__row').find((item) => item.text().includes('logs'))
    await row?.findAll('button').find((b) => b.text().includes('logs'))?.trigger('click')
    await flushPromises()

    expect(api.listInstanceFiles).toHaveBeenLastCalledWith('c1', 'web', '/logs')
  })

  it('creates folders', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('New folder'))?.trigger('click')
    await wrapper.findAll('.folder-form input')[0]?.setValue('assets')
    await wrapper.findAll('.folder-form button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.mkdirInstanceDir).toHaveBeenCalledWith('c1', 'web', '/assets')
    expect(ElMessage.success).toHaveBeenCalledWith('Folder created.')
  })

  it('requires a folder name', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text().includes('New folder'))?.trigger('click')
    await wrapper.findAll('.folder-form button').at(-1)?.trigger('click')

    expect(api.mkdirInstanceDir).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Enter a folder name.')
  })

  it('downloads files through the authenticated URL', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const row = wrapper.findAll('.el-table__row').find((item) => item.text().includes('app.log'))
    await row?.findAll('button').find((b) => b.text().includes('Download'))?.trigger('click')

    expect(window.open).toHaveBeenCalledWith(
      '/api/v1/clusters/c1/instances/web/files?path=%2Fapp.log',
    )
  })

  it('deletes files after confirmation', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const row = wrapper.findAll('.el-table__row').find((item) => item.text().includes('app.log'))
    await row?.findAll('button').find((b) => b.text().includes('Delete'))?.trigger('click')
    await flushPromises()

    expect(api.deleteInstanceFile).toHaveBeenCalledWith('c1', 'web', '/app.log')
    expect(ElMessage.success).toHaveBeenCalledWith('app.log deleted.')
  })

  it('navigates via breadcrumbs', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    // Enter the directory first so a deeper crumb exists.
    const row = wrapper.findAll('.el-table__row').find((item) => item.text().includes('logs'))
    await row?.findAll('button').find((b) => b.text().includes('logs'))?.trigger('click')
    await flushPromises()

    // The dialog only mounts after entering, so assert the deeper call then
    // navigate home through the store path the crumbs drive.
    expect(api.listInstanceFiles).toHaveBeenLastCalledWith('c1', 'web', '/logs')

    const home = wrapper.findAll('.el-breadcrumb__item .el-link')
    expect(home.length).toBeGreaterThanOrEqual(2)
    await home[0]?.trigger('click')
    await flushPromises()

    // The root listing is cached from the initial load, so no extra fetch
    // happens; the cached entries render again.
    expect(api.listInstanceFiles).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('logs')
  })

  it('opens the hidden upload input on demand', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    // The hidden input exists without inline styles in jsdom; clicking the
    // toolbar button must not throw and keeps the input mounted.
    expect(wrapper.find('input[type="file"]').exists()).toBe(true)

    const upload = wrapper.findAll('button').find((b) => b.text().includes('Upload file'))
    await upload?.trigger('click')
    await flushPromises()

    expect(wrapper.find('input[type="file"]').exists()).toBe(true)
  })

  it('uploads files into the current directory', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const input = wrapper.find('input[type="file"]')
    const file = new File(['welcome'], 'motd', { type: 'text/plain' })
    Object.defineProperty(input.element, 'files', { value: [file] })
    await input.trigger('change')
    await flushPromises()

    expect(api.uploadInstanceFile).toHaveBeenCalledWith('c1', 'web', '/motd', file)
    expect(ElMessage.success).toHaveBeenCalledWith('File uploaded.')
    expect(api.listInstanceFiles).toHaveBeenCalledTimes(2)
  })
})

import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '@/api'
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

    await wrapper.findAll('button').find((b) => b.text() === 'Add key')!.trigger('click')
    const dialog = wrapper.findAllComponents({ name: 'ElDialog' }).find((d) => d.props('title') === 'Add key')!
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

    const edit = wrapper.findAll('button').find((b) => b.text() === 'Edit')!
    await edit.trigger('click')
    await flushPromises()

    const dialog = wrapper.findAllComponents({ name: 'ElDialog' }).find((d) => d.props('title') === 'Edit key')!
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

    await wrapper.findAll('button').find((b) => b.text() === 'Delete')!.trigger('click')
    await flushPromises()

    expect(ElMessageBox.confirm).toHaveBeenCalled()
    expect(api.deleteKey).toHaveBeenCalledWith('key-1')
    expect(useAssetsStore().keyCache).toHaveLength(0)
  })

  it('reports a failed key deletion instead of swallowing it', async () => {
    const error = vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)
    vi.mocked(api.deleteKey).mockRejectedValueOnce(new Error('permission denied'))
    const wrapper = await mountView()

    await wrapper.findAll('button').find((b) => b.text() === 'Delete')!.trigger('click')
    await flushPromises()

    // The old `.catch(() => {})` hid real API failures (#12).
    expect(error).toHaveBeenCalledWith('Delete failed: permission denied')
    expect(useAssetsStore().keyCache).toHaveLength(1)
    error.mockRestore()
  })

  it('creates a script through the dialog', async () => {
    const wrapper = await mountView()

    await wrapper.findAll('button').find((b) => b.text() === 'Add script')!.trigger('click')
    const dialog = wrapper.findAllComponents({ name: 'ElDialog' }).find((d) => d.props('title') === 'Add script')!
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

    await wrapper.findAll('button').find((b) => b.text() === 'Add key')!.trigger('click')
    const dialog = wrapper.findAllComponents({ name: 'ElDialog' }).find((d) => d.props('title') === 'Add key')!
    await dialog.findAll('input')[0]!.setValue('dup')
    await dialog.find('textarea').setValue('ssh-ed25519 CCC dup')
    await dialog.findAll('button').at(-1)!.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalled()
  })
})

describe('AssetsView fleet-wide images and ISOs', () => {
  const imageFixture = {
    cluster: 'c1',
    fingerprint: 'fp-1234567890abcdef',
    alias: 'debian/13',
    type: 'container',
    os: 'debian',
    release: 'trixie',
    size: 1024 * 1024,
    cached: false,
    created_at: '2026-11-16T00:00:00Z',
  }
  const isoFixture = { name: 'debian.iso', pool: 'default', size: '3771187200', cluster: 'c1' }
  const smallIsoFixture = { name: 'tiny.iso', pool: 'default', size: '63963136', cluster: 'c2' }
  const brokenIsoFixture = { name: 'broken.iso', pool: 'default', size: '', cluster: 'c2' }

  beforeEach(() => {
    vi.mocked(api.listClusters).mockResolvedValue([
      { name: 'c1', display_name: 'C1', online: true },
      { name: 'c2', display_name: 'C2', online: true },
    ] as never)
    vi.mocked(api.listAllImagesWithErrors).mockResolvedValue({ items: [imageFixture], failedClusters: [] })
    vi.mocked(api.listAllISOsWithErrors).mockResolvedValue({
      items: [isoFixture, smallIsoFixture, brokenIsoFixture],
      failedClusters: [],
    })
    vi.mocked(api.copyImage).mockResolvedValue(undefined)
    vi.mocked(api.deleteISO).mockResolvedValue(undefined)
    vi.mocked(api.importISOWithProgress).mockResolvedValue({ name: 'debian.iso', pool: 'default' })
  })

  it('renders images and ISOs from every cluster with cluster tags', async () => {
    const wrapper = await mountView()

    expect(api.listAllImagesWithErrors).toHaveBeenCalled()
    expect(wrapper.text()).toContain('debian/13')
    expect(wrapper.text()).toContain('fp-123456789…')
    expect(wrapper.text()).toContain('debian.iso')
  })

  it('formats ISO sizes as human-readable byte counts', async () => {
    const wrapper = await mountView()

    // Raw volume-config byte strings render as formatted sizes, not raw
    // numbers (#13 F4).
    expect(wrapper.text()).toContain('3.5 GiB')
    expect(wrapper.text()).toContain('61.0 MiB')
    expect(wrapper.text()).not.toContain('3771187200')

    // A missing or unparseable size falls back to the unknown placeholder.
    const unknownRow = wrapper.findAll('.el-table__row').find((row) => row.text().includes('broken.iso'))
    expect(unknownRow?.text()).toContain('Unknown')
  })

  it('copies an image to a target cluster through the distribute dialog', async () => {
    // Real select: the global ElSelect stub does not emit selection updates.
    const wrapper = mount(AssetsView, {
      props: { modelValue: undefined },
      global: {
        plugins: [ElementPlus],
        stubs: { ElSelect: false, ElOption: false, teleport: false },
      },
      attachTo: document.body,
    })
    await flushPromises()

    const distribute = wrapper.findAll('button').find((b) => b.text() === 'Distribute')!
    await distribute.trigger('click')
    await flushPromises()

    const wrapperEl = wrapper.find('.el-select__wrapper')
    await wrapperEl.trigger('click')
    await flushPromises()
    const option = [...document.querySelectorAll('.el-select-dropdown__item')].find((o) => o.textContent?.trim() === 'C2')!
    option.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()

    const dialog = wrapper.findAllComponents({ name: 'ElDialog' }).find((d) => d.text().includes('Target cluster'))!
    await dialog.findAll('button').find((b) => b.text() === 'Distribute')!.trigger('click')
    await flushPromises()

    expect(api.copyImage).toHaveBeenCalledWith('c1', 'fp-1234567890abcdef', {
      target_cluster: 'c2',
      alias: 'debian/13',
    })

    wrapper.unmount()
  })

  it('deletes an ISO after confirmation', async () => {
    const wrapper = await mountView()

    const isoRow = wrapper.findAll('.el-table__row').find((row) => row.text().includes('debian.iso'))
    await isoRow?.findAll('button').find((b) => b.text() === 'Delete')!.trigger('click')
    await flushPromises()

    expect(api.deleteISO).toHaveBeenCalledWith('c1', 'default', 'debian.iso')
  })

  it('explains how to delete an ISO that is still attached to an instance', async () => {
    vi.mocked(api.deleteISO).mockRejectedValueOnce(
      new ApiError(409, 'in_use', 'iso "debian.iso": still attached'),
    )
    const wrapper = await mountView()

    const isoRow = wrapper.findAll('.el-table__row').find((row) => row.text().includes('debian.iso'))
    await isoRow?.findAll('button').find((b) => b.text() === 'Delete')!.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith(
      'This ISO is still attached to an instance. Detach it from the instance (Devices → ISO) and try again.',
    )
  })

  it('opens the tab requested in the URL and updates it when switching', async () => {
    window.history.replaceState(null, '', '/assets?tab=answers')
    const wrapper = await mountView()

    expect(wrapper.find('.el-tabs__item.is-active').text()).toBe('Answer profiles')

    await wrapper.findAll('.el-tabs__item').find((tab) => tab.text() === 'Scripts')!.trigger('click')
    await flushPromises()
    expect(window.location.search).toBe('?tab=scripts')

    window.history.replaceState(null, '', '/assets')
  })

  it('labels partial inventory failures instead of hiding them', async () => {
    vi.mocked(api.listAllImagesWithErrors).mockResolvedValue({
      items: [imageFixture],
      failedClusters: ['c2'],
    })
    vi.mocked(api.listAllISOsWithErrors).mockResolvedValue({
      items: [isoFixture],
      failedClusters: ['c9'],
    })

    const wrapper = await mountView()

    expect(wrapper.text()).toContain('c2')
    expect(wrapper.text()).toContain('c9')
    expect(wrapper.text()).toContain('could not be loaded')
  })

  it('shows inventory errors when every cluster fails', async () => {
    vi.mocked(api.listAllImagesWithErrors).mockRejectedValue(new Error('images down'))
    vi.mocked(api.listAllISOsWithErrors).mockRejectedValue(new Error('isos down'))

    const wrapper = await mountView()

    expect(wrapper.text()).toContain('images down')
    expect(wrapper.text()).toContain('isos down')
  })

  it('keeps the copy action disabled until a target is chosen', async () => {
    const wrapper = await mountView()

    await wrapper.findAll('button').find((b) => b.text() === 'Distribute')!.trigger('click')
    await flushPromises()
    const dialog = wrapper.findAllComponents({ name: 'ElDialog' }).find((d) => d.text().includes('Target cluster'))!
    const submit = dialog.findAll('button').find((b) => b.text() === 'Distribute')!

    expect(submit.attributes('disabled')).toBeDefined()
    expect(api.copyImage).not.toHaveBeenCalled()
  })

  it('uploads an ISO through the background upload task with progress', async () => {
    const wrapper = await mountView()

    // open the upload dialog
    const uploadButton = wrapper.findAll('button').find((b) => b.text() === 'Upload ISO')!
    await uploadButton.trigger('click')
    await flushPromises()

    // pick a file through the file input
    const input = wrapper.find('input[type="file"]')
    expect(input.exists()).toBe(true)
    const file = new File(['iso-bytes'], 'debian.iso', { type: 'application/octet-stream' })
    Object.defineProperty(input.element, 'files', { value: [file] })
    await input.trigger('change')
    await flushPromises()

    expect(api.importISOWithProgress).toHaveBeenCalledWith(
      'c1',
      { file, pool: 'default' },
      expect.any(Function),
      expect.any(AbortSignal),
    )
  })
})

  it('deletes an image after confirmation', async () => {
    const wrapper = await mountView()
    vi.mocked(api.deleteImage).mockResolvedValue(undefined)

    const imageRow = wrapper.findAll('.el-table__row').find((r) => r.text().includes('debian/13'))!
    await imageRow.findAll('button').find((b) => b.text() === 'Delete')!.trigger('click')
    await flushPromises()

    expect(api.deleteImage).toHaveBeenCalledWith('c1', 'fp-1234567890abcdef')
  })

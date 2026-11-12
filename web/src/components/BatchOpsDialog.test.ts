import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import BatchOpsDialog from './BatchOpsDialog.vue'

const targets = [
  { cluster: 'c1', instance: 'web' },
  { cluster: 'c2', instance: 'db' },
]

function mountDialog(mode: 'keys' | 'script') {
  return mount(BatchOpsDialog, {
    props: { modelValue: true, mode, targets },
    global: { plugins: [ElementPlus] },
  })
}

describe('BatchOpsDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listKeys).mockResolvedValue([
      { id: 'k1', name: 'desk', public_key: 'ssh-ed25519 AAA', created_at: '2026-01-01T00:00:00Z' },
    ])
    vi.mocked(api.listScripts).mockResolvedValue([
      { id: 's1', name: 'init', body: '#!/bin/sh', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
    ])
    vi.mocked(api.batchKeys).mockResolvedValue([
      { cluster: 'c1', instance: 'web', ok: true },
      { cluster: 'c2', instance: 'db', ok: false, error: 'agent down' },
    ])
    vi.mocked(api.batchScripts).mockResolvedValue([
      { cluster: 'c1', instance: 'web', ok: true },
      { cluster: 'c2', instance: 'db', ok: true },
    ])
  })

  it('requires keys before running the injection', async () => {
    const wrapper = mountDialog('keys')
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')

    expect(api.batchKeys).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith('Choose at least one key.')
  })

  it('injects selected keys into every target', async () => {
    const wrapper = mountDialog('keys')
    await flushPromises()

    const select = wrapper.findComponent({ name: 'ElSelect' })
    select.vm.$emit('update:modelValue', ['k1'])
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.batchKeys).toHaveBeenCalledWith({
      targets: [
        { cluster: 'c1', instance: 'web', user: 'root' },
        { cluster: 'c2', instance: 'db', user: 'root' },
      ],
      key_ids: ['k1'],
    })
    expect(ElMessage.warning).toHaveBeenCalledWith('Batch finished: 1 succeeded, 1 failed.')
  })

  it('runs a stored script on every target', async () => {
    const wrapper = mountDialog('script')
    await flushPromises()

    const select = wrapper.findComponent({ name: 'ElSelect' })
    select.vm.$emit('update:modelValue', 's1')
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.batchScripts).toHaveBeenCalledWith({
      targets,
      script_id: 's1',
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Batch finished: 2 succeeded.')
    expect(wrapper.emitted('done')).toHaveLength(1)
  })
})

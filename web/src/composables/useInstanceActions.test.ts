import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import type { MessageBoxData } from 'element-plus'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

import { api } from '@/api'
import { instanceSummary } from '@/test/fixtures'
import { useInstanceActions } from './useInstanceActions'

function withSetup<T>(useComposable: (refresh: () => Promise<void>) => T): {
  result: T
  refresh: ReturnType<typeof vi.fn>
} {
  const refresh = vi.fn().mockResolvedValue(undefined)
  let result: T | undefined

  const TestHost = defineComponent({
    setup() {
      result = useComposable(refresh)
      return () => h('div')
    },
  })

  mount(TestHost, { global: { plugins: [ElementPlus] } })
  return { result: result as T, refresh }
}

describe('useInstanceActions', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.instanceAction).mockResolvedValue(undefined)
    vi.mocked(api.deleteInstance).mockResolvedValue(undefined)
    vi.mocked(ElMessageBox.confirm).mockResolvedValue('confirm' as MessageBoxData)
  })

  it('runs start without a confirmation and refreshes', async () => {
    const { result, refresh } = withSetup((refresh) => useInstanceActions(refresh))
    const instance = instanceSummary()

    await result.act(instance, 'start')

    expect(ElMessageBox.confirm).not.toHaveBeenCalled()
    expect(api.instanceAction).toHaveBeenCalledWith('c1', 'web', { action: 'start', force: false })
    expect(ElMessage.success).toHaveBeenCalledWith('Started web.')
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(result.isBusy(instance)).toBe(false)
  })

  it('stops only after confirmation', async () => {
    const { result, refresh } = withSetup((refresh) => useInstanceActions(refresh))
    const instance = instanceSummary()

    vi.mocked(ElMessageBox.confirm).mockRejectedValueOnce('cancel')
    await result.act(instance, 'stop')

    expect(api.instanceAction).not.toHaveBeenCalled()
    expect(refresh).not.toHaveBeenCalled()

    await result.act(instance, 'stop')

    expect(api.instanceAction).toHaveBeenCalledWith('c1', 'web', { action: 'stop', force: false })
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('retries stop with force when the first attempt times out', async () => {
    const { result, refresh } = withSetup((refresh) => useInstanceActions(refresh))
    const instance = instanceSummary()

    vi.mocked(api.instanceAction)
      .mockRejectedValueOnce(new Error('Instance is busy: timed out'))
      .mockResolvedValueOnce(undefined)

    await result.act(instance, 'stop')
    await flushPromises()

    expect(api.instanceAction).toHaveBeenNthCalledWith(1, 'c1', 'web', {
      action: 'stop',
      force: false,
    })
    expect(api.instanceAction).toHaveBeenNthCalledWith(2, 'c1', 'web', {
      action: 'stop',
      force: true,
    })
    expect(ElMessageBox.confirm).toHaveBeenCalledTimes(2)
    expect(ElMessage.success).toHaveBeenCalledWith('Stopped web.')
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('keeps the error when force retry is declined', async () => {
    const { result } = withSetup((refresh) => useInstanceActions(refresh))
    const instance = instanceSummary()

    vi.mocked(api.instanceAction).mockRejectedValueOnce(new Error('stop timed out'))
    vi.mocked(ElMessageBox.confirm)
      .mockResolvedValueOnce('confirm' as MessageBoxData)
      .mockRejectedValueOnce('cancel')

    await result.act(instance, 'stop')
    await flushPromises()

    expect(api.instanceAction).toHaveBeenCalledTimes(1)
    expect(ElMessage.error).toHaveBeenCalledWith(
      'Action failed: stop timed out',
    )
  })

  it('deletes after the dialog confirms and clears busy state', async () => {
    const { result, refresh } = withSetup((refresh) => useInstanceActions(refresh))
    const instance = instanceSummary()

    // confirmDelete only arms the dialog; nothing is sent until submit.
    result.confirmDelete(instance)
    await flushPromises()
    expect(api.deleteInstance).not.toHaveBeenCalled()
    expect(result.deleteTarget.value).toEqual(instance)

    result.deleteVolumes.value = true
    await result.submitDelete()
    await flushPromises()

    expect(api.deleteInstance).toHaveBeenCalledWith('c1', 'web', true, true)
    expect(ElMessage.success).toHaveBeenCalledWith('Deleted web.')
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(result.isBusy(instance)).toBe(false)
    expect(result.deleteTarget.value).toBeNull()
  })

  it('cancelling the delete dialog sends nothing', async () => {
    const { result, refresh } = withSetup((refresh) => useInstanceActions(refresh))
    const instance = instanceSummary()

    result.confirmDelete(instance)
    result.cancelDelete()
    await result.submitDelete()
    await flushPromises()

    expect(api.deleteInstance).not.toHaveBeenCalled()
    expect(refresh).not.toHaveBeenCalled()
  })
})

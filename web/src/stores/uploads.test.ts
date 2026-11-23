import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { useUploadsStore } from './uploads'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('uploads store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.importISOWithProgress).mockReset()
  })

  it('tracks an upload until it completes', async () => {
    const pending = deferred<{ name: string; pool: string }>()
    vi.mocked(api.importISOWithProgress).mockReturnValue(pending.promise)
    const store = useUploadsStore()
    const file = new File(['abc'], 'debian.iso')

    const done = store.startISO('c1', 'default', file)
    expect(store.task).toMatchObject({ name: 'debian.iso', cluster: 'c1', loaded: 0, total: 3 })
    expect(store.active).toBe(true)
    expect(api.importISOWithProgress).toHaveBeenCalledWith(
      'c1',
      { file, pool: 'default' },
      expect.any(Function),
      expect.any(AbortSignal),
    )

    const onProgress = vi.mocked(api.importISOWithProgress).mock.calls[0]![2]
    onProgress(2, 3)
    expect(store.percent).toBe(67)

    pending.resolve({ name: 'debian.iso', pool: 'default' })
    await expect(done).resolves.toBe(true)
    expect(store.task).toBeNull()
    expect(store.active).toBe(false)
  })

  it('surfaces a failed upload', async () => {
    vi.mocked(api.importISOWithProgress).mockRejectedValue(new Error('network down'))
    const store = useUploadsStore()

    await expect(store.startISO('c1', '', new File(['x'], 'x.iso'))).resolves.toBe(false)
    expect(store.task?.error).toContain('network down')
    store.dismiss()
    expect(store.task).toBeNull()
  })

  it('computes transfer speed and ETA from progress samples', async () => {
    const now = vi.spyOn(performance, 'now')
    now.mockReturnValueOnce(0).mockReturnValue(1000)
    const pending = deferred<{ name: string; pool: string }>()
    vi.mocked(api.importISOWithProgress).mockReturnValue(pending.promise)
    const store = useUploadsStore()

    const done = store.startISO('c1', 'default', new File(['x'.repeat(1500)], 'big.iso'))
    const onProgress = vi.mocked(api.importISOWithProgress).mock.calls[0]![2]
    onProgress(500, 1500)

    expect(store.task?.speed).toBe(500)
    expect(store.etaSeconds).toBe(2)
    expect(store.percent).toBe(33)

    pending.resolve({ name: 'big.iso', pool: 'default' })
    await done
    now.mockRestore()
  })

  it('clears the card when the request aborts', async () => {
    vi.mocked(api.importISOWithProgress).mockImplementation(
      (_cluster, _request, _onProgress, signal) =>
        new Promise((_resolve, reject) => {
          signal?.addEventListener('abort', () => reject(new Error('aborted')))
        }),
    )
    const store = useUploadsStore()

    const done = store.startISO('c1', 'default', new File(['x'], 'x.iso'))
    store.cancel()

    await expect(done).resolves.toBe(false)
    expect(store.task).toBeNull()
  })

  it('cancels the active upload and aborts the request', async () => {
    const pending = deferred<{ name: string; pool: string }>()
    vi.mocked(api.importISOWithProgress).mockReturnValue(pending.promise)
    const store = useUploadsStore()

    void store.startISO('c1', 'default', new File(['abc'], 'debian.iso'))
    const signal = vi.mocked(api.importISOWithProgress).mock.calls[0]![3]!
    store.cancel()

    expect(signal.aborted).toBe(true)
    expect(store.task).toBeNull()
    expect(store.minimized).toBe(false)
  })

  it('dismisses a finished card without losing the store state model', () => {
    const store = useUploadsStore()
    store.minimized = true
    store.dismiss()
    expect(store.task).toBeNull()
    expect(store.minimized).toBe(false)
  })
})

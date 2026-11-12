import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import { useAssetsStore } from '@/stores/assets'

describe('assets store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('creates, updates, and deletes keys', async () => {
    vi.mocked(api.createKey).mockResolvedValue({ id: 'k1', name: 'desk', public_key: 'ssh-ed25519 AAA', created_at: '2026-01-01T00:00:00Z' })
    vi.mocked(api.listKeys).mockResolvedValue([{ id: 'k1', name: 'desk', public_key: 'ssh-ed25519 AAA', created_at: '2026-01-01T00:00:00Z' }])
    vi.mocked(api.updateKey).mockResolvedValue({ id: 'k1', name: 'desk2', public_key: 'ssh-ed25519 BBB', created_at: '2026-01-01T00:00:00Z' })
    vi.mocked(api.deleteKey).mockResolvedValue(undefined)
    const store = useAssetsStore()

    await store.createKey({ name: 'desk', public_key: 'ssh-ed25519 AAA' })
    expect(store.keyCache).toHaveLength(1)

    await store.updateKey('k1', { name: 'desk2', public_key: 'ssh-ed25519 BBB' })
    expect(api.updateKey).toHaveBeenCalledWith('k1', { name: 'desk2', public_key: 'ssh-ed25519 BBB' })

    await store.deleteKey('k1')
    expect(store.keyCache).toHaveLength(0)
  })

  it('runs batch operations and keeps the last result', async () => {
    vi.mocked(api.batchKeys).mockResolvedValue([
      { cluster: 'c1', instance: 'web', ok: true },
      { cluster: 'c2', instance: 'db', ok: false, error: 'agent down' },
    ])
    vi.mocked(api.batchScripts).mockResolvedValue([{ cluster: 'c1', instance: 'web', ok: true }])
    vi.mocked(api.batchPower).mockResolvedValue([{ cluster: 'c1', instance: 'web', ok: true }])
    const store = useAssetsStore()

    const targets = [
      { cluster: 'c1', instance: 'web', user: 'root' },
      { cluster: 'c2', instance: 'db', user: 'deploy' },
    ]
    const keyItems = await store.batchInjectKeys(targets, ['k1'])
    expect(keyItems).toHaveLength(2)
    expect(store.lastBatch).toHaveLength(2)

    const scriptItems = await store.batchRunScript(targets, 's1')
    expect(scriptItems.every((item) => item.ok)).toBe(true)

    const powerItems = await store.batchPower(targets, 'stop', true)
    expect(powerItems).toHaveLength(1)
    expect(api.batchPower).toHaveBeenCalledWith({
      targets,
      action: 'stop',
      force: true,
    })
  })

  it('surfaces batch failures through error state', async () => {
    vi.mocked(api.batchKeys).mockRejectedValue(new Error('denied'))
    const store = useAssetsStore()

    await expect(store.batchInjectKeys([], ['k1'])).rejects.toThrow('denied')
    expect(store.error).toContain('denied')
  })
})

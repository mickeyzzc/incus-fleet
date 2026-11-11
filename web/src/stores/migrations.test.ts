import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { MigrationRequest } from '@/api/types'
import { migration } from '@/test/fixtures'
import { useMigrationsStore } from './migrations'

const request: MigrationRequest = {
  source_cluster: 'c1',
  target_cluster: 'c2',
  instance: 'web',
  mode: 'move',
  live: false,
  include_snapshots: false,
  auto_start: true,
}

describe('migrations store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('fetches migrations', async () => {
    vi.mocked(api.listMigrations).mockResolvedValue([migration()])
    const store = useMigrationsStore()

    await store.fetchAll(10)

    expect(api.listMigrations).toHaveBeenCalledWith(10)
    expect(store.items).toHaveLength(1)
    expect(store.lastUpdated).toBeInstanceOf(Date)
    expect(store.loading).toBe(false)
  })

  it('records list errors', async () => {
    vi.mocked(api.listMigrations).mockRejectedValue(new Error('store down'))
    const store = useMigrationsStore()

    await store.fetchAll()

    expect(store.error).toBe('store down')
    expect(store.loading).toBe(false)
  })

  it('submits a migration and prepends it without duplicates', async () => {
    vi.mocked(api.submitMigration).mockResolvedValue(migration({ id: 'new', status: 'pending' }))
    const store = useMigrationsStore()
    store.items = [migration({ id: 'new', status: 'failed' }), migration({ id: 'old' })]

    const result = await store.submit(request)

    expect(api.submitMigration).toHaveBeenCalledWith(request)
    expect(result.id).toBe('new')
    expect(store.items.map((item) => item.id)).toEqual(['new', 'old'])
    expect(store.submitting).toBe(false)
    expect(store.active.map((item) => item.id)).toEqual(['new'])
    expect(store.hasActive).toBe(true)
  })

  it('records submit errors and rethrows', async () => {
    vi.mocked(api.submitMigration).mockRejectedValue(new Error('conflict'))
    const store = useMigrationsStore()

    await expect(store.submit(request)).rejects.toThrow('conflict')

    expect(store.error).toBe('conflict')
    expect(store.submitting).toBe(false)
  })

  it('refreshes an existing migration in place', async () => {
    vi.mocked(api.getMigration).mockResolvedValue(migration({ status: 'done' }))
    const store = useMigrationsStore()
    store.items = [migration({ status: 'transferring' })]

    const result = await store.refresh(migration().id)

    expect(result?.status).toBe('done')
    expect(store.items[0]?.status).toBe('done')
  })

  it('adds a migration returned by refresh when missing', async () => {
    vi.mocked(api.getMigration).mockResolvedValue(migration({ id: 'fresh' }))
    const store = useMigrationsStore()

    const result = await store.refresh('fresh')

    expect(result?.id).toBe('fresh')
    expect(store.items[0]?.id).toBe('fresh')
  })

  it('records refresh errors and returns null', async () => {
    vi.mocked(api.getMigration).mockRejectedValue(new Error('gone'))
    const store = useMigrationsStore()

    await expect(store.refresh('missing')).resolves.toBeNull()

    expect(store.error).toBe('gone')
  })

  it('polls while active and stops once everything finishes', async () => {
    vi.useFakeTimers()
    vi.mocked(api.listMigrations).mockResolvedValue([migration({ status: 'transferring' })])
    const store = useMigrationsStore()
    await store.fetchAll()

    expect(store.hasActive).toBe(true)

    store.startPolling(1000)
    store.startPolling(1000)
    expect(vi.getTimerCount()).toBe(1)

    await vi.advanceTimersByTimeAsync(1000)
    expect(api.listMigrations).toHaveBeenCalledTimes(2)

    vi.mocked(api.listMigrations).mockResolvedValue([migration({ status: 'done' })])
    await vi.advanceTimersByTimeAsync(1000)
    expect(api.listMigrations).toHaveBeenCalledTimes(3)

    await vi.advanceTimersByTimeAsync(3000)
    expect(api.listMigrations).toHaveBeenCalledTimes(3)
    expect(vi.getTimerCount()).toBe(0)

    store.stopPolling()
    store.stopPolling()
  })

  it('does not poll when no migration is active', async () => {
    vi.useFakeTimers()
    vi.mocked(api.listMigrations).mockResolvedValue([migration({ status: 'done' })])
    const store = useMigrationsStore()
    await store.fetchAll()

    store.startPolling(500)
    await vi.advanceTimersByTimeAsync(500)

    expect(api.listMigrations).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })
})

import { beforeEach, describe, expect, it } from 'vitest'

import type { PluginEntry, PluginInstallTaskStatus } from '@/api/types'
import i18n, { setLocale } from '@/i18n'
import {
  applyPluginTaskStatus,
  INSTALL_STEP_DEFS,
  PLUGIN_STATUSES,
  pluginDot,
  pluginInstallState,
  pluginStatus,
  type PluginInstallTask,
} from './model'

function entry(overrides: Partial<PluginEntry> = {}): PluginEntry {
  return {
    id: 'vm',
    name: 'VictoriaMetrics',
    version: '1.0.0',
    enabled: true,
    upstream: 'http://vm.example:8428',
    role: 'viewer',
    kind: 'external',
    ui: { path: '/plugins/vm', title_key: 'plugins.vm' },
    ...overrides,
  }
}

describe('plugin status vocabulary', () => {
  beforeEach(() => setLocale('en'))

  it('uses exactly five product-wide states with i18n labels', () => {
    expect(PLUGIN_STATUSES).toEqual(['installing', 'healthy', 'degraded', 'stopped', 'failed'])
    for (const status of PLUGIN_STATUSES) {
      expect(pluginStatus(entry({ status })).labelKey).toBe(`plugins.status.${status}`)
    }
  })

  it('infers stopped, degraded and healthy from the descriptor', () => {
    expect(pluginStatus(entry({ enabled: false })).status).toBe('stopped')
    expect(pluginStatus(entry({ upstream: undefined })).status).toBe('degraded')
    expect(pluginStatus(entry()).status).toBe('healthy')
  })

  it('resolves details through i18n, including the current locale', () => {
    expect(pluginStatus(entry({ status: 'healthy' })).detail).toContain('vm.example:8428')

    setLocale('zh-CN')
    expect(pluginStatus(entry({ status: 'healthy' })).detail).toContain('vm.example:8428')
    expect(i18n.global.t('plugins.status.healthy')).toBe('健康')
  })

  it('keeps a distinct dot colour per state', () => {
    const dots = PLUGIN_STATUSES.map((status) => pluginDot(status))
    expect(new Set(dots).size).toBe(PLUGIN_STATUSES.length)
  })

  it('defines the install task steps that the panel renders', () => {
    expect(INSTALL_STEP_DEFS.map((step) => step.key)).toEqual([
      'validate', 'placement', 'deploy', 'health', 'wiring',
    ])
  })
})

function runningTask(): PluginInstallTask {
  return {
    pluginId: 'vm',
    pluginName: 'VictoriaMetrics',
    taskId: 'task-1',
    state: 'running',
    currentStep: 0,
    steps: INSTALL_STEP_DEFS.map((step) => ({ ...step, state: 'pending' as const })),
    error: '',
    token: '',
    request: { id: 'vm' },
  }
}

function serverTask(overrides: Partial<PluginInstallTaskStatus> = {}): PluginInstallTaskStatus {
  return {
    id: 'task-1',
    plugin_id: 'vm',
    state: 'running',
    attempt: 1,
    created_at: '2026-11-27T10:00:00Z',
    updated_at: '2026-11-27T10:00:00Z',
    ...overrides,
  }
}

describe('install task status mapping', () => {
  it('folds queued, dispatched and running into one running state', () => {
    for (const state of ['queued', 'dispatched', 'accepted', 'running']) {
      expect(pluginInstallState(state)).toBe('running')
    }
    expect(pluginInstallState('succeeded')).toBe('succeeded')
    expect(pluginInstallState('failed')).toBe('failed')
    expect(pluginInstallState('canceled')).toBe('canceled')
    expect(pluginInstallState('something-new')).toBe('running')
  })

  it('maps the server checkpoint onto the five visible steps', () => {
    const updated = applyPluginTaskStatus(runningTask(), serverTask({
      progress: JSON.stringify({ step: 6 }),
    }))

    expect(updated.state).toBe('running')
    expect(updated.progress).toBe(60)
    expect(updated.steps.map((step) => step.state)).toEqual([
      'done', 'done', 'running', 'pending', 'pending',
    ])
    expect(updated.currentStep).toBe(2)
  })

  it('marks the failing step and keeps the server error text', () => {
    const updated = applyPluginTaskStatus(runningTask(), serverTask({
      state: 'failed',
      progress: JSON.stringify({ step: 7 }),
      error: 'health check failed',
    }))

    expect(updated.state).toBe('failed')
    expect(updated.error).toBe('health check failed')
    expect(updated.steps.map((step) => step.state)).toEqual([
      'done', 'done', 'done', 'failed', 'pending',
    ])
  })

  it('marks a canceled task without a red failure step', () => {
    const updated = applyPluginTaskStatus(runningTask(), serverTask({
      state: 'canceled', progress: JSON.stringify({ step: 2 }),
    }))

    expect(updated.state).toBe('canceled')
    expect(updated.progress).toBe(20)
    expect(updated.steps.map((step) => step.state)).toEqual([
      'done', 'done', 'pending', 'pending', 'pending',
    ])
  })

  it('completes every step for a succeeded task with no checkpoint', () => {
    const updated = applyPluginTaskStatus(runningTask(), serverTask({ state: 'succeeded' }))
    expect(updated.progress).toBe(100)
    expect(updated.steps.every((step) => step.state === 'done')).toBe(true)
    expect(updated.currentStep).toBe(INSTALL_STEP_DEFS.length)
  })

  it('ignores unknown progress payloads and keeps the task identity', () => {
    const original = runningTask()
    original.token = 'one-time-token'
    const updated = applyPluginTaskStatus(original, serverTask({
      progress: 'not json', state: 'dispatched', attempt: 3,
    }))

    expect(updated.progress).toBeUndefined()
    expect(updated.taskId).toBe('task-1')
    expect(updated.token).toBe('one-time-token')
    expect(updated.attempt).toBe(3)
    expect(updated.steps.every((step) => step.state === 'pending')).toBe(true)
  })
})

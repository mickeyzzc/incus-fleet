import { beforeEach, describe, expect, it } from 'vitest'

import type { PluginEntry } from '@/api/types'
import i18n, { setLocale } from '@/i18n'
import {
  INSTALL_STEP_DEFS,
  PLUGIN_STATUSES,
  pluginDot,
  pluginStatus,
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

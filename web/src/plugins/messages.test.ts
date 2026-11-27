import { describe, expect, it, vi } from 'vitest'

import i18n, { setLocale } from '@/i18n'
import { PLUGIN_MESSAGE_ROOT, humanText, mergePluginMessages, pluginMessagesFor } from './messages'

describe('plugin i18n merge', () => {
  it('merges both locales under an isolated namespace', () => {
    mergePluginMessages('msg-one', {
      en: { title: 'Metrics' },
      'zh-CN': { title: '指标' },
    })

    setLocale('en')
    expect(i18n.global.t(`${PLUGIN_MESSAGE_ROOT}.msg-one.title`)).toBe('Metrics')
    setLocale('zh-CN')
    expect(i18n.global.t(`${PLUGIN_MESSAGE_ROOT}.msg-one.title`)).toBe('指标')
  })

  it('falls back to English when a Chinese plugin string is missing', () => {
    mergePluginMessages('msg-two', { en: { title: 'Logs' } })

    setLocale('zh-CN')
    expect(i18n.global.t(`${PLUGIN_MESSAGE_ROOT}.msg-two.title`)).toBe('Logs')
  })

  it('ignores unsafe ids and empty tables', () => {
    const merge = vi.spyOn(i18n.global, 'mergeLocaleMessage')
    mergePluginMessages('Bad ID!', { en: { title: 'nope' } })
    mergePluginMessages('msg-empty', {})
    expect(merge).not.toHaveBeenCalled()
    merge.mockRestore()
  })

  it('resolves shared effect keys but keeps manifest sentences verbatim', () => {
    setLocale('en')
    expect(humanText('metrics')).toBe('Fleet will send its metrics to this plugin')
    expect(humanText('A custom community sentence')).toBe('A custom community sentence')
  })

  it('collects the message tables contributed by installed plugins', () => {
    const tables = pluginMessagesFor([
      {
        id: 'msg-source',
        name: 'Source',
        version: '1',
        enabled: true,
        role: 'viewer',
        kind: 'external',
        ui: {
          path: '/plugins/source',
          messages: { en: { title: 'Source' } },
        },
      },
      {
        id: 'msg-plain',
        name: 'Plain',
        version: '1',
        enabled: true,
        role: 'viewer',
        kind: 'external',
        ui: { path: '/plugins/plain' },
      },
    ])

    expect(Object.keys(tables)).toEqual(['msg-source'])
    expect(tables['msg-source']?.en?.title).toBe('Source')
  })
})

import { describe, expect, it } from 'vitest'

import { PLUGIN_ICON_KEYS, pluginIcon } from './icons'

describe('plugin icon map', () => {
  it('maps declarative icon names to reusable components', () => {
    for (const key of PLUGIN_ICON_KEYS) {
      expect(pluginIcon(key)).toBeDefined()
    }
  })

  it('falls back to a neutral icon for unknown or missing names', () => {
    expect(pluginIcon('not-a-real-icon')).toBe(pluginIcon('default'))
    expect(pluginIcon(undefined)).toBe(pluginIcon('default'))
  })
})

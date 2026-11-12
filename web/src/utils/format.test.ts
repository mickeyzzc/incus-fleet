import { describe, expect, it } from 'vitest'

import { setLocale } from '@/i18n'

import {
  formatBytes,
  formatDateTime,
  formatDuration,
  formatPercent,
  primaryAddress,
  shortID,
} from './format'

describe('formatBytes', () => {
  it('formats zero and invalid values', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(-10)).toBe('0 B')
    expect(formatBytes(Number.NaN)).toBe('0 B')
    expect(formatBytes(Number.POSITIVE_INFINITY)).toBe('0 B')
  })

  it('formats bytes and binary units', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1024)).toBe('1.0 KiB')
    expect(formatBytes(1024 ** 2)).toBe('1.0 MiB')
    expect(formatBytes(1.5 * 1024 ** 3)).toBe('1.5 GiB')
    expect(formatBytes(1024 ** 4)).toBe('1.0 TiB')
    expect(formatBytes(1024 ** 7)).toBe('1048576.0 PiB')
  })

  it('honours the decimal argument', () => {
    expect(formatBytes(1536, 2)).toBe('1.50 KiB')
    expect(formatBytes(1024, 0)).toBe('1 KiB')
  })
})

describe('formatPercent', () => {
  it('handles empty totals', () => {
    expect(formatPercent(10, 0)).toBe('0%')
    expect(formatPercent(10, Number.NaN)).toBe('0%')
  })

  it('formats and clamps percentages', () => {
    expect(formatPercent(50, 100)).toBe('50.0%')
    expect(formatPercent(200, 100)).toBe('100.0%')
    expect(formatPercent(-5, 100)).toBe('0.0%')
  })
})

describe('formatDateTime', () => {
  it('returns a dash for missing, invalid, and zero timestamps', () => {
    expect(formatDateTime()).toBe('—')
    expect(formatDateTime(null)).toBe('—')
    expect(formatDateTime('not-a-date')).toBe('—')
    expect(formatDateTime('0001-01-01T00:00:00Z')).toBe('—')
  })

  it('formats valid timestamps', () => {
    const formatted = formatDateTime('2026-11-11T10:30:00Z')

    expect(formatted).not.toBe('—')
    expect(formatted).toMatch(/2026/)
  })

  it('follows the active app locale', () => {
    setLocale('zh-CN')
    const formatted = formatDateTime('2026-11-11T10:30:00Z')

    expect(formatted).toMatch(/2026/)
    expect(formatted).toMatch(/11.*11|11日/)
  })
})

describe('formatDuration', () => {
  it('returns a dash without a start', () => {
    expect(formatDuration()).toBe('—')
    expect(formatDuration(null, '2026-11-11T10:00:00Z')).toBe('—')
  })

  it('returns a dash for invalid dates', () => {
    expect(formatDuration('nope', '2026-11-11T10:00:00Z')).toBe('—')
    expect(formatDuration('2026-11-11T10:00:00Z', 'nope')).toBe('—')
  })

  it('formats seconds, minutes, and hours', () => {
    expect(formatDuration('2026-11-11T10:00:00Z', '2026-11-11T10:00:30Z')).toBe('30s')
    expect(formatDuration('2026-11-11T10:00:00Z', '2026-11-11T10:02:05Z')).toBe('2m 5s')
    expect(formatDuration('2026-11-11T10:00:00Z', '2026-11-11T12:15:00Z')).toBe('2h 15m')
  })

  it('uses the current time when no end is supplied and clamps negatives', () => {
    expect(formatDuration('2026-11-11T10:00:00Z')).not.toBe('—')
    expect(formatDuration('2999-11-11T10:00:00Z', '2026-11-11T10:00:00Z')).toBe('0s')
  })
})

describe('shortID', () => {
  it('keeps short identifiers and shortens long ones', () => {
    expect(shortID('short')).toBe('short')
    expect(shortID('1234567890')).toBe('1234567890')
    expect(shortID('1234567890abcdef')).toBe('12345678…')
  })
})

describe('primaryAddress', () => {
  it('prefers IPv4 then IPv6 then a dash', () => {
    expect(primaryAddress(['10.0.0.1'], ['fd42::1'])).toBe('10.0.0.1')
    expect(primaryAddress([], ['fd42::1'])).toBe('fd42::1')
    expect(primaryAddress([], [])).toBe('—')
  })
})

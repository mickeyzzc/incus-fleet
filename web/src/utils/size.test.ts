import { describe, expect, it } from 'vitest'

import { parseSize, sizeErrorKey, validDeviceName, validGuestPath } from './size'

describe('parseSize', () => {
  it('accepts explicit units and keeps them', () => {
    expect(parseSize('512MiB')).toEqual({ valid: true, value: '512MiB' })
    expect(parseSize('2 GiB')).toEqual({ valid: true, value: '2GiB' })
    expect(parseSize('10GB')).toEqual({ valid: true, value: '10GB' })
  })

  it('treats a bare number as MiB', () => {
    expect(parseSize('512')).toEqual({ valid: true, value: '512MiB' })
    expect(parseSize('1.5')).toEqual({ valid: true, value: '1.5MiB' })
  })

  it('allows percentages only when requested', () => {
    expect(parseSize('50%', { allowPercent: true })).toEqual({ valid: true, value: '50%' })
    expect(parseSize('50%')).toEqual({ valid: false, value: '50%', reason: 'format' })
  })

  it('rejects malformed and non-positive values', () => {
    expect(parseSize('lots')).toEqual({ valid: false, value: 'lots', reason: 'format' })
    expect(parseSize('0GiB')).toEqual({ valid: false, value: '0GiB', reason: 'zero' })
    expect(parseSize('-1GiB')).toEqual({ valid: false, value: '-1GiB', reason: 'format' })
  })

  it('handles empty input according to allowEmpty', () => {
    expect(parseSize('')).toEqual({ valid: true, value: '' })
    expect(parseSize('   ', { allowEmpty: false })).toEqual({ valid: false, value: '' })
  })
})

describe('sizeErrorKey', () => {
  it('maps reasons to i18n keys', () => {
    expect(sizeErrorKey('zero')).toBe('validation.sizePositive')
    expect(sizeErrorKey('format')).toBe('validation.sizeFormat')
    expect(sizeErrorKey(undefined)).toBe('validation.sizeFormat')
  })
})

describe('validDeviceName', () => {
  it('accepts safe device names', () => {
    expect(validDeviceName('data0')).toBe(true)
    expect(validDeviceName('disk-1')).toBe(true)
    expect(validDeviceName('a')).toBe(true)
  })

  it('rejects empty, spaced, or punctuated names', () => {
    expect(validDeviceName('')).toBe(false)
    expect(validDeviceName('data 0')).toBe(false)
    expect(validDeviceName('-data')).toBe(false)
    expect(validDeviceName('data/0')).toBe(false)
  })
})

describe('validGuestPath', () => {
  it('accepts absolute paths without traversal', () => {
    expect(validGuestPath('/data')).toBe(true)
    expect(validGuestPath('/var/lib/data')).toBe(true)
  })

  it('rejects relative paths and traversal', () => {
    expect(validGuestPath('data')).toBe(false)
    expect(validGuestPath('/data/../etc')).toBe(false)
  })
})

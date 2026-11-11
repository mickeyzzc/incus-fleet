import { describe, expect, it } from 'vitest'

import { errorMessage } from './errors'

describe('errorMessage', () => {
  it('uses Error messages', () => {
    expect(errorMessage(new Error('boom'))).toBe('boom')
  })

  it('uses non-empty strings', () => {
    expect(errorMessage('plain failure')).toBe('plain failure')
  })

  it('falls back for empty and unknown values', () => {
    expect(errorMessage('')).toBe('Unexpected error')
    expect(errorMessage(new Error(''))).toBe('Unexpected error')
    expect(errorMessage(null)).toBe('Unexpected error')
    expect(errorMessage({ code: 1 })).toBe('Unexpected error')
  })
})

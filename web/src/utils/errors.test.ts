import { describe, expect, it } from 'vitest'

import { ApiError } from '@/api'
import { setLocale } from '@/i18n'

import { errorMessage } from './errors'

describe('errorMessage', () => {
  it('uses Error messages', () => {
    expect(errorMessage(new Error('boom'))).toBe('boom')
  })

  it('uses non-empty strings', () => {
    expect(errorMessage('plain failure')).toBe('plain failure')
  })

  it('translates known API error codes', () => {
    expect(errorMessage(new ApiError(400, 'invalid_request', 'instances: invalid request'))).toBe(
      'The request was rejected. Check the values and try again.',
    )
    expect(errorMessage(new ApiError(404, 'not_found', 'missing'))).toBe(
      'The requested object was not found.',
    )
    expect(errorMessage(new ApiError(409, 'conflict', 'busy'))).toBe(
      'The operation conflicts with another task.',
    )
    expect(errorMessage(new ApiError(503, 'cluster_unavailable', 'down'))).toBe(
      'The cluster is unavailable.',
    )
    expect(errorMessage(new ApiError(401, 'invalid_token', 'nope'))).toBe('Invalid admin token.')
  })

  it('keeps unknown API errors as-is and translates the empty fallback', () => {
    expect(errorMessage(new ApiError(500, 'internal_error', 'disk on fire'))).toBe('disk on fire')
    expect(errorMessage(new ApiError(500, 'internal_error', ''))).toBe('Something went wrong.')
  })

  it('follows the active locale', () => {
    setLocale('zh-CN')

    expect(errorMessage(new ApiError(404, 'not_found', 'missing'))).toBe('未找到请求的对象。')
    expect(errorMessage(null)).toBe('发生未知错误。')
  })

  it('falls back for empty and unknown values', () => {
    expect(errorMessage('')).toBe('Something went wrong.')
    expect(errorMessage(new Error(''))).toBe('Something went wrong.')
    expect(errorMessage(null)).toBe('Something went wrong.')
    expect(errorMessage({ code: 1 })).toBe('Something went wrong.')
  })
})

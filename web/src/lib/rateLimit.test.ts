import { describe, expect, it } from 'vitest'

import { ApiError } from '../services/auth'

import {
  RATE_LIMIT_FIRST_DELAY_MS,
  RATE_LIMIT_MAX_DELAY_MS,
  isRateLimited,
  rateLimitDelayMs,
} from './rateLimit'

describe('isRateLimited', () => {
  it('is true for a 429 only', () => {
    expect(isRateLimited(new ApiError(429, 'rate limit exceeded'))).toBe(true)
    expect(isRateLimited(new ApiError(503, 'unavailable'))).toBe(false)
    expect(isRateLimited(new ApiError(500, 'boom'))).toBe(false)
    expect(isRateLimited(new Error('network'))).toBe(false)
    expect(isRateLimited(undefined)).toBe(false)
  })
})

describe('rateLimitDelayMs', () => {
  it('doubles from the first delay and stops at the cap', () => {
    const err = new ApiError(429, 'rate limit exceeded', 1)
    expect(rateLimitDelayMs(err, 0)).toBe(RATE_LIMIT_FIRST_DELAY_MS)
    expect(rateLimitDelayMs(err, 1)).toBe(RATE_LIMIT_FIRST_DELAY_MS * 2)
    expect(rateLimitDelayMs(err, 2)).toBe(RATE_LIMIT_FIRST_DELAY_MS * 4)
    expect(rateLimitDelayMs(err, 10)).toBe(RATE_LIMIT_MAX_DELAY_MS)
  })

  it('never waits less than the server asked, nor more than the cap', () => {
    expect(rateLimitDelayMs(new ApiError(429, 'x', 7), 0)).toBe(7000)
    expect(rateLimitDelayMs(new ApiError(429, 'x', 600), 0)).toBe(RATE_LIMIT_MAX_DELAY_MS)
    expect(rateLimitDelayMs(new ApiError(429, 'x'), 0)).toBe(RATE_LIMIT_FIRST_DELAY_MS)
  })
})

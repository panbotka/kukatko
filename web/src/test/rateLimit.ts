import { act } from '@testing-library/react'
import { vi } from 'vitest'

import { RATE_LIMIT_MAX_DELAY_MS } from '../lib/rateLimit'
import { ApiError } from '../services/auth'

/** The error a service throws on the heavy vector throttle's 429 (`Retry-After: 1`). */
export function rateLimited(): ApiError {
  return new ApiError(429, 'rate limit exceeded', 1)
}

/**
 * Runs the scheduled rate-limit retry: advances fake timers (installed with
 * `vi.useFakeTimers({ shouldAdvanceTime: true })`) past the longest backoff, so
 * whatever retry is pending fires and its request settles.
 */
export async function elapseRateLimitWait(): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(RATE_LIMIT_MAX_DELAY_MS)
  })
}

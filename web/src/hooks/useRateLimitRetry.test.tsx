import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { RATE_LIMIT_FIRST_DELAY_MS, RATE_LIMIT_MAX_RETRIES } from '../lib/rateLimit'
import { ApiError } from '../services/auth'
import { rateLimited } from '../test/rateLimit'

import { useRateLimitRetry } from './useRateLimitRetry'

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useRateLimitRetry', () => {
  it('schedules a retry on a 429 and reports that it is waiting', () => {
    const { result } = renderHook(() => useRateLimitRetry())
    const retry = vi.fn()
    let handled = false
    act(() => {
      handled = result.current.handle(rateLimited(), retry)
    })
    expect(handled).toBe(true)
    expect(result.current.waiting).toBe(true)
    act(() => {
      vi.advanceTimersByTime(RATE_LIMIT_FIRST_DELAY_MS - 1)
    })
    expect(retry).not.toHaveBeenCalled()
    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(retry).toHaveBeenCalledTimes(1)

    act(() => {
      result.current.reset()
    })
    expect(result.current.waiting).toBe(false)
  })

  it('leaves any other error to the page', () => {
    const { result } = renderHook(() => useRateLimitRetry())
    const retry = vi.fn()
    let handled = true
    act(() => {
      handled = result.current.handle(new ApiError(503, 'unavailable'), retry)
    })
    expect(handled).toBe(false)
    expect(result.current.waiting).toBe(false)
    act(() => {
      vi.runAllTimers()
    })
    expect(retry).not.toHaveBeenCalled()
  })

  it('backs off between refusals and gives up after the cap', () => {
    const { result } = renderHook(() => useRateLimitRetry())
    const retry = vi.fn()
    for (let i = 0; i < RATE_LIMIT_MAX_RETRIES; i++) {
      act(() => {
        expect(result.current.handle(rateLimited(), retry)).toBe(true)
      })
      act(() => {
        vi.runOnlyPendingTimers()
      })
    }
    expect(retry).toHaveBeenCalledTimes(RATE_LIMIT_MAX_RETRIES)
    let handled = true
    act(() => {
      handled = result.current.handle(rateLimited(), retry)
    })
    expect(handled).toBe(false)
    expect(result.current.waiting).toBe(false)
  })

  it('cancels a scheduled retry on reset and on unmount', () => {
    const { result, unmount } = renderHook(() => useRateLimitRetry())
    const retry = vi.fn()
    act(() => {
      result.current.handle(rateLimited(), retry)
      result.current.reset()
    })
    act(() => {
      vi.runAllTimers()
    })
    expect(retry).not.toHaveBeenCalled()

    act(() => {
      result.current.handle(rateLimited(), retry)
    })
    unmount()
    vi.runAllTimers()
    expect(retry).not.toHaveBeenCalled()
  })
})

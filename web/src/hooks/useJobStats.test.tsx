import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { type JobStats } from '../services/import'

import { useJobStats } from './useJobStats'

vi.mock('../services/import', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/import')>()
  return { ...actual, fetchJobStats: vi.fn() }
})

const { fetchJobStats } = await import('../services/import')
const statsMock = vi.mocked(fetchJobStats)

/** Builds a job-stats snapshot with the given per-state counts. */
function stats(byState: Record<string, number>): JobStats {
  const total = Object.values(byState).reduce((sum, n) => sum + n, 0)
  return { by_state: byState, by_type: {}, total }
}

/** Overrides `document.hidden` and fires the matching visibilitychange event. */
function setHidden(hidden: boolean) {
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
  document.dispatchEvent(new Event('visibilitychange'))
}

/** Flushes the microtasks that settle the mocked fetch and its state update. */
async function flush() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

beforeEach(() => {
  vi.useFakeTimers()
  statsMock.mockReset()
  statsMock.mockResolvedValue(stats({ queued: 1 }))
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
})

afterEach(() => {
  vi.runOnlyPendingTimers()
  vi.useRealTimers()
})

describe('useJobStats', () => {
  it('issues no request while disabled', () => {
    renderHook(() => useJobStats(false))
    expect(statsMock).not.toHaveBeenCalled()
  })

  it('fetches immediately when enabled and exposes the stats', async () => {
    const { result } = renderHook(() => useJobStats(true))
    await flush()
    expect(statsMock).toHaveBeenCalledTimes(1)
    expect(result.current?.by_state.queued).toBe(1)
  })

  it('refreshes on the poll interval', async () => {
    renderHook(() => useJobStats(true))
    await flush()
    expect(statsMock).toHaveBeenCalledTimes(1)
    act(() => {
      vi.advanceTimersByTime(30_000)
    })
    await flush()
    expect(statsMock).toHaveBeenCalledTimes(2)
  })

  it('pauses polling while the tab is hidden and refreshes when it returns', async () => {
    renderHook(() => useJobStats(true))
    await flush()
    expect(statsMock).toHaveBeenCalledTimes(1)

    // Hidden: the interval is cleared, so advancing time fetches nothing.
    act(() => {
      setHidden(true)
    })
    act(() => {
      vi.advanceTimersByTime(90_000)
    })
    await flush()
    expect(statsMock).toHaveBeenCalledTimes(1)

    // Visible again: an immediate refresh runs.
    act(() => {
      setHidden(false)
    })
    await flush()
    expect(statsMock).toHaveBeenCalledTimes(2)
  })

  it('stops polling once unmounted', async () => {
    const { unmount } = renderHook(() => useJobStats(true))
    await flush()
    expect(statsMock).toHaveBeenCalledTimes(1)

    unmount()
    act(() => {
      vi.advanceTimersByTime(90_000)
    })
    await flush()
    expect(statsMock).toHaveBeenCalledTimes(1)
  })
})

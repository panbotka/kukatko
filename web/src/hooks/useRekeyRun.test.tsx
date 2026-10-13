import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { REKEY_POLL_IDLE_MS, REKEY_POLL_RUNNING_MS } from '../lib/rekeyProgress'
import type { RekeyRun, RekeyState } from '../services/rekey'

import { useRekeyRun } from './useRekeyRun'

vi.mock('../services/rekey', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/rekey')>()
  return { ...actual, fetchRekeyRun: vi.fn() }
})

const { fetchRekeyRun } = await import('../services/rekey')
const fetchMock = vi.mocked(fetchRekeyRun)

/** A run in `state` with `done` of 1000 photos moved. */
function run(state: RekeyState, done = 0): RekeyRun {
  return {
    uid: 'rk1',
    project_uid: 'pjv',
    state,
    target_prefix: 'projects/pjv/',
    total_photos: 1000,
    total_bytes: 1000 * 1024 * 1024,
    done_photos: done,
    done_bytes: done * 1024 * 1024,
    last_key: '',
    errors: [],
    error_count: 0,
    started_at: '2026-10-13T08:00:00Z',
    updated_at: '2026-10-13T08:00:00Z',
  }
}

/** Flushes the microtasks that settle a mocked fetch and its state update. */
async function flush() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

/** Lets `ms` elapse and whatever it triggered settle. */
async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
  await flush()
}

/** Makes the document claim it is hidden (or visible) and fires the event. */
function setVisibility(state: 'hidden' | 'visible') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state })
  act(() => {
    document.dispatchEvent(new Event('visibilitychange'))
  })
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  fetchMock.mockReset()
  setVisibility('visible')
})

afterEach(() => {
  vi.runOnlyPendingTimers()
  vi.useRealTimers()
  Reflect.deleteProperty(document, 'visibilityState')
})

describe('useRekeyRun', () => {
  it('reads at once and then every 3 s while the move runs', async () => {
    fetchMock.mockResolvedValue(run('running', 10))
    const { result } = renderHook(() => useRekeyRun('pjv'))
    await flush()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('pjv')
    expect(result.current.status).toBe('ready')
    expect(result.current.run?.state).toBe('running')

    await advance(REKEY_POLL_RUNNING_MS)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await advance(REKEY_POLL_RUNNING_MS)
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('polls every 15 s while paused or with no run', async () => {
    fetchMock.mockResolvedValueOnce(null).mockResolvedValue(run('paused', 10))
    const { result } = renderHook(() => useRekeyRun('pjv'))
    await flush()
    expect(result.current.run).toBeNull()

    await advance(REKEY_POLL_RUNNING_MS)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await advance(REKEY_POLL_IDLE_MS - REKEY_POLL_RUNNING_MS)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(result.current.run?.state).toBe('paused')

    await advance(REKEY_POLL_IDLE_MS - 1)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await advance(1)
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('stops once the move is done', async () => {
    fetchMock.mockResolvedValueOnce(run('running', 990)).mockResolvedValue(run('done', 1000))
    const { result } = renderHook(() => useRekeyRun('pjv'))
    await flush()

    await advance(REKEY_POLL_RUNNING_MS)
    expect(result.current.run?.state).toBe('done')
    expect(fetchMock).toHaveBeenCalledTimes(2)

    await advance(REKEY_POLL_IDLE_MS * 4)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('stands down in a hidden tab and asks at once when it comes back', async () => {
    fetchMock.mockResolvedValue(run('running', 10))
    renderHook(() => useRekeyRun('pjv'))
    await flush()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    setVisibility('hidden')
    await advance(REKEY_POLL_RUNNING_MS * 5)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    setVisibility('visible')
    await flush()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('measures the rate from the last two polls of a running move', async () => {
    fetchMock
      .mockResolvedValueOnce(run('running', 100))
      .mockResolvedValueOnce(run('running', 130))
      .mockResolvedValue(run('paused', 130))
    const { result } = renderHook(() => useRekeyRun('pjv'))
    await flush()
    expect(result.current.ratePerMinute).toBeNull()

    await advance(REKEY_POLL_RUNNING_MS)
    // 30 photos in 3 s; the timers advance the clock exactly.
    expect(result.current.ratePerMinute).toBeCloseTo(600, 0)

    await advance(REKEY_POLL_RUNNING_MS)
    expect(result.current.run?.state).toBe('paused')
    expect(result.current.ratePerMinute).toBeNull()
  })

  it('adopts an action answer and polls at its cadence', async () => {
    fetchMock.mockResolvedValue(run('paused', 10))
    const { result } = renderHook(() => useRekeyRun('pjv'))
    await flush()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    act(() => {
      result.current.adopt(run('running', 10))
    })
    expect(result.current.run?.state).toBe('running')

    fetchMock.mockResolvedValue(run('running', 20))
    await advance(REKEY_POLL_RUNNING_MS)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('reports a failed first read and retries on reload', async () => {
    fetchMock.mockRejectedValueOnce(new Error('offline')).mockResolvedValue(null)
    const { result } = renderHook(() => useRekeyRun('pjv'))
    await flush()
    expect(result.current.status).toBe('error')

    act(() => {
      result.current.reload()
    })
    await flush()
    expect(result.current.status).toBe('ready')
    expect(result.current.run).toBeNull()
  })
})

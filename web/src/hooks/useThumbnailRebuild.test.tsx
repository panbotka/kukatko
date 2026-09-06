import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { REBUILD_POLL_DELAYS_MS } from '../lib/renditionRebuild'
import { type PhotoDetail } from '../services/photos'

import { type RebuildWatch, useThumbnailRebuild } from './useThumbnailRebuild'

vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhoto: vi.fn() }
})

const { fetchPhoto } = await import('../services/photos')
const fetchPhotoMock = vi.mocked(fetchPhoto)

const WATCH: RebuildWatch = { uid: 'b', since: '2026-09-06T10:00:00Z' }

/** A detail whose thumbnail step ran at `at`. */
function builtAt(at: string): PhotoDetail {
  return { uid: 'b', processing: [{ step: 'thumbnail', state: 'done', at }] } as PhotoDetail
}

const STALE = builtAt('2026-09-06T09:00:00Z')
const FRESH = builtAt('2026-09-06T10:00:03Z')

/** Flushes the microtasks that settle a mocked fetch and its state update. */
async function flush() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

/** Lets the next scheduled poll fire and settle. */
async function nextPoll(index: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(REBUILD_POLL_DELAYS_MS[index])
  })
  await flush()
}

beforeEach(() => {
  vi.useFakeTimers()
  fetchPhotoMock.mockReset()
})

afterEach(() => {
  vi.runOnlyPendingTimers()
  vi.useRealTimers()
})

describe('useThumbnailRebuild', () => {
  it('asks nothing without a watch', () => {
    const { result } = renderHook(() => useThumbnailRebuild(null))
    expect(fetchPhotoMock).not.toHaveBeenCalled()
    expect(result.current).toBeNull()
  })

  it('reports at once when the first poll already shows the rebuild', async () => {
    fetchPhotoMock.mockResolvedValue(FRESH)
    const { result } = renderHook(() => useThumbnailRebuild(WATCH))
    await flush()

    expect(fetchPhotoMock).toHaveBeenCalledTimes(1)
    expect(fetchPhotoMock.mock.calls[0][0]).toBe('b')
    expect(result.current).toEqual({ watch: WATCH, photo: FRESH })
  })

  it('keeps polling on the schedule until the stamp moves past the save', async () => {
    fetchPhotoMock
      .mockResolvedValueOnce(STALE)
      .mockResolvedValueOnce(STALE)
      .mockResolvedValue(FRESH)
    const { result } = renderHook(() => useThumbnailRebuild(WATCH))
    await flush()
    expect(result.current).toBeNull()

    await nextPoll(0)
    expect(fetchPhotoMock).toHaveBeenCalledTimes(2)
    expect(result.current).toBeNull()

    await nextPoll(1)
    expect(fetchPhotoMock).toHaveBeenCalledTimes(3)
    expect(result.current?.photo).toBe(FRESH)

    // Answered: no further poll is pending.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000)
    })
    expect(fetchPhotoMock).toHaveBeenCalledTimes(3)
  })

  it('treats a failed poll as one to retry, not as an answer', async () => {
    fetchPhotoMock.mockRejectedValueOnce(new Error('boom')).mockResolvedValue(FRESH)
    const { result } = renderHook(() => useThumbnailRebuild(WATCH))
    await flush()
    expect(result.current).toBeNull()

    await nextPoll(0)
    expect(result.current?.photo).toBe(FRESH)
  })

  it('gives up once the schedule runs out', async () => {
    fetchPhotoMock.mockResolvedValue(STALE)
    const { result } = renderHook(() => useThumbnailRebuild(WATCH))
    await flush()

    for (let i = 0; i < REBUILD_POLL_DELAYS_MS.length; i += 1) {
      await nextPoll(i)
    }
    const polls = fetchPhotoMock.mock.calls.length
    expect(polls).toBe(REBUILD_POLL_DELAYS_MS.length + 1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000)
    })
    expect(fetchPhotoMock).toHaveBeenCalledTimes(polls)
    expect(result.current).toBeNull()
  })

  it('drops the previous watch when a new one arrives, and answers only the new one', async () => {
    fetchPhotoMock.mockResolvedValue(STALE)
    const { result, rerender } = renderHook(({ watch }) => useThumbnailRebuild(watch), {
      initialProps: { watch: WATCH },
    })
    await flush()

    const later: RebuildWatch = { uid: 'b', since: '2026-09-06T10:00:10Z' }
    fetchPhotoMock.mockResolvedValue(builtAt('2026-09-06T10:00:12Z'))
    rerender({ watch: later })
    await flush()

    expect(result.current?.watch).toBe(later)
    // The first watch's timer was cleared with it: only the new watch's polls run.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000)
    })
    expect(fetchPhotoMock).toHaveBeenCalledTimes(2)
  })
})

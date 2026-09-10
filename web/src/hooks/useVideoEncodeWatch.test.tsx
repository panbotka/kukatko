import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ENCODE_POLL_INTERVAL_MS } from '../lib/videoEncode'
import { type PhotoDetail, type ProcessingState } from '../services/photos'

import { useVideoEncodeWatch } from './useVideoEncodeWatch'

vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhoto: vi.fn() }
})

const { fetchPhoto } = await import('../services/photos')
const fetchPhotoMock = vi.mocked(fetchPhoto)

/** A clip detail whose streaming step is in the given state. */
function clip(state: ProcessingState, hls = false): PhotoDetail {
  return {
    uid: 'v1',
    media_type: 'video',
    hls,
    processing: [{ step: 'hls_transcode', state }],
  } as PhotoDetail
}

const RUNNING = clip('running')
const ENCODED = clip('done', true)

/** Flushes the microtasks that settle a mocked fetch and its state update. */
async function flush() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

/** Lets one poll interval elapse and its request settle. */
async function tick() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ENCODE_POLL_INTERVAL_MS)
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
  fetchPhotoMock.mockReset()
  setVisibility('visible')
})

afterEach(() => {
  vi.runOnlyPendingTimers()
  vi.useRealTimers()
  Reflect.deleteProperty(document, 'visibilityState')
})

describe('useVideoEncodeWatch', () => {
  it('asks nothing without a clip to watch', async () => {
    const { result } = renderHook(() => useVideoEncodeWatch(null))
    await tick()

    expect(fetchPhotoMock).not.toHaveBeenCalled()
    expect(result.current).toBeNull()
  })

  it('re-checks the clip on the interval while the encode runs', async () => {
    fetchPhotoMock.mockResolvedValue(RUNNING)
    const { result } = renderHook(() => useVideoEncodeWatch('v1'))

    // Nothing is asked before the first interval has passed: the caller only
    // starts a watch because the detail it just read said the encode is owed.
    expect(fetchPhotoMock).not.toHaveBeenCalled()

    await tick()
    expect(fetchPhotoMock).toHaveBeenCalledTimes(1)
    expect(fetchPhotoMock.mock.calls[0][0]).toBe('v1')
    expect(result.current).toBeNull()

    await tick()
    expect(fetchPhotoMock).toHaveBeenCalledTimes(2)
    expect(result.current).toBeNull()
  })

  it('reports the detail that first shows the rendition, and stops asking', async () => {
    fetchPhotoMock.mockResolvedValueOnce(RUNNING).mockResolvedValue(ENCODED)
    const { result } = renderHook(() => useVideoEncodeWatch('v1'))

    await tick()
    expect(result.current).toBeNull()

    await tick()
    expect(result.current).toBe(ENCODED)

    // Terminal is terminal: no further request, however long the viewer stays open.
    await tick()
    await tick()
    expect(fetchPhotoMock).toHaveBeenCalledTimes(2)
  })

  it('stops on a failed encode too', async () => {
    fetchPhotoMock.mockResolvedValue(clip('failed'))
    const { result } = renderHook(() => useVideoEncodeWatch('v1'))

    await tick()
    expect(result.current?.processing?.[0].state).toBe('failed')

    await tick()
    expect(fetchPhotoMock).toHaveBeenCalledTimes(1)
  })

  it('asks again after a failed poll', async () => {
    fetchPhotoMock.mockRejectedValueOnce(new Error('offline')).mockResolvedValue(ENCODED)
    const { result } = renderHook(() => useVideoEncodeWatch('v1'))

    await tick()
    expect(result.current).toBeNull()

    await tick()
    expect(fetchPhotoMock).toHaveBeenCalledTimes(2)
    expect(result.current).toBe(ENCODED)
  })

  it('stands down while the tab is hidden and catches up when it returns', async () => {
    fetchPhotoMock.mockResolvedValue(RUNNING)
    renderHook(() => useVideoEncodeWatch('v1'))

    setVisibility('hidden')
    await tick()
    await tick()
    expect(fetchPhotoMock).not.toHaveBeenCalled()

    // Coming back to the front asks immediately rather than after another wait.
    setVisibility('visible')
    await flush()
    expect(fetchPhotoMock).toHaveBeenCalledTimes(1)
  })

  it('drops the previous answer when the viewer moves to another clip', async () => {
    fetchPhotoMock.mockResolvedValue(ENCODED)
    const { result, rerender } = renderHook(
      ({ uid }: { uid: string | null }) => useVideoEncodeWatch(uid),
      { initialProps: { uid: 'v1' } },
    )
    await tick()
    expect(result.current).toBe(ENCODED)

    rerender({ uid: 'v2' })
    expect(result.current).toBeNull()
  })

  it('stops asking once unmounted', async () => {
    fetchPhotoMock.mockResolvedValue(RUNNING)
    const { unmount } = renderHook(() => useVideoEncodeWatch('v1'))

    await tick()
    expect(fetchPhotoMock).toHaveBeenCalledTimes(1)
    unmount()

    await tick()
    expect(fetchPhotoMock).toHaveBeenCalledTimes(1)
  })
})

import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PRELOAD_AHEAD, useSlideshow } from './useSlideshow'

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useSlideshow', () => {
  it('auto-advances on the configured interval while playing', () => {
    const { result } = renderHook(() => useSlideshow({ length: 3, intervalMs: 1000 }))

    expect(result.current.index).toBe(0)
    expect(result.current.playing).toBe(true)

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.index).toBe(1)

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.index).toBe(2)
  })

  it('wraps to the first photo at the end when there are no more pages', () => {
    const { result } = renderHook(() =>
      useSlideshow({ length: 2, intervalMs: 1000, hasMore: false }),
    )

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.index).toBe(1)

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.index).toBe(0)
  })

  it('does not auto-advance while paused, and resumes on play', () => {
    const { result } = renderHook(() =>
      useSlideshow({ length: 3, intervalMs: 1000, autoPlay: false }),
    )

    expect(result.current.playing).toBe(false)
    act(() => {
      vi.advanceTimersByTime(5000)
    })
    expect(result.current.index).toBe(0)

    act(() => {
      result.current.play()
    })
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.index).toBe(1)
  })

  it('toggles play/pause', () => {
    const { result } = renderHook(() => useSlideshow({ length: 3, intervalMs: 1000 }))

    act(() => {
      result.current.toggle()
    })
    expect(result.current.playing).toBe(false)

    act(() => {
      result.current.toggle()
    })
    expect(result.current.playing).toBe(true)
  })

  it('advances and rewinds manually with wrap-around', () => {
    const { result } = renderHook(() =>
      useSlideshow({ length: 3, intervalMs: 100000, autoPlay: false }),
    )

    act(() => {
      result.current.next()
    })
    expect(result.current.index).toBe(1)

    act(() => {
      result.current.prev()
      result.current.prev()
    })
    // 1 → 0 → wrap to last (2)
    expect(result.current.index).toBe(2)
  })

  it('a manual next resets the auto-advance countdown', () => {
    const { result } = renderHook(() => useSlideshow({ length: 5, intervalMs: 1000 }))

    act(() => {
      vi.advanceTimersByTime(800)
    })
    // Manual advance restarts the timer.
    act(() => {
      result.current.next()
    })
    expect(result.current.index).toBe(1)

    act(() => {
      vi.advanceTimersByTime(800)
    })
    // Only 800ms since the manual advance — no auto-advance yet.
    expect(result.current.index).toBe(1)

    act(() => {
      vi.advanceTimersByTime(200)
    })
    expect(result.current.index).toBe(2)
  })

  it('picks up a new interval mid-show without restarting playback', () => {
    const { result, rerender } = renderHook(
      ({ intervalMs }: { intervalMs: number }) => useSlideshow({ length: 3, intervalMs }),
      { initialProps: { intervalMs: 5000 } },
    )

    act(() => {
      vi.advanceTimersByTime(2000)
    })
    expect(result.current.index).toBe(0)

    // Speeding up keeps the current slide and the playing state; only the
    // countdown to the next slide changes.
    rerender({ intervalMs: 1000 })
    expect(result.current.index).toBe(0)
    expect(result.current.playing).toBe(true)

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.index).toBe(1)

    // Every following slide runs on the new interval too.
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.index).toBe(2)
  })

  it('is a no-op for an empty set', () => {
    const onLoadMore = vi.fn()
    const { result } = renderHook(() => useSlideshow({ length: 0, intervalMs: 1000, onLoadMore }))

    act(() => {
      vi.advanceTimersByTime(5000)
    })
    expect(result.current.index).toBe(0)

    act(() => {
      result.current.next()
      result.current.prev()
    })
    expect(result.current.index).toBe(0)
  })

  it('requests more pages as the cursor nears the loaded end', () => {
    const onLoadMore = vi.fn()
    // length 6, PRELOAD_AHEAD 5 → triggers once index >= 1.
    const { result } = renderHook(() =>
      useSlideshow({
        length: PRELOAD_AHEAD + 1,
        intervalMs: 1000,
        hasMore: true,
        autoPlay: false,
        onLoadMore,
      }),
    )

    expect(onLoadMore).not.toHaveBeenCalled()
    act(() => {
      result.current.next()
    })
    expect(onLoadMore).toHaveBeenCalled()
  })

  it('waits for more pages instead of wrapping at the end', () => {
    const onLoadMore = vi.fn()
    const { result } = renderHook(() =>
      useSlideshow({ length: 1, intervalMs: 1000, hasMore: true, autoPlay: false, onLoadMore }),
    )

    act(() => {
      result.current.next()
    })
    // Still on the last loaded photo; a page load was requested.
    expect(result.current.index).toBe(0)
    expect(onLoadMore).toHaveBeenCalled()
  })

  it('clamps the cursor when the loaded set shrinks', () => {
    const { result, rerender } = renderHook(
      ({ length }: { length: number }) =>
        useSlideshow({ length, intervalMs: 100000, autoPlay: false }),
      { initialProps: { length: 4 } },
    )

    act(() => {
      result.current.goTo(3)
    })
    expect(result.current.index).toBe(3)

    rerender({ length: 2 })
    expect(result.current.index).toBe(1)
  })
})

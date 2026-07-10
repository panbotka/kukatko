import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  MAX_HOLD_MS,
  PRELOAD_AHEAD,
  PRELOAD_BEHIND,
  preloadWindow,
  type SlideReadiness,
  useSlideshow,
} from './useSlideshow'

/** A readiness function reporting `pending` for the listed slides, `ready` otherwise. */
function pendingAt(...indices: number[]): (i: number) => SlideReadiness {
  return (i) => (indices.includes(i) ? 'pending' : 'ready')
}

/** A readiness function reporting `error` for the listed slides, `ready` otherwise. */
function brokenAt(...indices: number[]): (i: number) => SlideReadiness {
  return (i) => (indices.includes(i) ? 'error' : 'ready')
}

/** Always paintable — the identity a caller without preloading would pass. */
const allReady = (): SlideReadiness => 'ready'

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

describe('preloadWindow', () => {
  it('covers the slides ahead and behind, current one first', () => {
    expect(preloadWindow(4, 20)).toEqual([4, 5, 6, 7, 8, 9, 3])
    expect(PRELOAD_AHEAD).toBe(5)
    expect(PRELOAD_BEHIND).toBe(1)
  })

  it('wraps around, so the end of a show has the first slides ready', () => {
    expect(preloadWindow(8, 10)).toEqual([8, 9, 0, 1, 2, 3, 7])
  })

  it('deduplicates a set smaller than the window, and is empty for no photos', () => {
    expect(preloadWindow(0, 3)).toEqual([0, 1, 2])
    expect(preloadWindow(0, 1)).toEqual([0])
    expect(preloadWindow(0, 0)).toEqual([])
  })
})

describe('useSlideshow readiness gate', () => {
  it('does not advance while the next slide is still decoding', () => {
    const { result } = renderHook(() =>
      useSlideshow({ length: 3, intervalMs: 1000, readiness: pendingAt(1) }),
    )

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.index).toBe(0)
    expect(result.current.holding).toBe(true)

    // Still pending a while later: the show sits on the current slide.
    act(() => {
      vi.advanceTimersByTime(3000)
    })
    expect(result.current.index).toBe(0)
    expect(result.current.holding).toBe(true)
  })

  it('advances the instant the next slide becomes ready', () => {
    const { result, rerender } = renderHook(
      ({ readiness }: { readiness: (i: number) => SlideReadiness }) =>
        useSlideshow({ length: 3, intervalMs: 1000, readiness }),
      { initialProps: { readiness: pendingAt(1) } },
    )

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.index).toBe(0)

    // The image decodes: no second interval is waited out.
    rerender({ readiness: allReady })
    expect(result.current.index).toBe(1)
    expect(result.current.holding).toBe(false)

    // And the show resumes its normal cadence from there.
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.index).toBe(2)
  })

  it('advances anyway once the bounded wait elapses', () => {
    const { result } = renderHook(() =>
      useSlideshow({ length: 3, intervalMs: 1000, readiness: pendingAt(1) }),
    )

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    act(() => {
      vi.advanceTimersByTime(MAX_HOLD_MS - 1)
    })
    expect(result.current.index).toBe(0)

    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(result.current.index).toBe(1)
    expect(result.current.holding).toBe(false)
  })

  it('skips a slide whose image failed to load', () => {
    const { result } = renderHook(() =>
      useSlideshow({ length: 4, intervalMs: 1000, readiness: brokenAt(1) }),
    )

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    // Slide 1 is broken: the show goes straight to 2 without holding for it.
    expect(result.current.index).toBe(2)
    expect(result.current.holding).toBe(false)
  })

  it('holds for the first paintable slide after a run of broken ones', () => {
    const { result, rerender } = renderHook(
      ({ readiness }: { readiness: (i: number) => SlideReadiness }) =>
        useSlideshow({ length: 4, intervalMs: 1000, readiness }),
      {
        initialProps: {
          readiness: (i: number): SlideReadiness => (i === 1 ? 'error' : 'pending'),
        },
      },
    )

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.index).toBe(0)
    expect(result.current.holding).toBe(true)

    rerender({ readiness: brokenAt(1) })
    expect(result.current.index).toBe(2)
  })

  it('does not wait for decode on manual navigation', () => {
    const { result } = renderHook(() =>
      useSlideshow({
        length: 3,
        intervalMs: 100000,
        autoPlay: false,
        readiness: pendingAt(0, 1, 2),
      }),
    )

    act(() => {
      result.current.next()
    })
    expect(result.current.index).toBe(1)

    act(() => {
      result.current.prev()
    })
    expect(result.current.index).toBe(0)

    act(() => {
      result.current.goTo(2)
    })
    expect(result.current.index).toBe(2)
  })

  it('cancels a hold when paused, and starts a fresh interval on resume', () => {
    const { result } = renderHook(() =>
      useSlideshow({ length: 3, intervalMs: 1000, readiness: pendingAt(1) }),
    )

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.holding).toBe(true)

    act(() => {
      result.current.pause()
    })
    expect(result.current.holding).toBe(false)

    // Resuming does not immediately re-hold: the interval runs again first.
    act(() => {
      result.current.play()
    })
    expect(result.current.holding).toBe(false)
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.holding).toBe(true)
    expect(result.current.index).toBe(0)
  })

  it('changing the interval during a hold neither restarts nor doubles the timer', () => {
    const { result, rerender } = renderHook(
      ({ intervalMs }: { intervalMs: number }) =>
        useSlideshow({ length: 3, intervalMs, readiness: pendingAt(1) }),
      { initialProps: { intervalMs: 1000 } },
    )

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current.holding).toBe(true)

    rerender({ intervalMs: 5000 })
    // The bounded wait keeps its original deadline, measured from the hold.
    act(() => {
      vi.advanceTimersByTime(MAX_HOLD_MS - 1)
    })
    expect(result.current.index).toBe(0)

    act(() => {
      vi.advanceTimersByTime(1)
    })
    // Exactly one advance: a second, doubled timer would have moved on twice.
    expect(result.current.index).toBe(1)
  })

  it('a single-photo show neither holds nor advances', () => {
    const readiness = vi.fn(pendingAt(0))
    const { result } = renderHook(() => useSlideshow({ length: 1, intervalMs: 1000, readiness }))

    act(() => {
      vi.advanceTimersByTime(MAX_HOLD_MS * 2)
    })
    expect(result.current.index).toBe(0)
    expect(result.current.holding).toBe(false)
    expect(readiness).not.toHaveBeenCalled()
  })
})

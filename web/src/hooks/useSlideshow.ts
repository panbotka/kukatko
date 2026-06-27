import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * How many slides ahead of the cursor trigger a background page load, so the
 * next photos are fetched well before the slideshow reaches the loaded end.
 */
export const PRELOAD_AHEAD = 5

/** Options for {@link useSlideshow}. */
export interface UseSlideshowOptions {
  /**
   * The number of photos currently loaded; the cursor ranges over `[0, length)`.
   * It may grow as further pages load — advancing past the end then continues
   * into the newly loaded photos rather than wrapping prematurely.
   */
  length: number
  /** Whether more pages remain behind the loaded set (gates wrap-around). */
  hasMore?: boolean
  /** Auto-advance interval, in milliseconds. */
  intervalMs: number
  /** Whether playback starts automatically. Defaults to true. */
  autoPlay?: boolean
  /** Requests the next page when the cursor nears the loaded end. */
  onLoadMore?: () => void
}

/** Result of {@link useSlideshow}: the cursor, playback state and controls. */
export interface UseSlideshowResult {
  /** The index of the currently shown photo within the loaded set. */
  index: number
  /** Whether the slideshow is auto-advancing. */
  playing: boolean
  /** Advances to the next photo (loads more / wraps to the first at the end). */
  next: () => void
  /** Goes back to the previous photo (wraps to the last loaded at the start). */
  prev: () => void
  /** Starts auto-advancing. */
  play: () => void
  /** Pauses auto-advancing. */
  pause: () => void
  /** Toggles play / pause. */
  toggle: () => void
  /** Jumps to a specific index (clamped to the loaded set). */
  goTo: (index: number) => void
}

/**
 * Drives a slideshow over a (possibly still-growing) list of photos: it owns the
 * current index and play/pause state, auto-advances on the configured interval,
 * and supports manual next/prev with wrap-around. To handle large sets without
 * loading everything up front, it calls `onLoadMore` as the cursor nears the
 * loaded end ({@link PRELOAD_AHEAD} slides ahead) and, when it reaches the very
 * end with more pages pending, waits for them instead of wrapping. A manual
 * next/prev resets the auto-advance timer, and an empty set is a no-op (controls
 * do nothing, nothing auto-advances).
 */
export function useSlideshow(options: UseSlideshowOptions): UseSlideshowResult {
  const { length, hasMore = false, intervalMs, autoPlay = true, onLoadMore } = options

  const [index, setIndex] = useState(0)
  const [playing, setPlaying] = useState(autoPlay)

  // Refs let the stable next/prev callbacks read the latest values without being
  // re-created on every change (which would needlessly re-arm the timer).
  const lengthRef = useRef(length)
  lengthRef.current = length
  const hasMoreRef = useRef(hasMore)
  hasMoreRef.current = hasMore
  const onLoadMoreRef = useRef(onLoadMore)
  onLoadMoreRef.current = onLoadMore

  const next = useCallback(() => {
    setIndex((i) => {
      const len = lengthRef.current
      if (len === 0) {
        return 0
      }
      if (i + 1 < len) {
        return i + 1
      }
      // At the loaded end: wait for more pages if any, otherwise wrap around.
      if (hasMoreRef.current) {
        onLoadMoreRef.current?.()
        return i
      }
      return 0
    })
  }, [])

  const prev = useCallback(() => {
    setIndex((i) => {
      const len = lengthRef.current
      if (len === 0) {
        return 0
      }
      return i - 1 >= 0 ? i - 1 : len - 1
    })
  }, [])

  const goTo = useCallback((target: number) => {
    setIndex(() => {
      const len = lengthRef.current
      if (len === 0) {
        return 0
      }
      return Math.min(len - 1, Math.max(0, target))
    })
  }, [])

  const play = useCallback(() => {
    setPlaying(true)
  }, [])
  const pause = useCallback(() => {
    setPlaying(false)
  }, [])
  const toggle = useCallback(() => {
    setPlaying((p) => !p)
  }, [])

  // Auto-advance. Depending on `index` re-arms the timer after every advance,
  // so a manual next/prev also resets the countdown; depending on `length` picks
  // up freshly loaded pages when playback was waiting at the end.
  useEffect(() => {
    if (!playing || length === 0) {
      return
    }
    const id = window.setTimeout(next, intervalMs)
    return () => {
      window.clearTimeout(id)
    }
  }, [playing, intervalMs, length, index, next])

  // Prefetch upcoming pages before the cursor reaches the loaded end.
  useEffect(() => {
    if (hasMore && index >= length - PRELOAD_AHEAD) {
      onLoadMore?.()
    }
  }, [hasMore, index, length, onLoadMore])

  // Keep the cursor in range if the loaded set ever shrinks.
  useEffect(() => {
    if (index > 0 && index >= length) {
      setIndex(length === 0 ? 0 : length - 1)
    }
  }, [index, length])

  return { index, playing, next, prev, play, pause, toggle, goTo }
}

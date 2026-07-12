import { useRef } from 'react'

import { swipeAction, type SwipeDirection, type TouchPoint } from '../lib/gestures'

/** Options for {@link useSwipeNavigation}. */
export interface UseSwipeNavigationOptions {
  /** Called with the decided direction when a horizontal swipe completes. */
  onSwipe: (direction: SwipeDirection) => void
  /** When false the handlers are inert (still returned, but ignore touches). */
  enabled?: boolean
  /** Minimum horizontal travel (px); defaults to the shared threshold. */
  threshold?: number
}

/** The touch handlers to spread onto the swipeable element. */
export interface SwipeHandlers {
  onTouchStart: (event: React.TouchEvent) => void
  onTouchMove: (event: React.TouchEvent) => void
  onTouchEnd: (event: React.TouchEvent) => void
}

/**
 * Turns a horizontal touch swipe into a prev/next navigation callback, for the
 * photo-detail image. It reads only the start and end touch positions and never
 * calls `preventDefault`, so a mostly-vertical drag falls through to native page
 * scrolling ({@link swipeAction} makes that decision). The gesture is abandoned
 * when a second finger joins (a pinch, not a swipe) or when it starts on an
 * interactive control — any `button`/`a`/form control that is not explicitly
 * marked `data-swipe-surface` — so tapping a face box or the ‹/› arrows keeps
 * working. Desktop mouse input never reaches these touch handlers, so pointer
 * behaviour is unchanged; the swipe is purely additive for touch.
 */
export function useSwipeNavigation({
  onSwipe,
  enabled = true,
  threshold,
}: UseSwipeNavigationOptions): SwipeHandlers {
  // The start point of the in-flight swipe, or null when none is tracked.
  const start = useRef<TouchPoint | null>(null)
  // Set when the gesture is disqualified (multi-touch or an interactive target)
  // so touchend ignores it even though a start point may have been recorded.
  const cancelled = useRef(false)

  const onTouchStart = (event: React.TouchEvent): void => {
    if (!enabled || event.touches.length > 1) {
      start.current = null
      cancelled.current = true
      return
    }
    const target = event.target
    if (target instanceof Element) {
      const interactive = target.closest('button, a, input, textarea, select, [role="button"]')
      if (interactive !== null && !interactive.hasAttribute('data-swipe-surface')) {
        start.current = null
        cancelled.current = true
        return
      }
    }
    cancelled.current = false
    const touch = event.touches[0]
    start.current = { x: touch.clientX, y: touch.clientY }
  }

  const onTouchMove = (event: React.TouchEvent): void => {
    // A second finger turns the gesture into a pinch: abandon the swipe so it
    // does not page when the fingers lift.
    if (event.touches.length > 1) {
      cancelled.current = true
      start.current = null
    }
  }

  const onTouchEnd = (event: React.TouchEvent): void => {
    const origin = start.current
    start.current = null
    if (origin === null || cancelled.current) {
      return
    }
    const touch = event.changedTouches[0] as React.Touch | undefined
    if (touch === undefined) {
      return
    }
    const action = swipeAction(
      touch.clientX - origin.x,
      touch.clientY - origin.y,
      threshold !== undefined ? { threshold } : {},
    )
    if (action !== null) {
      onSwipe(action)
    }
  }

  return { onTouchStart, onTouchMove, onTouchEnd }
}

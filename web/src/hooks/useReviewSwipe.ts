import { useCallback, useRef, useState } from 'react'

import {
  swipeVerdict,
  type SwipeVerdict,
  type TouchPoint,
  VERDICT_HINT_THRESHOLD,
} from '../lib/gestures'

/** Options for {@link useReviewSwipe}. */
export interface UseReviewSwipeOptions {
  /** Called with the verdict a completed swipe decided on. */
  onVerdict: (verdict: SwipeVerdict) => void
  /** When false the handlers are inert — a breather card has nothing to answer. */
  enabled?: boolean
}

/** What {@link useReviewSwipe} gives the card to render itself with. */
export interface ReviewSwipe {
  /** Touch handlers to spread onto the swipe surface. */
  handlers: {
    onTouchStart: (event: React.TouchEvent) => void
    onTouchMove: (event: React.TouchEvent) => void
    onTouchEnd: (event: React.TouchEvent) => void
    onTouchCancel: () => void
  }
  /** How far the finger has travelled from where it went down (px). */
  offset: TouchPoint
  /** The verdict the drag would fire right now, or null while it is undecided. */
  hint: SwipeVerdict | null
  /** True while a finger is down and tracked. */
  dragging: boolean
}

/** No drag in progress — the resting offset, shared so renders stay stable. */
const AT_REST: TouchPoint = { x: 0, y: 0 }

/**
 * The review game's touch input: drag the card right for Ano, left for Ne, down
 * for Nevím.
 *
 * The card follows the finger and names the verdict it is heading for before the
 * finger lifts ({@link VERDICT_HINT_THRESHOLD}), because a gesture whose outcome
 * is only visible after it fires is one nobody dares use on real photos. A drag
 * that never reaches the commit threshold — or one that steers back — simply
 * springs the card home and answers nothing.
 *
 * It ignores gestures that begin on a control (the corner anchor out to the
 * photo, the answer buttons) unless the element opts in with
 * `data-swipe-surface`, and abandons the whole gesture when a second finger
 * joins: two fingers on a photo is a pinch, not an answer. Only touch input is
 * bound, so mouse and keyboard behave exactly as before.
 */
export function useReviewSwipe({ onVerdict, enabled = true }: UseReviewSwipeOptions): ReviewSwipe {
  const start = useRef<TouchPoint | null>(null)
  const cancelled = useRef(false)
  const [offset, setOffset] = useState<TouchPoint>(AT_REST)
  const [dragging, setDragging] = useState(false)

  const reset = useCallback(() => {
    start.current = null
    setOffset(AT_REST)
    setDragging(false)
  }, [])

  const onTouchStart = useCallback(
    (event: React.TouchEvent): void => {
      if (!enabled || event.touches.length > 1) {
        cancelled.current = true
        reset()
        return
      }
      const target = event.target
      if (target instanceof Element) {
        const interactive = target.closest('button, a, input, textarea, select, [role="button"]')
        if (interactive !== null && !interactive.hasAttribute('data-swipe-surface')) {
          cancelled.current = true
          reset()
          return
        }
      }
      cancelled.current = false
      const touch = event.touches[0]
      start.current = { x: touch.clientX, y: touch.clientY }
      setOffset(AT_REST)
      setDragging(true)
    },
    [enabled, reset],
  )

  const onTouchMove = useCallback(
    (event: React.TouchEvent): void => {
      if (event.touches.length > 1) {
        cancelled.current = true
        reset()
        return
      }
      const origin = start.current
      if (origin === null || cancelled.current) {
        return
      }
      const touch = event.touches[0]
      setOffset({ x: touch.clientX - origin.x, y: touch.clientY - origin.y })
    },
    [reset],
  )

  const onTouchEnd = useCallback(
    (event: React.TouchEvent): void => {
      const origin = start.current
      const wasCancelled = cancelled.current
      reset()
      if (origin === null || wasCancelled) {
        return
      }
      const touch = event.changedTouches[0] as React.Touch | undefined
      if (touch === undefined) {
        return
      }
      const verdict = swipeVerdict(touch.clientX - origin.x, touch.clientY - origin.y)
      if (verdict !== null) {
        onVerdict(verdict)
      }
    },
    [onVerdict, reset],
  )

  const onTouchCancel = useCallback((): void => {
    cancelled.current = true
    reset()
  }, [reset])

  return {
    handlers: { onTouchStart, onTouchMove, onTouchEnd, onTouchCancel },
    offset: dragging ? offset : AT_REST,
    hint: dragging ? swipeVerdict(offset.x, offset.y, { threshold: VERDICT_HINT_THRESHOLD }) : null,
    dragging,
  }
}

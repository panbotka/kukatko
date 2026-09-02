import { useEffect, useRef, useState } from 'react'

import {
  IDLE_LONG_PRESS,
  LONG_PRESS_MS,
  LONG_PRESS_SLOP,
  type LongPressEvent,
  type LongPressState,
  longPressStep,
} from '../lib/longPressSelect'

/** The attribute a tile carries so the gesture can name the photo under a finger. */
export const TILE_UID_ATTR = 'data-photo-uid'

/**
 * Whether this device has a touch screen — a feature test, never a user-agent
 * sniff. It is intentionally *not* the coarse-pointer media query
 * (`useCoarsePointer`): a laptop with a touchscreen has a fine primary pointer
 * and still deserves the gesture, and a phone paired with a mouse still has the
 * finger this is for. A device that cannot fire a touch event never reaches the
 * listeners anyway; the test only keeps them (and the context-menu suppression)
 * off a plain desktop entirely.
 *
 * The number of touch points is the question, deliberately, and not
 * `'ontouchstart' in window`: that one is a *dialect* — some engines expose the
 * handler property whether or not there is a screen to touch (jsdom does), so it
 * answers "does this engine know about touch", which is not what is being asked.
 */
export function supportsTouch(): boolean {
  if (typeof navigator === 'undefined') {
    return false
  }
  return navigator.maxTouchPoints > 0
}

/** Reads the tile UID off a node or its nearest tile ancestor. */
function uidOf(node: EventTarget | Element | null): string | null {
  if (!(node instanceof Element)) {
    return null
  }
  return node.closest(`[${TILE_UID_ATTR}]`)?.getAttribute(TILE_UID_ATTR) ?? null
}

/**
 * Names the tile under a viewport point. Hit-testing is the only way to follow a
 * drag: the touch events of a gesture all keep reporting the element the finger
 * *started* on, never the one it is over now. Environments without
 * `elementFromPoint` (jsdom) report nothing, so tests stub it.
 */
function uidAtPoint(x: number, y: number): string | null {
  if (typeof document === 'undefined' || typeof document.elementFromPoint !== 'function') {
    return null
  }
  return uidOf(document.elementFromPoint(x, y))
}

/** A short confirming buzz where the platform offers one. */
function buzz(): void {
  if (typeof navigator === 'undefined' || typeof navigator.vibrate !== 'function') {
    return
  }
  navigator.vibrate(12)
}

/** Options for {@link useLongPressSelect}. */
export interface UseLongPressSelectOptions {
  /** The element the tiles live in; the listeners are attached to it. */
  target: React.RefObject<HTMLElement | null>
  /** When false nothing is attached at all (no selection on offer, no gesture). */
  enabled: boolean
  /** Adds these photos to the selection. Called with one UID at a time as the drag crosses tiles. */
  onSelect: (uids: string[]) => void
  /** Hold before the press engages (ms). Default {@link LONG_PRESS_MS}. */
  holdMs?: number
  /** Travel that cancels a pending press (px). Default {@link LONG_PRESS_SLOP}. */
  slop?: number
}

/**
 * The touch gesture every phone gallery has taught its readers: **press and hold
 * a tile to start selecting, keep the finger down and drag to add every tile it
 * crosses.** It turns bulk curation on a phone from a checkbox-by-checkbox
 * errand into one stroke, and needs no "enter selection mode" step — the first
 * photo it picks is what puts the grid into its selection-first mode.
 *
 * It is wired natively rather than through React's `onTouch*` props on purpose:
 * React attaches those **passively**, and an engaged drag has to call
 * `preventDefault` to stop the page scrolling out from under it. Everything
 * before the press engages is left untouched, so an ordinary flick scrolls
 * exactly as it did — the gesture only takes the scroll once the reader has held
 * still long enough to mean it (`lib/longPressSelect` makes that decision, and
 * is unit-tested on its own).
 *
 * Three things are suppressed while a finger is down on a tile: the platform's
 * own long-press menu (`contextmenu`, which would otherwise offer "save image"
 * over the gesture), the scroll during the drag, and the synthetic `click` the
 * lift would fire — that last one would toggle the very tile the press just
 * selected straight back off.
 *
 * Desktop mouse input is untouched: a mouse fires none of these events, and the
 * listeners are not even attached where {@link supportsTouch} says there is no
 * touch to serve.
 */
export function useLongPressSelect({
  target,
  enabled,
  onSelect,
  holdMs = LONG_PRESS_MS,
  slop = LONG_PRESS_SLOP,
}: UseLongPressSelectOptions): { dragging: boolean } {
  const [dragging, setDragging] = useState(false)
  // The gesture's state lives in a ref: it moves on every touchmove and none of
  // those moves is worth a render — only engaging/ending the drag is.
  const stateRef = useRef<LongPressState>(IDLE_LONG_PRESS)
  const timerRef = useRef<number | null>(null)
  // Read through a ref so a caller passing a fresh closure each render does not
  // tear the listeners down and put them back mid-gesture.
  const onSelectRef = useRef(onSelect)
  onSelectRef.current = onSelect

  useEffect(() => {
    const node = target.current
    if (node === null || !enabled || !supportsTouch()) {
      return
    }

    const clearTimer = (): void => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current)
        timerRef.current = null
      }
    }

    const apply = (event: LongPressEvent): boolean => {
      const step = longPressStep(stateRef.current, event, { slop })
      stateRef.current = step.state
      if (step.added.length > 0) {
        onSelectRef.current([...step.added])
      }
      return step.engaged
    }

    const stop = (): void => {
      clearTimer()
      apply({ type: 'end' })
      setDragging(false)
    }

    const onTouchStart = (event: TouchEvent): void => {
      clearTimer()
      const touch = event.touches[0] as Touch | undefined
      // A second finger is a pinch or a two-finger scroll, and a touch that
      // started off any tile has nothing to select.
      if (event.touches.length !== 1 || touch === undefined) {
        stop()
        return
      }
      const uid = uidOf(event.target)
      if (uid === null) {
        stop()
        return
      }
      apply({ type: 'press', uid, point: { x: touch.clientX, y: touch.clientY } })
      timerRef.current = window.setTimeout(() => {
        timerRef.current = null
        if (apply({ type: 'hold' })) {
          setDragging(true)
          buzz()
        }
      }, holdMs)
    }

    const onTouchMove = (event: TouchEvent): void => {
      const phase = stateRef.current.phase
      if (phase === 'idle') {
        return
      }
      const touch = event.touches[0] as Touch | undefined
      if (event.touches.length !== 1 || touch === undefined) {
        stop()
        return
      }
      const point = { x: touch.clientX, y: touch.clientY }
      if (phase === 'selecting') {
        // The drag owns the finger now: it paints a selection, so the page must
        // not scroll under it.
        event.preventDefault()
        apply({ type: 'move', point, uid: uidAtPoint(point.x, point.y) })
        return
      }
      // Still pending: no hit test needed, only the slop decides. Moving out of
      // it drops the press for good and hands the touch back to the scroller.
      apply({ type: 'move', point, uid: null })
      if (stateRef.current.phase === 'idle') {
        clearTimer()
      }
    }

    const onTouchEnd = (event: TouchEvent): void => {
      const selecting = stateRef.current.phase === 'selecting'
      stop()
      if (selecting) {
        event.preventDefault()
      }
    }

    const onContextMenu = (event: Event): void => {
      // Only while a finger of ours is down — a desktop right-click never gets
      // here, and must keep its menu.
      if (stateRef.current.phase !== 'idle') {
        event.preventDefault()
      }
    }

    node.addEventListener('touchstart', onTouchStart, { passive: true })
    node.addEventListener('touchmove', onTouchMove, { passive: false })
    node.addEventListener('touchend', onTouchEnd, { passive: false })
    node.addEventListener('touchcancel', stop, { passive: true })
    node.addEventListener('contextmenu', onContextMenu)
    return () => {
      clearTimer()
      stateRef.current = IDLE_LONG_PRESS
      node.removeEventListener('touchstart', onTouchStart)
      node.removeEventListener('touchmove', onTouchMove)
      node.removeEventListener('touchend', onTouchEnd)
      node.removeEventListener('touchcancel', stop)
      node.removeEventListener('contextmenu', onContextMenu)
    }
  }, [target, enabled, holdMs, slop])

  return { dragging }
}

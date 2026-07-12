import { useCallback, useEffect, useRef, useState } from 'react'

import {
  clampPan,
  DOUBLE_TAP_SCALE,
  DOUBLE_TAP_SLOP,
  isDoubleTap,
  MIN_SCALE,
  pinchScale,
  swipeAction,
  type SwipeDirection,
  touchDistance,
  type TouchPoint,
} from '../lib/gestures'

/** What the current single-/multi-finger gesture is doing. */
type GestureMode = 'none' | 'swipe' | 'pan' | 'pinch'

/** Mutable, render-independent state of the in-flight gesture. */
interface GestureState {
  mode: GestureMode
  /** Start of the active single-finger gesture (swipe or pan). */
  startTouch: TouchPoint | null
  /** Image translation captured when a pan/pinch began. */
  startTranslate: TouchPoint
  /** Finger spread captured when a pinch began. */
  pinchStartDistance: number
  /** Zoom scale captured when a pinch began. */
  pinchStartScale: number
  /** `event.timeStamp` of the previous tap, for double-tap detection. */
  lastTapTime: number
  /** Position of the previous tap. */
  lastTapPoint: TouchPoint
}

/** The initial (rest) gesture state. */
function freshGesture(): GestureState {
  return {
    mode: 'none',
    startTouch: null,
    startTranslate: { x: 0, y: 0 },
    pinchStartDistance: 0,
    pinchStartScale: MIN_SCALE,
    lastTapTime: Number.NEGATIVE_INFINITY,
    lastTapPoint: { x: 0, y: 0 },
  }
}

/** Options for {@link usePinchZoom}. */
export interface UsePinchZoomOptions {
  /** Called when the user swipes horizontally while NOT zoomed (page prev/next). */
  onSwipe: (direction: SwipeDirection) => void
  /** Changing this resets the zoom (e.g. the shown photo's UID). */
  resetKey: string
  /** When false the handlers are inert. */
  enabled?: boolean
}

/** Zoom state plus the touch handlers to drive it. */
export interface PinchZoom {
  /** Current zoom scale (1 = fit-to-box). */
  scale: number
  /** Current pan translation on the X axis (px). */
  translateX: number
  /** Current pan translation on the Y axis (px). */
  translateY: number
  /** Whether the image is magnified beyond fit-to-box. */
  isZoomed: boolean
  /** True while a touch gesture is in progress (suppresses the CSS transition). */
  gesturing: boolean
  /** Touch handlers to spread onto the zoom surface. */
  handlers: {
    onTouchStart: (event: React.TouchEvent) => void
    onTouchMove: (event: React.TouchEvent) => void
    onTouchEnd: (event: React.TouchEvent) => void
  }
  /** Resets the zoom/pan back to fit-to-box. */
  reset: () => void
}

function pointFromTouch(touch: React.Touch): TouchPoint {
  return { x: touch.clientX, y: touch.clientY }
}

/** Viewport centre, used as the transform focal point / pan bound reference. */
function viewportSize(): { width: number; height: number; centre: TouchPoint } {
  const width = typeof window === 'undefined' ? 0 : window.innerWidth
  const height = typeof window === 'undefined' ? 0 : window.innerHeight
  return { width, height, centre: { x: width / 2, y: height / 2 } }
}

/**
 * Touch pinch-to-zoom + double-tap-to-zoom for the fullscreen lightbox image,
 * with drag-to-pan while zoomed and horizontal swipe-to-page while at rest.
 *
 * Two fingers pinch the scale between {@link MIN_SCALE} and the shared maximum;
 * a double-tap toggles between fit-to-box and {@link DOUBLE_TAP_SCALE}, zooming
 * toward the tapped point; a single-finger drag pans a zoomed image (clamped so
 * it cannot leave the screen) or, when not zoomed, decides a prev/next swipe via
 * {@link swipeAction} and calls `onSwipe`. The zoom resets whenever `resetKey`
 * changes (the shown photo), so paging always starts fit-to-box. The surface is
 * expected to carry `touch-action: none`, so no `preventDefault` is needed and
 * the browser never fights the gesture; desktop mouse/keyboard input does not go
 * through these handlers and is unaffected.
 */
export function usePinchZoom({
  onSwipe,
  resetKey,
  enabled = true,
}: UsePinchZoomOptions): PinchZoom {
  const [scale, setScale] = useState(MIN_SCALE)
  const [translate, setTranslate] = useState<TouchPoint>({ x: 0, y: 0 })
  const [gesturing, setGesturing] = useState(false)
  const gesture = useRef<GestureState>(freshGesture())

  const reset = useCallback((): void => {
    gesture.current = freshGesture()
    setScale(MIN_SCALE)
    setTranslate({ x: 0, y: 0 })
    setGesturing(false)
  }, [])

  // A new photo starts fresh at fit-to-box (closing the lightbox unmounts it).
  useEffect(() => {
    reset()
  }, [resetKey, reset])

  const toggleDoubleTapZoom = useCallback((point: TouchPoint): void => {
    setScale((current) => {
      if (current > MIN_SCALE) {
        setTranslate({ x: 0, y: 0 })
        return MIN_SCALE
      }
      const { width, height, centre } = viewportSize()
      // Translate so the tapped point stays put as the image grows.
      const focal = {
        x: -(point.x - centre.x) * (DOUBLE_TAP_SCALE - 1),
        y: -(point.y - centre.y) * (DOUBLE_TAP_SCALE - 1),
      }
      setTranslate(clampPan(focal, DOUBLE_TAP_SCALE, width, height))
      return DOUBLE_TAP_SCALE
    })
  }, [])

  const onTouchStart = useCallback(
    (event: React.TouchEvent): void => {
      if (!enabled) {
        return
      }
      // Let the close/prev/next buttons handle their own taps.
      if (event.target instanceof Element && event.target.closest('button') !== null) {
        return
      }
      const g = gesture.current
      setGesturing(true)
      if (event.touches.length >= 2) {
        g.mode = 'pinch'
        g.pinchStartDistance = touchDistance(
          pointFromTouch(event.touches[0]),
          pointFromTouch(event.touches[1]),
        )
        g.pinchStartScale = scale
        g.startTranslate = translate
        g.startTouch = null
        return
      }
      const point = pointFromTouch(event.touches[0])
      g.startTouch = point
      g.startTranslate = translate
      // Scale stays constant during a single-finger pan; capture it so the move
      // handler can clamp without reading (mutable) render state.
      g.pinchStartScale = scale
      g.mode = scale > MIN_SCALE ? 'pan' : 'swipe'
    },
    [enabled, scale, translate],
  )

  const onTouchMove = useCallback((event: React.TouchEvent): void => {
    const g = gesture.current
    if (g.mode === 'pinch' && event.touches.length >= 2) {
      const distance = touchDistance(
        pointFromTouch(event.touches[0]),
        pointFromTouch(event.touches[1]),
      )
      setScale(pinchScale(g.pinchStartScale, g.pinchStartDistance, distance))
      return
    }
    if (g.mode === 'pan' && g.startTouch !== null && event.touches.length === 1) {
      const touch = pointFromTouch(event.touches[0])
      const { width, height } = viewportSize()
      const next = {
        x: g.startTranslate.x + (touch.x - g.startTouch.x),
        y: g.startTranslate.y + (touch.y - g.startTouch.y),
      }
      setTranslate(clampPan(next, g.pinchStartScale, width, height))
    }
  }, [])

  const onTouchEnd = useCallback(
    (event: React.TouchEvent): void => {
      if (event.touches.length === 0) {
        setGesturing(false)
      }
      const g = gesture.current
      if (g.mode === 'pinch') {
        // Snap a barely-zoomed image back to fit-to-box when the pinch ends.
        setScale((current) => {
          if (current <= MIN_SCALE + 0.05) {
            setTranslate({ x: 0, y: 0 })
            return MIN_SCALE
          }
          return current
        })
        g.mode = 'none'
        g.startTouch = null
        return
      }
      const origin = g.startTouch
      g.mode = 'none'
      g.startTouch = null
      if (origin === null) {
        return
      }
      const touch = event.changedTouches[0] as React.Touch | undefined
      if (touch === undefined) {
        return
      }
      const end = pointFromTouch(touch)
      const dx = end.x - origin.x
      const dy = end.y - origin.y
      const moved = Math.hypot(dx, dy)
      if (moved < DOUBLE_TAP_SLOP) {
        // A tap: pair it with the previous one to detect a double-tap.
        const gap = event.timeStamp - g.lastTapTime
        if (isDoubleTap(gap, touchDistance(end, g.lastTapPoint))) {
          toggleDoubleTapZoom(end)
          g.lastTapTime = Number.NEGATIVE_INFINITY
        } else {
          g.lastTapTime = event.timeStamp
          g.lastTapPoint = end
        }
        return
      }
      // A drag: never a tap. Page only when not zoomed (a zoomed drag panned).
      g.lastTapTime = Number.NEGATIVE_INFINITY
      if (scale <= MIN_SCALE) {
        const action = swipeAction(dx, dy)
        if (action !== null) {
          onSwipe(action)
        }
      }
    },
    [onSwipe, scale, toggleDoubleTapZoom],
  )

  return {
    scale,
    translateX: translate.x,
    translateY: translate.y,
    isZoomed: scale > MIN_SCALE,
    gesturing,
    handlers: { onTouchStart, onTouchMove, onTouchEnd },
    reset,
  }
}

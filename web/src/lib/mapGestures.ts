/**
 * Touch gesture handling for the Leaflet maps.
 *
 * A tall map inside a scrolling page is a scroll trap on a phone: Leaflet claims
 * every one-finger drag for panning, so a user swiping to reach what is *below*
 * the map only pans it and can never scroll past. The fix is the convention
 * every embedded map (and the `leaflet-gesture-handling` plugin) settled on —
 * **one finger scrolls the page, two fingers drive the map** — implemented here
 * from Leaflet's own options instead of a dependency.
 *
 * It rests on a detail of Leaflet's stylesheet: the container's `touch-action`
 * follows the enabled handlers. With drag off and pinch on it is
 * `pan-x pan-y`, i.e. the *browser* owns one-finger panning (the page scrolls)
 * while the pinch still reaches Leaflet — and Leaflet's touch-zoom handler pans
 * as well as zooms, so two fingers move the map. Enabling drag again for the
 * span of a two-finger gesture tightens that to `touch-action: none`, so the
 * page stops scrolling underneath it.
 *
 * Only for coarse pointers: a mouse has no second finger, so on the desktop the
 * map keeps Leaflet's defaults (drag to pan, wheel to zoom).
 */

/** Media query identifying a device whose primary pointer is a finger. */
export const COARSE_POINTER_QUERY = '(pointer: coarse)'

/**
 * How far (in CSS pixels) a one-finger touch has to travel before it counts as
 * a drag rather than a tap. Below it the user is tapping a marker, and flashing
 * "use two fingers" at every tap would be noise.
 */
const DRAG_THRESHOLD_PX = 8

/**
 * Things on the map that answer a one-finger touch on their own: the location
 * picker's draggable pin and every Leaflet control. Dragging a pin is not a
 * failed attempt to pan, so telling the user to use two fingers there would be
 * wrong advice at exactly the wrong moment.
 */
const SELF_HANDLED_SELECTOR = '.leaflet-marker-draggable, .leaflet-control'

/** Whether the touch landed on something that handles one-finger drags itself. */
function isSelfHandled(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(SELF_HANDLED_SELECTOR) !== null
}

/** Narrows an unknown value to a usable {@link MediaQueryList}. */
function isMediaQueryList(value: unknown): value is MediaQueryList {
  return typeof value === 'object' && value !== null && 'matches' in value
}

/**
 * Reports whether this device should get the two-finger gesture handling —
 * true when the primary pointer is coarse (phone, tablet). Environments without
 * `matchMedia` (jsdom exposes the function but may return `undefined`) answer
 * `false`, so they keep Leaflet's mouse behaviour.
 */
export function prefersTouchGestures(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return false
  }
  const result: unknown = window.matchMedia(COARSE_POINTER_QUERY)
  return isMediaQueryList(result) ? result.matches : false
}

/**
 * The slice of the Leaflet map this module drives. Structural, so a test can
 * hand it a stub without building a real map.
 */
export interface DraggableMap {
  dragging: { enable: () => void; disable: () => void }
}

/**
 * Wires two-finger gesture handling onto an already-created map whose
 * `dragging` starts **disabled** (see {@link prefersTouchGestures} for when to
 * do that). Drag is turned on only while at least two fingers are down, so a
 * one-finger swipe is left to the page scroller.
 *
 * `onOneFingerDrag` fires once per gesture, when a single finger has actually
 * travelled far enough to be a drag — the moment the user is trying to pan and
 * needs to be told that two fingers do it. It stays quiet for a touch that
 * started on something with its own one-finger behaviour (see
 * {@link SELF_HANDLED_SELECTOR}).
 *
 * Returns a function that detaches the listeners and leaves dragging disabled.
 */
export function enableTwoFingerPan(
  map: DraggableMap,
  container: HTMLElement,
  onOneFingerDrag: () => void,
): () => void {
  // Where the current one-finger gesture started, or null while no single
  // finger is down (or once this gesture has already reported its drag).
  let oneFingerStart: { x: number; y: number } | null = null

  const handleTouchStart = (event: TouchEvent) => {
    if (event.touches.length >= 2) {
      oneFingerStart = null
      map.dragging.enable()
      return
    }
    // Disabling before Leaflet's own drag handler can see this touch is what
    // keeps the page scrollable: this listener is attached first, and disabling
    // removes Leaflet's listener before the event reaches it.
    map.dragging.disable()
    oneFingerStart =
      event.touches.length === 1 && !isSelfHandled(event.target)
        ? { x: event.touches[0].clientX, y: event.touches[0].clientY }
        : null
  }

  const handleTouchMove = (event: TouchEvent) => {
    if (oneFingerStart === null || event.touches.length !== 1) {
      return
    }
    const touch = event.touches[0]
    if (
      Math.abs(touch.clientX - oneFingerStart.x) < DRAG_THRESHOLD_PX &&
      Math.abs(touch.clientY - oneFingerStart.y) < DRAG_THRESHOLD_PX
    ) {
      return
    }
    // Report once per gesture, not once per move event.
    oneFingerStart = null
    onOneFingerDrag()
  }

  const handleTouchEnd = (event: TouchEvent) => {
    if (event.touches.length >= 2) {
      return
    }
    // The two-finger gesture is over (or never started): hand one-finger
    // swipes back to the page before the next one begins.
    oneFingerStart = null
    map.dragging.disable()
  }

  // Passive: this never calls `preventDefault` — Leaflet's own touch-zoom
  // handler does that for the gestures it claims.
  const options: AddEventListenerOptions = { passive: true }
  container.addEventListener('touchstart', handleTouchStart, options)
  container.addEventListener('touchmove', handleTouchMove, options)
  container.addEventListener('touchend', handleTouchEnd, options)
  container.addEventListener('touchcancel', handleTouchEnd, options)

  return () => {
    container.removeEventListener('touchstart', handleTouchStart, options)
    container.removeEventListener('touchmove', handleTouchMove, options)
    container.removeEventListener('touchend', handleTouchEnd, options)
    container.removeEventListener('touchcancel', handleTouchEnd, options)
  }
}

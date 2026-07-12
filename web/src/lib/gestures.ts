/**
 * Pure, DOM-free gesture-decision helpers shared by the touch hooks
 * (`useSwipeNavigation`, `usePinchZoom`). Keeping the direction/threshold/scale
 * arithmetic here — separate from the React event plumbing — means the decision
 * that actually drives navigation and zoom can be unit-tested directly, without
 * simulating a full touch sequence in jsdom.
 */

/** A single touch point in viewport (client) coordinates. */
export interface TouchPoint {
  /** Horizontal client coordinate (px). */
  x: number
  /** Vertical client coordinate (px). */
  y: number
}

/** A page-navigation direction decoded from a horizontal swipe. */
export type SwipeDirection = 'prev' | 'next'

/** Default minimum horizontal travel (px) before a drag counts as a swipe. */
export const SWIPE_THRESHOLD = 50

/** Tuning for {@link swipeAction}. */
export interface SwipeOptions {
  /** Minimum horizontal travel (px). Default {@link SWIPE_THRESHOLD}. */
  threshold?: number
  /**
   * How dominant the horizontal component must be: `|dx|` has to exceed
   * `ratio × |dy|`, so a mostly-vertical drag (page scrolling) never pages.
   * Default 1 (strictly more horizontal than vertical).
   */
  ratio?: number
}

/**
 * Decides whether a drag of `(dx, dy)` pixels is a horizontal navigation swipe.
 * A leftward swipe (`dx < 0`) pages to the NEXT photo and a rightward swipe to
 * the PREVIOUS one — the same direction convention the on-image ‹/› arrows use.
 * A drag shorter than the threshold, or one that is not clearly more horizontal
 * than vertical (so native vertical scrolling keeps working), yields `null`.
 */
export function swipeAction(
  dx: number,
  dy: number,
  options: SwipeOptions = {},
): SwipeDirection | null {
  const { threshold = SWIPE_THRESHOLD, ratio = 1 } = options
  const ax = Math.abs(dx)
  const ay = Math.abs(dy)
  if (ax < threshold || ax <= ay * ratio) {
    return null
  }
  return dx < 0 ? 'next' : 'prev'
}

/** Euclidean distance between two touch points. */
export function touchDistance(a: TouchPoint, b: TouchPoint): number {
  return Math.hypot(a.x - b.x, a.y - b.y)
}

/** Midpoint between two touch points — the focal point of a pinch. */
export function touchMidpoint(a: TouchPoint, b: TouchPoint): TouchPoint {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
}

/** Smallest zoom scale (fit-to-box, no magnification). */
export const MIN_SCALE = 1
/** Largest zoom scale a pinch/double-tap may reach. */
export const MAX_SCALE = 4
/** The zoom level a double-tap toggles to (from {@link MIN_SCALE}). */
export const DOUBLE_TAP_SCALE = 2.5

/** Clamps a zoom scale into the `[min, max]` range. */
export function clampScale(
  scale: number,
  min: number = MIN_SCALE,
  max: number = MAX_SCALE,
): number {
  return Math.min(max, Math.max(min, scale))
}

/**
 * The new zoom scale during a pinch: the scale at the pinch's start multiplied
 * by how much the two fingers have spread apart (`current / start` distance),
 * clamped into `[MIN_SCALE, MAX_SCALE]`. A non-positive start distance (both
 * fingers reported at the same point) leaves the scale unchanged.
 */
export function pinchScale(
  startScale: number,
  startDistance: number,
  currentDistance: number,
): number {
  if (startDistance <= 0) {
    return clampScale(startScale)
  }
  return clampScale(startScale * (currentDistance / startDistance))
}

/** Maximum gap (ms) between two taps for them to count as a double-tap. */
export const DOUBLE_TAP_MS = 300
/** Maximum travel (px) a tap may drift and still count toward a double-tap. */
export const DOUBLE_TAP_SLOP = 30

/**
 * Reports whether two consecutive taps form a double-tap: they must be close in
 * time (`0 ≤ dt ≤ DOUBLE_TAP_MS`) and close in space (`moveDistance ≤
 * DOUBLE_TAP_SLOP`). Callers seed `dt` with a very large value before the first
 * tap so a lone tap never registers.
 */
export function isDoubleTap(dtMs: number, moveDistance: number): boolean {
  return dtMs >= 0 && dtMs <= DOUBLE_TAP_MS && moveDistance <= DOUBLE_TAP_SLOP
}

/**
 * Clamps a pan translation so a zoomed image cannot be dragged entirely off
 * screen: at scale `s` the image overflows the viewport by `(s − 1)` of its
 * size, so each axis may travel at most half that overflow in either direction.
 */
export function clampPan(
  translate: TouchPoint,
  scale: number,
  viewportWidth: number,
  viewportHeight: number,
): TouchPoint {
  const maxX = (Math.max(scale, MIN_SCALE) - 1) * (viewportWidth / 2)
  const maxY = (Math.max(scale, MIN_SCALE) - 1) * (viewportHeight / 2)
  return {
    x: Math.min(maxX, Math.max(-maxX, translate.x)),
    y: Math.min(maxY, Math.max(-maxY, translate.y)),
  }
}

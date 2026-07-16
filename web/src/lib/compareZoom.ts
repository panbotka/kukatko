/**
 * The pure zoom/pan math behind the duplicate compare view's synchronised stage.
 *
 * It is deliberately separate from `lib/gestures.ts` (which backs `usePinchZoom`):
 * that one is touch-only and measures against the whole viewport, because the
 * lightbox's image fills the screen. Here two images share a mouse-driven zoom
 * inside their own half-width panes, so the box is passed in and both panes are
 * driven from one {@link ZoomView}. One view object for two images is what makes
 * the zoom synchronised — there is no syncing step to get wrong.
 */

/**
 * A zoom/pan state, applied as `translate(x, y) scale(scale)` with the CSS default
 * centre transform-origin. `x`/`y` are pixels in the pane's own coordinate space.
 */
export interface ZoomView {
  scale: number
  x: number
  y: number
}

/** The dimensions of a pane, in pixels. */
export interface Box {
  width: number
  height: number
}

/** Fit-to-pane: no magnification, no pan. The state both panes start and reset to. */
export const IDENTITY_VIEW: ZoomView = { scale: 1, x: 0, y: 0 }

/**
 * The smallest scale. Zooming out past fit-to-pane would shrink the photos inside
 * their panes, which compares nothing — the point of the view is to get closer.
 */
export const MIN_SCALE = 1

/**
 * The largest scale. Eight times is past the pixel level on any realistic pane, far
 * enough to tell a soft JPEG re-encode from the original, and bounded so a fast
 * scroll cannot leave the user lost in a grey field.
 */
export const MAX_SCALE = 8

/** The scale multiplier applied per wheel notch or +/- zoom-button press. */
export const ZOOM_STEP = 1.3

/** Clamps a value into [lo, hi]. */
function clamp(value: number, lo: number, hi: number): number {
  return Math.min(Math.max(value, lo), hi)
}

/**
 * Clamps a view into range: the scale into [MIN_SCALE, MAX_SCALE], and the pan so
 * the magnified image cannot be dragged clean out of its pane.
 *
 * The pan bound is `(scale - 1) * size / 2`, which is exact when the image fills
 * the pane and slightly generous when it is letterboxed (the pane is a fixed box
 * but the photos inside it are `object-fit: contain`, so their aspect ratios differ
 * from it and from each other). Being generous only lets a corner be dragged a
 * little further than strictly needed; measuring each rendered content box instead
 * would tie this pure function to the DOM and, worse, would give the two panes
 * different bounds — at which point the zoom would silently stop being synchronised,
 * which is the one thing this view must not do.
 */
export function clampView(view: ZoomView, box: Box): ZoomView {
  const scale = clamp(view.scale, MIN_SCALE, MAX_SCALE)
  const maxX = (Math.max(box.width, 0) * (scale - 1)) / 2
  const maxY = (Math.max(box.height, 0) * (scale - 1)) / 2
  return { scale, x: clamp(view.x, -maxX, maxX), y: clamp(view.y, -maxY, maxY) }
}

/**
 * Zooms `view` by `factor` about the point (px, py), given in pane coordinates with
 * the origin at the pane's top-left corner. The point under the cursor stays under
 * the cursor, which is what makes wheel-zoom feel like zooming into the detail you
 * are looking at rather than into the middle of the photo.
 *
 * Both panes are fed the same (px, py) — the cursor's position within whichever
 * pane it is over — so zooming the left photo's left eye zooms the right photo's
 * left eye, and the two stay comparable.
 */
export function zoomAt(view: ZoomView, factor: number, px: number, py: number, box: Box): ZoomView {
  const next = clamp(view.scale * factor, MIN_SCALE, MAX_SCALE)
  // The cursor relative to the pane centre, the origin the CSS transform scales
  // about.
  const cx = px - box.width / 2
  const cy = py - box.height / 2
  // Solving `c = t + s·q` for the fixed image point q and re-applying it at the new
  // scale gives `t' = c - (s'/s)·(c - t)`.
  const ratio = next / view.scale
  return clampView(
    { scale: next, x: cx - ratio * (cx - view.x), y: cy - ratio * (cy - view.y) },
    box,
  )
}

/**
 * Zooms `view` by `factor` about the pane's centre — what the +/- buttons and the
 * keyboard do, having no cursor position to zoom about.
 */
export function zoomCentre(view: ZoomView, factor: number, box: Box): ZoomView {
  return zoomAt(view, factor, box.width / 2, box.height / 2, box)
}

/**
 * Pans `view` by a pixel delta, clamped to the pane. A drag on either photo moves
 * both, since both render the same view.
 */
export function panBy(view: ZoomView, dx: number, dy: number, box: Box): ZoomView {
  return clampView({ scale: view.scale, x: view.x + dx, y: view.y + dy }, box)
}

/** Whether the view is magnified past fit-to-pane (so dragging pans). */
export function isZoomed(view: ZoomView): boolean {
  return view.scale > MIN_SCALE
}

/** The CSS `transform` value that renders `view`. */
export function viewTransform(view: ZoomView): string {
  return `translate(${String(view.x)}px, ${String(view.y)}px) scale(${String(view.scale)})`
}

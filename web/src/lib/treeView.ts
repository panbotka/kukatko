/**
 * The pure pan/zoom math of the family tree's stage.
 *
 * It is deliberately not `lib/compareZoom.ts`, which magnifies a photograph
 * inside a fixed pane and therefore never zooms *out* (`MIN_SCALE` is 1 there:
 * shrinking two photos compares nothing). A family tree is the opposite problem
 * — four generations are several thousand layout units wide — so this one zooms
 * out as readily as in, opens either fitted or at natural size on the person it
 * is about ({@link initialView}), and pans without bounds, because a tree has no
 * edge that means anything to keep a reader inside.
 *
 * A {@link TreeView} is applied as `translate(x, y) scale(scale)` on the SVG's
 * content group, with the default top-left origin: `x`/`y` are viewport pixels,
 * everything inside the group stays in layout units, and nothing in the drawing
 * has to know it is being looked at from anywhere in particular.
 */

/** A pan/zoom state: content scaled about the origin, then translated. */
export interface TreeView {
  scale: number
  x: number
  y: number
}

/** A width and a height, in whatever unit the caller is measuring in. */
export interface Size {
  width: number
  height: number
}

/**
 * The smallest scale. A twentieth is far enough out to hold a village's worth of
 * generations in one screen while a name is still a legible smudge of a word;
 * past that the drawing is a texture and the reader has lost the thread.
 */
export const MIN_SCALE = 0.05

/** The largest scale. Twice life size is a comfortable reading zoom on a phone. */
export const MAX_SCALE = 2

/** The scale multiplier of one wheel notch or one press of the zoom buttons. */
export const ZOOM_STEP = 1.25

/** Clamps a value into [lo, hi]. */
function clamp(value: number, lo: number, hi: number): number {
  return Math.min(Math.max(value, lo), hi)
}

/** Clamps a scale into [{@link MIN_SCALE}, {@link MAX_SCALE}]. */
export function clampScale(scale: number): number {
  return clamp(scale, MIN_SCALE, MAX_SCALE)
}

/**
 * The view that shows the whole drawing: scaled down to fit and centred, never
 * scaled *up* past 1 — a two-person tree blown up to fill a desktop screen would
 * look like a mistake rather than like a small family.
 *
 * A viewport or a drawing of zero size (a first render, before the container has
 * been measured) yields the identity view rather than a division by zero.
 */
export function fitView(content: Size, viewport: Size): TreeView {
  if (content.width <= 0 || content.height <= 0 || viewport.width <= 0 || viewport.height <= 0) {
    return { scale: 1, x: 0, y: 0 }
  }
  const scale = clampScale(
    Math.min(viewport.width / content.width, viewport.height / content.height, 1),
  )
  return {
    scale,
    x: (viewport.width - content.width * scale) / 2,
    y: Math.max((viewport.height - content.height * scale) / 2, 0),
  }
}

/**
 * The view a drawing opens at: the whole of it when it fits at its natural size
 * (a scale of 1, where a card's name is 14 px and its face 40 px), otherwise
 * natural size **centred on the person the page is about**.
 *
 * Fitting a three-generation family into a 390 px phone drew cards with 8 px
 * names and 16 px faces — nothing on them could be read or tapped, and the first
 * thing every reader did was zoom in on the one person they had come for. So the
 * drawing now opens there and the reader pans out to the rest; the fit button
 * still shows the whole of it on demand. On a desktop a family that fits keeps
 * opening fitted, because there nothing is lost by seeing it all.
 *
 * Without a `focus` (a drawing about nobody in particular) the fitted view is
 * the only honest answer.
 */
export function initialView(
  content: Size,
  viewport: Size,
  focus: { x: number; y: number } | null | undefined,
): TreeView {
  const fitted = fitView(content, viewport)
  if (fitted.scale >= 1 || focus == null) {
    return fitted
  }
  return centreOn({ scale: 1, x: 0, y: 0 }, focus, viewport)
}

/**
 * Zooms `view` by `factor` about the point (px, py), given in viewport pixels
 * from the stage's top-left corner. The point under the cursor stays under the
 * cursor, which is what makes wheel-zoom feel like zooming into the branch you
 * are looking at rather than into the middle of the family.
 *
 * The factor is applied to the clamped scale, so holding the wheel down at the
 * limit does not build up an invisible reserve that has to be unwound before
 * the drawing moves again.
 */
export function zoomAt(view: TreeView, factor: number, px: number, py: number): TreeView {
  const scale = clampScale(view.scale * factor)
  const ratio = scale / view.scale
  return {
    scale,
    x: px - (px - view.x) * ratio,
    y: py - (py - view.y) * ratio,
  }
}

/** Drags the drawing by a viewport-pixel delta. */
export function panBy(view: TreeView, dx: number, dy: number): TreeView {
  return { scale: view.scale, x: view.x + dx, y: view.y + dy }
}

/**
 * Moves the view so that a point given in **layout** units sits in the middle of
 * the viewport, at the current scale. It is how the page returns to the person
 * the tree is rooted at after the reader has wandered off into a side branch.
 */
export function centreOn(
  view: TreeView,
  point: { x: number; y: number },
  viewport: Size,
): TreeView {
  return {
    scale: view.scale,
    x: viewport.width / 2 - point.x * view.scale,
    y: viewport.height / 2 - point.y * view.scale,
  }
}

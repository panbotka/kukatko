import { type CSSProperties } from 'react'

import { type Bbox } from '../services/people'
import { type PhotoEdit } from '../services/photos'

import { type Frame, rotateBbox } from './faceGeometry'

/**
 * A neutral edit: no crop, no rotation, neutral brightness and contrast. Used to
 * seed the edit controls and to clear an edit.
 */
export const NEUTRAL_EDIT: PhotoEdit = { rotation: 0, brightness: 0, contrast: 0 }

/** The rotations the edit UI offers, in clockwise quarter turns. */
export const ROTATIONS: readonly number[] = [0, 90, 180, 270]

/**
 * Reports whether the edit carries a complete crop rectangle (all four
 * normalised coordinates set). Crop is all-or-nothing.
 */
export function hasCrop(edit: PhotoEdit): boolean {
  return (
    edit.crop_x !== undefined &&
    edit.crop_y !== undefined &&
    edit.crop_w !== undefined &&
    edit.crop_h !== undefined
  )
}

/**
 * Reports whether the edit leaves the image unchanged: no crop, no rotation and
 * neutral brightness and contrast.
 */
export function isIdentityEdit(edit: PhotoEdit): boolean {
  return !hasCrop(edit) && edit.rotation === 0 && edit.brightness === 0 && edit.contrast === 0
}

/**
 * Turns a rotation by `quarters` quarter turns, positive clockwise, and
 * normalises the result into the 0/90/180/270 the backend accepts — including for
 * a negative turn, where a plain `%` in JavaScript would yield -90 and the save
 * would be rejected.
 */
export function rotateBy(rotation: number, quarters: number): number {
  return normalizeRotation(rotation + quarters * 90)
}

/** Returns the next clockwise quarter-turn rotation after the given one. */
export function rotateRight(rotation: number): number {
  return rotateBy(rotation, 1)
}

/** Returns the next counter-clockwise quarter-turn rotation after the given one. */
export function rotateLeft(rotation: number): number {
  return rotateBy(rotation, -1)
}

/**
 * Builds the CSS `filter` value matching the backend's brightness/contrast
 * rendering: each is applied as `brightness(1+b)` / `contrast(1+c)` so the live
 * preview matches the downloaded image. Returns `'none'` when both are neutral.
 */
export function editFilter(edit: PhotoEdit): string {
  const parts: string[] = []
  if (edit.brightness !== 0) {
    parts.push(`brightness(${1 + edit.brightness})`)
  }
  if (edit.contrast !== 0) {
    parts.push(`contrast(${1 + edit.contrast})`)
  }
  return parts.length > 0 ? parts.join(' ') : 'none'
}

/** Builds the CSS `transform` for the edit's rotation, or `'none'` when upright. */
export function editTransform(edit: PhotoEdit): string {
  return edit.rotation !== 0 ? `rotate(${edit.rotation}deg)` : 'none'
}

/**
 * Builds the CSS `clip-path` `inset(...)` that crops the preview to the edit's
 * normalised rectangle, or `undefined` when there is no crop.
 */
export function cropClipPath(edit: PhotoEdit): string | undefined {
  const { crop_x, crop_y, crop_w, crop_h } = edit
  if (
    crop_x === undefined ||
    crop_y === undefined ||
    crop_w === undefined ||
    crop_h === undefined
  ) {
    return undefined
  }
  const pct = (n: number): string => `${(n * 100).toFixed(4)}%`
  return `inset(${pct(crop_y)} ${pct(1 - (crop_x + crop_w))} ${pct(1 - (crop_y + crop_h))} ${pct(crop_x)})`
}

/**
 * Combines an edit into the CSS the preview applies: a brightness/contrast
 * `filter`, a rotation `transform`, and a crop `clip-path`. Neutral parts are
 * omitted so the style stays minimal.
 *
 * The edit to hand in is a **delta** ({@link editDelta}), never the saved edit
 * on its own: the renditions already carry the saved edit, and styling them with
 * it a second time is how a photo saved at 90° came to lie on its side.
 */
export function editPreviewStyle(edit: PhotoEdit): CSSProperties {
  const style: CSSProperties = {}
  const filter = editFilter(edit)
  if (filter !== 'none') {
    style.filter = filter
  }
  const transform = editTransform(edit)
  if (transform !== 'none') {
    style.transform = transform
  }
  const clip = cropClipPath(edit)
  if (clip !== undefined) {
    style.clipPath = clip
  }
  return style
}

/** Normalises any multiple of 90 — negative included — into the 0/90/180/270 the API stores. */
export function normalizeRotation(rotation: number): number {
  return ((rotation % 360) + 360) % 360
}

/** Whether a rotation swaps the picture's width and height. */
export function isQuarterTurn(rotation: number): boolean {
  const angle = normalizeRotation(rotation)
  return angle === 90 || angle === 270
}

/**
 * Rounds a delta multiplier back onto a short decimal, so a ratio such as
 * `1.5 / 1.5` lands on `0` rather than on a `2e-16` that would put a pointless
 * `brightness(1)` into the style.
 */
function tidy(n: number): number {
  return Math.round(n * 1e6) / 1e6
}

/**
 * The adjustment CSS must still apply to a rendition that already carries
 * `rendered` so that the picture on screen shows `shown` — the **delta**
 * between the two edits.
 *
 * The renditions the viewer draws are not the original: the thumbnailer bakes
 * the saved edit into them (`internal/thumb` `WithEdits`), so a rotation the
 * server has already turned must not be turned again by a transform. Only what
 * the draft changes on top of the saved edit is the client's to draw:
 *
 * - rotation: the difference, normalised into a quarter turn;
 * - brightness and contrast: the backend and CSS both apply them as
 *   multipliers (`1 + value`), so the delta is the ratio of the two multipliers
 *   — approximate once both are non-neutral (a brightness applied after a
 *   contrast is not the same picture as the reverse), exact whenever one side is
 *   neutral, which is every real preview; a rendered multiplier of zero (a
 *   fully black picture) has no ratio, so the draft's own value is used;
 * - crop: the draft's rectangle expressed inside the rendered one, then turned
 *   into the rendered frame (the server crops before it rotates). A pixel the
 *   rendition no longer has cannot be brought back, so a draft that lifts or
 *   widens a saved crop previews as the rendered crop, i.e. no clip at all.
 *
 * Two identical edits yield the neutral edit, and a rendition of the neutral
 * edit yields `shown` itself.
 */
export function editDelta(rendered: PhotoEdit, shown: PhotoEdit): PhotoEdit {
  const delta: PhotoEdit = {
    rotation: normalizeRotation(shown.rotation - rendered.rotation),
    brightness: multiplierDelta(rendered.brightness, shown.brightness),
    contrast: multiplierDelta(rendered.contrast, shown.contrast),
  }
  const crop = cropDelta(rendered, shown)
  if (crop !== undefined) {
    const [crop_x, crop_y, crop_w, crop_h] = crop
    return { ...delta, crop_x, crop_y, crop_w, crop_h }
  }
  return delta
}

/** The value `d` such that `(1 + rendered) * (1 + d) = 1 + shown`, or `shown` when there is none. */
function multiplierDelta(rendered: number, shown: number): number {
  const base = 1 + rendered
  if (base <= 0) {
    return shown
  }
  return tidy((1 + shown) / base - 1)
}

/**
 * The draft's crop as a rectangle of the rendered picture, or `undefined` when
 * nothing is left to clip: the draft crops nothing, or the rendition is already
 * cropped at least as tight.
 */
function cropDelta(rendered: PhotoEdit, shown: PhotoEdit): Bbox | undefined {
  if (!hasCrop(shown)) {
    return undefined
  }
  const [sx, sy, sw, sh] = cropBox(shown)
  const [rx, ry, rw, rh] = hasCrop(rendered) ? cropBox(rendered) : [0, 0, 1, 1]
  if (rw <= 0 || rh <= 0) {
    return undefined
  }
  // The draft's rectangle in the rendered crop's own unit square, clamped to the
  // pixels the rendition actually has.
  const left = Math.max((sx - rx) / rw, 0)
  const top = Math.max((sy - ry) / rh, 0)
  const right = Math.min((sx + sw - rx) / rw, 1)
  const bottom = Math.min((sy + sh - ry) / rh, 1)
  if (right - left <= 0 || bottom - top <= 0) {
    return undefined
  }
  // Tidied, so `0.8 - 0.2` reads as the 0.6 it is rather than as float noise.
  const within: Bbox = [tidy(left), tidy(top), tidy(right - left), tidy(bottom - top)]
  if (within[0] === 0 && within[1] === 0 && within[2] === 1 && within[3] === 1) {
    return undefined
  }
  // The rendition is the cropped picture turned by the rendered rotation, so the
  // rectangle has to be turned the same way to land on it.
  return rotateBbox(within, rendered.rotation)
}

/** The crop of an edit known to carry one, as a bbox. */
function cropBox(edit: PhotoEdit): Bbox {
  return [edit.crop_x ?? 0, edit.crop_y ?? 0, edit.crop_w ?? 1, edit.crop_h ?? 1]
}

/**
 * The shape a rendition has once `edit` is baked into it: the frame cropped to
 * the edit's rectangle, then transposed for a quarter turn. It is the estimate
 * the viewer holds the figure at until the rendition itself arrives and states
 * its own size — so a photo saved sideways opens in a portrait box instead of
 * snapping from landscape on load. A frame with a non-positive side is returned
 * unchanged; callers already treat that as unusable.
 */
export function editedFrame(frame: Frame, edit: PhotoEdit): Frame {
  if (frame.width <= 0 || frame.height <= 0) {
    return frame
  }
  let { width, height } = frame
  if (hasCrop(edit)) {
    width = Math.max(1, Math.round(width * (edit.crop_w ?? 1)))
    height = Math.max(1, Math.round(height * (edit.crop_h ?? 1)))
  }
  return isQuarterTurn(edit.rotation) ? { width: height, height: width } : { width, height }
}

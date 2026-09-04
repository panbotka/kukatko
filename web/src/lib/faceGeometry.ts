import { type CSSProperties } from 'react'

import { type Bbox } from '../services/people'

/** Clamps a fraction to the closed unit interval so styles never go negative. */
function clampUnit(v: number): number {
  if (v < 0) {
    return 0
  }
  if (v > 1) {
    return 1
  }
  return v
}

/** Formats a 0..1 fraction as a CSS percentage string (e.g. 0.25 → "25%"). */
function pct(v: number): string {
  return `${clampUnit(v) * 100}%`
}

/**
 * Positions a face box over an image from a normalised `[x, y, w, h]` bbox,
 * returning absolute-position CSS percentages relative to the image's rendered
 * box. Because the values are percentages they stay correct at any rendered size,
 * so the overlay needs no pixel measurements.
 */
export function faceBoxStyle(bbox: Bbox): Pick<CSSProperties, 'left' | 'top' | 'width' | 'height'> {
  const [x, y, w, h] = bbox
  return {
    left: pct(x),
    top: pct(y),
    width: pct(w),
    height: pct(h),
  }
}

/**
 * Maps a normalised bbox from the upright photo into the frame of that photo
 * turned `rotation` degrees **clockwise** — the direction CSS `rotate()` turns,
 * and the direction the editor's rotation means.
 *
 * Face detection runs on the upright original and its boxes are stored against
 * it, so a photo the user turned in the editor needs its boxes turned the same
 * way or every rectangle lands on the wrong part of the picture. A quarter turn
 * also swaps the frame's proportions, hence the swap of `w` and `h`: the returned
 * box is normalised against the ROTATED frame (see {@link rotatedFrameStyle},
 * which gives that frame its box on screen).
 *
 * Anything other than 90/180/270 (0 included, and any value the backend would
 * have rejected) returns the bbox unchanged, so a caller may pass a rotation
 * straight through without a guard.
 */
export function rotateBbox(bbox: Bbox, rotation: number): Bbox {
  const [x, y, w, h] = bbox
  switch (((rotation % 360) + 360) % 360) {
    case 90:
      // The top-left corner becomes the top-right one: (x, y) → (1 - y, x).
      return [1 - y - h, x, h, w]
    case 180:
      return [1 - x - w, 1 - y - h, w, h]
    case 270:
      // (x, y) → (y, 1 - x).
      return [y, 1 - x - w, h, w]
    default:
      return bbox
  }
}

/**
 * Positions and sizes the layer that a rotated photo's boxes are drawn in, given
 * the wrapper it sits in and the wrapper's `width / height` ratio.
 *
 * The wrapper is the *unrotated* photo's box (the viewer gives the figure the
 * photo's own aspect ratio), and CSS rotates the image about its centre — so a
 * quarter turn paints the photo in a box of the wrapper's height by the wrapper's
 * width, centred on the same point. Percentages of the wrapper cannot describe
 * that box without knowing its proportions, which is why the ratio is a
 * parameter: at ratio `a`, the rotated box is `100 / a` percent wide and `100 * a`
 * percent tall, pulled back onto the centre by a translate.
 *
 * A half turn keeps the frame's shape, so it — like no rotation at all — simply
 * fills the wrapper. An unusable (non-positive or non-finite) ratio also falls
 * back to filling it: a full-size layer puts the boxes slightly wrong, where
 * `NaN` percentages would drop them off the page entirely.
 */
export function rotatedFrameStyle(rotation: number, ratio: number | undefined): CSSProperties {
  const angle = ((rotation % 360) + 360) % 360
  const turned = angle === 90 || angle === 270
  if (!turned || ratio === undefined || !Number.isFinite(ratio) || ratio <= 0) {
    return { left: 0, top: 0, width: '100%', height: '100%' }
  }
  return {
    left: '50%',
    top: '50%',
    width: `${100 / ratio}%`,
    height: `${100 * ratio}%`,
    transform: 'translate(-50%, -50%)',
  }
}

/** The vertical centre of a normalised bbox. */
function centreY(bbox: Bbox): number {
  return bbox[1] + bbox[3] / 2
}

/** The horizontal centre of a normalised bbox. */
function centreX(bbox: Bbox): number {
  return bbox[0] + bbox[2] / 2
}

/**
 * Whether two face boxes sit on the same row of the photo: their vertical centres
 * are closer than half the taller box, i.e. the boxes genuinely overlap rather
 * than merely being near each other.
 *
 * Taking the *taller* box is the forgiving choice on purpose. Merging two rows is
 * the cheap mistake — inside a band the order is still left to right, which is how
 * the eye crosses them anyway — while splitting one real row scatters its
 * numbering, which is exactly the complaint the ordering exists to fix.
 */
function sameRow(a: Bbox, b: Bbox): boolean {
  return Math.abs(centreY(a) - centreY(b)) <= Math.max(a[3], b[3]) / 2
}

/**
 * Orders faces the way a reader crosses the photo: the top row left to right,
 * then the next row down, and so on. It is what makes the number on a box and the
 * position of its row in the faces panel mean something — detection order is the
 * model's, and on a group photo it puts #1 in the middle and #5 at the far left,
 * so matching a row to a person means hunting for a tiny numeric badge.
 *
 * Rows are found by a single greedy pass down the photo: faces are visited from
 * the top and each one either joins the open band ({@link sameRow} against the
 * band's **topmost** member) or starts a new one. Anchoring on the topmost member
 * rather than on the last one added is what stops a slow drift down a crowd from
 * chaining every face into one endless row.
 *
 * The sort is stable, so faces that genuinely share a position keep the order they
 * arrived in.
 */
export function readingOrder<T extends { bbox: Bbox }>(faces: readonly T[]): T[] {
  const topDown = [...faces].sort((a, b) => centreY(a.bbox) - centreY(b.bbox))
  const ordered: T[] = []
  let band: T[] = []

  const flush = () => {
    band.sort((a, b) => centreX(a.bbox) - centreX(b.bbox))
    ordered.push(...band)
    band = []
  }

  for (const face of topDown) {
    if (band.length > 0 && sameRow(band[0].bbox, face.bbox)) {
      band.push(face)
      continue
    }
    flush()
    band = [face]
  }
  flush()
  return ordered
}

/**
 * Expands a face bbox by `padding` of its own width/height on every side and
 * clamps the result to the unit square. The default 30 % is the outlier-review
 * crop: a tight crop of a face you are asked to judge is unjudgeable — the
 * padding keeps enough of the surrounding photo to recognise the person, while
 * the face itself stays dominant.
 */
export function padBbox(bbox: Bbox, padding = 0.3): Bbox {
  const [x, y, w, h] = bbox
  const left = clampUnit(x - w * padding)
  const top = clampUnit(y - h * padding)
  const right = clampUnit(x + w * (1 + padding))
  const bottom = clampUnit(y + h * (1 + padding))
  return [left, top, right - left, bottom - top]
}

/**
 * Positions an inner box within a crop region, both normalised to the same full
 * frame, returning absolute-position CSS percentages **relative to the crop**.
 * It is how the face rectangle is drawn inside a padded context crop: the crop
 * is rendered as the visible tile and the rectangle lands on the face within
 * it. A degenerate (zero-area) crop yields a full-size box rather than NaNs.
 */
export function boxWithinCrop(
  bbox: Bbox,
  crop: Bbox,
): Pick<CSSProperties, 'left' | 'top' | 'width' | 'height'> {
  const [x, y, w, h] = bbox
  const [cx, cy, cw, ch] = crop
  if (cw <= 0 || ch <= 0) {
    return { left: '0%', top: '0%', width: '100%', height: '100%' }
  }
  return {
    left: pct((x - cx) / cw),
    top: pct((y - cy) / ch),
    width: pct(w / cw),
    height: pct(h / ch),
  }
}

/** A style object carrying the `--kk-face-*` custom properties `outliers.css` reads. */
export type FaceMarkerStyle = CSSProperties & Record<`--kk-face-${string}`, string>

/**
 * Formats a 0..1 fraction as a CSS percentage, rounded to four decimals. The
 * rounding is worth a line of its own: `0.2 / 0.32` is `62.499999999999986` in
 * binary floating point, which would put arithmetic noise into the DOM (and into
 * every assertion about it) for a difference of a millionth of a tile.
 */
function markerPct(v: number): string {
  return `${Math.round(clampUnit(v) * 1e6) / 1e4}%`
}

/** The whole frame, i.e. the crop that crops nothing. */
const FULL_FRAME: Bbox = [0, 0, 1, 1]

/**
 * Describes the face rectangle inside a context crop as the four `--kk-face-*`
 * custom properties the `.kk-face-marker` and `.kk-face-box` rules consume: the
 * box's **centre** and its size, both as percentages of the crop.
 *
 * It is the centre-anchored twin of {@link boxWithinCrop}, and the reason is the
 * minimum apparent size the stylesheet enforces. A marker that grows from its
 * top-left corner slides off the face as it hits that minimum; one anchored at
 * its centre and pulled back by `translate(-50%, -50%)` grows around the face
 * instead. Keeping the `max()`/`clamp()` in CSS rather than inline is also what
 * makes the rule survive a jsdom test — its CSSOM mangles `clamp()` in `left`,
 * but passes custom properties through verbatim.
 *
 * The crop defaults to the whole frame, which is the case of the markers drawn
 * straight onto the photograph (`FaceOverlay`): there the bbox's own percentages
 * already are percentages of the layer.
 *
 * A degenerate (zero-area) crop yields a centred, full-size marker rather than
 * NaNs.
 */
export function faceMarkerStyle(bbox: Bbox, crop: Bbox = FULL_FRAME): FaceMarkerStyle {
  const [x, y, w, h] = bbox
  const [cx, cy, cw, ch] = crop
  if (cw <= 0 || ch <= 0) {
    return {
      '--kk-face-x': '50%',
      '--kk-face-y': '50%',
      '--kk-face-w': '100%',
      '--kk-face-h': '100%',
    }
  }
  return {
    '--kk-face-x': markerPct((x + w / 2 - cx) / cw),
    '--kk-face-y': markerPct((y + h / 2 - cy) / ch),
    '--kk-face-w': markerPct(w / cw),
    '--kk-face-h': markerPct(h / ch),
  }
}

/**
 * Builds the CSS that renders only the `crop` region of a full-frame image
 * inside a `position: relative; overflow: hidden` container: the image is
 * absolutely positioned and scaled (in percentages of the container) so exactly
 * the crop fills it. Pair with an `aspect-ratio` of
 * `(crop w × frame width) / (crop h × frame height)` on the container so the
 * photo keeps its proportions. A degenerate crop falls back to the full frame.
 */
export function cropImageStyle(crop: Bbox): CSSProperties {
  const [cx, cy, cw, ch] = crop
  if (cw <= 0 || ch <= 0) {
    return { position: 'absolute', left: '0%', top: '0%', width: '100%', height: '100%' }
  }
  return {
    position: 'absolute',
    left: `${(-cx / cw) * 100}%`,
    top: `${(-cy / ch) * 100}%`,
    width: `${(1 / cw) * 100}%`,
    height: `${(1 / ch) * 100}%`,
  }
}

/**
 * The pixel dimensions of a photo as it is *displayed*, i.e. after the EXIF
 * orientation has been applied — which is the frame a normalised bbox is
 * measured against.
 */
export interface Frame {
  width: number
  height: number
}

/**
 * Resolves a photo's stored pixel dimensions and raw EXIF orientation tag (1–8,
 * or 0 when absent) into the frame the viewer actually sees. Orientations 5–8
 * rotate the image a quarter turn, so they swap width and height — the
 * thumbnailer bakes that rotation in (`internal/thumb` `applyOrientation`), and
 * bboxes are stored in that same display space, so anything reasoning in pixels
 * about a bbox has to swap them too.
 *
 * **The `width`/`height` passed in must be the photo's STORED, pre-rotation
 * dimensions** — the bytes on disk, which `orientation` still has to be applied
 * to. That is the invariant `photos.file_width`/`file_height` and a face row's
 * `photo_width`/`photo_height` carry, and it is not free: PhotoPrism reports its
 * file dimensions with the tag already applied, so an import that took them
 * verbatim stored a pair this function then rotated a *second* time. The viewer
 * sizes its figure from the result (`PhotoDetailPage`), so a transposed frame
 * letterboxes the photo inside its own box and every percentage-positioned face
 * box drifts off the faces. `internal/exif` `RawDimensions` is where the
 * importers undo that swap, and `kukatko maintenance repair --dimensions`
 * (dry run: `maintenance scan`) is where already-imported rows are corrected.
 *
 * A frame with a non-positive side is returned as-is; callers treat it as
 * unusable rather than dividing by it.
 */
export function displayFrame(width: number, height: number, orientation: number): Frame {
  if (orientation >= 5 && orientation <= 8) {
    return { width: height, height: width }
  }
  return { width, height }
}

/**
 * Turns a normalised face bbox into a crop that is **square in pixel space** and
 * still lies inside the frame, so rendering it in a square box shows the face at
 * its true proportions.
 *
 * This is what keeps a face from being stretched. A bbox is normalised against a
 * frame that is almost never square, so a "square" region of the unit box (equal
 * w and h) is an oblong in pixels; scaling it into a square tile squashes the
 * face. The fix is to do the squaring in pixels: take the padded box, grow the
 * shorter pixel side out from its centre until both sides match, then push the
 * result back inside the frame (and, for a frame shorter than the square itself,
 * shrink to the frame's smaller side). The returned crop, rendered by
 * {@link cropImageStyle} in a square container, is undistorted by construction.
 *
 * A degenerate frame or bbox yields the whole unit box, which crops nothing.
 */
export function squareCrop(bbox: Bbox, frame: Frame): Bbox {
  const [x, y, w, h] = bbox
  if (frame.width <= 0 || frame.height <= 0 || w <= 0 || h <= 0) {
    return [0, 0, 1, 1]
  }
  // Work in pixels: normalised units are not comparable across the two axes.
  const side = Math.min(Math.max(w * frame.width, h * frame.height), frame.width, frame.height)
  const centerX = (x + w / 2) * frame.width
  const centerY = (y + h / 2) * frame.height
  // Centre the square on the face, then slide it back inside the frame rather
  // than clipping it — a crop that keeps its size stays square.
  const left = Math.min(Math.max(centerX - side / 2, 0), frame.width - side)
  const top = Math.min(Math.max(centerY - side / 2, 0), frame.height - side)
  return [left / frame.width, top / frame.height, side / frame.width, side / frame.height]
}

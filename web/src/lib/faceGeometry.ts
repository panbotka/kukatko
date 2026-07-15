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
 * Builds the CSS for a square thumbnail cropped to a face's bbox, given the URL
 * of the full thumbnail. The background is scaled so the bbox region fills the
 * crop box and positioned so it is centred; a near-square face shows with
 * negligible distortion. Used by the cluster and outlier face previews.
 */
export function faceCropStyle(url: string, bbox: Bbox): CSSProperties {
  const [x, y, w, h] = bbox
  // Guard against a full-width/height bbox (denominator 0): fall back to no
  // positional offset, which simply centres the (already full) region.
  const posX = w >= 1 ? 0 : x / (1 - w)
  const posY = h >= 1 ? 0 : y / (1 - h)
  const sizeX = w > 0 ? 100 / w : 100
  const sizeY = h > 0 ? 100 / h : 100
  return {
    backgroundImage: `url("${url}")`,
    backgroundRepeat: 'no-repeat',
    backgroundSize: `${sizeX}% ${sizeY}%`,
    backgroundPosition: `${pct(posX)} ${pct(posY)}`,
  }
}

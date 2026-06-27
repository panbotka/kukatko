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

import { type CSSProperties } from 'react'

import { type PhotoEdit } from '../services/photos'

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

/** Returns the next clockwise quarter-turn rotation after the given one. */
export function rotateRight(rotation: number): number {
  return (rotation + 90) % 360
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
 * Combines an edit into the CSS the preview applies so it reflects the saved (or
 * in-progress) adjustments: a brightness/contrast `filter`, a rotation
 * `transform`, and a crop `clip-path`. Neutral parts are omitted so the style
 * stays minimal.
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

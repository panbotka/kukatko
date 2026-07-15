/**
 * Pure logic of the /expand page ("grow an album or a label with visually
 * similar photos"): the threshold dial's defaults, the request arithmetic and
 * the source-picker ordering. Kept out of the components so the maths and the
 * ordering rules are unit-testable without rendering anything.
 *
 * The threshold UI speaks percent (a "how similar must it be" dial) and reuses
 * the face-search slider's 20–80 range and its percent↔distance conversion
 * (`lib/faceThreshold`); only the default differs. Expanding a collection wants
 * precision first (70 %, the backend's own default distance of 0.30), where the
 * face hunt casts a wider net (50 %).
 */

import { percentToDistance, THRESHOLD_MAX_PERCENT, THRESHOLD_MIN_PERCENT } from './faceThreshold'

/** Where the expand threshold slider starts: precise matches first. */
export const EXPAND_THRESHOLD_DEFAULT_PERCENT = 70

/** Smallest accepted result cap. */
export const EXPAND_LIMIT_MIN = 1
/** Largest accepted result cap. */
export const EXPAND_LIMIT_MAX = 200
/** Result cap before the user touches the field. */
export const EXPAND_LIMIT_DEFAULT = 50

/**
 * clampExpandThresholdPercent keeps a percentage inside the slider's supported
 * range, guarding against an out-of-range value arriving from a URL query
 * parameter. A non-numeric value falls back to the expand default (70 %), not
 * the face-search one.
 */
export function clampExpandThresholdPercent(percent: number): number {
  if (!Number.isFinite(percent)) {
    return EXPAND_THRESHOLD_DEFAULT_PERCENT
  }
  if (percent < THRESHOLD_MIN_PERCENT) {
    return THRESHOLD_MIN_PERCENT
  }
  if (percent > THRESHOLD_MAX_PERCENT) {
    return THRESHOLD_MAX_PERCENT
  }
  return percent
}

/**
 * expandThresholdDistance converts the slider's percentage into the cosine
 * distance the expand endpoints accept, rounded to four decimals so the value
 * survives a round-trip through the URL without float noise (70 % → 0.3, not
 * 0.30000000000000004).
 */
export function expandThresholdDistance(percent: number): number {
  return Number(percentToDistance(percent).toFixed(4))
}

/**
 * clampExpandLimit keeps the result cap inside 1–200, truncating fractions and
 * falling back to the default for a non-numeric value (an empty input, a
 * garbled URL parameter).
 */
export function clampExpandLimit(limit: number): number {
  if (!Number.isFinite(limit)) {
    return EXPAND_LIMIT_DEFAULT
  }
  return Math.min(Math.max(Math.trunc(limit), EXPAND_LIMIT_MIN), EXPAND_LIMIT_MAX)
}

/** One pickable source collection, unified over albums and labels. */
export interface ExpandSource {
  /** The album or label UID. */
  uid: string
  /** The album title or label name shown in the picker. */
  name: string
  /** How many photos the collection holds, shown as the option's hint. */
  photoCount: number
}

/**
 * expandSources orders collections for the source picker: photo count
 * descending — the collections worth expanding are the ones that already have
 * material — with an alphabetical tiebreak, and collections with zero photos
 * dropped entirely (there is nothing to be similar to).
 */
export function expandSources(sources: ExpandSource[]): ExpandSource[] {
  return sources
    .filter((source) => source.photoCount > 0)
    .sort((a, b) => b.photoCount - a.photoCount || a.name.localeCompare(b.name))
}

/**
 * similarityPercent renders a candidate's cosine similarity (0..1) as the whole
 * percentage shown on its tile.
 */
export function similarityPercent(similarity: number): number {
  return Math.round(similarity * 100)
}

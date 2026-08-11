/**
 * Pure logic of the /expand page ("grow an album or a label with visually
 * similar photos"): the threshold dial's defaults, the request arithmetic and
 * the source-picker ordering. Kept out of the components so the maths and the
 * ordering rules are unit-testable without rendering anything.
 *
 * The threshold UI speaks percent (a "how similar must it be" dial) and borrows
 * the percent↔distance conversion from `lib/faceThreshold`, but not its 20–80
 * range: faces and photos are two different embedding models with two different
 * distance scales, and since the image tower became SigLIP 2 the face slider's
 * stops mean nothing here. This dial has its own range, read off the library's
 * own distances — at 65 % (distance 0.35) about 18 % of all photo pairs already
 * qualify, which is the widest net still worth offering, and 90 % (0.10) is as
 * tight as "similar" gets before it means "the same shot". The default 80 % is
 * the backend's own `expand.max_distance` of 0.20; see docs/THRESHOLDS.md.
 */

import { percentToDistance } from './faceThreshold'

/** Loosest similarity the expand slider offers (widest net, most results). */
export const EXPAND_THRESHOLD_MIN_PERCENT = 65
/** Tightest similarity the expand slider offers (best matches only). */
export const EXPAND_THRESHOLD_MAX_PERCENT = 90
/** Where the expand threshold slider starts: precise matches first. */
export const EXPAND_THRESHOLD_DEFAULT_PERCENT = 80

/** Smallest accepted result cap. */
export const EXPAND_LIMIT_MIN = 1
/** Largest accepted result cap. */
export const EXPAND_LIMIT_MAX = 200
/** Result cap before the user touches the field. */
export const EXPAND_LIMIT_DEFAULT = 50

/**
 * clampExpandThresholdPercent keeps a percentage inside the slider's supported
 * range, guarding against an out-of-range value arriving from a URL query
 * parameter. A non-numeric value falls back to the expand default (80 %), not
 * the face-search one.
 */
export function clampExpandThresholdPercent(percent: number): number {
  if (!Number.isFinite(percent)) {
    return EXPAND_THRESHOLD_DEFAULT_PERCENT
  }
  if (percent < EXPAND_THRESHOLD_MIN_PERCENT) {
    return EXPAND_THRESHOLD_MIN_PERCENT
  }
  if (percent > EXPAND_THRESHOLD_MAX_PERCENT) {
    return EXPAND_THRESHOLD_MAX_PERCENT
  }
  return percent
}

/**
 * expandThresholdDistance converts the slider's percentage into the cosine
 * distance the expand endpoints accept, rounded to four decimals so the value
 * survives a round-trip through the URL without float noise (80 % → 0.2, not
 * 0.19999999999999996).
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

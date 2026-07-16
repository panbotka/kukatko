/**
 * The pure model behind the /outliers review page.
 *
 * A face the backend ranked as suspicious moves through a tiny lifecycle as the
 * curator works: `pending` (no verdict yet), `removed` (unassigned from the
 * person — the ✓ "yes, this is wrong"), `confirmed` (vouched for — the ✗ "no,
 * this really is them"), `error` (its write failed and can be retried). Unlike
 * the /faces grid, **neither verdict removes the card**: it flips where it
 * stands, so a long list never reflows under the cursor mid-review.
 *
 * The threshold dial is the other half. The UI speaks **percent** ("how far from
 * the centroid must a face be to show up") while the endpoint speaks **cosine
 * distance**; keeping the conversion here — pure and unit-tested — means the
 * slider, the request builder and the per-card distance label cannot drift apart.
 */

import { type OutlierFace } from '../services/people'

/** Where an outlier face is in its review lifecycle. */
export type OutlierStatus = 'pending' | 'removed' | 'confirmed' | 'error'

/** An outlier face paired with its live review status. */
export interface OutlierItem {
  face: OutlierFace
  status: OutlierStatus
}

/**
 * The cosine distance the 100 % end of the slider maps to. Cosine distance runs
 * 0..2 in principle, but two embeddings of *different* people already sit around
 * 1.0 — beyond that lies nothing a face review would ever want to hide, so the
 * dial spends its whole travel in the range that matters.
 */
export const OUTLIER_MAX_DISTANCE = 1

/** Lowest the slider goes: no filter at all, every ranked face is shown. */
export const OUTLIER_THRESHOLD_MIN_PERCENT = 0
/** Highest the slider goes: only the faces furthest from the centroid. */
export const OUTLIER_THRESHOLD_MAX_PERCENT = 100
/** Slider granularity, in percentage points. */
export const OUTLIER_THRESHOLD_STEP_PERCENT = 5
/**
 * Where the slider starts: **show everything**. The list is already ranked
 * most-suspicious-first, so an unfiltered view is a useful view; a non-zero
 * default would hide faces without ever saying so.
 */
export const OUTLIER_THRESHOLD_DEFAULT_PERCENT = 0

/**
 * How many faces the page asks for. The ranking puts the suspicious ones first,
 * so a cap costs a well-tagged person nothing — but it does keep a person with
 * thousands of faces from shipping all of them into the browser. When the cap
 * bites, the page says so rather than quietly truncating.
 */
export const OUTLIER_LIMIT = 200

/** A stable key for an outlier face, unique within a subject. */
export function outlierKey(face: OutlierFace): string {
  return `${face.photo_uid}:${String(face.face_index)}`
}

/** Seeds a working list from a fresh outlier response. */
export function toOutlierItems(faces: readonly OutlierFace[]): OutlierItem[] {
  return faces.map((face) => ({ face, status: 'pending' }))
}

/**
 * Reports whether an item still awaits a verdict. An errored card counts: its
 * write failed, so it is still the user's to decide.
 */
export function isActionable(item: OutlierItem): boolean {
  return item.status === 'pending' || item.status === 'error'
}

/**
 * Reports whether a face can be unassigned at all. A face with no marker is not
 * tied to the subject through one, so there is nothing for the assign endpoint
 * to detach.
 */
export function canUnassign(face: OutlierFace): boolean {
  return face.marker_uid !== undefined && face.marker_uid !== ''
}

/**
 * clampOutlierThresholdPercent keeps a percentage inside the slider's range,
 * guarding against a garbled URL query parameter. A non-numeric value falls back
 * to the default (show everything).
 */
export function clampOutlierThresholdPercent(percent: number): number {
  if (!Number.isFinite(percent)) {
    return OUTLIER_THRESHOLD_DEFAULT_PERCENT
  }
  if (percent < OUTLIER_THRESHOLD_MIN_PERCENT) {
    return OUTLIER_THRESHOLD_MIN_PERCENT
  }
  if (percent > OUTLIER_THRESHOLD_MAX_PERCENT) {
    return OUTLIER_THRESHOLD_MAX_PERCENT
  }
  return percent
}

/**
 * outlierThresholdDistance converts the slider's percentage into the minimum
 * cosine distance the endpoint accepts, rounded to four decimals so the value
 * survives a round-trip through the URL without float noise. 0 % → 0, which the
 * endpoint reads as "return everything".
 */
export function outlierThresholdDistance(percent: number): number {
  const ratio = clampOutlierThresholdPercent(percent) / 100
  return Number((ratio * OUTLIER_MAX_DISTANCE).toFixed(4))
}

/**
 * distancePercent renders a cosine distance as the whole percentage the cards
 * show. It is deliberately *not* a similarity: on this page a bigger number
 * means "further from the person", which is the thing being judged.
 */
export function distancePercent(distance: number): number {
  return Math.round(distance * 100)
}

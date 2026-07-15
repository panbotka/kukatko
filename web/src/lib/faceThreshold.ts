/**
 * Threshold arithmetic for the "find a person in untagged photos" search.
 *
 * The whole UI speaks a **percentage** — a friendly "how sure must the match be"
 * dial — while the backend speaks **cosine distance** (smaller is a closer match).
 * The two are complements: `distance = 1 - percent/100`. Keeping the conversion in
 * one tiny, unit-tested module means the slider, the request builder and the
 * per-card match label can never drift apart on the maths.
 */

/** Lowest similarity the slider offers (widest net, most results). */
export const THRESHOLD_MIN_PERCENT = 20
/** Highest similarity the slider offers (tightest net, best matches). */
export const THRESHOLD_MAX_PERCENT = 80
/** Slider granularity, in percentage points. */
export const THRESHOLD_STEP_PERCENT = 5
/** Where the slider starts before the user touches it. */
export const THRESHOLD_DEFAULT_PERCENT = 50

/**
 * percentToDistance converts a similarity percentage (0..100) into the maximum
 * cosine distance the backend accepts. A higher percentage demands a closer match,
 * so it maps to a smaller distance: `1 - percent/100`.
 */
export function percentToDistance(percent: number): number {
  return 1 - percent / 100
}

/**
 * distanceToPercent converts a cosine distance back into a similarity percentage,
 * rounded to a whole number for display. It is the inverse of
 * {@link percentToDistance} and is also how a candidate's `distance` becomes the
 * "match %" shown on its card.
 */
export function distanceToPercent(distance: number): number {
  return Math.round((1 - distance) * 100)
}

/**
 * clampThresholdPercent keeps a percentage inside the slider's supported range,
 * guarding against an out-of-range value arriving from a URL query parameter.
 */
export function clampThresholdPercent(percent: number): number {
  if (!Number.isFinite(percent)) {
    return THRESHOLD_DEFAULT_PERCENT
  }
  if (percent < THRESHOLD_MIN_PERCENT) {
    return THRESHOLD_MIN_PERCENT
  }
  if (percent > THRESHOLD_MAX_PERCENT) {
    return THRESHOLD_MAX_PERCENT
  }
  return percent
}

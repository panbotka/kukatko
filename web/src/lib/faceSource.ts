import { type Frame } from './faceGeometry'

import { type Bbox } from '../services/people'

/**
 * The thumbnails a face crop may be cut from, ascending, as
 * `internal/thumb/sizes.go` registers them. They must all be `fit_*` sizes: those
 * keep the whole frame, which is what a normalised bbox is measured against. A
 * `tile_*` size is a centre-cropped square, so the frame it shows is not the
 * frame the box was normalised to and the crop would land beside the face.
 *
 * Every rung is generated for every photo (`GenerateAll`, on ingest and by the
 * `thumbnail` job), so any of them is a legitimate address — see
 * {@link smallerFaceSource} for what a caller does when one is nevertheless
 * missing from the object store.
 */
const FACE_SOURCE_SIZES = [720, 1280, 1920, 2560, 3840] as const

/**
 * The ceiling for a face shown as a small chip or tile — the people grid, a
 * cluster sample, a marker thumbnail.
 *
 * It stops at 1920 deliberately. A grid of little tiles each pulling a 2560/3840
 * original would cost megabytes to sharpen faces that are a few dozen pixels in
 * the original anyway — past a point the pixels simply are not there, and the
 * honest answer for a chip is a soft crop rather than a slow page.
 */
export const FACE_SOURCE_TILE_MAX = 1920

/**
 * The ceiling for a crop whose entire job is to be **judged**: the `/outliers`
 * review card, where the question on screen is "is this the right person?" and a
 * smear is not an answer. There the extra rungs earn their bytes — the cards are
 * large, one screen holds a handful of them, and they are lazily loaded.
 */
export const FACE_SOURCE_REVIEW_MAX = 3840

/**
 * How many real pixels a crop should have across it by default, sized for the
 * people grid's tiles (~150 CSS px) with headroom for a 2× display.
 */
export const DEFAULT_TARGET_PX = 300

/**
 * How many real pixels an `/outliers` **context crop** should have across it.
 *
 * The bar is really about the face inside it. 96 px is about where a face stops
 * being a smudge and becomes a person: it is triple the ~32 px a recognition
 * model works from, and roughly the width a face is *rendered* at in the densest
 * grid the review page offers (ten columns of a ~1400 px page put the face — 62.5
 * % of a tile, since the crop is defined from the box — at ~88 px). So at maximum
 * density the crop is essentially 1:1, and at the default it is a ~2× upscale
 * instead of the ~7× a hard-coded `fit_720` produced.
 *
 * The crop is the face grown 30 % per side, i.e. 1.6× its width, so 96 px across
 * the face is ~154 px across the crop. Raising the bar past this mostly buys
 * bytes: on the reported library the average face is 4.9 % of the frame, so every
 * further step pushes another slice of the collection onto `fit_3840`.
 */
export const OUTLIER_TARGET_PX = 154

/**
 * Picks the smallest thumbnail that still puts about `targetPx` real pixels
 * across the crop, never going past `maxSize`.
 *
 * A fixed size cannot serve both cases. A face filling half the frame is sharp
 * from `fit_720`; a face 2 % across it is 13 pixels there, and blowing 13 pixels
 * up into a tile is a smear, not a person — which defeats the whole point of
 * showing a face. So the source scales with how small the face is: the smaller
 * the crop's share of the frame, the bigger the thumbnail it is cut from, and a
 * big face never pays for a face it is not.
 *
 * `fit_N` bounds the frame's LONGEST side and never upscales, so the crop's width
 * in a given thumbnail is its width in the original times `min(1, N / longest)` —
 * which also means a rung past the original's own resolution is never chosen, as
 * it would be the same pixels under a different URL.
 *
 * A degenerate crop or frame yields the smallest source: it tells us nothing, so
 * it should not cost anything.
 */
export function faceSourceSize(
  crop: Bbox,
  frame: Frame,
  targetPx: number,
  maxSize: number = FACE_SOURCE_TILE_MAX,
): string {
  const ladder = FACE_SOURCE_SIZES.filter((size) => size <= maxSize)
  const usable = ladder.length > 0 ? ladder : [FACE_SOURCE_SIZES[0]]
  const cropPx = crop[2] * frame.width
  const longSide = Math.max(frame.width, frame.height)
  if (!Number.isFinite(cropPx) || cropPx <= 0 || longSide <= 0) {
    return `fit_${String(usable[0])}`
  }
  const enough = usable.find((size) => cropPx * Math.min(1, size / longSide) >= targetPx)
  if (enough !== undefined) {
    return `fit_${String(enough)}`
  }
  // No rung clears the bar — the pixels are simply not in the original. Take the
  // biggest source that still *adds* any: past the original's own long side every
  // rung is the same image under a different URL, and a needless cache entry.
  const capped = usable.find((size) => size >= longSide) ?? usable[usable.length - 1]
  return `fit_${String(capped)}`
}

/**
 * The next rung down from `size`, or `null` at the bottom (or for a name that is
 * not on the ladder at all).
 *
 * This is the degrade path. On a storage backend that publishes its objects the
 * thumbnail route does not generate anything — it redirects to the bucket — so a
 * size that never reached it answers 404, and a card would show a broken image
 * where a face should be. Stepping down on `onError` costs one failed request
 * and lands, at worst, on the `fit_720` that has always been there.
 */
export function smallerFaceSource(size: string): string | null {
  const index = FACE_SOURCE_SIZES.findIndex((known) => `fit_${String(known)}` === size)
  if (index <= 0) {
    return null
  }
  return `fit_${String(FACE_SOURCE_SIZES[index - 1])}`
}

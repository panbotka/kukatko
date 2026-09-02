/**
 * Which cached rendition an image in the UI is fetched at.
 *
 * Every thumbnail the app paints costs bytes on the wire, and the cheapest byte
 * is the one never sent: a tile 180 CSS pixels across drawn from a 500 px
 * rendition ships roughly eight times the pixels the browser puts on the glass.
 * So each place that fetches a rendition asks for the smallest rung that still
 * covers the box it is drawn into, at the screen's device-pixel ratio.
 *
 * The rungs are the sizes `internal/thumb/sizes.go` registers, and nothing here
 * may name one it does not — an unregistered size is a 400 from the thumb route
 * and, on a publishing storage backend, a signed URL to an object that was never
 * generated.
 */

/**
 * The `fit_*` rungs, ascending — aspect-preserving renditions whose number is
 * the longest side.
 */
export const FIT_RENDITION_SIZES: readonly number[] = [720, 1280, 1920, 2560, 3840]

/**
 * The `tile_*` rungs, ascending — centre-cropped squares whose number is the
 * side.
 */
export const SQUARE_RENDITION_SIZES: readonly number[] = [100, 224, 500]

/**
 * The most a rendition may be scaled up before the next rung is worth its bytes.
 * Slightly above 1 because a thumbnail stretched a few per cent is invisible,
 * while stepping a whole rung early roughly doubles what the page downloads.
 */
export const RENDITION_TOLERANCE = 1.15

/**
 * The device-pixel ratio to size for, capped. A 3× phone screen showing a photo
 * across a third of its width does not need three times the pixels of it, and
 * past 2× the difference is not one anybody sees on a photograph — while the
 * bytes very much are, on exactly the connection least able to afford them.
 */
export const MAX_RENDITION_DPR = 2

/** The screen's device-pixel ratio, capped and defaulted for a usable value. */
export function renditionDpr(dpr?: number): number {
  if (dpr === undefined || !Number.isFinite(dpr) || dpr <= 0) return 1
  return Math.min(dpr, MAX_RENDITION_DPR)
}

/** The current screen's capped device-pixel ratio, or 1 outside a browser. */
export function currentRenditionDpr(): number {
  if (typeof window === 'undefined') return 1
  return renditionDpr(window.devicePixelRatio)
}

/**
 * The smallest rung in `rungs` that covers `neededPx` device pixels within
 * {@link RENDITION_TOLERANCE}, or the largest rung when nothing does. An
 * unusable need (unmeasured, zero, non-finite) yields the smallest rung: a box
 * whose size is unknown is far likelier to be ordinary than enormous, and
 * guessing small costs sharpness for a moment where guessing large costs bytes
 * every time.
 */
export function pickRendition(
  rungs: readonly number[],
  neededPx: number,
  tolerance = RENDITION_TOLERANCE,
): number {
  const smallest = rungs[0] ?? 0
  if (!Number.isFinite(neededPx) || neededPx <= 0) return smallest
  for (const rung of rungs) {
    if (rung * tolerance >= neededPx) return rung
  }
  return rungs.at(-1) ?? smallest
}

/**
 * The `fit_*` size name for an image whose longest painted side is
 * `longestSideCssPx` CSS pixels on a screen of `dpr` device pixels per CSS pixel.
 */
export function fitRenditionName(longestSideCssPx: number, dpr?: number): string {
  const rung = pickRendition(FIT_RENDITION_SIZES, longestSideCssPx * renditionDpr(dpr))
  return `fit_${String(rung)}`
}

/** The `tile_*` size name for a square box `sideCssPx` CSS pixels across. */
export function squareRenditionName(sideCssPx: number, dpr?: number): string {
  const rung = pickRendition(SQUARE_RENDITION_SIZES, sideCssPx * renditionDpr(dpr))
  return `tile_${String(rung)}`
}

/** A rectangle in CSS pixels — a viewport, a stage, a measured element. */
export interface Box {
  /** Width in CSS pixels. */
  width: number
  /** Height in CSS pixels. */
  height: number
}

/**
 * The longest side, in CSS pixels, an image of `media`'s proportions occupies
 * once it is fitted inside `box` — the `object-fit: contain` a viewer stage
 * draws. Returns the box's own longest side when the proportions are unknown or
 * unusable, which is the conservative answer: no photograph fitted into a box
 * paints larger than the box.
 *
 * This is the whole reason a phone need not download a desktop-sized preview. A
 * 4:3 photograph on a 390 × 844 phone is painted 390 × 293 — its longest side is
 * the phone's *width*, not the diagonal-ish 844 the naive reading would use, and
 * sizing for 844 fetches nearly twice the pixels it can show.
 */
export function paintedLongestSide(box: Box, media: Box | null): number {
  const boxLongest = Math.max(box.width, box.height)
  if (media === null) return boxLongest
  const { width, height } = media
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
    return boxLongest
  }
  const scale = Math.min(box.width / width, box.height / height)
  return Math.max(width, height) * scale
}

/**
 * The `fit_*` rungs a full-screen viewer stage (the photo detail page, the
 * slideshow) may draw from.
 *
 * It is deliberately a two-rung subset with `fit_1920` on top: that is the size
 * those stages fetched unconditionally before they measured anything, so this
 * can only ever ask for *fewer* bytes than they used to, never more — a retina
 * desktop keeps the preview it always had. And `fit_1280` at the bottom leaves a
 * phone stage roughly three times the pixels it paints, which is the headroom a
 * pinch-zoom spends.
 */
export const STAGE_RENDITION_SIZES: readonly number[] = [1280, 1920]

/**
 * The stage allows no upscale at all, unlike a tile. A grid tile stretched a few
 * per cent is invisible at a glance; a stage is where somebody has stopped to
 * *look* at the photograph, and it fills the screen, so the same few per cent is
 * the one place in the app they would notice. The bytes are saved by dropping a
 * rung only when a whole rung is genuinely spare.
 */
export const STAGE_RENDITION_TOLERANCE = 1

/**
 * The `fit_*` size a viewer stage of `box` CSS pixels should draw a photograph
 * of `media`'s proportions from. Pass `media` as null when the proportions are
 * not known yet; the answer is then the conservative one.
 */
export function stageRenditionName(box: Box, media: Box | null, dpr?: number): string {
  const needed = paintedLongestSide(box, media) * renditionDpr(dpr)
  const rung = pickRendition(STAGE_RENDITION_SIZES, needed, STAGE_RENDITION_TOLERANCE)
  return `fit_${String(rung)}`
}

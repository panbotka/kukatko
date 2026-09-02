/**
 * Which thumbnail rendition a justified wall tile is drawn from.
 *
 * A square grid asked one question of every tile and got one answer; a justified
 * one does not. A tile is now as wide as its photograph, so at a low density (or
 * on a wide desktop) a panorama can span most of the window, and the rendition
 * the payload carries — `fit_720`, the size that covers an ordinary tile on a
 * high-DPI screen — would be blown up past its own pixels and go visibly soft.
 *
 * So the tile picks its rung from the width it was actually laid out at. The
 * common case is the one the payload already answers, which matters: that URL is
 * signed and served from the edge, while any other rung has to be fetched
 * through this application's own thumb route.
 */

import { GRID_PREVIEW_SIZE } from '../services/photos'

import { MAX_RENDITION_DPR, pickRendition, RENDITION_TOLERANCE, renditionDpr } from './rendition'

/**
 * The `fit_*` rungs a wall tile may be drawn from, ascending. They are a subset
 * of the sizes `internal/thumb/sizes.go` registers; the first is the one every
 * payload carries as `preview_url`.
 */
export const TILE_RENDITION_SIZES: readonly number[] = [720, 1280, 1920, 2560]

/** The wall's scale-up tolerance — the shared {@link RENDITION_TOLERANCE}. */
export const TILE_RENDITION_TOLERANCE = RENDITION_TOLERANCE

/** The wall's device-pixel-ratio cap — the shared {@link MAX_RENDITION_DPR}. */
export const TILE_MAX_DPR = MAX_RENDITION_DPR

/**
 * The `fit_*` size a tile laid out `widthPx` CSS pixels wide should be drawn
 * from, on a screen of `dpr` device pixels per CSS pixel. Returns
 * {@link GRID_PREVIEW_SIZE} for anything an ordinary tile covers — which is what
 * lets the caller use the signed `preview_url` the payload already carries — and
 * the next rung up only once the tile would visibly outrun it.
 *
 * An unusable width (unmeasured, zero, non-finite) yields the default rung: a
 * tile whose size is unknown is far likelier to be ordinary than enormous.
 */
export function tileRenditionSize(widthPx: number, dpr = 1): number {
  return pickRendition(TILE_RENDITION_SIZES, widthPx * renditionDpr(dpr))
}

/** The thumbnail size name (`fit_720`…) for a tile of `widthPx` CSS pixels. */
export function tileRenditionName(widthPx: number, dpr = 1): string {
  return `fit_${String(tileRenditionSize(widthPx, dpr))}`
}

/** Whether a tile of this width is covered by the payload's own `preview_url`. */
export function tileUsesPreviewURL(widthPx: number, dpr = 1): boolean {
  return tileRenditionName(widthPx, dpr) === GRID_PREVIEW_SIZE
}

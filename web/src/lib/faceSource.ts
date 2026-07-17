import { type Frame } from './faceGeometry'

import { type Bbox } from '../services/people'

/**
 * The thumbnails a face crop may be cut from, ascending. They must all be `fit_*`
 * sizes: those keep the whole frame, which is what a normalised bbox is measured
 * against. A `tile_*` size is a centre-cropped square, so the frame it shows is
 * not the frame the box was normalised to and the crop would land beside the face.
 *
 * The ladder stops at 1920 deliberately. A grid of tiles each pulling a 2560/3840
 * original would cost megabytes to sharpen faces that are a few dozen pixels in
 * the original anyway — past a point the pixels simply are not there, and the
 * honest answer is a soft crop rather than a slow page.
 */
const FACE_SOURCE_SIZES = [720, 1280, 1920] as const

/**
 * Picks the smallest thumbnail that still puts about `targetPx` real pixels
 * across the crop.
 *
 * A fixed size cannot serve both cases. A face filling half the frame is sharp
 * from `fit_720`; a face 2 % across it is 13 pixels there, and blowing 13 pixels
 * up into a tile is a smear, not a person — which defeats the whole point of
 * showing a face. So the source scales with how small the face is: the smaller
 * the crop's share of the frame, the bigger the thumbnail it is cut from, and a
 * big face never pays for a face it is not.
 *
 * `fit_N` bounds the frame's LONGEST side and never upscales, so the crop's width
 * in a given thumbnail is its width in the original times `min(1, N / longest)`.
 */
export function faceSourceSize(crop: Bbox, frame: Frame, targetPx: number): string {
  const cropPx = crop[2] * frame.width
  const longSide = Math.max(frame.width, frame.height)
  if (cropPx <= 0 || longSide <= 0) {
    return `fit_${FACE_SOURCE_SIZES[0]}`
  }
  const enough = FACE_SOURCE_SIZES.find((size) => cropPx * Math.min(1, size / longSide) >= targetPx)
  return `fit_${enough ?? FACE_SOURCE_SIZES[FACE_SOURCE_SIZES.length - 1]}`
}

/**
 * How many real pixels a crop should have across it by default, sized for the
 * people grid's tiles (~150 CSS px) with headroom for a 2× display.
 */
export const DEFAULT_TARGET_PX = 300

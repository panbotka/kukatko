import { type ReactEventHandler, type RefCallback, useCallback, useState } from 'react'

import { displayFrame, type Frame } from '../lib/faceGeometry'

/** What {@link useImageFrame} needs to know about the photo being shown. */
export interface ImageFrameOptions {
  /**
   * Identity of the image on screen — the photo's UID, or its URL where one
   * component shows several. A change discards the previous measurement, so the
   * old photo's frame never sizes the new one.
   */
  source: string
  /** The catalogue row's stored width in pixels, **before** EXIF orientation. */
  width: number
  /** The catalogue row's stored height in pixels, **before** EXIF orientation. */
  height: number
  /** The raw EXIF orientation tag (1–8, or 0 when absent). */
  orientation: number
}

/** The `<img>` props {@link useImageFrame} needs on the image it measures. */
export interface ImageFrameProps {
  ref: RefCallback<HTMLImageElement>
  onLoad: ReactEventHandler<HTMLImageElement>
}

/** What {@link useImageFrame} reports about the frame to draw boxes against. */
export interface ImageFrame {
  /**
   * The frame the wrapper is to be sized from: the loaded image's own natural
   * size once known, the catalogue row's estimate before that. A side may be
   * non-positive when neither source gives one — treat that frame as unusable.
   */
  frame: Frame
  /**
   * Whether {@link frame} is the loaded image's own size (`true`) or still the
   * catalogue row's estimate (`false`). **Gate every box drawn over the photo on
   * this**: an estimate can be wrong, and a box positioned against a wrong frame
   * lands off its face and then jumps when the real frame arrives.
   */
  measured: boolean
  /** The frame as a CSS `aspect-ratio` value, or undefined when it is unusable. */
  aspectRatio: string | undefined
  /** The frame's width ÷ height, or undefined when it is unusable. */
  ratio: number | undefined
  /** Spread onto the `<img>` whose rendered box the boxes are positioned against. */
  imgProps: ImageFrameProps
}

/**
 * The frame a face box is positioned against, taken from the **loaded image**
 * rather than from the catalogue row.
 *
 * Everywhere the app draws a box over a full-frame photo, the wrapper around the
 * image *is* the box's coordinate system: the box is placed in percentages of it,
 * so the wrapper has to sit exactly on the rendered pixels. Sizing that wrapper
 * from `photos.file_width`/`file_height` plus the raw EXIF orientation
 * ({@link displayFrame}) is only as good as the row, and the row is not always
 * good: the PhotoPrism import stored dimensions that were **already oriented** and
 * then rotated them a second time, so roughly one photo in twelve carries a
 * transposed pair. The image itself still renders correctly (the browser applies
 * the orientation on its own), which is what makes the failure so odd to look at —
 * the photo is fine, every box on it is stretched, and one face ends up off the
 * edge entirely.
 *
 * The browser knows the truth as soon as the image has loaded: `naturalWidth` and
 * `naturalHeight` are post-orientation. So the row is kept only as the **initial
 * estimate** — it holds the layout still while the image loads — and the measured
 * frame replaces it on load. Note that the measurement describes the *shape* of
 * what is rendered (a `fit_*` thumbnail, not the original), so use it for
 * proportions, never as the original's pixel count.
 *
 * Callers must spread {@link ImageFrame.imgProps} onto the image and draw no box
 * until {@link ImageFrame.measured} is true. The `ref` half of those props is not
 * redundant with `onLoad`: an image served from cache can be complete before React
 * has attached the handler, and the ref then catches it (only when the element
 * reports itself `complete` — see the ref itself).
 */
export function useImageFrame({
  source,
  width,
  height,
  orientation,
}: ImageFrameOptions): ImageFrame {
  // Keyed by source so a measurement of the previous photo is discarded on sight
  // rather than in an effect — a render with the new source must never be sized
  // by the old one's frame.
  const [natural, setNatural] = useState<{ source: string; frame: Frame } | null>(null)

  const measure = useCallback(
    (img: HTMLImageElement | null): void => {
      // A broken or not-yet-decoded image reports zeroes; there is nothing to learn
      // from it, so the estimate stays.
      if (img === null || img.naturalWidth <= 0 || img.naturalHeight <= 0) {
        return
      }
      const frame: Frame = { width: img.naturalWidth, height: img.naturalHeight }
      setNatural((prev) =>
        prev !== null &&
        prev.source === source &&
        prev.frame.width === frame.width &&
        prev.frame.height === frame.height
          ? prev
          : { source, frame },
      )
    },
    [source],
  )

  const ref = useCallback<RefCallback<HTMLImageElement>>(
    (node) => {
      // Only a **complete** image may be measured on sight. An element whose `src`
      // has just been pointed at another photo keeps reporting the previous
      // image's natural size until the new one arrives, and `complete` is exactly
      // "the current request finished and nothing is pending" — without that
      // check this path would stamp the old photo's frame onto the new one.
      if (node?.complete === true) {
        measure(node)
      }
    },
    [measure],
  )
  const onLoad = useCallback<ReactEventHandler<HTMLImageElement>>(
    (event) => {
      measure(event.currentTarget)
    },
    [measure],
  )

  const measured = natural !== null && natural.source === source
  const frame = measured ? natural.frame : displayFrame(width, height, orientation)
  const usable = frame.width > 0 && frame.height > 0
  return {
    frame,
    measured,
    aspectRatio: usable ? `${String(frame.width)} / ${String(frame.height)}` : undefined,
    ratio: usable ? frame.width / frame.height : undefined,
    imgProps: { ref, onLoad },
  }
}

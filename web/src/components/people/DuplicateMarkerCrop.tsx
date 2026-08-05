import { type CSSProperties, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cropImageStyle, displayFrame, faceMarkerStyle, padBbox } from '../../lib/faceGeometry'
import {
  FACE_SOURCE_REVIEW_MAX,
  faceSourceSize,
  OUTLIER_TARGET_PX,
  smallerFaceSource,
} from '../../lib/faceSource'
import { type DuplicateMarker } from '../../services/dupmarkers'
import { thumbUrl } from '../../services/photos'

import './outliers.css'

/** How much of the surrounding photo is kept around the marker box, per side. */
const CONTEXT_PADDING = 0.3

/** Props for {@link DuplicateMarkerCrop}. */
export interface DuplicateMarkerCropProps {
  /** The photo the marker sits on. */
  photoUid: string
  /** The marker to crop to. */
  marker: DuplicateMarker
  /** The photo's stored (pre-rotation) width and height. */
  width: number
  height: number
  /** The photo's raw EXIF orientation tag (1–8, or 0 when absent). */
  orientation: number
}

/**
 * The close-up of one marker in a repeated-marker card: the box grown 30 % per
 * side, with the box itself outlined inside it.
 *
 * The padding is the point. Deciding which of three boxes of "Marie" is the real
 * Marie means comparing faces, and a crop tight on the box gives you no hair, no
 * shoulders and no neighbours to compare against. The geometry is the same one
 * `/outliers` uses (`lib/faceGeometry`), so it needs no pixel measurement — the
 * container's `aspect-ratio` carries it — and the source thumbnail is picked per
 * marker by `lib/faceSource`, because most faces span a few percent of the frame
 * and a fixed `fit_720` would render them as mush.
 *
 * It must be cut from a `fit_*` size: those keep the whole frame, which is what a
 * normalised bbox is measured against. A `tile_*` is a centre-cropped square, so
 * the box would land beside the face. A size missing from the object store
 * degrades down the ladder on `onError` rather than showing a broken image.
 */
export function DuplicateMarkerCrop({
  photoUid,
  marker,
  width,
  height,
  orientation,
}: DuplicateMarkerCropProps) {
  const { t } = useTranslation()
  const crop = padBbox(marker.bbox, CONTEXT_PADDING)
  const frame = displayFrame(width, height, orientation)
  const known = frame.width > 0 && frame.height > 0

  const frameStyle: CSSProperties = {
    // The crop's own proportions, so it is not stretched — and a fallback for a
    // photo whose dimensions were never recorded, which would ask for `0 / 0`.
    aspectRatio: known
      ? `${String(crop[2] * frame.width)} / ${String(crop[3] * frame.height)}`
      : '1 / 1',
    background: 'var(--bs-dark)',
  }

  const preferred = faceSourceSize(crop, frame, OUTLIER_TARGET_PX, FACE_SOURCE_REVIEW_MAX)
  const [degraded, setDegraded] = useState<{ from: string; size: string } | null>(null)
  const source = degraded !== null && degraded.from === preferred ? degraded.size : preferred

  return (
    <div className="position-relative overflow-hidden rounded" style={frameStyle}>
      <img
        src={thumbUrl(photoUid, source)}
        data-testid="dup-marker-crop"
        data-thumb-size={source}
        alt={t('duplicateMarkers.card.cropAlt')}
        loading="lazy"
        decoding="async"
        style={{ ...cropImageStyle(crop), objectFit: 'cover' }}
        onError={() => {
          const next = smallerFaceSource(source)
          if (next !== null) {
            setDegraded({ from: preferred, size: next })
          }
        }}
      />
      <span className="kk-face-marker" style={faceMarkerStyle(marker.bbox, crop)} />
    </div>
  )
}

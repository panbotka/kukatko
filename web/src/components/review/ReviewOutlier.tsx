import { type CSSProperties, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cropImageStyle, displayFrame, faceMarkerStyle, padBbox } from '../../lib/faceGeometry'
import {
  FACE_SOURCE_REVIEW_BUDGET_PX,
  FACE_SOURCE_REVIEW_MAX,
  faceSourceSize,
  OUTLIER_TARGET_PX,
  smallerFaceSource,
} from '../../lib/faceSource'
import { type Bbox } from '../../services/people'
import { type Photo, thumbUrl } from '../../services/photos'

import { ReviewPhoto } from './ReviewPhoto'
import { OpenPhotoLink } from './ReviewStage'

/**
 * How much of the photo around the face the crop keeps, per side. A little more
 * than the /outliers grid's 30 %: the question there is asked next to a dozen
 * other faces of the same person, here it is asked alone, and the hair, the
 * shoulders and a hint of the scene are what a human actually recognises
 * somebody by.
 */
const OUTLIER_CROP_PADDING = 0.4

/** Props for {@link ReviewOutlier}. */
export interface ReviewOutlierProps {
  /** The photo the face is on. */
  photo: Photo
  /** The face box, normalised against the photo's display frame. */
  bbox: Bbox
  /** Where the photo's own page is (`/photos/{uid}`). */
  href: string
  /** The game's way back, handed to the photo's page; see {@link ReviewStage}. */
  linkState?: object
  /** Accessible description of the face crop. */
  alt: string
}

/**
 * The outlier check's stage: the face itself, large, with the whole photo beside
 * it for context.
 *
 * A crop, not a rectangle drawn on the full frame, because the question is about
 * the face and only the face — "is this really Anna?" over a wedding group where
 * Anna is forty pixels across is a question nobody can answer honestly. The
 * /outliers page settled that argument already, and this reuses its geometry
 * (`padBbox` → `cropImageStyle`, the box outlined inside by `faceMarkerStyle`)
 * and its source ladder.
 *
 * **The crop is cut from a full-frame `fit_*` preview**, never a `tile_*`: a
 * tile is a centre-cropped square, i.e. a different frame from the one the box
 * was normalised against, so cropping one lands beside the face on anything but
 * a square photo. Which rung of the ladder is chosen depends on how small the
 * face is (`lib/faceSource`), at the review budget — one card on screen at a
 * time is worth its bytes. A rung missing from the object store degrades down
 * the ladder on error rather than leaving a hole where a face should be.
 *
 * The full photo stays beside it because a face out of its scene is also how a
 * curator gets it wrong: the same person looks different at a wedding and on a
 * ski slope, and the context is often what decides it. On a portrait phone the
 * pair stacks (see review.css).
 */
export function ReviewOutlier({ photo, bbox, href, linkState, alt }: ReviewOutlierProps) {
  const { t } = useTranslation()
  const crop = padBbox(bbox, OUTLIER_CROP_PADDING)
  const frame = displayFrame(photo.file_width, photo.file_height, photo.file_orientation ?? 0)
  const known = frame.width > 0 && frame.height > 0

  const frameStyle: CSSProperties = {
    // The crop's own proportions, so nothing is stretched — and a photo whose
    // dimensions were never recorded falls back to a square instead of asking
    // for the invalid `0 / 0`.
    aspectRatio: known
      ? `${String(crop[2] * frame.width)} / ${String(crop[3] * frame.height)}`
      : '1 / 1',
  }

  const preferred = faceSourceSize(crop, frame, OUTLIER_TARGET_PX, {
    maxSize: FACE_SOURCE_REVIEW_MAX,
    budgetPx: FACE_SOURCE_REVIEW_BUDGET_PX,
  })
  const [degraded, setDegraded] = useState<{ from: string; size: string } | null>(null)
  const source = degraded !== null && degraded.from === preferred ? degraded.size : preferred

  return (
    <div className="review-outlier" data-testid="review-outlier">
      <div className="review-outlier__face" style={frameStyle}>
        <img
          src={thumbUrl(photo.uid, source)}
          data-testid="review-outlier-face"
          data-thumb-size={source}
          alt={alt}
          decoding="async"
          style={{ ...cropImageStyle(crop), objectFit: 'cover' }}
          onError={() => {
            const next = smallerFaceSource(source)
            if (next !== null) {
              setDegraded({ from: preferred, size: next })
            }
          }}
        />
        <span
          className="kk-face-marker"
          style={faceMarkerStyle(bbox, crop)}
          data-testid="review-outlier-bbox"
        />
        {/* The way out, the single-photo stage's own corner anchor: a real
            anchor so the URL can be copied, going where the stage's goes. */}
        <OpenPhotoLink href={href} linkState={linkState} />
      </div>
      <div className="review-outlier__context">
        <ReviewPhoto
          photo={photo}
          href={href}
          linkState={linkState}
          bbox={bbox}
          alt={t('review.outlier.contextAlt')}
        />
      </div>
    </div>
  )
}

import { useEffect, useState } from 'react'

import { faceBoxStyle, padBbox } from '../../lib/faceGeometry'
import { type Bbox } from '../../services/people'
import { type Photo, thumbUrl } from '../../services/photos'
import { Icon } from '../Icon'

/**
 * The stage's preview size. `fit_*` (whole frame), never a square `tile_*`,
 * because the face box coordinates are relative to the full photo; 1280 because
 * the photo is the whole screen here.
 */
export const REVIEW_PREVIEW_SIZE = 'fit_1280'

/** Props for {@link ReviewPhoto}. */
export interface ReviewPhotoProps {
  /** The photo under question. */
  photo: Photo
  /**
   * The tight face box, normalised `[x, y, w, h]` in display space (face
   * questions only). The drawn rectangle is padded ~30 % around it — a person
   * is unrecognisable from a tight face crop.
   */
  bbox?: Bbox
  /** Accessible description of the photo. */
  alt: string
}

/**
 * displayAspect returns the CSS `aspect-ratio` of a photo in display
 * (EXIF-oriented) space; orientations 5–8 swap width and height. Falls back to
 * 3:2 when dimensions are unknown so the stage never collapses to zero height.
 */
function displayAspect(orientation: number, fileWidth: number, fileHeight: number): number {
  const rotated = orientation >= 5 && orientation <= 8
  const width = rotated ? fileHeight : fileWidth
  const height = rotated ? fileWidth : fileHeight
  if (width <= 0 || height <= 0) {
    return 1.5
  }
  return width / height
}

/**
 * The review game's photo stage: the full frame as large as the viewport
 * allows, with the face under question marked by a generously padded rectangle
 * and a gentle dim over everything outside it. The frame is width-driven with
 * `aspect-ratio` and capped against the stage height, so the normalised box
 * lines up with no pixel measurement (the {@link CandidateFaceImage} approach,
 * scaled to a full screen).
 */
export function ReviewPhoto({ photo, bbox, alt }: ReviewPhotoProps) {
  const [failed, setFailed] = useState(false)

  // A new photo is a clean slate for the load-failure flag.
  useEffect(() => {
    setFailed(false)
  }, [photo.uid])

  const ratio = displayAspect(photo.file_orientation ?? 0, photo.file_width, photo.file_height)
  const frameStyle = {
    aspectRatio: String(ratio),
    maxWidth: `min(100%, calc(var(--review-stage-h) * ${String(ratio)}))`,
  }

  return (
    <div className="review-photo" style={frameStyle}>
      {failed ? (
        <div className="d-flex align-items-center justify-content-center w-100 h-100 text-secondary">
          <Icon name="images" />
        </div>
      ) : (
        <img
          src={thumbUrl(photo.uid, REVIEW_PREVIEW_SIZE)}
          alt={alt}
          decoding="async"
          onError={() => {
            setFailed(true)
          }}
          className="review-photo__img"
        />
      )}
      {bbox !== undefined && (
        <div
          className="position-absolute top-0 start-0 w-100 h-100"
          style={{ pointerEvents: 'none' }}
          aria-hidden="true"
        >
          <span
            className="review-photo__box"
            style={faceBoxStyle(padBbox(bbox))}
            data-testid="review-bbox"
          />
        </div>
      )}
    </div>
  )
}

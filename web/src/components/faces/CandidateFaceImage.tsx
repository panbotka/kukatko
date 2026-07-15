import { type CSSProperties, useEffect, useState } from 'react'

import { faceBoxStyle } from '../../lib/faceGeometry'
import { type Bbox } from '../../services/people'
import { thumbUrl } from '../../services/photos'
import { Icon } from '../Icon'

/** Props for {@link CandidateFaceImage}. */
export interface CandidateFaceImageProps {
  /** The photo to show — a full-frame preview, not a cropped tile. */
  photoUid: string
  /** Raw EXIF orientation (1–8); orientations 5–8 swap width and height. */
  orientation: number
  /** Display width in pixels, before orientation is applied. */
  fileWidth: number
  /** Display height in pixels, before orientation is applied. */
  fileHeight: number
  /** The candidate face box, normalised `[x, y, w, h]` in display space (0..1). */
  bbox: Bbox
  /** Bootstrap contextual colour name (`info`/`warning`/`success`) for the rectangle. */
  variant: string
  /** When true the face is confirmed: dim the photo and stamp a check. */
  done: boolean
  /** Accessible description of the photo. */
  alt: string
}

/**
 * displayAspect returns the CSS `aspect-ratio` of a photo in display (EXIF-oriented)
 * space, so the box the image sits in matches the frame exactly and the normalised
 * face rectangle lines up without any pixel measurement. Falls back to a square when
 * dimensions are unknown.
 */
function displayAspect(orientation: number, fileWidth: number, fileHeight: number): string {
  const rotated = orientation >= 5 && orientation <= 8
  const width = rotated ? fileHeight : fileWidth
  const height = rotated ? fileWidth : fileHeight
  if (width <= 0 || height <= 0) {
    return '1 / 1'
  }
  return `${String(width)} / ${String(height)}`
}

/**
 * CandidateFaceImage draws a full-frame photo preview with the candidate face marked
 * as a coloured rectangle. It deliberately uses the `fit_720` size (whole frame),
 * not the square `tile_500`, because the box coordinates are relative to the full
 * photo — a centre-cropped tile would put the rectangle in the wrong place. You need
 * the surrounding context to judge whether it is really the person.
 */
export function CandidateFaceImage({
  photoUid,
  orientation,
  fileWidth,
  fileHeight,
  bbox,
  variant,
  done,
  alt,
}: CandidateFaceImageProps) {
  const [failed, setFailed] = useState(false)

  // A new photo is a clean slate for the load-failure flag.
  useEffect(() => {
    setFailed(false)
  }, [photoUid])

  const wrapStyle: CSSProperties = {
    aspectRatio: displayAspect(orientation, fileWidth, fileHeight),
    background: 'var(--bs-dark)',
  }
  const boxStyle: CSSProperties = {
    ...faceBoxStyle(bbox),
    borderStyle: 'solid',
    borderWidth: 3,
    borderColor: `var(--bs-${variant})`,
    // A faint dark halo keeps the box visible over a light patch of photo.
    boxShadow: '0 0 0 1px rgba(0, 0, 0, 0.55)',
  }

  return (
    <div className="position-relative overflow-hidden rounded-top" style={wrapStyle}>
      {failed ? (
        <div className="d-flex align-items-center justify-content-center w-100 h-100 text-secondary">
          <Icon name="images" />
        </div>
      ) : (
        <img
          src={thumbUrl(photoUid, 'fit_720')}
          alt={alt}
          loading="lazy"
          decoding="async"
          onError={() => {
            setFailed(true)
          }}
          className="w-100 h-100"
          style={{ objectFit: 'contain', opacity: done ? 0.55 : 1 }}
        />
      )}
      <div
        className="position-absolute top-0 start-0 w-100 h-100"
        style={{ pointerEvents: 'none' }}
        data-testid="candidate-overlay"
      >
        <span className="position-absolute" style={boxStyle} data-testid="candidate-bbox" />
      </div>
      {done && (
        <span
          className="position-absolute top-50 start-50 translate-middle display-4 text-success"
          aria-hidden="true"
        >
          <Icon name="check-lg" />
        </span>
      )}
    </div>
  )
}

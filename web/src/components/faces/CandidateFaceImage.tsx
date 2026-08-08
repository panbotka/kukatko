import { type CSSProperties, useEffect, useState } from 'react'

import { useImageFrame } from '../../hooks/useImageFrame'
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
  /** The row's width in pixels, before orientation — the frame's initial estimate. */
  fileWidth: number
  /** The row's height in pixels, before orientation — the frame's initial estimate. */
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
 * CandidateFaceImage draws a full-frame photo preview with the candidate face marked
 * as a coloured rectangle. It deliberately uses the `fit_720` size (whole frame),
 * not the square `tile_500`, because the box coordinates are relative to the full
 * photo — a centre-cropped tile would put the rectangle in the wrong place. You need
 * the surrounding context to judge whether it is really the person.
 *
 * The wrapper's `aspect-ratio` — which *is* the rectangle's coordinate system, since
 * the rectangle is placed in percentages of it — comes from {@link useImageFrame}:
 * the loaded preview's own natural size, with the catalogue row only as the estimate
 * that holds the card's height still until it arrives. The rectangle waits for the
 * measurement rather than being drawn against the estimate; on a row with a
 * transposed dimension pair it would otherwise land off the face (and visibly jump
 * once the real frame arrived).
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
  const stage = useImageFrame({
    source: photoUid,
    width: fileWidth,
    height: fileHeight,
    orientation,
  })

  // A new photo is a clean slate for the load-failure flag.
  useEffect(() => {
    setFailed(false)
  }, [photoUid])

  const wrapStyle: CSSProperties = {
    // A square is the fallback for a photo with no dimensions at all: the card
    // needs *some* height, and with no frame there is no rectangle to misplace.
    aspectRatio: stage.aspectRatio ?? '1 / 1',
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
    <div
      className="position-relative overflow-hidden rounded-top"
      style={wrapStyle}
      data-testid="candidate-frame"
    >
      {failed ? (
        <div className="d-flex align-items-center justify-content-center w-100 h-100 text-secondary">
          <Icon name="images" />
        </div>
      ) : (
        <img
          {...stage.imgProps}
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
        {stage.measured && (
          <span className="position-absolute" style={boxStyle} data-testid="candidate-bbox" />
        )}
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

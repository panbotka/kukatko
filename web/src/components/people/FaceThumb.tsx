import { type CSSProperties } from 'react'

import { faceCropStyle } from '../../lib/faceGeometry'
import { type Bbox } from '../../services/people'
import { GRID_THUMB_SIZE, thumbUrl } from '../../services/photos'

/** Props for {@link FaceThumb}. */
export interface FaceThumbProps {
  /** Photo the face belongs to. */
  photoUid: string
  /** Normalised `[x, y, w, h]` face box within the photo. */
  bbox: Bbox
  /** Accessible label for the crop. */
  label: string
  /** Edge length of the (square) crop in CSS pixels. Defaults to 96. */
  size?: number
  /** Thumbnail size to crop from (defaults to the grid tile). */
  thumbSize?: string
  /** Extra class names appended to the crop box. */
  className?: string
}

/**
 * A square preview of a single face, cropped from the photo's cached thumbnail
 * using the normalised bbox (no dedicated face-thumbnail endpoint exists). It is
 * a presentational building block reused by the cluster review and outlier views.
 */
export function FaceThumb({
  photoUid,
  bbox,
  label,
  size = 96,
  thumbSize = GRID_THUMB_SIZE,
  className,
}: FaceThumbProps) {
  const url = thumbUrl(photoUid, thumbSize)
  const style: CSSProperties = {
    width: `${size}px`,
    height: `${size}px`,
    ...faceCropStyle(url, bbox),
  }
  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      className={`d-inline-block rounded bg-secondary-subtle flex-shrink-0${
        className ? ` ${className}` : ''
      }`}
      style={style}
    />
  )
}
